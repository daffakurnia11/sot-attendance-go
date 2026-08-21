package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	// Embeds the timezone database in the binary. The production image is
	// alpine with no tzdata package, and unlike the build container there is no
	// Go toolchain to fall back on, so LoadLocation("Asia/Jakarta") fails
	// without this. cmd/bot already imports it for the same reason.
	_ "time/tzdata"

	"github.com/daffakurniawan/sot-discord-bot/internal/api"
	attendancehistory "github.com/daffakurniawan/sot-discord-bot/internal/attendance"
	appauth "github.com/daffakurniawan/sot-discord-bot/internal/auth"
	"github.com/daffakurniawan/sot-discord-bot/internal/crafting"
	"github.com/daffakurniawan/sot-discord-bot/internal/dashboard"
	"github.com/daffakurniawan/sot-discord-bot/internal/database"
	"github.com/daffakurniawan/sot-discord-bot/internal/member"
	dbsettings "github.com/daffakurniawan/sot-discord-bot/internal/settings"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	config, err := api.LoadConfig()
	if err != nil {
		logger.Error("load API configuration", "error", err)
		os.Exit(1)
	}
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		logger.Error("load API configuration", "error", "DATABASE_URL is required")
		os.Exit(1)
	}

	startupContext, cancelStartup := context.WithTimeout(context.Background(), 10*time.Second)
	pool, err := database.Open(startupContext, databaseURL)
	skipMigrations := database.SkipMigrations(os.Getenv("SKIP_MIGRATIONS"))
	if err == nil && !skipMigrations {
		err = database.Migrate(startupContext, pool)
	}
	cancelStartup()
	if err != nil {
		if pool != nil {
			pool.Close()
		}
		logger.Error("initialize API database", "error", err)
		os.Exit(1)
	}
	if skipMigrations {
		logger.Warn("startup migrations skipped", "reason", "SKIP_MIGRATIONS")
	}
	defer pool.Close()

	issuer, err := appauth.NewIssuer(config.JWTSecret, config.JWTTTL)
	if err != nil {
		logger.Error("create token issuer", "error", err)
		os.Exit(1)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	cfxClient := dashboard.NewCFXClient(client, config.FiveMCFXID, config.FiveMPlayerID)
	location, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		logger.Error("load API timezone", "error", err)
		os.Exit(1)
	}
	settingsRepository := dbsettings.NewRepository(pool)
	handler := api.NewHandlerWithCrafting(api.NewDiscordVerifier(client), member.NewRepository(pool), issuer, issuer, dashboard.NewRepository(pool, cfxClient, logger), attendancehistory.NewReportRepository(pool, location), logger, settingsRepository, crafting.NewRepository(pool))
	server := &http.Server{
		Addr:              config.Address,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	serverErrors := make(chan error, 1)
	go func() { serverErrors <- server.ListenAndServe() }()
	logger.Info("web API listening", "address", config.Address)

	select {
	case <-ctx.Done():
	case err = <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Error("web API stopped", "error", err)
			os.Exit(1)
		}
	}

	shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShutdown()
	if err := server.Shutdown(shutdownContext); err != nil {
		logger.Error("shutdown web API", "error", err)
		os.Exit(1)
	}
	logger.Info("web API stopped")
}
