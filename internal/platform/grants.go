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
		`GRANT INSERT,UPDATE,DELETE ON facilities,staff,staff_facilities,sessions,refresh_tokens,auth_limits,patients,outbox,staff_mfa,mfa_recovery_codes TO ` + r,
		`GRANT INSERT,UPDATE ON login_challenges TO ` + r,
		`GRANT INSERT ON audit_events,idempotency_records,development_event_sink TO ` + r,
		`REVOKE INSERT,UPDATE,DELETE,TRUNCATE ON audit_seals FROM ` + r,
		`REVOKE EXECUTE ON FUNCTION seal_audit_events(interval,integer) FROM ` + r,
		`GRANT EXECUTE ON FUNCTION verify_audit_chain(), audit_event_digest(audit_events) TO ` + r,
		`GRANT USAGE,SELECT ON ALL SEQUENCES IN SCHEMA public TO ` + r,
	}
	for _, q := range statements {
		if _, e = tx.Exec(ctx, q); e != nil {
			return e
		}
	}
	return tx.Commit(ctx)
}
