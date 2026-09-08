package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bwmarrin/discordgo"
	attendancescheduler "github.com/daffakurniawan/sot-discord-bot/internal/attendance"
	commandcrafting "github.com/daffakurniawan/sot-discord-bot/internal/command/crafting"
	commandmoney "github.com/daffakurniawan/sot-discord-bot/internal/command/money"
	commandrecap "github.com/daffakurniawan/sot-discord-bot/internal/command/recap"
	"github.com/daffakurniawan/sot-discord-bot/internal/command/router"
	"github.com/daffakurniawan/sot-discord-bot/internal/config"
	craftingdomain "github.com/daffakurniawan/sot-discord-bot/internal/crafting"
	"github.com/daffakurniawan/sot-discord-bot/internal/dashboard"
	"github.com/daffakurniawan/sot-discord-bot/internal/database"
	"github.com/daffakurniawan/sot-discord-bot/internal/member"
	moneydomain "github.com/daffakurniawan/sot-discord-bot/internal/money"
	"github.com/daffakurniawan/sot-discord-bot/internal/presence"
	"github.com/daffakurniawan/sot-discord-bot/internal/serverlog"
	dbsettings "github.com/daffakurniawan/sot-discord-bot/internal/settings"
	"github.com/jackc/pgx/v5/pgxpool"
)

type cfxPlayerReader interface {
	Players(context.Context) ([]dashboard.CFXPlayer, error)
	// Rosters also returns every player on the server, not only the
	// configured family. Reconciliation has to match against all of them: a
	// member absent from the full roster has really left.
	Rosters(context.Context) ([]dashboard.CFXPlayer, []dashboard.CFXPlayer, error)
}

type Bot struct {
	session            *discordgo.Session
	status             *presence.Counter
	router             *router.Router
	logger             *slog.Logger
	pollInterval       time.Duration
	cfxPollInterval    time.Duration
	statusPollInterval time.Duration
	attendance         *attendancescheduler.Scheduler
	cfx                cfxPlayerReader
	members            *member.Repository
	settings           *dbsettings.Repository
	crafting           craftingdomain.Store
	money              *moneydomain.Repository
	craftDrafts        *craftDraftStore
	location           *time.Location
	database           *pgxpool.Pool
	guildID            string
	// announces is true only in production. A local run holds the same token
	// and usually the same database as the deployed bot, so anything it does is
	// done twice: every announcement posted again, the roster sync fighting the
	// deployed process over the same rows, the bot status written by both. A
	// local bot reads the gateway, answers GET /presence, and acts on nothing.
	announces            bool
	adminRoleIDs         []string
	memberRoleID         string
	officeMoneyChannelID string
	dirtyMoneyChannelID  string
	serverLogChannelID   string
	serverLogs           *serverlog.Repository
	serverLogCursor      int64
	adminSync            sync.Mutex
	ready                atomic.Bool
	cfxCount             atomic.Int64
	showCFXStatus        bool
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
	craftingRepository := craftingdomain.NewRepository(pool)

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
		session:              session,
		status:               presence.NewCounter(cfg.GuildID, cfg.ServerName, cfg.DiscordRoleID, logger),
		router:               router.NewRouter(cfg.CommandPrefix),
		logger:               logger,
		pollInterval:         cfg.PollInterval,
		cfxPollInterval:      cfg.CFXPollInterval,
		statusPollInterval:   cfg.StatusPollInterval,
		attendance:           attendance,
		cfx:                  dashboard.NewCFXClient(&http.Client{Timeout: 5 * time.Second}, cfg.CFXServerID, cfg.CFXPlayerID),
		members:              members,
		settings:             settingsRepository,
		crafting:             craftingRepository,
		money:                moneydomain.NewRepository(pool),
		craftDrafts:          newCraftDraftStore(10 * time.Minute),
		location:             location,
		database:             pool,
		guildID:              cfg.GuildID,
		announces:            cfg.Announces(),
		adminRoleIDs:         cfg.DiscordAdminRoleIDs,
		memberRoleID:         cfg.DiscordMemberRoleID,
		serverLogChannelID:   cfg.ServerLogChannelID,
		serverLogs:           serverlog.NewRepository(pool),
		officeMoneyChannelID: cfg.OfficeMoneyChannelID,
		dirtyMoneyChannelID:  cfg.DirtyMoneyChannelID,
	}
	bot.cfxCount.Store(-1)
	session.AddHandler(bot.onReady)
	session.AddHandler(bot.onDisconnect)
	session.AddHandler(bot.onResumed)
	session.AddHandler(bot.onGuildCreate)
	session.AddHandler(bot.onMessageCreate)
	session.AddHandler(bot.onInteractionCreate)

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

	// Live Discord presence for the API, which has no gateway of its own.
	//
	// The API pulls this per dashboard request rather than the bot pushing
	// snapshots: presence is only ever wanted as of now, so a pull needs no
	// cache, no staleness rule and no shared secret. The port this serves on is
	// not published outside the compose network.
	//
	// This replaced writing every transition to activity_logs. The game server
	// reports the same events over the webhook and reports them better, so
	// presence is worth serving live and not worth storing.
	mux.HandleFunc("GET /presence", func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/json; charset=utf-8")
		response.Header().Set("Cache-Control", "no-store")
		if !b.ready.Load() {
			response.WriteHeader(http.StatusServiceUnavailable)
			_, _ = response.Write([]byte(`{"error":"gateway_not_ready"}` + "\n"))
			return
		}
		presences, err := b.status.Snapshot(b.session)
		if err != nil {
			b.logger.Warn("presence snapshot unavailable", "error", err)
			response.WriteHeader(http.StatusServiceUnavailable)
			_, _ = response.Write([]byte(`{"error":"presence_unavailable"}` + "\n"))
			return
		}
		if err := json.NewEncoder(response).Encode(map[string]any{
			"observed_at": time.Now().UTC(),
			"members":     presences,
		}); err != nil {
			b.logger.Error("encode presence snapshot", "error", err)
		}
	})
	return mux
}

func (b *Bot) Run(ctx context.Context) error {
	if err := b.session.Open(); err != nil {
		return fmt.Errorf("open Discord gateway: %w", err)
	}
	b.logger.Info("bot connected", "announces", b.announces)
	go b.runCFXPoller(ctx)
	if b.announces {
		go b.attendance.Run(ctx, b.session)
		go b.runServerLogPoller(ctx)
	}

	discordTicker := time.NewTicker(b.pollInterval)
	defer discordTicker.Stop()
	statusTicker := time.NewTicker(b.statusPollInterval)
	defer statusTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			goto shutdown
		case <-discordTicker.C:
			b.status.Refresh(b.session)
		case <-statusTicker.C:
			b.rotateStatus()
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

// serverLogPollInterval is how often the bot looks for new FiveM webhook
// events to announce. The webhook writes to server_logs from cmd/api, which
// holds no Discord session, so the bot polls rather than being pushed to.
const serverLogPollInterval = 10 * time.Second

// serverLogAnnounceLimit caps one pass so a backlog cannot flood the channel
// in a single tick.
const serverLogAnnounceLimit = 20

// runServerLogPoller announces FiveM webhook events in the player log channel.
//
// The cursor starts at the newest stored row, so a first run - or a run after
// the table has been filling while the bot was down - announces nothing rather
// than replaying history into the channel. Events that arrive while the bot is
// offline are therefore not announced; server_logs remains the record.
func (b *Bot) runServerLogPoller(ctx context.Context) {
	seedContext, cancel := context.WithTimeout(ctx, 10*time.Second)
	latest, err := b.serverLogs.LatestEventID(seedContext)
	cancel()
	if err != nil {
		b.logger.Error("seed server log cursor", "error", err)
		return
	}
	b.serverLogCursor = latest
	b.logger.Info("server log announcer started", "channel_id", b.serverLogChannelID, "cursor", latest)

	ticker := time.NewTicker(serverLogPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			b.announceServerLogs(ctx)
		}
	}
}

func (b *Bot) announceServerLogs(ctx context.Context) {
	requestContext, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	announcements, err := b.serverLogs.AnnouncementsAfter(requestContext, b.serverLogCursor, serverLogAnnounceLimit)
	if err != nil {
		b.logger.Error("read server log announcements", "cursor", b.serverLogCursor, "error", err)
		return
	}
	if len(announcements) == 0 {
		return
	}
	for _, announcement := range announcements {
		event := presence.ServerLogEvent{
			PlayerName: announcement.PlayerName,
			Username:   announcement.Username,
			Status:     announcement.Status,
			// The footer time is plain text, so it is rendered in the
			// location the channel's readers are in rather than left in UTC.
			OccurredAt: announcement.OccurredAt.In(b.location),
			ServerID:   announcement.ServerID,
			Reason:     announcement.Reason,
			StartedAt:  announcement.StartedAt,
			// The mention resolves to the live Discord handle, so a reader can
			// chase an entry to the account without the bot storing a name.
			DiscordUserID: announcement.DiscordUserID,
		}
		if _, err := b.session.ChannelMessageSendEmbed(b.serverLogChannelID, presence.ServerLogEmbed(event)); err != nil {
			// Stop at the first failure and leave the cursor behind it, so the
			// next tick retries this event instead of skipping past it.
			b.logger.Error("send server log", "channel_id", b.serverLogChannelID, "event_id", announcement.ID, "error", err)
			return
		}
		b.serverLogCursor = announcement.ID
	}
	b.logger.Info("server logs announced", "count", len(announcements), "cursor", b.serverLogCursor)
}

func (b *Bot) runCFXPoller(ctx context.Context) {
	b.refreshCFX(ctx)
	ticker := time.NewTicker(b.cfxPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			b.refreshCFX(ctx)
		}
	}
}

func (b *Bot) refreshCFX(ctx context.Context) {
	requestContext, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	players, roster, err := b.cfx.Rosters(requestContext)
	if err != nil {
		b.logger.Warn("refresh CFX player count", "error", err)
		return
	}
	count := int64(len(players))
	previous := b.cfxCount.Swap(count)
	if previous != count {
		b.logger.Info("CFX player count updated", "count", count)
	}
	b.reconcileServerSessions(ctx, roster)
}

// reconcileServerSessions closes visits the game server no longer lists.
//
// A disconnected event that never arrives leaves a visit open for twelve hours
// and credits the player attendance for the whole window. The roster already
// polled above answers the question that event would have, so it is used to
// close what it contradicts.
func (b *Bot) reconcileServerSessions(ctx context.Context, roster []dashboard.CFXPlayer) {
	if !b.announces || b.serverLogs == nil || len(roster) == 0 {
		return
	}
	present := make([]string, 0, len(roster))
	for _, player := range roster {
		present = append(present, player.Name)
	}

	requestContext, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	closed, err := b.serverLogs.CloseAbsentSessions(requestContext, present, time.Now().UTC(), serverlog.ReconcileGrace)
	if err != nil {
		b.logger.Error("reconcile open server sessions", "error", err)
		return
	}
	if closed > 0 {
		b.logger.Info("open server sessions closed from the CFX roster", "sessions", closed, "roster", len(roster))
	}
}

func (b *Bot) rotateStatus() {
	if !b.announces {
		return
	}
	discordCount, discordAvailable := b.status.Count()
	cfxCount := b.cfxCount.Load()
	status, nextCFX, ok := rotatingStatus(shortServerName(b.status.ServerName()), discordCount, discordAvailable, int(cfxCount), cfxCount >= 0, b.showCFXStatus)
	if !ok {
		return
	}
	b.showCFXStatus = nextCFX
	if err := b.session.UpdateGameStatus(0, status); err != nil {
		b.logger.Error("update rotating bot status", "status", status, "error", err)
		return
	}
	b.logger.Debug("rotating bot status updated", "status", status)
}

func rotatingStatus(serverLabel string, discordCount int, discordAvailable bool, cfxCount int, cfxAvailable bool, showCFX bool) (string, bool, bool) {
	if showCFX && cfxAvailable {
		return fmt.Sprintf("%d %s players on CFX", cfxCount, serverLabel), false, true
	}
	if discordAvailable {
		return fmt.Sprintf("%d %s players on Discord", discordCount, serverLabel), true, true
	}
	if cfxAvailable {
		return fmt.Sprintf("%d %s players on CFX", cfxCount, serverLabel), false, true
	}
	return "", showCFX, false
}

func shortServerName(serverName string) string {
	parts := strings.Fields(serverName)
	if len(parts) == 0 {
		return "CR"
	}
	return parts[0]
}

func (b *Bot) onReady(session *discordgo.Session, event *discordgo.Ready) {
	b.ready.Store(true)
	b.logger.Info("Discord gateway ready", "bot_user_id", event.User.ID, "bot_username", event.User.Username, "announces", b.announces)
	if !b.announces {
		// Registering commands, upserting the roster and writing the bot status
		// are all things the deployed bot is already doing with this same
		// token. Doing them again from a laptop duplicates or fights them.
		b.logger.Warn("APP_ENV is not production: this bot reads and serves presence only")
		return
	}
	go b.registerSlashCommands(session, event.User.ID)
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
		players = append(players, member.Player{DiscordUserID: guildMember.User.ID, Username: guildMember.User.Username, DisplayName: guildMember.DisplayName()})
	}
	if err := b.members.UpsertGuildMembers(ctx, players); err != nil {
		b.logger.Error("upsert Discord role members", "guild_id", b.guildID, "error", err)
		return
	}
	adminUserIDs := matchingRoleMemberIDs(members, b.adminRoleIDs)
	// An empty list is never an instruction to demote everyone. It means the
	// admin roles are misconfigured, the member fetch came back thin, or this
	// process is watching a guild that does not hold those roles - and the
	// sync would otherwise clear is_admin for every member in the database,
	// locking the whole roster out of the admin-only pages.
	if len(adminUserIDs) == 0 {
		b.logger.Warn("admin sync skipped: no member holds a configured admin role",
			"guild_id", b.guildID, "admin_role_ids", len(b.adminRoleIDs))
	} else if err := b.members.SyncAdmins(ctx, adminUserIDs); err != nil {
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
	if b.announces && event.ID == b.status.GuildID() {
		b.status.Refresh(session)
	}
}

func (b *Bot) onMessageCreate(session *discordgo.Session, message *discordgo.MessageCreate) {
	if message.Author == nil || message.Author.Bot || message.GuildID == "" {
		return
	}
	// Slash commands were already scoped to the watched guild; prefix commands
	// were not, so both the deployed bot and a local one answered the same
	// message. A quiet bot answers nothing at all.
	if !b.announces || message.GuildID != b.guildID {
		return
	}
	commandName := b.router.Match(message.Content)
	if commandName == "" {
		return
	}

	var err error
	switch commandName {
	case commandrecap.Command:
		err = b.handleRecap(session, message)
	case commandrecap.CheckCommand:
		err = b.handleCheck(session, message)
	case commandcrafting.Command:
		err = b.handleCraft(session, message)
	case commandmoney.Command:
		err = b.handleMoney(session, message)
	}
	if err == nil {
		return
	}

	b.logger.Error("handle command", "command", commandName, "guild_id", message.GuildID, "channel_id", message.ChannelID, "user_id", message.Author.ID, "error", err)
	if _, sendErr := session.ChannelMessageSendReply(message.ChannelID, "Could not run that command. Check your permissions or try again shortly.", message.Reference()); sendErr != nil {
		b.logger.Error("send command error", "command", commandName, "channel_id", message.ChannelID, "error", sendErr)
	}
}

func (b *Bot) handleMoney(session *discordgo.Session, message *discordgo.MessageCreate) error {
	request, err := commandmoney.Parse(message.Content, b.router.Prefix())
	if err != nil {
		_, sendErr := session.ChannelMessageSendReply(message.ChannelID, commandmoney.Usage(b.router.Prefix()), message.Reference())
		return sendErr
	}
	account, validChannel := b.moneyAccountForChannel(message.ChannelID)
	if !validChannel {
		_, sendErr := session.ChannelMessageSendReply(message.ChannelID, "Money commands can only be used in <#"+b.officeMoneyChannelID+"> or <#"+b.dirtyMoneyChannelID+">.", message.Reference())
		return sendErr
	}
	request.Account = account
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	currentMember, err := b.members.FindByDiscordUserID(ctx, message.Author.ID)
	if err != nil {
		return fmt.Errorf("find money command member: %w", err)
	}
	if request.Action == "balance" {
		balance, err := b.money.Balance(ctx, request.Account)
		if err != nil {
			return err
		}
		_, err = session.ChannelMessageSendEmbed(message.ChannelID, commandmoney.BalanceEmbed(request.Account, balance))
		return err
	}
	if !currentMember.IsAdmin {
		_, sendErr := session.ChannelMessageSendReply(message.ChannelID, "Administrator permission is required to change money balances.", message.Reference())
		return sendErr
	}
	transaction, err := b.money.Transact(ctx, moneydomain.Transaction{Account: request.Account, Action: request.Action, Amount: request.Amount, Reason: request.Reason, ActorMemberID: currentMember.ID})
	if err != nil {
		if errors.Is(err, moneydomain.ErrInsufficientFunds) {
			_, sendErr := session.ChannelMessageSendReply(message.ChannelID, "Insufficient balance for that withdrawal.", message.Reference())
			return sendErr
		}
		return err
	}
	if _, err := session.ChannelMessageSendEmbed(message.ChannelID, commandmoney.TransactionEmbed(transaction, currentMember.CharacterName)); err != nil {
		return fmt.Errorf("send money transaction: %w", err)
	}
	b.logger.Info("money transaction applied", "guild_id", message.GuildID, "channel_id", message.ChannelID, "user_id", message.Author.ID, "member_id", currentMember.ID, "transaction_id", transaction.ID, "account", transaction.Account, "action", transaction.Action, "amount", transaction.Amount)
	return nil
}

func (b *Bot) moneyChannelID(account moneydomain.Account) string {
	if account == moneydomain.AccountDirty {
		return b.dirtyMoneyChannelID
	}
	return b.officeMoneyChannelID
}

func (b *Bot) moneyAccountForChannel(channelID string) (moneydomain.Account, bool) {
	switch channelID {
	case b.officeMoneyChannelID:
		return moneydomain.AccountOffice, true
	case b.dirtyMoneyChannelID:
		return moneydomain.AccountDirty, true
	default:
		return "", false
	}
}

func (b *Bot) handleCraft(session *discordgo.Session, message *discordgo.MessageCreate) error {
	items, err := commandcrafting.Parse(message.Content, b.router.Prefix())
	if err != nil {
		if errors.Is(err, commandcrafting.ErrInvalidSyntax) {
			_, sendErr := session.ChannelMessageSendReply(message.ChannelID, commandcrafting.Usage(b.router.Prefix()), message.Reference())
			return sendErr
		}
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := craftingdomain.CalculateBatch(ctx, b.crafting, items)
	if err != nil {
		if errors.Is(err, craftingdomain.ErrNotFound) || errors.Is(err, craftingdomain.ErrDuplicateRecipe) || errors.Is(err, craftingdomain.ErrInvalidBatch) {
			_, sendErr := session.ChannelMessageSendReply(message.ChannelID, fmt.Sprintf("Invalid crafting request: %s\n%s", userCraftError(err), commandcrafting.Usage(b.router.Prefix())), message.Reference())
			return sendErr
		}
		return err
	}
	if _, err := session.ChannelMessageSendEmbed(message.ChannelID, commandcrafting.Embed(result)); err != nil {
		return fmt.Errorf("send crafting calculation: %w", err)
	}
	b.logger.Info("crafting command sent", "guild_id", message.GuildID, "channel_id", message.ChannelID, "user_id", message.Author.ID, "recipe_count", len(result.Recipes), "weapon_quantity", result.TotalRequestedQuantity)
	return nil
}

func userCraftError(err error) string {
	switch {
	case errors.Is(err, craftingdomain.ErrDuplicateRecipe):
		return "each weapon may only appear once"
	case errors.Is(err, craftingdomain.ErrNotFound):
		return "weapon recipe was not found"
	default:
		return "quantity must be between 1 and 10000, with 1 to 20 products"
	}
}

func (b *Bot) handleCheck(session *discordgo.Session, message *discordgo.MessageCreate) error {
	targetUserID := message.Author.ID
	if len(message.Mentions) > 0 {
		if len(message.Mentions) != 1 || message.Mentions[0] == nil {
			return fmt.Errorf("check command requires exactly one member mention")
		}
		targetUserID = message.Mentions[0].ID
	}
	now := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	embed, participants, attendanceStart, err := b.buildCheckEmbed(ctx, targetUserID, now)
	if err != nil {
		return err
	}

	if _, err := session.ChannelMessageSendEmbed(message.ChannelID, embed); err != nil {
		return fmt.Errorf("send attendance check: %w", err)
	}
	b.logger.Info("attendance check sent", "guild_id", message.GuildID, "channel_id", message.ChannelID, "user_id", message.Author.ID, "target_user_id", targetUserID, "participants", participants, "attendance_start", attendanceStart)
	return nil
}

func (b *Bot) handleRecap(session *discordgo.Session, message *discordgo.MessageCreate) error {
	now := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	embed, participants, attendanceStart, err := b.buildRecapEmbed(ctx, now)
	if err != nil {
		return err
	}
	if _, err := session.ChannelMessageSendEmbed(message.ChannelID, embed); err != nil {
		return fmt.Errorf("send attendance recap: %w", err)
	}
	b.logger.Info("attendance recap sent", "guild_id", message.GuildID, "channel_id", message.ChannelID, "user_id", message.Author.ID, "players", participants, "attendance_start", attendanceStart)
	return nil
}
