package identity

import (
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"spartial.local/backend/internal/security"
	"time"
)

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
	SessionID   string   `json:"-"`
	ClientType  string   `json:"-"`
	Permissions []string `json:"permissions"`
	// MFAVerified is the session's assurance level; Privileged comes from the live role row.
	MFAVerified bool `json:"mfa_verified"`
	Privileged  bool `json:"-"`
}

func (a Actor) Can(p string) bool {
	for _, v := range a.Permissions {
		if v == p {
			return true
		}
	}
	return false
}

type Repository struct{ DB *pgxpool.Pool }
type querier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func userBy(ctx context.Context, q querier, clause string, arg any) (User, string, error) {
	var u User
	var hash string
	err := q.QueryRow(ctx, `SELECT s.id::text,s.email,s.display_name,s.role_name,s.active,s.version,s.password_hash,COALESCE((SELECT array_agg(facility_id::text ORDER BY facility_id) FROM staff_facilities WHERE staff_id=s.id),'{}') FROM staff s WHERE `+clause, arg).Scan(&u.ID, &u.Email, &u.DisplayName, &u.Role, &u.Active, &u.Version, &hash, &u.FacilityIDs)
	return u, hash, err
}
func (r Repository) ByEmail(ctx context.Context, email string) (User, string, error) {
	return userBy(ctx, r.DB, "s.email=$1", email)
}
func (r Repository) Actor(ctx context.Context, token string) (Actor, error) {
	var a Actor
	err := r.DB.QueryRow(ctx, `UPDATE sessions s SET last_seen_at=now() FROM staff u WHERE u.id=s.staff_id AND s.access_hash=$1 AND u.active AND s.revoked_at IS NULL AND s.access_expires_at>now() AND s.expires_at>now() AND (s.client_type<>'web' OR s.last_seen_at>now()-interval '15 minutes') RETURNING u.id::text,u.email,u.display_name,u.role_name,u.active,u.version,s.id::text,s.client_type,
 COALESCE((SELECT array_agg(sf.facility_id::text ORDER BY sf.facility_id) FROM staff_facilities sf JOIN facilities f ON f.id=sf.facility_id AND f.active WHERE sf.staff_id=u.id),'{}'),
 COALESCE((SELECT array_agg(permission_name ORDER BY permission_name) FROM role_permissions WHERE role_name=u.role_name),'{}'),
 s.mfa_verified,(SELECT privileged FROM roles WHERE name=u.role_name)
 `, security.Digest(token)).Scan(&a.ID, &a.Email, &a.DisplayName, &a.Role, &a.Active, &a.Version, &a.SessionID, &a.ClientType, &a.FacilityIDs, &a.Permissions, &a.MFAVerified, &a.Privileged)
	return a, err
}
func (r Repository) Limit(ctx context.Context, bucket string) (bool, error) {
	var n int
	err := r.DB.QueryRow(ctx, `INSERT INTO auth_limits(bucket,window_start,attempts) VALUES($1,now(),1)
 ON CONFLICT(bucket) DO UPDATE SET attempts=CASE WHEN auth_limits.window_start<now()-interval '15 minutes' THEN 1 ELSE auth_limits.attempts+1 END,
 window_start=CASE WHEN auth_limits.window_start<now()-interval '15 minutes' THEN now() ELSE auth_limits.window_start END RETURNING attempts`, bucket).Scan(&n)
	return n <= 10, err
}
func lockStaff(ctx context.Context, tx pgx.Tx) error {
	_, e := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(8201771)`)
	return e
}
func insertUser(ctx context.Context, tx pgx.Tx, in StaffInput, hash string) (User, error) {
	var id string
	err := tx.QueryRow(ctx, `INSERT INTO staff(email,display_name,password_hash,role_name) VALUES($1,$2,$3,$4) RETURNING id::text`, in.Email, in.DisplayName, hash, in.Role).Scan(&id)
	if err != nil {
		return User{}, err
	}
	if err = assign(ctx, tx, id, in.FacilityIDs); err != nil {
		return User{}, err
	}
	u, _, err := userBy(ctx, tx, "s.id=$1::uuid", id)
	return u, err
}
func assign(ctx context.Context, tx pgx.Tx, id string, facilities []string) error {
	_, err := tx.Exec(ctx, `DELETE FROM staff_facilities WHERE staff_id=$1::uuid`, id)
	if err != nil {
		return err
	}
	for _, f := range facilities {
		tag, e := tx.Exec(ctx, `INSERT INTO staff_facilities(staff_id,facility_id) SELECT $1::uuid,id FROM facilities WHERE id=$2::uuid AND active ON CONFLICT DO NOTHING`, id, f)
		if e != nil {
			return e
		}
		if tag.RowsAffected() != 1 {
			return pgx.ErrNoRows
		}
	}
	return nil
}
func (r Repository) List(ctx context.Context, after string, limit int) ([]User, error) {
	rows, e := r.DB.Query(ctx, `SELECT s.id::text,s.email,s.display_name,s.role_name,s.active,s.version,COALESCE((SELECT array_agg(facility_id::text ORDER BY facility_id) FROM staff_facilities WHERE staff_id=s.id),'{}') FROM staff s WHERE ($1='' OR s.id>NULLIF($1,'')::uuid) ORDER BY s.id LIMIT $2`, after, limit)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	users := []User{}
	for rows.Next() {
		var u User
		if e = rows.Scan(&u.ID, &u.Email, &u.DisplayName, &u.Role, &u.Active, &u.Version, &u.FacilityIDs); e != nil {
			return nil, e
		}
		users = append(users, u)
	}
	return users, rows.Err()
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

func (r Repository) Sessions(ctx context.Context, id string) ([]Session, error) {
	rows, e := r.DB.Query(ctx, `SELECT id::text,client_type,created_at,expires_at,revoked_at,last_seen_at,device_label FROM sessions WHERE staff_id=$1::uuid ORDER BY created_at DESC LIMIT 100`, id)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Session{}
	for rows.Next() {
		var s Session
		if e = rows.Scan(&s.ID, &s.ClientType, &s.CreatedAt, &s.ExpiresAt, &s.RevokedAt, &s.LastSeenAt, &s.DeviceLabel); e != nil {
			return nil, e
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func createSession(ctx context.Context, tx pgx.Tx, uid, client, accessHash, refreshHash string, mfa bool, label string) (string, error) {
	var sid string
	e := tx.QueryRow(ctx, `INSERT INTO sessions(staff_id,client_type,access_hash,access_expires_at,expires_at,mfa_verified,device_label) VALUES($1::uuid,$2,$3,now()+interval '15 minutes',now()+interval '7 days',$4,$5) RETURNING id::text`, uid, client, accessHash, mfa, label).Scan(&sid)
	if e != nil {
		return "", e
	}
	if e = insertRefresh(ctx, tx, sid, refreshHash); e != nil {
		return "", e
	}
	return sid, nil
}
func insertRefresh(ctx context.Context, tx pgx.Tx, sid, hash string) error {
	_, e := tx.Exec(ctx, `INSERT INTO refresh_tokens(token_hash,session_id) VALUES($1,$2::uuid)`, hash, sid)
	return e
}
func findRefresh(ctx context.Context, tx pgx.Tx, hash, client string) (sid, uid string, err error) {
	err = tx.QueryRow(ctx, `SELECT r.session_id::text,s.staff_id::text FROM refresh_tokens r JOIN sessions s ON s.id=r.session_id WHERE r.token_hash=$1 AND s.client_type=$2`, hash, client).Scan(&sid, &uid)
	return
}
func lockSession(ctx context.Context, tx pgx.Tx, sid string) (revoked *time.Time, expires time.Time, idleExpired bool, err error) {
	err = tx.QueryRow(ctx, `SELECT revoked_at,expires_at,(client_type='web' AND last_seen_at<=now()-interval '15 minutes') FROM sessions WHERE id=$1::uuid FOR UPDATE`, sid).Scan(&revoked, &expires, &idleExpired)
	return
}
func lockRefresh(ctx context.Context, tx pgx.Tx, hash string) (used *time.Time, err error) {
	err = tx.QueryRow(ctx, `SELECT used_at FROM refresh_tokens WHERE token_hash=$1 FOR UPDATE`, hash).Scan(&used)
	return
}
func revokeSession(ctx context.Context, tx pgx.Tx, sid string) error {
	_, e := tx.Exec(ctx, `UPDATE sessions SET revoked_at=COALESCE(revoked_at,now()) WHERE id=$1::uuid`, sid)
	return e
}
func revokeOwnedSession(ctx context.Context, tx pgx.Tx, sid, uid string) (bool, error) {
	tag, e := tx.Exec(ctx, `UPDATE sessions SET revoked_at=COALESCE(revoked_at,now()) WHERE id=$1::uuid AND staff_id=$2::uuid`, sid, uid)
	return tag.RowsAffected() == 1, e
}
func consumeRefresh(ctx context.Context, tx pgx.Tx, hash string) error {
	_, e := tx.Exec(ctx, `UPDATE refresh_tokens SET used_at=now() WHERE token_hash=$1`, hash)
	return e
}
func rotateAccess(ctx context.Context, tx pgx.Tx, sid, hash string) error {
	_, e := tx.Exec(ctx, `UPDATE sessions SET last_seen_at=now(),access_hash=$1,access_expires_at=LEAST(now()+interval '15 minutes',expires_at) WHERE id=$2::uuid`, hash, sid)
	return e
}
func revokeAllSessions(ctx context.Context, tx pgx.Tx, uid string) error {
	_, e := tx.Exec(ctx, `UPDATE sessions SET revoked_at=COALESCE(revoked_at,now()) WHERE staff_id=$1::uuid`, uid)
	return e
}
func isAdmin(ctx context.Context, tx pgx.Tx, id string) (ok bool, err error) {
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM staff WHERE id=$1::uuid AND active AND role_name='administrator')`, id).Scan(&ok)
	return
}
func countAdmins(ctx context.Context, tx pgx.Tx) (n int, err error) {
	err = tx.QueryRow(ctx, `SELECT count(*) FROM staff WHERE active AND role_name='administrator'`).Scan(&n)
	return
}
func updateUser(ctx context.Context, tx pgx.Tx, id, name string, in UpdateStaff) error {
	_, e := tx.Exec(ctx, `UPDATE staff SET display_name=$1,role_name=$2,active=$3,version=version+1,updated_at=now() WHERE id=$4::uuid`, name, in.Role, *in.Active, id)
	return e
}
func setPassword(ctx context.Context, tx pgx.Tx, id, hash string) error {
	_, e := tx.Exec(ctx, `UPDATE staff SET password_hash=$1,version=version+1,updated_at=now() WHERE id=$2::uuid`, hash, id)
	return e
}
