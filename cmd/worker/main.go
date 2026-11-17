package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"spartial.local/backend/internal/config"
	"spartial.local/backend/internal/platform"
	"syscall"
	"time"
)

func main() {
	if e := run(); e != nil {
		slog.Error("worker_failed", "reason", e.Error())
		os.Exit(1)
	}
}
func run() error {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	cfg, e := config.Load()
	if e != nil {
		return e
	}
	if cfg.Env != "development" {
		return errors.New("only the explicit development event sink is implemented; production consumers are not configured")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	db, e := platform.Open(ctx, cfg.DatabaseURL)
	if e != nil {
		return e
	}
	defer db.Close()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	slog.Info("worker_started", "consumer", "development_sink", "external_delivery", false)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
			work, cancel := context.WithTimeout(ctx, 10*time.Second)
			if e = platform.ReapExhaustedJobs(work, db); e != nil {
				cancel()
				slog.Error("job_reaper_failed")
				continue
			}
			j, err := platform.ClaimJob(work, db)
			if errors.Is(err, platform.ErrNoJob) {
				cancel()
				continue
			}
			if err != nil {
				cancel()
				slog.Error("job_claim_failed")
				continue
			}
			if err = platform.CompleteDevelopmentJob(work, db, j); err != nil {
				if e = platform.FailJob(work, db, j); e != nil {
					slog.Error("job_retry_failed", "job_id", j.ID)
				}
			} else {
				slog.Info("development_event_observed", "job_id", j.ID, "event_type", j.Type, "external_delivery", false)
			}
			cancel()
		}
	}
}
