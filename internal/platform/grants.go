package platform

import (
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// GrantRuntime must be run by the migration owner, never by the API.
func GrantRuntime(ctx context.Context, db *pgxpool.Pool, role string) error {
	tx, e := db.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	r := pgx.Identifier{role}.Sanitize()
	statements := []string{
		`GRANT USAGE ON SCHEMA public TO ` + r,
		`GRANT SELECT ON ALL TABLES IN SCHEMA public TO ` + r,
		`GRANT INSERT,UPDATE,DELETE ON facilities,staff,staff_facilities,sessions,refresh_tokens,auth_limits,patients,outbox TO ` + r,
		`GRANT INSERT ON audit_events,idempotency_records,development_event_sink TO ` + r,
		`GRANT USAGE,SELECT ON ALL SEQUENCES IN SCHEMA public TO ` + r,
	}
	for _, q := range statements {
		if _, e = tx.Exec(ctx, q); e != nil {
			return e
		}
	}
	return tx.Commit(ctx)
}
