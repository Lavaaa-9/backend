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
		`REVOKE ALL ON FUNCTION seal_audit_events(interval,integer),verify_audit_chain(),audit_event_digest(audit_events),has_care_access(uuid,uuid),can_access_patient(uuid,uuid,uuid,boolean),sync_patient_scope() FROM PUBLIC`,
		`GRANT SELECT ON ALL TABLES IN SCHEMA public TO ` + r,
		`GRANT INSERT,UPDATE,DELETE ON facilities,staff,staff_facilities,sessions,refresh_tokens,auth_limits,patients,outbox,staff_mfa,mfa_recovery_codes TO ` + r,
		`GRANT INSERT,UPDATE ON login_challenges TO ` + r,
		`GRANT INSERT ON audit_events,idempotency_records,development_event_sink TO ` + r,
		`REVOKE INSERT,UPDATE,DELETE,TRUNCATE ON audit_seals FROM ` + r,
		`REVOKE EXECUTE ON FUNCTION seal_audit_events(interval,integer) FROM ` + r,
		`GRANT EXECUTE ON FUNCTION verify_audit_chain(), audit_event_digest(audit_events) TO ` + r,
		`GRANT INSERT ON care_assignments,break_glass_access TO ` + r,
		`GRANT UPDATE(revoked_at,revoked_by) ON care_assignments TO ` + r,
		`GRANT UPDATE(reviewed_by,reviewed_at,review_outcome,review_note) ON break_glass_access TO ` + r,
		`GRANT EXECUTE ON FUNCTION has_care_access(uuid,uuid),can_access_patient(uuid,uuid,uuid,boolean) TO ` + r,
		`REVOKE DELETE ON patients FROM ` + r,
		`GRANT INSERT ON approval_requests,privacy_alerts,patient_identifiers TO ` + r,
		`GRANT UPDATE(status,reviewed_by,reviewed_at,review_note) ON approval_requests TO ` + r,
		`GRANT UPDATE(event_count) ON privacy_alerts TO ` + r,
		`GRANT USAGE,SELECT ON ALL SEQUENCES IN SCHEMA public TO ` + r,
	}
	for _, q := range statements {
		if _, e = tx.Exec(ctx, q); e != nil {
			return e
		}
	}
	return tx.Commit(ctx)
}
