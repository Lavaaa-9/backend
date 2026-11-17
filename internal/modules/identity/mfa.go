package identity

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"spartial.local/backend/internal/platform"
	"spartial.local/backend/internal/security"
)

// Challenge-based second-factor authentication; no regulatory certification is implied.
// Lock order everywhere: staff row, then challenge/session, then staff_mfa, then recovery codes.

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

func mfaAAD(staffID string) []byte { return []byte("staff_mfa:" + staffID) }

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

// issueChallenge is called inside Login's transaction after the password was verified.
func issueChallenge(ctx context.Context, tx pgx.Tx, u User, client, request, label string) (Challenge, error) {
	token, e := security.Token()
	if e != nil {
		return Challenge{}, e
	}
	id, e := insertChallenge(ctx, tx, security.Digest(token), u.ID, u.Version, client, challengeTTL, label)
	if e != nil {
		return Challenge{}, e
	}
	if e = platform.Audit(ctx, tx, u.ID, "auth.mfa_challenge", id, "", request, "success"); e != nil {
		return Challenge{}, e
	}
	return Challenge{true, token, int(challengeTTL.Seconds())}, nil
}

// VerifyMFA completes a login challenge with a TOTP code or a single-use recovery code.
func (s Service) VerifyMFA(ctx context.Context, in MFAVerify, ip, request string) (Tokens, error) {
	if (in.Code == "") == (in.RecoveryCode == "") {
		return Tokens{}, platform.Fail(422, "validation", "Provide exactly one of code or recovery_code")
	}
	ok, e := s.Repo.Limit(ctx, "mfa-ip:"+security.Digest(ip))
	if e != nil {
		return Tokens{}, e
	}
	if !ok {
		return Tokens{}, platform.Fail(429, "rate_limited", "Too many authentication attempts; retry after 15 minutes")
	}
	hash := security.Digest(in.ChallengeToken)
	staffID, e := challengeStaff(ctx, s.Repo.DB, hash, in.ClientType)
	if errors.Is(e, pgx.ErrNoRows) {
		return Tokens{}, platform.Unauthorized()
	}
	if e != nil {
		return Tokens{}, e
	}
	if ok, e = s.Repo.Limit(ctx, "mfa-staff:"+staffID); e != nil {
		return Tokens{}, e
	} else if !ok {
		return Tokens{}, platform.Fail(429, "rate_limited", "Too many authentication attempts; retry after 15 minutes")
	}
	tx, e := s.Repo.DB.Begin(ctx)
	if e != nil {
		return Tokens{}, e
	}
	defer tx.Rollback(ctx)
	u, _, e := userBy(ctx, tx, "s.id=$1::uuid FOR UPDATE", staffID)
	if e != nil {
		return Tokens{}, e
	}
	challenge, e := lockChallenge(ctx, tx, hash)
	if e != nil {
		return Tokens{}, e
	}

	reject := func(reason string, countAttempt bool) (Tokens, error) {
		if countAttempt {
			if e := countChallengeAttempt(ctx, tx, challenge.ID); e != nil {
				return Tokens{}, e
			}
		}
		if e := platform.AuditReason(ctx, tx, u.ID, "auth.mfa_verify", challenge.ID, "", request, "denied", reason); e != nil {
			return Tokens{}, e
		}
		if e := tx.Commit(ctx); e != nil {
			return Tokens{}, e
		}
		return Tokens{}, platform.Unauthorized()
	}
	switch {
	case challenge.Consumed != nil:
		return reject("challenge_reused", false)
	case !challenge.Expires.After(s.now()):
		return reject("challenge_expired", false)
	case challenge.Attempts >= maxChallengeTries:
		return reject("challenge_exhausted", false)
	case !u.Active || u.Version != challenge.Version:
		return reject("account_changed", false)
	}
	method, ok, e := s.checkFactor(ctx, tx, u.ID, in.Code, in.RecoveryCode)
	if e != nil {
		return Tokens{}, e
	}
	if !ok {
		return reject("invalid_"+method, true)
	}
	if e = consumeChallenge(ctx, tx, challenge.ID); e != nil {
		return Tokens{}, e
	}
	t, e := s.openSession(ctx, tx, u, in.ClientType, true, request, method, challenge.DeviceLabel)
	if e != nil {
		return Tokens{}, e
	}
	return t, tx.Commit(ctx)
}

// checkFactor verifies a TOTP code (advancing the replay step) or consumes a recovery code.
// The staff row must already be locked by the caller.
func (s Service) checkFactor(ctx context.Context, tx pgx.Tx, staffID, code, recovery string) (string, bool, error) {
	if recovery != "" {
		on, e := mfaEnabled(ctx, tx, staffID)
		if e != nil {
			return "recovery_code", false, e
		}
		if !on {
			return "recovery_code", false, nil
		}
		norm := security.NormalizeRecoveryCode(recovery)
		if norm == "" {
			return "recovery_code", false, nil
		}
		used, e := consumeRecoveryCode(ctx, tx, staffID, security.Digest(norm))
		return "recovery_code", used && e == nil, e
	}
	keys, e := s.keys()
	if e != nil {
		return "totp", false, e
	}
	blob, last, e := confirmedMFASecret(ctx, tx, staffID)
	if errors.Is(e, pgx.ErrNoRows) {
		return "totp", false, nil
	}
	if e != nil {
		return "totp", false, e
	}
	secret, e := keys.Open(blob, mfaAAD(staffID))
	if e != nil {
		// A key-configuration fault must never look like a wrong code to the user or the audit.
		return "totp", false, platform.Fail(503, "mfa_unavailable", "Second-factor secret could not be decrypted; check key configuration")
	}
	step, ok := security.VerifyTOTP(secret, code, s.now(), last)
	if !ok {
		return "totp", false, nil
	}
	e = advanceMFAStep(ctx, tx, staffID, step)
	return "totp", e == nil, e
}

// StartEnrollment creates (or replaces) an unconfirmed TOTP secret after password re-authentication.
func (s Service) StartEnrollment(ctx context.Context, a Actor, password, request string) (Enrollment, error) {
	if e := s.limitMFAManagement(ctx, a.ID); e != nil {
		return Enrollment{}, e
	}
	keys, e := s.keys()
	if e != nil {
		return Enrollment{}, e
	}
	tx, e := s.Repo.DB.Begin(ctx)
	if e != nil {
		return Enrollment{}, e
	}
	defer tx.Rollback(ctx)
	u, hash, e := lockMFAActor(ctx, tx, a)
	if e != nil {
		return Enrollment{}, e
	}
	if !security.VerifyPassword(hash, password) {
		return Enrollment{}, rejectMFAManagement(ctx, tx, u.ID, "auth.mfa_enroll_start", request, "reauthentication_failed", platform.Unauthorized())
	}
	if on, e := mfaEnabled(ctx, tx, u.ID); e != nil {
		return Enrollment{}, e
	} else if on {
		return Enrollment{}, platform.Fail(409, "mfa_already_enabled", "Second factor is already enabled; disable it first to re-enroll")
	}
	secret, e := security.NewTOTPSecret()
	if e != nil {
		return Enrollment{}, e
	}
	blob, e := keys.Seal(secret, mfaAAD(u.ID))
	if e != nil {
		return Enrollment{}, e
	}
	if e = savePendingMFA(ctx, tx, u.ID, blob); e != nil {
		return Enrollment{}, e
	}
	if e = platform.Audit(ctx, tx, u.ID, "auth.mfa_enroll_start", u.ID, "", request, "success"); e != nil {
		return Enrollment{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return Enrollment{}, e
	}
	issuer := s.Issuer
	if issuer == "" {
		issuer = "Spartial Polyclinic"
	}
	return Enrollment{security.EncodeTOTPSecret(secret), security.ProvisioningURI(issuer, u.Email, secret)}, nil
}

// ConfirmEnrollment proves possession of the new secret, issues recovery codes once,
// upgrades the current session and revokes every other session.
func (s Service) ConfirmEnrollment(ctx context.Context, a Actor, code, request string) ([]string, error) {
	if e := s.limitMFAManagement(ctx, a.ID); e != nil {
		return nil, e
	}
	keys, e := s.keys()
	if e != nil {
		return nil, e
	}
	tx, e := s.Repo.DB.Begin(ctx)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	if _, _, e = lockMFAActor(ctx, tx, a); e != nil {
		return nil, e
	}
	blob, e := pendingMFASecret(ctx, tx, a.ID)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, platform.Fail(409, "mfa_not_pending", "No unexpired pending second-factor enrollment")
	}
	if e != nil {
		return nil, e
	}
	secret, e := keys.Open(blob, mfaAAD(a.ID))
	if e != nil {
		return nil, platform.Fail(503, "mfa_unavailable", "Second-factor secret could not be decrypted; check key configuration")
	}
	step, ok := security.VerifyTOTP(secret, code, s.now(), 0)
	if !ok {
		return nil, rejectMFAManagement(ctx, tx, a.ID, "auth.mfa_confirm", request, "invalid_totp", platform.Fail(422, "invalid_mfa_code", "The code is not valid for this enrollment"))
	}
	if e = confirmMFA(ctx, tx, a.ID, step); e != nil {
		return nil, e
	}
	if e = bumpSecurityVersion(ctx, tx, a.ID); e != nil {
		return nil, e
	}
	codes, e := replaceRecoveryCodes(ctx, tx, a.ID)
	if e != nil {
		return nil, e
	}
	if e = revokeOtherSessions(ctx, tx, a.ID, a.SessionID); e != nil {
		return nil, e
	}
	if e = markSessionMFA(ctx, tx, a.ID, a.SessionID); e != nil {
		return nil, e
	}
	if e = platform.Audit(ctx, tx, a.ID, "auth.mfa_enabled", a.ID, "", request, "success"); e != nil {
		return nil, e
	}
	return codes, tx.Commit(ctx)
}

// DisableMFA requires the password and a current factor, then revokes every session.
func (s Service) DisableMFA(ctx context.Context, a Actor, password, code, recovery, request string) error {
	if e := s.limitMFAManagement(ctx, a.ID); e != nil {
		return e
	}
	if (code == "") == (recovery == "") {
		return platform.Fail(422, "validation", "Provide exactly one of code or recovery_code")
	}
	tx, e := s.Repo.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	_, hash, e := lockMFAActor(ctx, tx, a)
	if e != nil {
		return e
	}
	if !security.VerifyPassword(hash, password) {
		return rejectMFAManagement(ctx, tx, a.ID, "auth.mfa_disabled", request, "reauthentication_failed", platform.Unauthorized())
	}
	if on, e := mfaEnabled(ctx, tx, a.ID); e != nil {
		return e
	} else if !on {
		return platform.Fail(409, "mfa_not_enabled", "Second factor is not enabled")
	}
	method, ok, e := s.checkFactor(ctx, tx, a.ID, code, recovery)
	if e != nil {
		return e
	}
	if !ok {
		return rejectMFAManagement(ctx, tx, a.ID, "auth.mfa_disabled", request, "invalid_"+method, platform.Fail(422, "invalid_mfa_code", "The second-factor code is not valid"))
	}
	if e = clearRecoveryCodes(ctx, tx, a.ID); e != nil {
		return e
	}
	if e = deleteMFA(ctx, tx, a.ID); e != nil {
		return e
	}
	if e = bumpSecurityVersion(ctx, tx, a.ID); e != nil {
		return e
	}
	if e = revokeAllSessions(ctx, tx, a.ID); e != nil {
		return e
	}
	if e = platform.AuditReason(ctx, tx, a.ID, "auth.mfa_disabled", a.ID, "", request, "success", method); e != nil {
		return e
	}
	return tx.Commit(ctx)
}

func replaceRecoveryCodes(ctx context.Context, tx pgx.Tx, staffID string) ([]string, error) {
	if e := clearRecoveryCodes(ctx, tx, staffID); e != nil {
		return nil, e
	}
	codes := make([]string, 0, recoveryCodeCount)
	for len(codes) < recoveryCodeCount {
		c, e := security.NewRecoveryCode()
		if e != nil {
			return nil, e
		}
		if e = insertRecoveryCode(ctx, tx, staffID, security.Digest(security.NormalizeRecoveryCode(c))); e != nil {
			return nil, e
		}
		codes = append(codes, c)
	}
	return codes, nil
}

// lockMFAActor rechecks the live account and calling session inside the mutation transaction.
func lockMFAActor(ctx context.Context, tx pgx.Tx, a Actor) (User, string, error) {
	u, hash, e := userBy(ctx, tx, "s.id=$1::uuid FOR UPDATE", a.ID)
	if e != nil {
		return User{}, "", e
	}
	_, e = lockLiveSession(ctx, tx, a.ID, a.SessionID)
	if errors.Is(e, pgx.ErrNoRows) || !u.Active {
		return User{}, "", platform.Unauthorized()
	}
	if e != nil {
		return User{}, "", e
	}
	return u, hash, nil
}
func (s Service) limitMFAManagement(ctx context.Context, id string) error {
	ok, e := s.Repo.Limit(ctx, "mfa-manage:"+id)
	if e != nil {
		return e
	}
	if !ok {
		return platform.Fail(429, "rate_limited", "Too many second-factor management attempts; retry after 15 minutes")
	}
	return nil
}
func rejectMFAManagement(ctx context.Context, tx pgx.Tx, id, action, request, reason string, err error) error {
	if e := platform.AuditReason(ctx, tx, id, action, id, "", request, "denied", reason); e != nil {
		return e
	}
	if e := tx.Commit(ctx); e != nil {
		return e
	}
	return err
}

// RegenerateRecoveryCodes replaces the entire set after password and second-factor reauthentication.
// Other sessions are revoked, and pending login challenges are invalidated by the version bump.
func (s Service) RegenerateRecoveryCodes(ctx context.Context, a Actor, password, code, recovery, request string) ([]string, error) {
	if (code == "") == (recovery == "") {
		return nil, platform.Fail(422, "validation", "Provide exactly one of code or recovery_code")
	}
	if e := s.limitMFAManagement(ctx, a.ID); e != nil {
		return nil, e
	}
	tx, e := s.Repo.DB.Begin(ctx)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	u, hash, e := lockMFAActor(ctx, tx, a)
	if e != nil {
		return nil, e
	}
	if !security.VerifyPassword(hash, password) {
		return nil, rejectMFAManagement(ctx, tx, u.ID, "auth.mfa_recovery_regenerate", request, "reauthentication_failed", platform.Unauthorized())
	}
	on, e := mfaEnabled(ctx, tx, u.ID)
	if e != nil {
		return nil, e
	}
	if !on {
		return nil, platform.Fail(409, "mfa_not_enabled", "Second factor is not enabled")
	}
	method, ok, e := s.checkFactor(ctx, tx, u.ID, code, recovery)
	if e != nil {
		return nil, e
	}
	if !ok {
		return nil, rejectMFAManagement(ctx, tx, u.ID, "auth.mfa_recovery_regenerate", request, "invalid_"+method, platform.Fail(422, "invalid_mfa_code", "The second-factor code is not valid"))
	}
	codes, e := replaceRecoveryCodes(ctx, tx, u.ID)
	if e != nil {
		return nil, e
	}
	if e = bumpSecurityVersion(ctx, tx, u.ID); e != nil {
		return nil, e
	}
	if e = revokeOtherSessions(ctx, tx, u.ID, a.SessionID); e != nil {
		return nil, e
	}
	if e = platform.AuditReason(ctx, tx, u.ID, "auth.mfa_recovery_regenerate", u.ID, "", request, "success", method); e != nil {
		return nil, e
	}
	return codes, tx.Commit(ctx)
}
