package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/bwmarrin/discordgo"
	attendancescheduler "github.com/daffakurniawan/sot-discord-bot/internal/attendance"
	"github.com/daffakurniawan/sot-discord-bot/internal/command/attendance"
	"github.com/daffakurniawan/sot-discord-bot/internal/command/profile"
	commandrecap "github.com/daffakurniawan/sot-discord-bot/internal/command/recap"
	"github.com/daffakurniawan/sot-discord-bot/internal/command/router"
	"github.com/daffakurniawan/sot-discord-bot/internal/config"
	"github.com/daffakurniawan/sot-discord-bot/internal/database"
	"github.com/daffakurniawan/sot-discord-bot/internal/member"
	"github.com/daffakurniawan/sot-discord-bot/internal/presence"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Bot struct {
	session             *discordgo.Session
	status              *presence.Counter
	router              *router.Router
	logger              *slog.Logger
	pollInterval        time.Duration
	attendance          *attendancescheduler.Scheduler
	members             *member.Repository
	attendanceStartTime time.Duration
	attendanceEndTime   time.Duration
	attendancePlaytime  time.Duration
	location            *time.Location
	database            *pgxpool.Pool
}

func New(cfg config.Config, logger *slog.Logger) (*Bot, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := database.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	if err := database.Migrate(ctx, pool); err != nil {
		pool.Close()
		return nil, err
	}
	members := member.NewRepository(pool)

	session, err := discordgo.New("Bot " + cfg.Token)
	if err != nil {
		pool.Close()
		return nil, fmt.Errorf("create Discord session: %w", err)
	}

	session.Identify.Intents = discordgo.IntentsGuilds |
		discordgo.IntentsGuildMessages |
		discordgo.IntentsMessageContent |
		discordgo.IntentsGuildPresences |
		discordgo.IntentsGuildMembers

	location, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		pool.Close()
		return nil, fmt.Errorf("load Asia/Jakarta timezone: %w", err)
	}
	endAction := func(ctx context.Context, session *discordgo.Session, now time.Time) error {
		attendanceStart, attendanceEnd := commandrecap.AttendanceWindow(now, cfg.AttendanceStartTime, cfg.AttendanceEndTime, location)
		recaps, err := members.PlaytimeRecap(ctx, attendanceStart, attendanceEnd)
		if err != nil {
			return err
		}
		if err := members.SaveAttendanceRecap(ctx, recaps, attendanceStart, attendanceEnd, cfg.AttendancePlaytime); err != nil {
			return err
		}
		if _, err := session.ChannelMessageSendEmbed(cfg.PlayerRecapChannelID, commandrecap.Embed(recaps, attendanceStart, now, cfg.AttendancePlaytime)); err != nil {
			return fmt.Errorf("send attendance recap: %w", err)
		}
		logger.Info("attendance recap sent", "channel_id", cfg.PlayerRecapChannelID, "players", len(recaps), "attendance_start", attendanceStart, "automatic", true)
		return nil
	}
	attendance, err := attendancescheduler.NewScheduler(cfg.PlayerChatChannelID, cfg.ServerName, cfg.AttendanceStartTime, cfg.AttendanceEndTime, endAction, logger)
	if err != nil {
		pool.Close()
		return nil, err
	}

	bot := &Bot{
		session:             session,
		status:              presence.NewCounter(cfg.GuildID, cfg.ServerName, cfg.PlayerLogChannelID, cfg.PollInterval, cfg.BlacklistedUserIDs, members, logger),
		router:              router.NewRouter(cfg.CommandPrefix),
		logger:              logger,
		pollInterval:        cfg.PollInterval,
		attendance:          attendance,
		members:             members,
		attendanceStartTime: cfg.AttendanceStartTime,
		attendanceEndTime:   cfg.AttendanceEndTime,
		attendancePlaytime:  cfg.AttendancePlaytime,
		location:            location,
		database:            pool,
	}
	session.AddHandler(bot.onReady)
	session.AddHandler(bot.onGuildCreate)
	session.AddHandler(bot.onMessageCreate)

	return bot, nil
}

func (b *Bot) Run(ctx context.Context) error {
	if err := b.session.Open(); err != nil {
		return fmt.Errorf("open Discord gateway: %w", err)
	}
	b.logger.Info("bot connected")
	go b.attendance.Run(ctx, b.session)

	ticker := time.NewTicker(b.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			goto shutdown
		case <-ticker.C:
			b.status.Refresh(b.session)
		}
	}

shutdown:
	b.logger.Info("bot shutting down")
	if err := b.session.Close(); err != nil {
		b.database.Close()
		return fmt.Errorf("close Discord session: %w", err)
	}
	b.database.Close()
	return nil
}

func (b *Bot) onReady(session *discordgo.Session, event *discordgo.Ready) {
	b.logger.Info("Discord gateway ready", "bot_user_id", event.User.ID, "bot_username", event.User.Username)
	b.status.Refresh(session)
}

func (b *Bot) onGuildCreate(session *discordgo.Session, event *discordgo.GuildCreate) {
	if event.ID == b.status.GuildID() {
		b.status.Refresh(session)
	}
}

func (b *Bot) onMessageCreate(session *discordgo.Session, message *discordgo.MessageCreate) {
	if message.Author == nil || message.Author.Bot || message.GuildID == "" {
		return
	}
	commandName := b.router.Match(message.Content)
	if commandName == "" {
		return
	}

	var err error
	switch commandName {
	case "me":
		err = b.handleMe(session, message)
	case commandrecap.Command:
		err = b.handleRecap(session, message)
	case attendance.AttendanceStart, attendance.AttendanceEnd:
		err = b.handleAttendance(session, message, commandName)
	}
	if err == nil {
		return
	}

	b.logger.Error("handle command", "command", commandName, "guild_id", message.GuildID, "channel_id", message.ChannelID, "user_id", message.Author.ID, "error", err)
	if _, sendErr := session.ChannelMessageSendReply(message.ChannelID, "Could not run that command. Check your permissions or try again shortly.", message.Reference()); sendErr != nil {
		b.logger.Error("send command error", "command", commandName, "channel_id", message.ChannelID, "error", sendErr)
	}
}

func (b *Bot) handleRecap(session *discordgo.Session, message *discordgo.MessageCreate) error {
	now := time.Now()
	attendanceStart, attendanceEnd := commandrecap.AttendanceWindow(now, b.attendanceStartTime, b.attendanceEndTime, b.location)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	recaps, err := b.members.PlaytimeRecap(ctx, attendanceStart, attendanceEnd)
	if err != nil {
		return err
	}
	if _, err := session.ChannelMessageSendEmbed(message.ChannelID, commandrecap.Embed(recaps, attendanceStart, now, b.attendancePlaytime)); err != nil {
		return fmt.Errorf("send attendance recap: %w", err)
	}
	b.logger.Info("attendance recap sent", "guild_id", message.GuildID, "channel_id", message.ChannelID, "user_id", message.Author.ID, "players", len(recaps), "attendance_start", attendanceStart)
	return nil
}

func (b *Bot) handleAttendance(session *discordgo.Session, message *discordgo.MessageCreate, commandName string) error {
	permissions, err := session.UserChannelPermissions(message.Author.ID, message.ChannelID)
	if err != nil {
		return fmt.Errorf("get caller permissions: %w", err)
	}
	if permissions&discordgo.PermissionAdministrator == 0 {
		return errors.New("administrator permission required")
	}

	if _, err := session.ChannelMessageSendComplex(message.ChannelID, attendance.Announcement(commandName, b.status.ServerName(), time.Now())); err != nil {
		return fmt.Errorf("send attendance announcement: %w", err)
	}
	b.logger.Info("attendance announced", "command", commandName, "guild_id", message.GuildID, "channel_id", message.ChannelID, "user_id", message.Author.ID)
	return nil
}

func (b *Bot) handleMe(session *discordgo.Session, message *discordgo.MessageCreate) error {
	member := message.Member
	if member == nil || member.User == nil {
		var err error
		member, err = session.State.Member(message.GuildID, message.Author.ID)
		if err != nil {
			member, err = session.GuildMember(message.GuildID, message.Author.ID)
			if err != nil {
				return fmt.Errorf("get member: %w", err)
			}
		}
	}

	memberPresence, err := session.State.Presence(message.GuildID, message.Author.ID)
	if err != nil && !errors.Is(err, discordgo.ErrStateNotFound) {
		return fmt.Errorf("get presence: %w", err)
	}

	var activity *discordgo.Activity
	if memberPresence != nil {
		activity = presence.MatchingActivity(memberPresence.Activities, b.status.ServerName())
	}

	memberProfile := profile.Build(member, activity, time.Now())
	if _, err := session.ChannelMessageSendEmbed(message.ChannelID, profile.Embed(memberProfile)); err != nil {
		return fmt.Errorf("send profile: %w", err)
	}
	return nil
}
