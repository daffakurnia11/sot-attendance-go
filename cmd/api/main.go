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

	"github.com/daffakurniawan/sot-discord-bot/internal/api"
	appauth "github.com/daffakurniawan/sot-discord-bot/internal/auth"
	"github.com/daffakurniawan/sot-discord-bot/internal/database"
	"github.com/daffakurniawan/sot-discord-bot/internal/member"
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
	if err == nil {
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
	defer pool.Close()

	issuer, err := appauth.NewIssuer(config.JWTSecret, config.JWTTTL)
	if err != nil {
		logger.Error("create token issuer", "error", err)
		os.Exit(1)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	handler := api.NewHandler(api.NewDiscordVerifier(client), member.NewRepository(pool), issuer, logger)
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
