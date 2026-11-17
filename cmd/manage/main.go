package main

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"log/slog"
	"os"
	"spartial.local/backend/internal/config"
	"spartial.local/backend/internal/maintenance"
	"spartial.local/backend/internal/modules/identity"
	"spartial.local/backend/internal/platform"
	"spartial.local/backend/internal/security"
	"time"
)

func main() {
	if e := entry(); e != nil {
		slog.Error("management_command_failed", "reason", e.Error())
		os.Exit(1)
	}
}
func entry() error {
	if len(os.Args) == 2 && os.Args[1] == "bootstrap-development" {
		cfg, e := config.Load()
		if e != nil {
			return e
		}
		if cfg.Env != "development" {
			return fmt.Errorf("bootstrap-development is development only")
		}
		for _, command := range []string{"migrate", "grant-runtime", "seed"} {
			if e = run(command); e != nil {
				return e
			}
		}
		return nil
	}
	if len(os.Args) != 2 {
		return fmt.Errorf("usage: manage migrate|grant-runtime|seed|audit-seal|audit-verify|new-encryption-key|rotate-keys|bootstrap-development")
	}
	return run(os.Args[1])
}
func run(command string) error {
	if command == "new-encryption-key" {
		// Prints a fresh key for PHI_ENCRYPTION_KEYS; store it in the secrets manager, never in git.
		k, e := security.NewKeyMaterial()
		if e != nil {
			return e
		}
		fmt.Println(k)
		return nil
	}
	if len(os.Args) != 2 {
		return fmt.Errorf("usage: manage migrate|grant-runtime|seed")
	}
	cfg, e := config.Load()
	if e != nil {
		return e
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	adminURL := os.Getenv("DATABASE_ADMIN_URL")
	if adminURL == "" {
		adminURL = cfg.DatabaseURL
	}
	if command == "migrate" {
		return platform.Migrate(ctx, adminURL)
	}
	db, e := platform.Open(ctx, adminURL)
	if e != nil {
		return e
	}
	defer db.Close()
	switch command {
	case "rotate-keys":
		result, e := maintenance.RotateKeys(ctx, db, cfg.Keys)
		if e != nil {
			return e
		}
		slog.Info("key_rotation_complete", "mfa_rows", result.MFA, "identifier_rows", result.Identifiers)
		return nil
	case "grant-runtime":
		role := os.Getenv("APP_DB_ROLE")
		password := os.Getenv("APP_DB_PASSWORD")
		if role == "" || len(password) < 16 {
			return fmt.Errorf("APP_DB_ROLE and APP_DB_PASSWORD (16+ characters) are required")
		}
		var exists bool
		if e = db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname=$1)`, role).Scan(&exists); e != nil {
			return e
		}
		if !exists {
			// Password is escaped as a SQL literal; it is never logged. CREATE ROLE cannot use bind placeholders.
			if _, e = db.Exec(ctx, `CREATE ROLE `+pgx.Identifier{role}.Sanitize()+` LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION PASSWORD `+sqlLiteral(password)); e != nil {
				return fmt.Errorf("could not create runtime role")
			}
		}
		return platform.GrantRuntime(ctx, db, role)
	case "audit-seal":
		// Run on a schedule (e.g. every 5 minutes). Ship the printed head off-host (SIEM/WORM).
		s, e := platform.SealAudit(ctx, db, 5*time.Minute)
		if e != nil {
			return e
		}
		slog.Info("audit_sealed", "events", s.Count, "last_event_id", s.LastEventID, "chain_hash", s.ChainHash)
		return nil
	case "audit-verify":
		r, e := platform.VerifyAuditChain(ctx, db)
		if e != nil {
			return e
		}
		if !r.Intact() {
			slog.Error("audit_chain_broken", "first_broken_seal", *r.BrokenSeal, "seals_checked", r.Seals)
			return fmt.Errorf("audit chain verification failed at seal %d", *r.BrokenSeal)
		}
		slog.Info("audit_chain_intact", "seals", r.Seals, "events", r.Events, "unsealed_events", r.Unsealed)
		return nil
	case "seed":
		if cfg.Env != "development" {
			return fmt.Errorf("seed is allowed only with APP_ENV=development")
		}
		f, e := identity.Seed(ctx, db, os.Getenv("SEED_ADMIN_PASSWORD"), os.Getenv("SEED_RECEPTION_PASSWORD"))
		if e != nil {
			return e
		}
		fmt.Println("Synthetic development accounts ready: admin@spartial.test, reception@spartial.test; facility=" + f)
		return nil
	default:
		return fmt.Errorf("unknown management command")
	}
}
func sqlLiteral(s string) string {
	out := "'"
	for _, r := range s {
		if r == '\'' {
			out += "''"
		} else {
			out += string(r)
		}
	}
	return out + "'"
}
