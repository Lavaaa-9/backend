package identity

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"spartial.local/backend/internal/platform"
	"spartial.local/backend/internal/security"
)

// Roles an administrator may assign (system roles in spartial-db).
const roleList = "administrator receptionist nurse doctor lab_technician lab_supervisor pharmacist cashier billing_supervisor manager compliance_officer"

type User struct {
	ID          string   `json:"id"`
	Email       string   `json:"email"`
	DisplayName string   `json:"display_name"`
	Role        string   `json:"role"`
	Active      bool     `json:"active"`
	Version     int      `json:"version"`
	FacilityIDs []string `json:"facility_ids"`
}
type Actor struct {
	User
	SessionID    string   `json:"-"`
	ClientType   string   `json:"-"`
	OrgID        string   `json:"-"`
	MembershipID string   `json:"-"`
	Permissions  []string `json:"permissions"`
	MFAVerified  bool     `json:"mfa_verified"`
	MFAEnabled   bool     `json:"mfa_enabled"`
}

func (a Actor) Can(p string) bool {
	for _, v := range a.Permissions {
		if v == p {
			return true
		}
	}
	return false
}
func (a Actor) InFacility(id string) bool {
	for _, f := range a.FacilityIDs {
		if f == id {
			return true
		}
	}
	return false
}

// withAliases adds the permission names the web client already understands.
func withAliases(perms []string) []string {
	has := map[string]bool{}
	for _, p := range perms {
		has[p] = true
	}
	out := append([]string{}, perms...)
	add := func(alias string, ok bool) {
		if ok && !has[alias] {
			out = append(out, alias)
		}
	}
	add("patients.read", has["patient.read"])
	add("patients.write", has["patient.create"] || has["patient.update"])
	add("facilities.read", true)
	add("facilities.manage", has["admin.facility.manage"])
	add("staff.read", has["admin.user.manage"])
	add("staff.manage", has["admin.user.manage"])
	add("metrics.read", has["report.view"])
	add("privacy.report", has["audit.read"] || has["privacy.request.manage"])
	add("break_glass.request", has["break_glass.use"])
	add("break_glass.review", has["audit.read"])
	add("approvals.request", has["approval.request"])
	add("approvals.review", has["approval.review"])
	return out
}

type Credentials struct {
	Email       string `json:"email" validate:"required,email,max=254"`
	Password    string `json:"password" validate:"required,max=256"`
	ClientType  string `json:"client_type" validate:"required,oneof=web mobile"`
	DeviceLabel string `json:"device_label" validate:"max=100"`
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
	DisplayName string   `json:"display_name" validate:"required,min=1,max=150"`
	Password    string   `json:"password" validate:"required,min=12,max=256"`
	Role        string   `json:"role" validate:"required,oneof=administrator receptionist nurse doctor lab_technician lab_supervisor pharmacist cashier billing_supervisor manager compliance_officer"`
	FacilityIDs []string `json:"facility_ids" validate:"required,min=1,max=30,unique,dive,uuid"`
}
type UpdateStaff struct {
	DisplayName string   `json:"display_name" validate:"required,min=1,max=150"`
	Role        string   `json:"role" validate:"required,oneof=administrator receptionist nurse doctor lab_technician lab_supervisor pharmacist cashier billing_supervisor manager compliance_officer"`
	Active      *bool    `json:"active" validate:"required"`
	FacilityIDs []string `json:"facility_ids" validate:"required,min=1,max=30,unique,dive,uuid"`
	Version     int      `json:"version" validate:"required,min=1"`
}
type Session struct {
	ID          string     `json:"id"`
	ClientType  string     `json:"client_type"`
	CreatedAt   time.Time  `json:"created_at"`
	ExpiresAt   time.Time  `json:"expires_at"`
	RevokedAt   *time.Time `json:"revoked_at"`
	LastSeenAt  time.Time  `json:"last_seen_at"`
	DeviceLabel string     `json:"device_label"`
}

type Service struct {
	DB        *pgxpool.Pool
	DummyHash string
	Keys      *security.Keyring
	Now       func() time.Time
	// Production enables strict production defaults.
	Production bool
}

func rateLimitDisabled() bool {
	return os.Getenv("APP_ENV") == "development" && os.Getenv("DISABLE_AUTH_RATE_LIMIT") == "true"
}

// loadActor binds the request context for sid inside tx and reads the caller's profile.
func loadActor(ctx context.Context, tx pgx.Tx, sid, request string) (Actor, error) {
	var a Actor
	var s platform.Session
	if e := platform.BindRequest(ctx, tx, sid, request, &s); e != nil {
		return a, e
	}
	var roles, facilities []string
	var active, mfaEnabled bool
	e := tx.QueryRow(ctx, `SELECT p.email,p.display_name,p.row_version,p.roles,p.facility_ids::text[],(u.status='active'),
 EXISTS(SELECT 1 FROM iam.mfa_factor f WHERE f.user_id=u.id AND f.status='active')
 FROM iam.my_profile() p, iam.user_account u WHERE u.id=sec.current_user_id()`).Scan(&a.Email, &a.DisplayName, &a.Version, &roles, &facilities, &active, &mfaEnabled)
	if e != nil {
		return a, e
	}
	a.ID, a.OrgID, a.MembershipID, a.SessionID = s.UserID, s.OrgID, s.MembershipID, sid
	a.Active, a.FacilityIDs, a.Permissions, a.MFAEnabled = active, facilities, withAliases(s.Permissions), mfaEnabled
	if len(roles) > 0 {
		a.Role = roles[0]
	}
	a.MFAVerified = false
	return a, nil
}

// Actor authenticates an access token.
func (s Service) Actor(ctx context.Context, token, request string) (Actor, error) {
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return Actor{}, e
	}
	defer tx.Rollback(ctx)
	var sid, client string
	var mfa bool
	e = tx.QueryRow(ctx, `SELECT id::text,client_type,mfa_verified_at IS NOT NULL FROM iam.session WHERE token_hash=decode($1,'hex')`, security.Digest(token)).Scan(&sid, &client, &mfa)
	if e != nil {
		return Actor{}, e
	}
	a, e := loadActor(ctx, tx, sid, request)
	if e != nil {
		return Actor{}, e
	}
	a.ClientType, a.MFAVerified = client, mfa
	return a, tx.Commit(ctx)
}

func (s Service) Login(ctx context.Context, in Credentials, ip, ua, request string) (Tokens, *Challenge, error) {
	email := strings.ToLower(strings.TrimSpace(in.Email))
	var uid, hash, status string
	var failed, version int
	var locked *time.Time
	var mfaRequired, mfaActive bool
	err := s.DB.QueryRow(ctx, `SELECT u.id::text,coalesce(p.password_hash,''),u.status,u.failed_login_count,u.row_version,u.locked_until,u.mfa_required,
 EXISTS(SELECT 1 FROM iam.mfa_factor f WHERE f.user_id=u.id AND f.factor_type='totp' AND f.status='active')
 FROM iam.user_account u LEFT JOIN iam.password_credential p ON p.user_id=u.id WHERE u.account_type='staff' AND u.email=$1`, email).
		Scan(&uid, &hash, &status, &failed, &version, &locked, &mfaRequired, &mfaActive)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Tokens{}, nil, err
	}
	if hash == "" {
		hash = s.DummyHash
	}
	valid := security.VerifyPassword(hash, in.Password)
	if err == nil && !rateLimitDisabled() && locked != nil && locked.After(s.now()) {
		return Tokens{}, nil, platform.Fail(429, "rate_limited", "Account temporarily locked after repeated failures; retry later")
	}
	if err != nil || !valid || status != "active" {
		if uid != "" {
			s.recordFailure(ctx, uid, failed, ip, ua)
		}
		return Tokens{}, nil, platform.Unauthorized()
	}
	if mfaActive {
		if _, e := s.keys(); e != nil {
			return Tokens{}, nil, e
		}
		ch, e := s.issueChallenge(ctx, uid, version, in.ClientType, deviceLabel(in.DeviceLabel, ua), ip, ua)
		if e != nil {
			return Tokens{}, nil, e
		}
		return Tokens{}, &ch, nil
	}
	if mfaRequired {
		return Tokens{}, nil, platform.Fail(403, "mfa_enrollment_required", "This account requires a second factor, but none is enrolled")
	}
	t, e := s.createSession(ctx, uid, in.ClientType, "password", false, deviceLabel(in.DeviceLabel, ua), ip, ua, request)
	return t, nil, e
}

func (s Service) createSession(ctx context.Context, uid, clientType, authMethod string, mfaVerified bool, label, ip, ua, request string) (Tokens, error) {
	access, e := security.Token()
	if e != nil {
		return Tokens{}, e
	}
	refresh, e := security.Token()
	if e != nil {
		return Tokens{}, e
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return Tokens{}, e
	}
	defer tx.Rollback(ctx)
	t, e := s.createSessionTx(ctx, tx, uid, clientType, authMethod, mfaVerified, access, refresh, label, ip, ua, request)
	if e != nil {
		return Tokens{}, e
	}
	return t, tx.Commit(ctx)
}

func (s Service) createSessionTx(ctx context.Context, tx pgx.Tx, uid, clientType, authMethod string, mfaVerified bool, access, refresh, label, ip, ua, request string) (Tokens, error) {
	var membership, org string
	e := tx.QueryRow(ctx, `SELECT membership_id::text,org_id::text FROM iam.memberships_for_login($1::uuid) ORDER BY org_name LIMIT 1`, uid).Scan(&membership, &org)
	if errors.Is(e, pgx.ErrNoRows) {
		return Tokens{}, platform.Fail(403, "no_membership", "No active organization membership for this account")
	}
	if e != nil {
		return Tokens{}, e
	}
	// The API selects the tenant before the session exists so the organization row is readable.
	if _, e = tx.Exec(ctx, `SELECT set_config('app.org_id',$1,true)`, org); e != nil {
		return Tokens{}, e
	}
	var sid string
	e = tx.QueryRow(ctx, `INSERT INTO iam.session(user_id,org_id,membership_id,token_hash,client_type,auth_method,ip_address,user_agent,idle_timeout,idle_expires_at,absolute_expires_at,mfa_verified_at)
 SELECT $1::uuid,o.id,$2::uuid,decode($3,'hex'),$4,$5,NULLIF($6,'')::inet,NULLIF($7,''),o.staff_idle_timeout,now()+o.staff_idle_timeout,now()+o.staff_session_max,CASE WHEN $8 THEN now() END FROM org.organization o WHERE o.id=$9::uuid RETURNING id::text`,
		uid, membership, security.Digest(access), clientType, authMethod, ip, label, mfaVerified, org).Scan(&sid)
	if e != nil {
		return Tokens{}, e
	}
	if e = insertRefresh(ctx, tx, sid, security.Digest(refresh)); e != nil {
		return Tokens{}, e
	}
	if _, e = tx.Exec(ctx, `UPDATE iam.user_account SET failed_login_count=0,locked_until=NULL,last_login_at=now() WHERE id=$1::uuid`, uid); e != nil {
		return Tokens{}, e
	}
	if e = platform.AuditPreAuth(ctx, tx, "auth.login", "success", sid, org, uid, authMethod, ip, ua); e != nil {
		return Tokens{}, e
	}
	a, e := loadActor(ctx, tx, sid, request)
	if e != nil {
		return Tokens{}, e
	}
	return Tokens{access, "Bearer", 900, a.User, refresh}, nil
}
func deviceLabel(label, ua string) string {
	if label != "" {
		return label
	}
	return ua
}
func (s Service) recordFailure(ctx context.Context, uid string, failed int, ip, ua string) {
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return
	}
	defer tx.Rollback(ctx)
	lock := ""
	if !rateLimitDisabled() && failed+1 >= 5 {
		lock = "15 minutes"
	}
	tx.Exec(ctx, `UPDATE iam.user_account SET failed_login_count=failed_login_count+1,locked_until=CASE WHEN $2<>'' THEN now()+$2::interval ELSE locked_until END WHERE id=$1::uuid`, uid, lock)
	platform.AuditPreAuth(ctx, tx, "auth.login", "denied", "", "", uid, "bad_credentials", ip, ua)
	tx.Commit(ctx)
}
func insertRefresh(ctx context.Context, tx pgx.Tx, sid, hash string) error {
	_, e := tx.Exec(ctx, `INSERT INTO iam.refresh_token(session_id,family_id,token_hash,expires_at) SELECT id,gen_random_uuid(),decode($2,'hex'),least(now()+interval '7 days',absolute_expires_at) FROM iam.session WHERE id=$1::uuid`, sid, hash)
	return e
}

func (s Service) Refresh(ctx context.Context, token, client, request string) (Tokens, error) {
	if len(token) < 32 || len(token) > 128 {
		return Tokens{}, platform.Unauthorized()
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return Tokens{}, e
	}
	defer tx.Rollback(ctx)
	var rid, sid, family, userID, orgID, sessionClient, userStatus string
	var used, revoked, sessionRevoked *time.Time
	var expires, idleExpires, absExpires time.Time
	e = tx.QueryRow(ctx, `SELECT r.id::text,r.session_id::text,r.family_id::text,r.used_at,r.revoked_at,r.expires_at,s.user_id::text,coalesce(s.org_id::text,''),s.client_type,s.revoked_at,s.idle_expires_at,s.absolute_expires_at,u.status
 FROM iam.refresh_token r JOIN iam.session s ON s.id=r.session_id JOIN iam.user_account u ON u.id=s.user_id WHERE r.token_hash=decode($1,'hex') FOR UPDATE OF r,s`, security.Digest(token)).
		Scan(&rid, &sid, &family, &used, &revoked, &expires, &userID, &orgID, &sessionClient, &sessionRevoked, &idleExpires, &absExpires, &userStatus)
	if errors.Is(e, pgx.ErrNoRows) {
		return Tokens{}, platform.Unauthorized()
	}
	if e != nil {
		return Tokens{}, e
	}
	if sessionClient != client {
		return Tokens{}, platform.Unauthorized()
	}
	now := time.Now()
	if used != nil {
		// Reuse of a rotated token: revoke the whole session.
		tx.Exec(ctx, `UPDATE iam.session SET revoked_at=coalesce(revoked_at,now()),revoked_reason=coalesce(revoked_reason,'token_reuse_detected') WHERE id=$1::uuid`, sid)
		platform.AuditPreAuth(ctx, tx, "auth.refresh_reuse", "denied", sid, orgID, userID, "token_reuse", "", "")
		tx.Commit(ctx)
		return Tokens{}, platform.Unauthorized()
	}
	if revoked != nil || sessionRevoked != nil || userStatus != "active" || !expires.After(now) || !idleExpires.After(now) || !absExpires.After(now) {
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
	var newID string
	e = tx.QueryRow(ctx, `INSERT INTO iam.refresh_token(session_id,family_id,token_hash,expires_at) SELECT $1::uuid,$2::uuid,decode($3,'hex'),least(now()+interval '7 days',absolute_expires_at) FROM iam.session WHERE id=$1::uuid RETURNING id::text`, sid, family, security.Digest(refresh)).Scan(&newID)
	if e != nil {
		return Tokens{}, e
	}
	if _, e = tx.Exec(ctx, `UPDATE iam.refresh_token SET used_at=now(),replaced_by_id=$2::uuid WHERE id=$1::uuid`, rid, newID); e != nil {
		return Tokens{}, e
	}
	if _, e = tx.Exec(ctx, `UPDATE iam.session SET token_hash=decode($2,'hex'),last_seen_at=now(),idle_expires_at=least(now()+idle_timeout,absolute_expires_at) WHERE id=$1::uuid`, sid, security.Digest(access)); e != nil {
		return Tokens{}, e
	}
	a, e := loadActor(ctx, tx, sid, request)
	if e != nil {
		return Tokens{}, e
	}
	if e = platform.Audit(ctx, tx, "auth.refresh", "authentication", "success", "session", sid, "", ""); e != nil {
		return Tokens{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return Tokens{}, e
	}
	return Tokens{access, "Bearer", 900, a.User, refresh}, nil
}

// Revoke ends one of the caller's own sessions.
func (s Service) Revoke(ctx context.Context, a Actor, sid, request string) error {
	tx, _, e := platform.BeginRequest(ctx, s.DB, a.SessionID, request)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	tag, e := tx.Exec(ctx, `UPDATE iam.session SET revoked_at=now(),revoked_reason='logout' WHERE id=$1::uuid AND user_id=$2::uuid AND revoked_at IS NULL`, sid, a.ID)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return platform.NotFound()
	}
	if _, e = tx.Exec(ctx, `UPDATE iam.refresh_token SET revoked_at=coalesce(revoked_at,now()) WHERE session_id=$1::uuid`, sid); e != nil {
		return e
	}
	if e = platform.Audit(ctx, tx, "auth.logout", "authentication", "success", "session", sid, "", ""); e != nil {
		return e
	}
	return tx.Commit(ctx)
}

func (s Service) Sessions(ctx context.Context, userID string) ([]Session, error) {
	rows, e := s.DB.Query(ctx, `SELECT id::text,client_type,created_at,absolute_expires_at,revoked_at,last_seen_at,coalesce(user_agent,'') FROM iam.session WHERE user_id=$1::uuid ORDER BY created_at DESC LIMIT 100`, userID)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Session{}
	for rows.Next() {
		var v Session
		if e = rows.Scan(&v.ID, &v.ClientType, &v.CreatedAt, &v.ExpiresAt, &v.RevokedAt, &v.LastSeenAt, &v.DeviceLabel); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s Service) ChangePassword(ctx context.Context, a Actor, current, next, request string) error {
	if !security.PasswordAllowed(next) {
		return platform.Fail(422, "weak_password", "Choose a longer password that is not on the common-password list")
	}
	hash, e := security.HashPassword(next)
	if e != nil {
		return e
	}
	tx, _, e := platform.BeginRequest(ctx, s.DB, a.SessionID, request)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var old string
	if e = tx.QueryRow(ctx, `SELECT password_hash FROM iam.password_credential WHERE user_id=$1::uuid FOR UPDATE`, a.ID).Scan(&old); e != nil {
		return e
	}
	if !security.VerifyPassword(old, current) {
		return platform.Unauthorized()
	}
	if _, e = tx.Exec(ctx, `INSERT INTO iam.password_history(user_id,password_hash) VALUES($1::uuid,$2)`, a.ID, old); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `UPDATE iam.password_credential SET password_hash=$2,changed_at=now() WHERE user_id=$1::uuid`, a.ID, hash); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `UPDATE iam.user_account SET must_change_password=false WHERE id=$1::uuid`, a.ID); e != nil {
		return e
	}
	if e = revokeUserSessions(ctx, tx, a.ID, "password_changed"); e != nil {
		return e
	}
	if e = platform.Audit(ctx, tx, "auth.password_change", "authentication", "success", "user", a.ID, "", ""); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func revokeUserSessions(ctx context.Context, tx pgx.Tx, uid, reason string) error {
	if _, e := tx.Exec(ctx, `UPDATE iam.refresh_token SET revoked_at=coalesce(revoked_at,now()) WHERE session_id IN (SELECT id FROM iam.session WHERE user_id=$1::uuid AND revoked_at IS NULL)`, uid); e != nil {
		return e
	}
	_, e := tx.Exec(ctx, `UPDATE iam.session SET revoked_at=now(),revoked_reason=$2 WHERE user_id=$1::uuid AND revoked_at IS NULL`, uid, reason)
	return e
}

// ---- staff administration (requires admin.user.manage; RLS enforces it again) ----

const userSelect = `SELECT u.id::text,u.email::text,u.display_name,coalesce(r.code,''),(u.status='active' AND m.status='active'),u.row_version,coalesce(f.ids,'{}'::text[])
 FROM iam.membership m JOIN iam.user_account u ON u.id=m.user_id
 LEFT JOIN LATERAL (SELECT (array_agg(ro.code ORDER BY ro.code))[1] AS code FROM iam.membership_role mr JOIN iam.role ro ON ro.id=mr.role_id WHERE mr.membership_id=m.id AND mr.revoked_at IS NULL) r ON true
 LEFT JOIN LATERAL (SELECT array_agg(DISTINCT fa.id::text) AS ids FROM iam.membership_role mr JOIN org.facility fa ON fa.org_id=mr.org_id AND (mr.facility_id IS NULL OR mr.facility_id=fa.id) WHERE mr.membership_id=m.id AND mr.revoked_at IS NULL) f ON true
 WHERE m.org_id=sec.current_org_id()`

func scanUser(row pgx.Row) (User, error) {
	var u User
	e := row.Scan(&u.ID, &u.Email, &u.DisplayName, &u.Role, &u.Active, &u.Version, &u.FacilityIDs)
	return u, e
}
func (s Service) List(ctx context.Context, a Actor, after string, limit int, request string) ([]User, error) {
	tx, _, e := platform.BeginRequest(ctx, s.DB, a.SessionID, request)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	rows, e := tx.Query(ctx, userSelect+` AND ($1='' OR u.id>NULLIF($1,'')::uuid) ORDER BY u.id LIMIT $2`, after, limit)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []User{}
	for rows.Next() {
		u, e := scanUser(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, u)
	}
	return out, rows.Err()
}
func (s Service) assignRole(ctx context.Context, tx pgx.Tx, org, membership, role string, facilities []string) error {
	var roleID string
	if e := tx.QueryRow(ctx, `SELECT id::text FROM iam.role WHERE org_id IS NULL AND code=$1 AND status='active'`, role).Scan(&roleID); e != nil {
		return e
	}
	for _, f := range facilities {
		var ok bool
		if e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM org.facility WHERE id=$1::uuid AND status='active')`, f).Scan(&ok); e != nil {
			return e
		}
		if !ok {
			return pgx.ErrNoRows
		}
		if _, e := tx.Exec(ctx, `INSERT INTO iam.membership_role(org_id,membership_id,role_id,facility_id) VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid)`, org, membership, roleID, f); e != nil {
			return e
		}
	}
	return nil
}
func (s Service) CreateStaff(ctx context.Context, a Actor, in StaffInput, request string) (User, error) {
	if !a.Can("admin.user.manage") {
		return User{}, platform.Forbidden()
	}
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	in.DisplayName = strings.TrimSpace(in.DisplayName)
	if in.DisplayName == "" {
		return User{}, platform.Fail(422, "validation", "Display name is required")
	}
	if !security.PasswordAllowed(in.Password) {
		return User{}, platform.Fail(422, "weak_password", "Choose a longer password that is not on the common-password list")
	}
	hash, e := security.HashPassword(in.Password)
	if e != nil {
		return User{}, e
	}
	tx, sess, e := platform.BeginRequest(ctx, s.DB, a.SessionID, request)
	if e != nil {
		return User{}, e
	}
	defer tx.Rollback(ctx)
	var uid, mid string
	if e = tx.QueryRow(ctx, `INSERT INTO iam.user_account(account_type,email,display_name,status,mfa_required) VALUES('staff',$1,$2,'active',false) RETURNING id::text`, in.Email, in.DisplayName).Scan(&uid); e != nil {
		return User{}, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO iam.password_credential(user_id,password_hash) VALUES($1::uuid,$2)`, uid, hash); e != nil {
		return User{}, e
	}
	if e = tx.QueryRow(ctx, `INSERT INTO iam.membership(org_id,user_id,status) VALUES($1::uuid,$2::uuid,'active') RETURNING id::text`, sess.OrgID, uid).Scan(&mid); e != nil {
		return User{}, e
	}
	if e = s.assignRole(ctx, tx, sess.OrgID, mid, in.Role, in.FacilityIDs); e != nil {
		return User{}, e
	}
	if e = platform.Audit(ctx, tx, "staff.user.create", "administration", "success", "user", uid, "", ""); e != nil {
		return User{}, e
	}
	u, e := scanUser(tx.QueryRow(ctx, userSelect+` AND u.id=$1::uuid`, uid))
	if e != nil {
		return User{}, e
	}
	return u, tx.Commit(ctx)
}
func (s Service) UpdateStaff(ctx context.Context, a Actor, id string, in UpdateStaff, request string) (User, error) {
	if !a.Can("admin.user.manage") {
		return User{}, platform.Forbidden()
	}
	if id == a.ID {
		return User{}, platform.Fail(409, "self_change", "Administrators cannot change their own role or status")
	}
	in.DisplayName = strings.TrimSpace(in.DisplayName)
	if in.DisplayName == "" {
		return User{}, platform.Fail(422, "validation", "Display name is required")
	}
	tx, sess, e := platform.BeginRequest(ctx, s.DB, a.SessionID, request)
	if e != nil {
		return User{}, e
	}
	defer tx.Rollback(ctx)
	var mid string
	if e = tx.QueryRow(ctx, `SELECT m.id::text FROM iam.membership m JOIN iam.user_account u ON u.id=m.user_id WHERE m.org_id=sec.current_org_id() AND u.id=$1::uuid FOR UPDATE OF m`, id).Scan(&mid); e != nil {
		return User{}, e
	}
	status, mstatus := "active", "active"
	if !*in.Active {
		status, mstatus = "deactivated", "suspended"
	}
	tag, e := tx.Exec(ctx, `UPDATE iam.user_account SET display_name=$2,status=$3 WHERE id=$1::uuid AND row_version=$4`, id, in.DisplayName, status, in.Version)
	if e != nil {
		return User{}, e
	}
	if tag.RowsAffected() != 1 {
		return User{}, platform.Conflict()
	}
	if _, e = tx.Exec(ctx, `UPDATE iam.membership SET status=$2 WHERE id=$1::uuid AND status<>$2 AND status<>'ended'`, mid, mstatus); e != nil {
		return User{}, e
	}
	if _, e = tx.Exec(ctx, `UPDATE iam.membership_role SET revoked_at=now(),revoked_by=sec.current_user_id(),revoke_reason='role or scope changed' WHERE membership_id=$1::uuid AND revoked_at IS NULL`, mid); e != nil {
		return User{}, e
	}
	if e = s.assignRole(ctx, tx, sess.OrgID, mid, in.Role, in.FacilityIDs); e != nil {
		return User{}, e
	}
	reason := "admin_revoked"
	if !*in.Active {
		reason = "account_disabled"
	}
	if e = revokeUserSessions(ctx, tx, id, reason); e != nil {
		return User{}, e
	}
	if e = platform.Audit(ctx, tx, "staff.user.update", "administration", "success", "user", id, "", ""); e != nil {
		return User{}, e
	}
	u, e := scanUser(tx.QueryRow(ctx, userSelect+` AND u.id=$1::uuid`, id))
	if e != nil {
		return User{}, e
	}
	return u, tx.Commit(ctx)
}
