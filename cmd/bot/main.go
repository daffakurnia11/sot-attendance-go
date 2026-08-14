package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
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

	if err := botApp.Run(ctx); err != nil {
		logger.Error("bot stopped", "error", err)
		os.Exit(1)
	}
}
