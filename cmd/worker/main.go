package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"spartial.local/backend/internal/config"
	"spartial.local/backend/internal/modules/privacy"
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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	startup, cancelStartup := context.WithTimeout(ctx, 10*time.Second)
	defer cancelStartup()
	db, e := platform.Open(startup, cfg.DatabaseURL)
	if e != nil {
		return e
	}
	defer db.Close()
	if e = platform.CheckSchema(startup, db); e != nil {
		return e
	}
	if e = platform.CheckRuntimeRole(startup, db); e != nil {
		return e
	}
	lastPrivacyCheck := time.Time{}
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	consumer := "not_configured"
	if cfg.Env == "development" {
		consumer = "development_sink"
	}
	slog.Info("worker_started", "consumer", consumer, "external_delivery", false)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
			work, cancel := context.WithTimeout(ctx, 10*time.Second)
			if time.Since(lastPrivacyCheck) >= time.Minute {
				if e = privacy.DetectAlerts(work, db, cfg.PrivacyReadThreshold, cfg.PrivacyMFAThreshold); e != nil {
					slog.Error("privacy_monitor_failed")
				} else {
					lastPrivacyCheck = time.Now()
				}
			}
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
			if cfg.Env == "development" {
				err = platform.CompleteDevelopmentJob(work, db, j)
			} else {
				err = platform.Fail(503, "consumer_not_configured", "No delivery consumer is configured")
			}
			if err != nil {
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
