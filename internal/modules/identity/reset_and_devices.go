package identity

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"spartial.local/backend/internal/platform"
	"spartial.local/backend/internal/security"
)

// RequestPasswordReset creates a 30-minute reset token if an active staff account matches email.
// Callers must never reveal whether an account exists in HTTP responses.
func (s Service) RequestPasswordReset(ctx context.Context, email, ip, ua string) (string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return "", nil
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return "", e
	}
	defer tx.Rollback(ctx)
	var uid string
	e = tx.QueryRow(ctx, `SELECT id::text FROM iam.user_account WHERE account_type='staff' AND email=$1 AND status='active'`, email).Scan(&uid)
	if errors.Is(e, pgx.ErrNoRows) {
		return "", nil
	}
	if e != nil {
		return "", e
	}
	token, e := security.Token()
	if e != nil {
		return "", e
	}
	var org string
	_ = tx.QueryRow(ctx, `SELECT org_id::text FROM iam.memberships_for_login($1::uuid) ORDER BY org_name LIMIT 1`, uid).Scan(&org)
	var rid string
	e = tx.QueryRow(ctx, `INSERT INTO iam.password_reset(user_id,token_hash,expires_at,requested_ip)
 VALUES($1::uuid,decode($2,'hex'),now()+interval '30 minutes',NULLIF($3,'')::inet) RETURNING id::text`,
		uid, security.Digest(token), ip).Scan(&rid)
	if e != nil {
		return "", e
	}
	if e = platform.AuditPreAuth(ctx, tx, "auth.password_reset_request", "success", rid, org, uid, "", ip, ua); e != nil {
		return "", e
	}
	if e = tx.Commit(ctx); e != nil {
		return "", e
	}
	return token, nil
}

// ConfirmPasswordReset consumes a valid reset token, sets the new password, and revokes all active sessions.
func (s Service) ConfirmPasswordReset(ctx context.Context, token, newPassword, ip, ua string) error {
	if !security.PasswordAllowed(newPassword) {
		return platform.Fail(422, "weak_password", "Choose a longer password that is not on the common-password list")
	}
	hash, e := security.HashPassword(newPassword)
	if e != nil {
		return e
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)

	var rid, uid string
	var expires time.Time
	var consumed *time.Time
	e = tx.QueryRow(ctx, `SELECT id::text,user_id::text,expires_at,consumed_at FROM iam.password_reset WHERE token_hash=decode($1,'hex') FOR UPDATE`,
		security.Digest(token)).Scan(&rid, &uid, &expires, &consumed)
	if errors.Is(e, pgx.ErrNoRows) {
		return platform.Unauthorized()
	}
	if e != nil {
		return e
	}
	if consumed != nil || !expires.After(s.now()) {
		return platform.Unauthorized()
	}
	var oldHash string
	if e = tx.QueryRow(ctx, `SELECT password_hash FROM iam.password_credential WHERE user_id=$1::uuid FOR UPDATE`, uid).Scan(&oldHash); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO iam.password_history(user_id,password_hash) VALUES($1::uuid,$2)`, uid, oldHash); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `UPDATE iam.password_credential SET password_hash=$2,changed_at=now() WHERE user_id=$1::uuid`, uid, hash); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `UPDATE iam.user_account SET must_change_password=false,failed_login_count=0,locked_until=NULL WHERE id=$1::uuid`, uid); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `UPDATE iam.password_reset SET consumed_at=now() WHERE id=$1::uuid`, rid); e != nil {
		return e
	}
	if e = revokeUserSessions(ctx, tx, uid, "password_changed"); e != nil {
		return e
	}
	var org string
	_ = tx.QueryRow(ctx, `SELECT org_id::text FROM iam.memberships_for_login($1::uuid) ORDER BY org_name LIMIT 1`, uid).Scan(&org)
	if e = platform.AuditPreAuth(ctx, tx, "auth.password_reset", "success", rid, org, uid, "", ip, ua); e != nil {
		return e
	}
	return tx.Commit(ctx)
}

// ---------------------------------------------------------------- user push devices (comms.device)

type DeviceInput struct {
	Platform   string `json:"platform" validate:"required,oneof=android ios web"`
	PushToken  string `json:"push_token" validate:"required,min=16,max=1024"`
	AppVersion string `json:"app_version" validate:"max=40"`
}

const devJ = `(to_jsonb(d) - 'push_token_ciphertext' - 'push_token_bidx' - 'key_ref')`

func (s Service) ListDevices(ctx context.Context, a Actor, request string) ([]platform.Raw, error) {
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) ([]platform.Raw, error) {
		return platform.Many(ctx, tx, `SELECT `+devJ+` FROM comms.device d WHERE d.user_id=$1::uuid ORDER BY d.last_seen_at DESC LIMIT 50`, a.ID)
	})
}

func (s Service) RegisterDevice(ctx context.Context, a Actor, in DeviceInput, indexKey []byte, request string) (platform.Raw, error) {
	keys, e := s.keys()
	if e != nil {
		return nil, e
	}
	if len(indexKey) != 32 {
		return nil, platform.Fail(503, "encryption_unavailable", "Device encryption keys are not configured on this server")
	}
	tok := strings.TrimSpace(in.PushToken)
	ct, e := keys.Seal([]byte(tok), []byte("comms_device:"+a.ID))
	if e != nil {
		return nil, e
	}
	mac := hmac.New(sha256.New, indexKey)
	mac.Write([]byte("comms_device"))
	mac.Write([]byte{0})
	mac.Write([]byte(tok))
	bidx := mac.Sum(nil)
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) (platform.Raw, error) {
		var id string
		e := tx.QueryRow(ctx, `INSERT INTO comms.device(user_id,platform,push_token_ciphertext,push_token_bidx,key_ref,app_version)
 VALUES($1::uuid,$2,$3,$4,$5,NULLIF($6,''))
 ON CONFLICT (push_token_bidx) DO UPDATE SET last_seen_at=now(),revoked_at=NULL,app_version=EXCLUDED.app_version
 WHERE comms.device.user_id=$1::uuid
 RETURNING id::text`, a.ID, in.Platform, ct, bidx, keys.ActiveKeyID(), strings.TrimSpace(in.AppVersion)).Scan(&id)
		if e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, `SELECT `+devJ+` FROM comms.device d WHERE d.id=$1::uuid`, id)
	})
}

func (s Service) RevokeDevice(ctx context.Context, a Actor, id, request string) (platform.Raw, error) {
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) (platform.Raw, error) {
		tag, e := tx.Exec(ctx, `UPDATE comms.device SET revoked_at=now() WHERE id=$1::uuid AND user_id=$2::uuid AND revoked_at IS NULL`, id, a.ID)
		if e != nil {
			return nil, e
		}
		if e = platform.Changed(ctx, tx, tag, "comms.device", id); e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, `SELECT `+devJ+` FROM comms.device d WHERE d.id=$1::uuid`, id)
	})
}
