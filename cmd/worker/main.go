// worker runs background maintenance against spartial_hms using the restricted spartial_worker role:
// transactional outbox dispatch, tamper-evident audit sealing, and privacy anomaly detection.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	"spartial.local/backend/internal/config"
	"spartial.local/backend/internal/platform"
)

func main() {
	once := flag.Bool("once", false, "run one pass of outbox dispatch, audit sealing, and privacy alert detection then exit")
	flag.Parse()
	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.Error("worker_failed", "reason", "could not load .env")
		os.Exit(1)
	}
	if e := run(*once); e != nil {
		slog.Error("worker_failed", "reason", e.Error())
		os.Exit(1)
	}
}

func run(once bool) error {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	cfg, e := config.Load()
	if e != nil {
		return e
	}
	dbURL := os.Getenv("DATABASE_WORKER_URL")
	if dbURL == "" {
		dbURL = cfg.DatabaseURL
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	startup, cancelStartup := context.WithTimeout(ctx, 10*time.Second)
	defer cancelStartup()
	db, e := platform.Open(startup, dbURL)
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

	workerID := "worker-" + uuid.NewString()[:8]
	consumer := "not_configured"
	if cfg.Env == "development" || cfg.Env == "test" {
		consumer = "development_sink"
	}
	slog.Info("worker_started", "worker_id", workerID, "consumer", consumer, "once", once)

	if once {
		work, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		return runPass(work, db, cfg, workerID, true)
	}

	lastPrivacy := time.Time{}
	lastSeal := time.Time{}
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
			work, cancel := context.WithTimeout(ctx, 10*time.Second)
			if time.Since(lastPrivacy) >= time.Minute {
				if n, err := platform.DetectPrivacyAlerts(work, db, cfg.PrivacyReadThreshold, cfg.PrivacyMFAThreshold); err != nil {
					slog.Error("privacy_monitor_failed", "reason", err.Error())
				} else {
					if n > 0 {
						slog.Info("privacy_alerts_detected", "new_alerts", n)
					}
					lastPrivacy = time.Now()
				}
			}
			if time.Since(lastSeal) >= 5*time.Minute {
				if sn, rc, ok, err := platform.SealAuditPending(work, db); err != nil {
					slog.Error("audit_seal_failed", "reason", err.Error())
				} else {
					if ok {
						slog.Info("audit_sealed", "seal_no", sn, "row_count", rc)
					}
					lastSeal = time.Now()
				}
			}
			_ = runPass(work, db, cfg, workerID, false)
			cancel()
		}
	}
}

func runPass(ctx context.Context, db *pgxpool.Pool, cfg config.Config, workerID string, includeMaintenance bool) error {
	if _, err := platform.ReapStaleOutbox(ctx, db); err != nil {
		slog.Error("outbox_reap_failed", "reason", err.Error())
		if includeMaintenance {
			return err
		}
	}
	for i := 0; i < 50; i++ {
		j, err := platform.ClaimOutbox(ctx, db, workerID)
		if errors.Is(err, platform.ErrNoJob) {
			break
		}
		if err != nil {
			slog.Error("outbox_claim_failed", "reason", err.Error())
			if includeMaintenance {
				return err
			}
			break
		}
		if cfg.Env == "development" || cfg.Env == "test" {
			if err = platform.CompleteOutbox(ctx, db, j); err != nil {
				slog.Error("outbox_complete_failed", "job_id", j.ID, "reason", err.Error())
			} else {
				slog.Info("outbox_event_dispatched", "job_id", j.ID, "event_type", j.EventType, "aggregate_type", j.AggregateType, "sink", "development")
			}
		} else {
			if err = platform.FailOutbox(ctx, db, j, "no_external_provider_configured"); err != nil {
				slog.Error("outbox_fail_failed", "job_id", j.ID, "reason", err.Error())
			}
		}
	}
	if includeMaintenance {
		if n, err := platform.DetectPrivacyAlerts(ctx, db, cfg.PrivacyReadThreshold, cfg.PrivacyMFAThreshold); err != nil {
			return err
		} else if n > 0 {
			slog.Info("privacy_alerts_detected", "new_alerts", n)
		}
		if sn, rc, ok, err := platform.SealAuditPending(ctx, db); err != nil {
			return err
		} else if ok {
			slog.Info("audit_sealed", "seal_no", sn, "row_count", rc)
		}
	}
	return nil
}
