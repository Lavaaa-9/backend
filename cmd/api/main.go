package main

import (
	"context"
	"github.com/gofiber/fiber/v3"
	"log/slog"
	"os"
	"os/signal"
	"spartial.local/backend/internal/config"
	"spartial.local/backend/internal/httpapi"
	"spartial.local/backend/internal/platform"
	"syscall"
	"time"
)

func main() {
	if err := run(); err != nil {
		slog.Error("startup_failed", "reason", err.Error())
		os.Exit(1)
	}
}
func run() error {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(log)
	cfg, e := config.Load()
	if e != nil {
		return e
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db, e := platform.Open(ctx, cfg.DatabaseURL)
	if e != nil {
		return e
	}
	defer db.Close()
	if e = platform.CheckSchema(ctx, db); e != nil {
		return e
	}
	if e = platform.CheckRuntimeRole(ctx, db); e != nil {
		return e
	}
	app, e := httpapi.New(cfg, db, log)
	if e != nil {
		return e
	}
	stopCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	done := make(chan error, 1)
	go func() { done <- app.Listen(cfg.Addr, fiber.ListenConfig{DisableStartupMessage: true}) }()
	log.Info("api_started", "address", cfg.Addr, "environment", cfg.Env)
	select {
	case e := <-done:
		return e
	case <-stopCtx.Done():
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return app.ShutdownWithContext(ctx)
	}
}
