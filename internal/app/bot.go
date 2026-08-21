package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"sync"
	"sync/atomic"
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
	dbsettings "github.com/daffakurniawan/sot-discord-bot/internal/settings"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Bot struct {
	session      *discordgo.Session
	status       *presence.Counter
	router       *router.Router
	logger       *slog.Logger
	pollInterval time.Duration
	attendance   *attendancescheduler.Scheduler
	members      *member.Repository
	settings     *dbsettings.Repository
	location     *time.Location
	database     *pgxpool.Pool
	guildID      string
	adminRoleIDs []string
	memberRoleID string
	adminSync    sync.Mutex
	ready        atomic.Bool
}

func New(cfg config.Config, logger *slog.Logger) (*Bot, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := database.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	if database.SkipMigrations(os.Getenv("SKIP_MIGRATIONS")) {
		logger.Warn("startup migrations skipped", "reason", "SKIP_MIGRATIONS")
	} else if err := database.Migrate(ctx, pool); err != nil {
		pool.Close()
		return nil, err
	}
	settingsRepository := dbsettings.NewRepository(pool)
	if _, err := settingsRepository.LoadAttendance(ctx); err != nil {
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
		attendanceConfig, err := settingsRepository.LoadAttendance(ctx)
		if err != nil {
			return fmt.Errorf("reload attendance settings: %w", err)
		}
		attendanceStart, attendanceEnd := commandrecap.AttendanceWindow(now, attendanceConfig.StartTime, attendanceConfig.EndTime, location)
		recaps, err := members.PlaytimeRecap(ctx, attendanceStart, attendanceEnd)
		if err != nil {
			return err
		}
		if err := members.SaveAttendanceRecap(ctx, recaps, attendanceStart, attendanceEnd, attendanceConfig.PlaytimeThreshold); err != nil {
			return err
		}
		if _, err := session.ChannelMessageSendEmbed(cfg.PlayerRecapChannelID, commandrecap.Embed(recaps, attendanceStart, now, attendanceConfig.PlaytimeThreshold)); err != nil {
			return fmt.Errorf("send attendance recap: %w", err)
		}
		logger.Info("attendance recap sent", "channel_id", cfg.PlayerRecapChannelID, "players", len(recaps), "attendance_start", attendanceStart, "automatic", true)
		return nil
	}
	attendance, err := attendancescheduler.NewDynamicScheduler(cfg.PlayerChatChannelID, cfg.ServerName, func(ctx context.Context) (attendancescheduler.ScheduleTimes, error) {
		latest, err := settingsRepository.LoadAttendance(ctx)
		if err != nil {
			return attendancescheduler.ScheduleTimes{}, err
		}
		return attendancescheduler.ScheduleTimes{Start: latest.StartTime, End: latest.EndTime}, nil
	}, endAction, logger)
	if err != nil {
		pool.Close()
		return nil, err
	}

	bot := &Bot{
		session:      session,
		status:       presence.NewCounter(cfg.GuildID, cfg.ServerName, cfg.PlayerLogChannelID, cfg.DiscordRoleID, cfg.PollInterval, cfg.BlacklistedUserIDs, members, logger),
		router:       router.NewRouter(cfg.CommandPrefix),
		logger:       logger,
		pollInterval: cfg.PollInterval,
		attendance:   attendance,
		members:      members,
		settings:     settingsRepository,
		location:     location,
		database:     pool,
		guildID:      cfg.GuildID,
		adminRoleIDs: cfg.DiscordAdminRoleIDs,
		memberRoleID: cfg.DiscordMemberRoleID,
	}
	session.AddHandler(bot.onReady)
	session.AddHandler(bot.onDisconnect)
	session.AddHandler(bot.onResumed)
	session.AddHandler(bot.onGuildCreate)
	session.AddHandler(bot.onMessageCreate)

	return bot, nil
}

// HealthHandler reports Discord gateway readiness. Process-only liveness is
// insufficient: a bot can keep running while disconnected and doing no work.
func (b *Bot) HealthHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/json; charset=utf-8")
		response.Header().Set("Cache-Control", "no-store")
		if !b.ready.Load() {
			response.WriteHeader(http.StatusServiceUnavailable)
			_, _ = response.Write([]byte(`{"status":"not_ready"}` + "\n"))
			return
		}
		_, _ = response.Write([]byte(`{"status":"ok"}` + "\n"))
	})
	return mux
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
	b.ready.Store(true)
	b.logger.Info("Discord gateway ready", "bot_user_id", event.User.ID, "bot_username", event.User.Username)
	go b.syncGuildMembers(session)
	b.status.Refresh(session)
}

func (b *Bot) onDisconnect(_ *discordgo.Session, _ *discordgo.Disconnect) {
	b.ready.Store(false)
	b.logger.Warn("Discord gateway disconnected")
}

func (b *Bot) onResumed(_ *discordgo.Session, event *discordgo.Resumed) {
	b.ready.Store(true)
	b.logger.Info("Discord gateway resumed", "trace_entries", len(event.Trace))
}

func (b *Bot) syncGuildMembers(session *discordgo.Session) {
	if !b.adminSync.TryLock() {
		return
	}
	defer b.adminSync.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	members, err := fetchGuildMembers(session, b.guildID)
	if err != nil {
		b.logger.Error("fetch Discord members for startup sync", "guild_id", b.guildID, "error", err)
		return
	}
	roleMembers := matchingRoleMembers(members, []string{b.memberRoleID})
	players := make([]member.Player, 0, len(roleMembers))
	for _, guildMember := range roleMembers {
		players = append(players, member.Player{UserID: guildMember.User.ID, Username: guildMember.User.Username, DisplayName: guildMember.DisplayName()})
	}
	if err := b.members.UpsertGuildMembers(ctx, players, time.Now()); err != nil {
		b.logger.Error("upsert Discord role members", "guild_id", b.guildID, "error", err)
		return
	}
	adminUserIDs := matchingRoleMemberIDs(members, b.adminRoleIDs)
	if err := b.members.SyncAdmins(ctx, adminUserIDs); err != nil {
		b.logger.Error("sync member admins", "guild_id", b.guildID, "error", err)
		return
	}
	b.logger.Info("guild members synced", "guild_id", b.guildID, "discord_members", len(members), "role_members", len(players), "admins", len(adminUserIDs))
}

func fetchGuildMembers(session *discordgo.Session, guildID string) ([]*discordgo.Member, error) {
	all := make([]*discordgo.Member, 0)
	after := ""
	for {
		page, err := session.GuildMembers(guildID, after, 1000)
		if err != nil {
			return nil, fmt.Errorf("list guild members: %w", err)
		}
		all = append(all, page...)
		if len(page) < 1000 {
			return all, nil
		}
		after = page[len(page)-1].User.ID
	}
}

func matchingRoleMemberIDs(members []*discordgo.Member, roleIDs []string) []string {
	matched := matchingRoleMembers(members, roleIDs)
	result := make([]string, 0, len(matched))
	for _, guildMember := range matched {
		result = append(result, guildMember.User.ID)
	}
	return result
}

func matchingRoleMembers(members []*discordgo.Member, roleIDs []string) []*discordgo.Member {
	result := make([]*discordgo.Member, 0)
	if len(roleIDs) == 0 || (len(roleIDs) == 1 && roleIDs[0] == "") {
		return result
	}
	for _, guildMember := range members {
		if guildMember == nil || guildMember.User == nil || guildMember.User.Bot {
			continue
		}
		if slices.ContainsFunc(guildMember.Roles, func(roleID string) bool { return slices.Contains(roleIDs, roleID) }) {
			result = append(result, guildMember)
		}
	}
	return result
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
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	attendanceConfig, err := b.settings.LoadAttendance(ctx)
	if err != nil {
		return fmt.Errorf("reload attendance settings: %w", err)
	}
	attendanceStart, attendanceEnd := commandrecap.AttendanceWindow(now, attendanceConfig.StartTime, attendanceConfig.EndTime, b.location)
	recaps, err := b.members.PlaytimeRecap(ctx, attendanceStart, attendanceEnd)
	if err != nil {
		return err
	}
	if _, err := session.ChannelMessageSendEmbed(message.ChannelID, commandrecap.Embed(recaps, attendanceStart, now, attendanceConfig.PlaytimeThreshold)); err != nil {
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
