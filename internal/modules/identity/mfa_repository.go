package identity

import (
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
)

func mfaEnabled(ctx context.Context, tx pgx.Tx, id string) (ok bool, err error) {
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM staff_mfa WHERE staff_id=$1::uuid AND confirmed_at IS NOT NULL)`, id).Scan(&ok)
	return
}
func insertChallenge(ctx context.Context, tx pgx.Tx, hash, id string, version int, client string, ttl time.Duration) (cid string, err error) {
	err = tx.QueryRow(ctx, `INSERT INTO login_challenges(token_hash,staff_id,staff_version,client_type,expires_at) VALUES($1,$2::uuid,$3,$4,now()+make_interval(secs=>$5)) RETURNING id::text`, hash, id, version, client, ttl.Seconds()).Scan(&cid)
	return
}
func challengeStaff(ctx context.Context, db *pgxpool.Pool, hash, client string) (id string, err error) {
	err = db.QueryRow(ctx, `SELECT staff_id::text FROM login_challenges WHERE token_hash=$1 AND client_type=$2`, hash, client).Scan(&id)
	return
}

type loginChallenge struct {
	ID                string
	Version, Attempts int
	Expires           time.Time
	Consumed          *time.Time
}

func lockChallenge(ctx context.Context, tx pgx.Tx, hash string) (c loginChallenge, err error) {
	err = tx.QueryRow(ctx, `SELECT id::text,staff_version,attempts,expires_at,consumed_at FROM login_challenges WHERE token_hash=$1 FOR UPDATE`, hash).Scan(&c.ID, &c.Version, &c.Attempts, &c.Expires, &c.Consumed)
	return
}
func countChallengeAttempt(ctx context.Context, tx pgx.Tx, id string) error {
	_, e := tx.Exec(ctx, `UPDATE login_challenges SET attempts=attempts+1 WHERE id=$1::uuid`, id)
	return e
}
func consumeChallenge(ctx context.Context, tx pgx.Tx, id string) error {
	_, e := tx.Exec(ctx, `UPDATE login_challenges SET consumed_at=now() WHERE id=$1::uuid`, id)
	return e
}
func consumeRecoveryCode(ctx context.Context, tx pgx.Tx, id, hash string) (bool, error) {
	tag, e := tx.Exec(ctx, `UPDATE mfa_recovery_codes SET used_at=now() WHERE code_hash=$1 AND staff_id=$2::uuid AND used_at IS NULL`, hash, id)
	return tag.RowsAffected() == 1, e
}
func confirmedMFASecret(ctx context.Context, tx pgx.Tx, id string) (blob []byte, last int64, err error) {
	err = tx.QueryRow(ctx, `SELECT secret_ciphertext,last_used_step FROM staff_mfa WHERE staff_id=$1::uuid AND confirmed_at IS NOT NULL FOR UPDATE`, id).Scan(&blob, &last)
	return
}
func pendingMFASecret(ctx context.Context, tx pgx.Tx, id string) (blob []byte, err error) {
	err = tx.QueryRow(ctx, `SELECT secret_ciphertext FROM staff_mfa WHERE staff_id=$1::uuid AND confirmed_at IS NULL AND updated_at>now()-interval '10 minutes' FOR UPDATE`, id).Scan(&blob)
	return
}
func advanceMFAStep(ctx context.Context, tx pgx.Tx, id string, step int64) error {
	_, e := tx.Exec(ctx, `UPDATE staff_mfa SET last_used_step=$1,updated_at=now() WHERE staff_id=$2::uuid`, step, id)
	return e
}
func savePendingMFA(ctx context.Context, tx pgx.Tx, id string, blob []byte) error {
	_, e := tx.Exec(ctx, `INSERT INTO staff_mfa(staff_id,secret_ciphertext) VALUES($1::uuid,$2) ON CONFLICT(staff_id) DO UPDATE SET secret_ciphertext=EXCLUDED.secret_ciphertext,last_used_step=0,updated_at=now() WHERE staff_mfa.confirmed_at IS NULL`, id, blob)
	return e
}
func confirmMFA(ctx context.Context, tx pgx.Tx, id string, step int64) error {
	_, e := tx.Exec(ctx, `UPDATE staff_mfa SET confirmed_at=now(),last_used_step=$1,updated_at=now() WHERE staff_id=$2::uuid`, step, id)
	return e
}
func revokeOtherSessions(ctx context.Context, tx pgx.Tx, id, sid string) error {
	_, e := tx.Exec(ctx, `UPDATE sessions SET revoked_at=COALESCE(revoked_at,now()) WHERE staff_id=$1::uuid AND id<>$2::uuid`, id, sid)
	return e
}
func markSessionMFA(ctx context.Context, tx pgx.Tx, id, sid string) error {
	_, e := tx.Exec(ctx, `UPDATE sessions SET mfa_verified=true WHERE id=$1::uuid AND staff_id=$2::uuid`, sid, id)
	return e
}
func clearRecoveryCodes(ctx context.Context, tx pgx.Tx, id string) error {
	_, e := tx.Exec(ctx, `DELETE FROM mfa_recovery_codes WHERE staff_id=$1::uuid`, id)
	return e
}
func deleteMFA(ctx context.Context, tx pgx.Tx, id string) error {
	_, e := tx.Exec(ctx, `DELETE FROM staff_mfa WHERE staff_id=$1::uuid`, id)
	return e
}
func insertRecoveryCode(ctx context.Context, tx pgx.Tx, id, hash string) error {
	_, e := tx.Exec(ctx, `INSERT INTO mfa_recovery_codes(code_hash,staff_id) VALUES($1,$2::uuid)`, hash, id)
	return e
}
func lockLiveSession(ctx context.Context, tx pgx.Tx, id, sid string) (verified bool, err error) {
	err = tx.QueryRow(ctx, `SELECT mfa_verified FROM sessions WHERE id=$1::uuid AND staff_id=$2::uuid AND revoked_at IS NULL AND access_expires_at>now() AND expires_at>now() FOR UPDATE`, sid, id).Scan(&verified)
	return
}
func bumpSecurityVersion(ctx context.Context, tx pgx.Tx, id string) error {
	_, e := tx.Exec(ctx, `UPDATE staff SET version=version+1,updated_at=now() WHERE id=$1::uuid`, id)
	return e
}
