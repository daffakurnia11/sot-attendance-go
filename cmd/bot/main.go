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
	_ "time/tzdata"

	"github.com/daffakurniawan/sot-discord-bot/internal/app"
	"github.com/daffakurniawan/sot-discord-bot/internal/config"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	cfg, err := config.Load()
	if err != nil {
		logger.Error("load configuration", "error", err)
		os.Exit(1)
	}

	botApp, err := app.New(cfg, logger)
	if err != nil {
		logger.Error("create bot", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	healthAddress := os.Getenv("BOT_HEALTH_ADDRESS")
	if healthAddress == "" {
		healthAddress = ":8081"
	}
	healthServer := &http.Server{
		Addr:              healthAddress,
		Handler:           botApp.HealthHandler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      5 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	healthErrors := make(chan error, 1)
	go func() { healthErrors <- healthServer.ListenAndServe() }()
	botErrors := make(chan error, 1)
	go func() { botErrors <- botApp.Run(ctx) }()
	logger.Info("bot health server listening", "address", healthAddress)

	var runErr error
	botFinished := false
	select {
	case <-ctx.Done():
	case runErr = <-botErrors:
		botFinished = true
		stop()
	case runErr = <-healthErrors:
		stop()
		if errors.Is(runErr, http.ErrServerClosed) {
			runErr = nil
		}
	}
	stop()
	if !botFinished {
		select {
		case botErr := <-botErrors:
			if runErr == nil {
				runErr = botErr
			}
		case <-time.After(10 * time.Second):
			if runErr == nil {
				runErr = errors.New("bot shutdown timed out")
			}
		}
	}

	shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelShutdown()
	if err := healthServer.Shutdown(shutdownContext); err != nil && runErr == nil {
		runErr = err
	}
	if runErr != nil {
		logger.Error("bot stopped", "error", runErr)
		os.Exit(1)
	}
}
