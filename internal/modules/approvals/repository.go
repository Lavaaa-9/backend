package approvals

import (
	"context"
	"github.com/jackc/pgx/v5"
)

const columns = `id::text,kind,subject_id::text,subject_version,requested_by::text,reason,expires_at,created_at,status,reviewed_by::text,reviewed_at,review_note`

func scan(row pgx.Row) (a Request, err error) {
	err = row.Scan(&a.ID, &a.Kind, &a.SubjectID, &a.SubjectVersion, &a.RequestedBy, &a.Reason, &a.ExpiresAt, &a.CreatedAt, &a.Status, &a.ReviewedBy, &a.ReviewedAt, &a.ReviewNote)
	return
}
func liveAdministrator(ctx context.Context, tx pgx.Tx, id, session string) (ok bool, err error) {
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM staff u JOIN sessions s ON s.staff_id=u.id WHERE u.id=$1::uuid AND u.active AND u.role_name='administrator' AND s.id=$2::uuid AND s.mfa_verified AND s.revoked_at IS NULL AND s.access_expires_at>now() AND s.expires_at>now() AND (s.client_type<>'web' OR s.last_seen_at>now()-interval '15 minutes'))`, id, session).Scan(&ok)
	return
}
func requesterStillEligible(ctx context.Context, tx pgx.Tx, id string) (ok bool, err error) {
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM staff u JOIN staff_mfa m ON m.staff_id=u.id AND m.confirmed_at IS NOT NULL WHERE u.id=$1::uuid AND u.active AND u.role_name='administrator')`, id).Scan(&ok)
	return
}
func target(ctx context.Context, tx pgx.Tx, id string) (version int, role string, active, mfa bool, err error) {
	err = tx.QueryRow(ctx, `SELECT u.version,u.role_name,u.active,EXISTS(SELECT 1 FROM staff_mfa m WHERE m.staff_id=u.id AND m.confirmed_at IS NOT NULL) FROM staff u WHERE u.id=$1::uuid FOR UPDATE`, id).Scan(&version, &role, &active, &mfa)
	return
}
func create(ctx context.Context, tx pgx.Tx, actor string, in Input, version int) (Request, error) {
	return scan(tx.QueryRow(ctx, `INSERT INTO approval_requests(kind,subject_id,subject_version,requested_by,reason) VALUES($1,$2::uuid,$3,$4::uuid,$5) RETURNING `+columns, in.Kind, in.SubjectID, version, actor, in.Reason))
}
func lockRequest(ctx context.Context, tx pgx.Tx, id string) (Request, error) {
	return scan(tx.QueryRow(ctx, `SELECT `+columns+` FROM approval_requests WHERE id=$1::uuid FOR UPDATE`, id))
}
func decide(ctx context.Context, tx pgx.Tx, id, actor, decision, note string) (Request, error) {
	return scan(tx.QueryRow(ctx, `UPDATE approval_requests SET status=$1,reviewed_by=$2::uuid,reviewed_at=now(),review_note=$3 WHERE id=$4::uuid AND status='pending' RETURNING `+columns, decision, actor, note, id))
}
func expire(ctx context.Context, tx pgx.Tx, id string) error {
	_, e := tx.Exec(ctx, `UPDATE approval_requests SET status='expired' WHERE id=$1::uuid AND status='pending'`, id)
	return e
}
func apply(ctx context.Context, tx pgx.Tx, a Request) error {
	switch a.Kind {
	case "admin_role_grant":
		if _, e := tx.Exec(ctx, `UPDATE staff SET role_name='administrator',version=version+1,updated_at=now() WHERE id=$1::uuid`, a.SubjectID); e != nil {
			return e
		}
	case "mfa_reset":
		for _, q := range []string{`DELETE FROM mfa_recovery_codes WHERE staff_id=$1::uuid`, `DELETE FROM staff_mfa WHERE staff_id=$1::uuid`, `UPDATE staff SET version=version+1,updated_at=now() WHERE id=$1::uuid`} {
			if _, e := tx.Exec(ctx, q, a.SubjectID); e != nil {
				return e
			}
		}
	}
	_, e := tx.Exec(ctx, `UPDATE sessions SET revoked_at=COALESCE(revoked_at,now()) WHERE staff_id=$1::uuid`, a.SubjectID)
	return e
}
func list(ctx context.Context, tx pgx.Tx, after string, limit int) ([]Request, error) {
	rows, e := tx.Query(ctx, `SELECT `+columns+` FROM approval_requests WHERE ($1='' OR id>NULLIF($1,'')::uuid) ORDER BY id LIMIT $2`, after, limit)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Request{}
	for rows.Next() {
		a, e := scan(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
