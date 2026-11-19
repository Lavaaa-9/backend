package integration

import (
	"context"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	"spartial.local/backend/internal/platform"
)

// TestMigrationsOnDisposableDatabase verifies db/tools/migrate.sh applies all
// spartial_hms migrations (0000-0018) deterministically to a clean database.
func TestMigrationsOnDisposableDatabase(t *testing.T) {
	_ = godotenv.Load("../../.env")
	adminBase := os.Getenv("TEST_DATABASE_URL")
	if adminBase == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping disposable database migration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	adminPool, err := pgxpool.New(ctx, adminBase)
	if err != nil {
		t.Fatalf("connect admin postgres: %v", err)
	}
	defer adminPool.Close()

	dbName := "spartial_mig_test_" + strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	if _, err := adminPool.Exec(ctx, "CREATE DATABASE "+dbName+" OWNER spartial_owner"); err != nil {
		t.Fatalf("create disposable db: %v", err)
	}
	t.Cleanup(func() {
		c, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_, _ = adminPool.Exec(c, `SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname=$1`, dbName)
		_, _ = adminPool.Exec(c, "DROP DATABASE IF EXISTS "+dbName)
	})

	u, err := url.Parse(adminBase)
	if err != nil {
		t.Fatalf("parse TEST_DATABASE_URL: %v", err)
	}
	pw, _ := u.User.Password()
	script, err := filepath.Abs("../../db/tools/migrate.sh")
	if err != nil {
		t.Fatalf("abs migrate.sh: %v", err)
	}
	// Run twice to confirm idempotency and checksum verification.
	for pass := 1; pass <= 2; pass++ {
		cmd := exec.Command(script, dbName)
		cmd.Env = append(os.Environ(), "PGHOST="+u.Hostname(), "PGPORT="+u.Port(), "PGUSER="+u.User.Username(), "PGPASSWORD="+pw)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("migrate.sh pass %d failed: %v\n%s", pass, err, string(out))
		}
	}

	u.Path = "/" + dbName
	targetPool, err := pgxpool.New(ctx, u.String())
	if err != nil {
		t.Fatalf("connect disposable db: %v", err)
	}
	defer targetPool.Close()

	if err := platform.CheckSchema(ctx, targetPool); err != nil {
		t.Fatalf("CheckSchema: %v", err)
	}
	var badAudit int
	if err := targetPool.QueryRow(ctx, `SELECT count(*) FROM audit.verify_integrity()`).Scan(&badAudit); err != nil {
		t.Fatalf("audit.verify_integrity: %v", err)
	}
	if badAudit != 0 {
		t.Fatalf("expected 0 audit integrity defects, got %d", badAudit)
	}
}
