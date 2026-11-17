package identity

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"spartial.local/backend/internal/platform"
	"spartial.local/backend/internal/security"
	"strings"
	"time"
)

type Service struct {
	Repo      Repository
	DummyHash string
	// Keys encrypts TOTP secrets; nil disables enrollment (503) but never weakens login.
	Keys   *security.Keyring
	Issuer string
	// Now is overridable in tests; production uses time.Now.
	Now func() time.Time
}
type Credentials struct {
	Email      string `json:"email" validate:"required,email,max=254"`
	Password   string `json:"password" validate:"required,max=256"`
	ClientType string `json:"client_type" validate:"required,oneof=web mobile"`
}
type Tokens struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	User         User   `json:"user"`
	RefreshToken string `json:"-"`
}
type StaffInput struct {
	Email       string   `json:"email" validate:"required,email,max=254"`
	DisplayName string   `json:"display_name" validate:"required,min=1,max=160"`
	Password    string   `json:"password" validate:"required,min=12,max=256"`
	Role        string   `json:"role" validate:"required,oneof=administrator reception nurse doctor laboratory pharmacy cashier management privacy-reviewer"`
	FacilityIDs []string `json:"facility_ids" validate:"required,min=1,max=30,unique,dive,uuid"`
}
type UpdateStaff struct {
	DisplayName string   `json:"display_name" validate:"required,min=1,max=160"`
	Role        string   `json:"role" validate:"required,oneof=administrator reception nurse doctor laboratory pharmacy cashier management privacy-reviewer"`
	Active      *bool    `json:"active" validate:"required"`
	FacilityIDs []string `json:"facility_ids" validate:"required,min=1,max=30,unique,dive,uuid"`
	Version     int      `json:"version" validate:"required,min=1"`
}

// Login verifies the password. Staff with a confirmed second factor receive a Challenge and no
// session; VerifyMFA then issues tokens. Otherwise a password-assurance session is issued.
func (s Service) Login(ctx context.Context, in Credentials, ip, request string) (Tokens, *Challenge, error) {
	for _, key := range []string{"ip:" + security.Digest(ip), "email:" + security.Digest(strings.ToLower(in.Email))} {
		ok, e := s.Repo.Limit(ctx, key)
		if e != nil {
			return Tokens{}, nil, e
		}
		if !ok {
			return Tokens{}, nil, platform.Fail(429, "rate_limited", "Too many authentication attempts; retry after 15 minutes")
		}
	}
	u, hash, err := s.Repo.ByEmail(ctx, strings.ToLower(in.Email))
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Tokens{}, nil, err
	}
	if hash == "" {
		hash = s.DummyHash
	}
	valid := security.VerifyPassword(hash, in.Password)
	if err != nil || !valid || !u.Active {
		if e := s.auditDenied(ctx, u.ID, request); e != nil {
			return Tokens{}, nil, e
		}
		return Tokens{}, nil, platform.Unauthorized()
	}
	tx, e := s.Repo.DB.Begin(ctx)
	if e != nil {
		return Tokens{}, nil, e
	}
	defer tx.Rollback(ctx)
	// Lock the staff row so suspension or a password change cannot race new sessions.
	current, currentHash, e := userBy(ctx, tx, "s.id=$1::uuid FOR UPDATE", u.ID)
	if e != nil {
		return Tokens{}, nil, e
	}
	if !current.Active || currentHash != hash {
		return Tokens{}, nil, platform.Unauthorized()
	}
	enrolled, e := mfaEnabled(ctx, tx, u.ID)
	if e != nil {
		return Tokens{}, nil, e
	}
	if enrolled {
		c, e := issueChallenge(ctx, tx, current, in.ClientType, request)
		if e != nil {
			return Tokens{}, nil, e
		}
		return Tokens{}, &c, tx.Commit(ctx)
	}
	t, e := s.openSession(ctx, tx, current, in.ClientType, false, request, "")
	if e != nil {
		return Tokens{}, nil, e
	}
	return t, nil, tx.Commit(ctx)
}

// openSession creates a session inside the caller's transaction (staff row already locked).
func (s Service) openSession(ctx context.Context, tx pgx.Tx, u User, client string, mfa bool, request, method string) (Tokens, error) {
	access, e := security.Token()
	if e != nil {
		return Tokens{}, e
	}
	refresh, e := security.Token()
	if e != nil {
		return Tokens{}, e
	}
	sid, e := createSession(ctx, tx, u.ID, client, security.Digest(access), security.Digest(refresh), mfa)
	if e != nil {
		return Tokens{}, e
	}
	action := "auth.login"
	if mfa {
		action = "auth.mfa_verify"
	}
	if e = platform.AuditReason(ctx, tx, u.ID, action, sid, "", request, "success", method); e != nil {
		return Tokens{}, e
	}
	return Tokens{access, "Bearer", 900, u, refresh}, nil
}

func (s Service) auditDenied(ctx context.Context, id, request string) error {
	tx, e := s.Repo.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if e = platform.Audit(ctx, tx, id, "auth.login", "", "", request, "denied"); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (s Service) Refresh(ctx context.Context, token, client, request string) (Tokens, error) {
	if len(token) < 32 || len(token) > 128 {
		return Tokens{}, platform.Unauthorized()
	}
	tx, e := s.Repo.DB.Begin(ctx)
	if e != nil {
		return Tokens{}, e
	}
	defer tx.Rollback(ctx)
	sid, uid, e := findRefresh(ctx, tx, security.Digest(token), client)
	if errors.Is(e, pgx.ErrNoRows) {
		return Tokens{}, platform.Unauthorized()
	}
	if e != nil {
		return Tokens{}, e
	}
	// Lock ordering is always staff, then session, then refresh token.
	u, _, e := userBy(ctx, tx, "s.id=$1::uuid FOR UPDATE", uid)
	if e != nil {
		return Tokens{}, e
	}
	revoked, expires, e := lockSession(ctx, tx, sid)
	if e != nil {
		return Tokens{}, e
	}
	used, e := lockRefresh(ctx, tx, security.Digest(token))
	if e != nil {
		return Tokens{}, e
	}
	if used != nil {
		if e = revokeSession(ctx, tx, sid); e != nil {
			return Tokens{}, e
		}
		if e = platform.Audit(ctx, tx, uid, "auth.refresh_reuse", sid, "", request, "denied"); e != nil {
			return Tokens{}, e
		}
		if e = tx.Commit(ctx); e != nil {
			return Tokens{}, e
		}
		return Tokens{}, platform.Unauthorized()
	}
	if !u.Active || revoked != nil || !expires.After(time.Now()) {
		return Tokens{}, platform.Unauthorized()
	}
	access, e := security.Token()
	if e != nil {
		return Tokens{}, e
	}
	refresh, e := security.Token()
	if e != nil {
		return Tokens{}, e
	}
	if e = consumeRefresh(ctx, tx, security.Digest(token)); e != nil {
		return Tokens{}, e
	}
	if e = insertRefresh(ctx, tx, sid, security.Digest(refresh)); e != nil {
		return Tokens{}, e
	}
	if e = rotateAccess(ctx, tx, sid, security.Digest(access)); e != nil {
		return Tokens{}, e
	}
	if e = platform.Audit(ctx, tx, uid, "auth.refresh", sid, "", request, "success"); e != nil {
		return Tokens{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return Tokens{}, e
	}
	return Tokens{access, "Bearer", min(900, max(0, int(time.Until(expires).Seconds()))), u, refresh}, nil
}
func (s Service) Revoke(ctx context.Context, a Actor, sid, request string) error {
	tx, e := s.Repo.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	found, e := revokeOwnedSession(ctx, tx, sid, a.ID)
	if e != nil {
		return e
	}
	if !found {
		return platform.NotFound()
	}
	if e = platform.Audit(ctx, tx, a.ID, "auth.logout", sid, "", request, "success"); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (s Service) CreateStaff(ctx context.Context, a Actor, in StaffInput, request string) (User, error) {
	if a.Role != "administrator" || !a.Can("staff.manage") {
		return User{}, platform.Forbidden()
	}
	in.Email = strings.ToLower(in.Email)
	in.DisplayName = strings.TrimSpace(in.DisplayName)
	if in.DisplayName == "" {
		return User{}, platform.Fail(422, "validation", "Display name is required")
	}
	hash, e := security.HashPassword(in.Password)
	if e != nil {
		return User{}, e
	}
	tx, e := s.Repo.DB.Begin(ctx)
	if e != nil {
		return User{}, e
	}
	defer tx.Rollback(ctx)
	if e = lockStaff(ctx, tx); e != nil {
		return User{}, e
	}
	if e = ensureAdmin(ctx, tx, a.ID); e != nil {
		return User{}, e
	}
	u, e := insertUser(ctx, tx, in, hash)
	if e != nil {
		return User{}, e
	}
	if e = platform.Audit(ctx, tx, a.ID, "staff.create", u.ID, "", request, "success"); e != nil {
		return User{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return User{}, e
	}
	return u, nil
}
func ensureAdmin(ctx context.Context, tx pgx.Tx, id string) error {
	ok, e := isAdmin(ctx, tx, id)
	if e != nil {
		return e
	}
	if !ok {
		return platform.Forbidden()
	}
	return nil
}
func (s Service) UpdateStaff(ctx context.Context, a Actor, id string, in UpdateStaff, request string) (User, error) {
	if a.Role != "administrator" || !a.Can("staff.manage") {
		return User{}, platform.Forbidden()
	}
	tx, e := s.Repo.DB.Begin(ctx)
	if e != nil {
		return User{}, e
	}
	defer tx.Rollback(ctx)
	if e = lockStaff(ctx, tx); e != nil {
		return User{}, e
	}
	if e = ensureAdmin(ctx, tx, a.ID); e != nil {
		return User{}, e
	}
	old, _, e := userBy(ctx, tx, "s.id=$1::uuid FOR UPDATE", id)
	if e != nil {
		return User{}, e
	}
	if old.Version != in.Version {
		return User{}, platform.Conflict()
	}
	if old.Active && old.Role == "administrator" && (!*in.Active || in.Role != "administrator") {
		n, err := countAdmins(ctx, tx)
		if err != nil {
			return User{}, err
		}
		if n <= 1 {
			return User{}, platform.Fail(409, "last_administrator", "Cannot remove the last active administrator")
		}
	}
	name := strings.TrimSpace(in.DisplayName)
	if name == "" {
		return User{}, platform.Fail(422, "validation", "Display name is required")
	}
	if e = updateUser(ctx, tx, id, name, in); e != nil {
		return User{}, e
	}
	if e = assign(ctx, tx, id, in.FacilityIDs); e != nil {
		return User{}, e
	}
	// Any account/permission assignment change revokes all existing credentials.
	if e = revokeAllSessions(ctx, tx, id); e != nil {
		return User{}, e
	}
	if e = platform.Audit(ctx, tx, a.ID, "staff.update", id, "", request, "success"); e != nil {
		return User{}, e
	}
	u, _, e := userBy(ctx, tx, "s.id=$1::uuid", id)
	if e != nil {
		return User{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return User{}, e
	}
	return u, nil
}
func (s Service) ChangePassword(ctx context.Context, a Actor, old, password, request string) error {
	tx, e := s.Repo.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	_, hash, e := userBy(ctx, tx, "s.id=$1::uuid FOR UPDATE", a.ID)
	if e != nil {
		return e
	}
	if !security.VerifyPassword(hash, old) {
		return platform.Unauthorized()
	}
	hash, e = security.HashPassword(password)
	if e != nil {
		return e
	}
	if e = setPassword(ctx, tx, a.ID, hash); e != nil {
		return e
	}
	if e = revokeAllSessions(ctx, tx, a.ID); e != nil {
		return e
	}
	if e = platform.Audit(ctx, tx, a.ID, "auth.password_change", a.ID, "", request, "success"); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
