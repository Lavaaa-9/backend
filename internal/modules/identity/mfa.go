package identity

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"spartial.local/backend/internal/platform"
	"spartial.local/backend/internal/security"
)

const (
	challengeTTL      = 5 * time.Minute
	maxChallengeTries = 5
	recoveryCodeCount = 10
)

type Challenge struct {
	MFARequired    bool   `json:"mfa_required"`
	ChallengeToken string `json:"challenge_token"`
	ExpiresIn      int    `json:"expires_in"`
}
type MFAVerify struct {
	ChallengeToken string `json:"challenge_token" validate:"required,min=32,max=128"`
	Code           string `json:"code" validate:"omitempty,len=6,numeric"`
	RecoveryCode   string `json:"recovery_code" validate:"omitempty,max=32"`
	ClientType     string `json:"client_type" validate:"required,oneof=web mobile"`
}
type Enrollment struct {
	Secret          string `json:"secret"`
	ProvisioningURI string `json:"provisioning_uri"`
}

func mfaAAD(userID string) []byte { return []byte("iam_mfa_factor:" + userID) }

func (s Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s Service) keys() (*security.Keyring, error) {
	if s.Keys == nil {
		return nil, platform.Fail(503, "mfa_unavailable", "Second-factor authentication is not configured on this server")
	}
	return s.Keys, nil
}

func (s Service) issueChallenge(ctx context.Context, uid string, userVersion int, clientType, label, ip, ua string) (Challenge, error) {
	token, e := security.Token()
	if e != nil {
		return Challenge{}, e
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return Challenge{}, e
	}
	defer tx.Rollback(ctx)
	var org string
	_ = tx.QueryRow(ctx, `SELECT org_id::text FROM iam.memberships_for_login($1::uuid) ORDER BY org_name LIMIT 1`, uid).Scan(&org)
	var cid string
	e = tx.QueryRow(ctx, `INSERT INTO iam.mfa_challenge(user_id,token_hash,user_version,client_type,device_label,expires_at)
 VALUES($1::uuid,decode($2,'hex'),$3,$4,NULLIF($5,''),now()+$6::interval) RETURNING id::text`,
		uid, security.Digest(token), userVersion, clientType, label, "5 minutes").Scan(&cid)
	if e != nil {
		return Challenge{}, e
	}
	if e = platform.AuditPreAuth(ctx, tx, "auth.mfa_challenge", "success", cid, org, uid, "", ip, ua); e != nil {
		return Challenge{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return Challenge{}, e
	}
	return Challenge{MFARequired: true, ChallengeToken: token, ExpiresIn: int(challengeTTL.Seconds())}, nil
}

// VerifyMFA completes a login challenge with a TOTP code or a single-use recovery code.
func (s Service) VerifyMFA(ctx context.Context, in MFAVerify, ip, ua, request string) (Tokens, error) {
	if (in.Code == "") == (in.RecoveryCode == "") {
		return Tokens{}, platform.Fail(422, "validation", "Provide either a 6-digit code or a recovery_code")
	}
	keys, e := s.keys()
	if e != nil {
		return Tokens{}, e
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return Tokens{}, e
	}
	defer tx.Rollback(ctx)

	var cid, uid, chClient, label string
	var chVersion, attempts int
	var expires time.Time
	var consumed *time.Time
	e = tx.QueryRow(ctx, `SELECT id::text,user_id::text,user_version,client_type,coalesce(device_label,''),attempts,expires_at,consumed_at
 FROM iam.mfa_challenge WHERE token_hash=decode($1,'hex') FOR UPDATE`, security.Digest(in.ChallengeToken)).
		Scan(&cid, &uid, &chVersion, &chClient, &label, &attempts, &expires, &consumed)
	if errors.Is(e, pgx.ErrNoRows) {
		return Tokens{}, platform.Unauthorized()
	}
	if e != nil {
		return Tokens{}, e
	}
	now := s.now()
	if consumed != nil || chClient != in.ClientType || !expires.After(now) || attempts >= maxChallengeTries {
		return Tokens{}, platform.Unauthorized()
	}

	var status string
	var version, failed int
	var locked *time.Time
	e = tx.QueryRow(ctx, `SELECT status,row_version,failed_login_count,locked_until FROM iam.user_account WHERE id=$1::uuid FOR UPDATE`, uid).
		Scan(&status, &version, &failed, &locked)
	if e != nil {
		return Tokens{}, e
	}
	if !rateLimitDisabled() && locked != nil && locked.After(now) {
		return Tokens{}, platform.Fail(429, "rate_limited", "Account temporarily locked after repeated failures; retry later")
	}
	if status != "active" || version != chVersion {
		return Tokens{}, platform.Unauthorized()
	}

	var org string
	_ = tx.QueryRow(ctx, `SELECT org_id::text FROM iam.memberships_for_login($1::uuid) ORDER BY org_name LIMIT 1`, uid).Scan(&org)

	ok, method, e := s.verifySecondFactor(ctx, tx, keys, uid, in.Code, in.RecoveryCode, now)
	if e != nil {
		return Tokens{}, e
	}
	if !ok {
		lock := ""
		if !rateLimitDisabled() && failed+1 >= 5 {
			lock = "15 minutes"
		}
		_, _ = tx.Exec(ctx, `UPDATE iam.user_account SET failed_login_count=failed_login_count+1,locked_until=CASE WHEN $2<>'' THEN now()+$2::interval ELSE locked_until END WHERE id=$1::uuid`, uid, lock)
		if _, e = tx.Exec(ctx, `UPDATE iam.mfa_challenge SET attempts=attempts+1,user_version=user_version+1,consumed_at=CASE WHEN attempts+1>=$2::smallint THEN now() ELSE consumed_at END WHERE id=$1::uuid`, cid, maxChallengeTries); e != nil {
			return Tokens{}, e
		}
		_ = platform.AuditPreAuth(ctx, tx, "auth.mfa_verify", "denied", cid, org, uid, "invalid_code", ip, ua)
		_ = tx.Commit(ctx)
		return Tokens{}, platform.Unauthorized()
	}

	if _, e = tx.Exec(ctx, `UPDATE iam.mfa_challenge SET consumed_at=now() WHERE id=$1::uuid`, cid); e != nil {
		return Tokens{}, e
	}
	access, e := security.Token()
	if e != nil {
		return Tokens{}, e
	}
	refresh, e := security.Token()
	if e != nil {
		return Tokens{}, e
	}
	t, e := s.createSessionTx(ctx, tx, uid, in.ClientType, "password_mfa", true, access, refresh, label, ip, ua, request)
	if e != nil {
		return Tokens{}, e
	}
	if e = platform.AuditPreAuth(ctx, tx, "auth.mfa_verify", "success", cid, org, uid, method, ip, ua); e != nil {
		return Tokens{}, e
	}
	return t, tx.Commit(ctx)
}

func (s Service) verifySecondFactor(ctx context.Context, tx pgx.Tx, keys *security.Keyring, uid, code, recovery string, now time.Time) (bool, string, error) {
	if code != "" {
		var fid string
		var blob []byte
		var lastStep *int64
		e := tx.QueryRow(ctx, `SELECT id::text,secret_ciphertext,last_used_step FROM iam.mfa_factor WHERE user_id=$1::uuid AND factor_type='totp' AND status='active' FOR UPDATE`, uid).
			Scan(&fid, &blob, &lastStep)
		if errors.Is(e, pgx.ErrNoRows) {
			return false, "", nil
		}
		if e != nil {
			return false, "", e
		}
		secret, e := keys.Open(blob, mfaAAD(uid))
		if e != nil {
			return false, "", e
		}
		prev := int64(0)
		if lastStep != nil {
			prev = *lastStep
		}
		step, ok := security.VerifyTOTP(secret, code, now, prev)
		if !ok {
			return false, "", nil
		}
		if _, e = tx.Exec(ctx, `UPDATE iam.mfa_factor SET last_used_step=$2,last_used_at=now() WHERE id=$1::uuid`, fid, step); e != nil {
			return false, "", e
		}
		return true, "totp", nil
	}
	norm := security.NormalizeRecoveryCode(recovery)
	if norm == "" {
		return false, "", nil
	}
	tag, e := tx.Exec(ctx, `UPDATE iam.mfa_recovery_code SET used_at=now() WHERE user_id=$1::uuid AND code_hash=decode($2,'hex') AND used_at IS NULL`, uid, security.Digest(norm))
	if e != nil {
		return false, "", e
	}
	if tag.RowsAffected() != 1 {
		return false, "", nil
	}
	return true, "recovery_code", nil
}

func verifyCurrentPassword(ctx context.Context, tx pgx.Tx, uid, current string) error {
	var hash string
	if e := tx.QueryRow(ctx, `SELECT password_hash FROM iam.password_credential WHERE user_id=$1::uuid FOR UPDATE`, uid).Scan(&hash); e != nil {
		return e
	}
	if !security.VerifyPassword(hash, current) {
		return platform.Unauthorized()
	}
	return nil
}

func replaceRecoveryCodes(ctx context.Context, tx pgx.Tx, uid string) ([]string, error) {
	if _, e := tx.Exec(ctx, `DELETE FROM iam.mfa_recovery_code WHERE user_id=$1::uuid`, uid); e != nil {
		return nil, e
	}
	codes := make([]string, 0, recoveryCodeCount)
	for i := 0; i < recoveryCodeCount; i++ {
		c, e := security.NewRecoveryCode()
		if e != nil {
			return nil, e
		}
		norm := security.NormalizeRecoveryCode(c)
		if _, e = tx.Exec(ctx, `INSERT INTO iam.mfa_recovery_code(user_id,code_hash) VALUES($1::uuid,decode($2,'hex'))`, uid, security.Digest(norm)); e != nil {
			return nil, e
		}
		codes = append(codes, c)
	}
	return codes, nil
}

// StartEnrollment creates a pending TOTP factor for the authenticated user.
func (s Service) StartEnrollment(ctx context.Context, a Actor, currentPassword, request string) (Enrollment, error) {
	keys, e := s.keys()
	if e != nil {
		return Enrollment{}, e
	}
	tx, _, e := platform.BeginRequest(ctx, s.DB, a.SessionID, request)
	if e != nil {
		return Enrollment{}, e
	}
	defer tx.Rollback(ctx)
	if e = verifyCurrentPassword(ctx, tx, a.ID, currentPassword); e != nil {
		return Enrollment{}, e
	}
	var active bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM iam.mfa_factor WHERE user_id=$1::uuid AND factor_type='totp' AND status='active')`, a.ID).Scan(&active); e != nil {
		return Enrollment{}, e
	}
	if active {
		return Enrollment{}, platform.Fail(409, "mfa_already_enabled", "Second-factor authentication is already enabled")
	}
	if _, e = tx.Exec(ctx, `UPDATE iam.mfa_factor SET status='revoked' WHERE user_id=$1::uuid AND factor_type='totp' AND status='pending'`, a.ID); e != nil {
		return Enrollment{}, e
	}
	secret, e := security.NewTOTPSecret()
	if e != nil {
		return Enrollment{}, e
	}
	blob, e := keys.Seal(secret, mfaAAD(a.ID))
	if e != nil {
		return Enrollment{}, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO iam.mfa_factor(user_id,factor_type,label,secret_ciphertext,key_ref,status) VALUES($1::uuid,'totp','default',$2,$3,'pending')`,
		a.ID, blob, keys.ActiveKeyID()); e != nil {
		return Enrollment{}, e
	}
	if e = platform.Audit(ctx, tx, "auth.mfa_enroll_start", "authentication", "success", "user", a.ID, "", ""); e != nil {
		return Enrollment{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return Enrollment{}, e
	}
	return Enrollment{
		Secret:          security.EncodeTOTPSecret(secret),
		ProvisioningURI: security.ProvisioningURI("Spartial", a.Email, secret),
	}, nil
}

// ConfirmEnrollment verifies the first TOTP code, activates MFA, marks the current session verified, and returns recovery codes once.
func (s Service) ConfirmEnrollment(ctx context.Context, a Actor, code, request string) ([]string, error) {
	keys, e := s.keys()
	if e != nil {
		return nil, e
	}
	tx, _, e := platform.BeginRequest(ctx, s.DB, a.SessionID, request)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	var fid string
	var blob []byte
	e = tx.QueryRow(ctx, `SELECT id::text,secret_ciphertext FROM iam.mfa_factor WHERE user_id=$1::uuid AND factor_type='totp' AND status='pending' FOR UPDATE`, a.ID).Scan(&fid, &blob)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, platform.Fail(409, "mfa_not_pending", "Start MFA enrollment before confirming")
	}
	if e != nil {
		return nil, e
	}
	secret, e := keys.Open(blob, mfaAAD(a.ID))
	if e != nil {
		return nil, e
	}
	step, ok := security.VerifyTOTP(secret, code, s.now(), 0)
	if !ok {
		return nil, platform.Fail(422, "invalid_code", "Invalid verification code")
	}
	if _, e = tx.Exec(ctx, `UPDATE iam.mfa_factor SET status='active',verified_at=now(),last_used_at=now(),last_used_step=$2 WHERE id=$1::uuid`, fid, step); e != nil {
		return nil, e
	}
	if _, e = tx.Exec(ctx, `UPDATE iam.user_account SET mfa_required=true WHERE id=$1::uuid`, a.ID); e != nil {
		return nil, e
	}
	if _, e = tx.Exec(ctx, `UPDATE iam.session SET mfa_verified_at=now(),auth_method='password_mfa' WHERE id=$1::uuid`, a.SessionID); e != nil {
		return nil, e
	}
	codes, e := replaceRecoveryCodes(ctx, tx, a.ID)
	if e != nil {
		return nil, e
	}
	if e = platform.Audit(ctx, tx, "auth.mfa_enroll_confirm", "authentication", "success", "user", a.ID, "", ""); e != nil {
		return nil, e
	}
	return codes, tx.Commit(ctx)
}

// DisableMFA disables TOTP after verifying password and second factor, then revokes all user sessions.
func (s Service) DisableMFA(ctx context.Context, a Actor, currentPassword, code, recoveryCode, request string) error {
	if (code == "") == (recoveryCode == "") {
		return platform.Fail(422, "validation", "Provide either a 6-digit code or a recovery_code")
	}
	keys, e := s.keys()
	if e != nil {
		return e
	}
	tx, _, e := platform.BeginRequest(ctx, s.DB, a.SessionID, request)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if e = verifyCurrentPassword(ctx, tx, a.ID, currentPassword); e != nil {
		return e
	}
	ok, method, e := s.verifySecondFactor(ctx, tx, keys, a.ID, code, recoveryCode, s.now())
	if e != nil {
		return e
	}
	if !ok {
		return platform.Unauthorized()
	}
	if _, e = tx.Exec(ctx, `UPDATE iam.mfa_factor SET status='revoked' WHERE user_id=$1::uuid AND factor_type='totp' AND status<>'revoked'`, a.ID); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `DELETE FROM iam.mfa_recovery_code WHERE user_id=$1::uuid`, a.ID); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `UPDATE iam.user_account SET mfa_required=false WHERE id=$1::uuid`, a.ID); e != nil {
		return e
	}
	if e = revokeUserSessions(ctx, tx, a.ID, "superseded"); e != nil {
		return e
	}
	if e = platform.Audit(ctx, tx, "auth.mfa_disable", "authentication", "success", "user", a.ID, "", method); e != nil {
		return e
	}
	return tx.Commit(ctx)
}

// RegenerateRecoveryCodes replaces all recovery codes after verifying password and second factor.
func (s Service) RegenerateRecoveryCodes(ctx context.Context, a Actor, currentPassword, code, recoveryCode, request string) ([]string, error) {
	if (code == "") == (recoveryCode == "") {
		return nil, platform.Fail(422, "validation", "Provide either a 6-digit code or a recovery_code")
	}
	keys, e := s.keys()
	if e != nil {
		return nil, e
	}
	tx, _, e := platform.BeginRequest(ctx, s.DB, a.SessionID, request)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	if e = verifyCurrentPassword(ctx, tx, a.ID, currentPassword); e != nil {
		return nil, e
	}
	ok, method, e := s.verifySecondFactor(ctx, tx, keys, a.ID, code, recoveryCode, s.now())
	if e != nil {
		return nil, e
	}
	if !ok {
		return nil, platform.Unauthorized()
	}
	codes, e := replaceRecoveryCodes(ctx, tx, a.ID)
	if e != nil {
		return nil, e
	}
	if e = platform.Audit(ctx, tx, "auth.mfa_recovery_regenerate", "authentication", "success", "user", a.ID, "", method); e != nil {
		return nil, e
	}
	return codes, tx.Commit(ctx)
}
