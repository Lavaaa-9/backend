package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"time"
)

type Error struct {
	Status        int
	Code, Message string
}

func (e *Error) Error() string                    { return e.Message }
func Fail(status int, code, message string) error { return &Error{status, code, message} }
func NotFound() error                             { return Fail(404, "not_found", "Record not found") }
func Forbidden() error                            { return Fail(403, "forbidden", "Permission denied") }
func Unauthorized() error {
	return Fail(401, "unauthorized", "Authentication required or credentials expired")
}
func Conflict() error { return Fail(409, "conflict", "Record changed or conflicts with existing data") }
func Normalize(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return NotFound()
	}
	var p *pgconn.PgError
	if errors.As(err, &p) {
		switch p.Code {
		case "SPT01", "SPI01", "SPS01", "SPB01":
			// these messages are written for users by the database rules
			return Fail(409, "conflict", p.Message)
		case "SPV01", "SPC01", "SPX01":
			return Fail(422, "rule_violation", p.Message)
		case "23505", "23P01", "40001", "40P01":
			return Conflict()
		case "23503", "23514", "22P02":
			return Fail(422, "invalid_reference", "Invalid value or reference")
		case "SPA01", "42501":
			return Forbidden()
		case "28000":
			return Unauthorized()
		}
	}
	return err
}
func Open(ctx context.Context, url string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("invalid database configuration")
	}
	cfg.MaxConns = 10
	cfg.MinConns = 1
	cfg.MaxConnLifetime = 30 * time.Minute
	cfg.ConnConfig.RuntimeParams["timezone"] = "UTC"
	cfg.ConnConfig.RuntimeParams["datestyle"] = "ISO, YMD"
	cfg.AfterConnect = func(_ context.Context, conn *pgx.Conn) error {
		conn.TypeMap().RegisterType(&pgtype.Type{Name: "timestamptz", OID: pgtype.TimestamptzOID, Codec: &pgtype.TimestamptzCodec{ScanLocation: time.UTC}})
		return nil
	}
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = "10000"
	p, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, errors.New("database connection failed")
	}
	if err = p.Ping(ctx); err != nil {
		p.Close()
		return nil, errors.New("database unavailable")
	}
	return p, nil
}

// LatestMigration is the newest spartial-db migration this API was written against.
const LatestMigration = "0018_compliance_document_read"

func CheckSchema(ctx context.Context, db *pgxpool.Pool) error {
	var ok bool
	if e := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM public.schema_migration WHERE version=$1)`, LatestMigration).Scan(&ok); e != nil || !ok {
		return Fail(503, "not_ready", "Database schema version does not match this API")
	}
	return nil
}

// CheckRuntimeRole refuses privileged connections: the API must run as spartial_app so RLS applies.
func CheckRuntimeRole(ctx context.Context, db *pgxpool.Pool) error {
	var unsafe bool
	e := db.QueryRow(ctx, `SELECT rolsuper OR rolreplication OR rolbypassrls OR rolcreaterole OR rolcreatedb OR pg_has_role(current_user,'spartial_owner','MEMBER') FROM pg_roles WHERE rolname=current_user`).Scan(&unsafe)
	if e != nil {
		return e
	}
	if unsafe {
		return fmt.Errorf("API requires the restricted spartial_app runtime role in DATABASE_URL")
	}
	return nil
}

// Session is the identity the database derived from the session in sec.begin_request.
type Session struct {
	UserID, OrgID, MembershipID, ActorType string
	Permissions                            []string
}

// BeginRequest opens a transaction whose first statement binds the request context. The
// database validates the session itself; RLS, triggers and audit then rely on that context.
func BeginRequest(ctx context.Context, db *pgxpool.Pool, sessionID, requestID string) (pgx.Tx, Session, error) {
	var s Session
	tx, e := db.Begin(ctx)
	if e != nil {
		return nil, s, e
	}
	if e = BindRequest(ctx, tx, sessionID, requestID, &s); e != nil {
		tx.Rollback(ctx)
		return nil, s, e
	}
	return tx, s, nil
}

// BindRequest calls sec.begin_request inside an existing transaction.
func BindRequest(ctx context.Context, tx pgx.Tx, sessionID, requestID string, s *Session) error {
	var membership, patient *string
	e := tx.QueryRow(ctx, `SELECT user_id::text,org_id::text,membership_id::text,patient_id::text,actor_type,permissions FROM sec.begin_request($1::uuid,$2::uuid)`, sessionID, requestID).Scan(&s.UserID, &s.OrgID, &membership, &patient, &s.ActorType, &s.Permissions)
	if membership != nil {
		s.MembershipID = *membership
	}
	return e
}

type clientMetaKey struct{}

type ClientMeta struct {
	IP, UserAgent string
}

// WithClientMeta attaches the caller's IP and User-Agent to ctx for automatic audit enrichment.
func WithClientMeta(ctx context.Context, ip, ua string) context.Context {
	return context.WithValue(ctx, clientMetaKey{}, ClientMeta{IP: ip, UserAgent: ua})
}

func clientMeta(ctx context.Context) ClientMeta {
	if v, ok := ctx.Value(clientMetaKey{}).(ClientMeta); ok {
		return v
	}
	return ClientMeta{}
}

// Audit writes an explicit audit event through audit.log (request context supplies actor and tenant).
func Audit(ctx context.Context, tx pgx.Tx, action, category, outcome, resourceType, resourceID, patientID, reason string) error {
	m := clientMeta(ctx)
	_, e := tx.Exec(ctx, `SELECT audit.log($1,$2,$3,NULLIF($4,''),NULLIF($5,'')::uuid,NULLIF($6,'')::uuid,NULLIF($7,''),NULLIF($8,'')::inet,NULLIF($9,''))`,
		action, category, outcome, resourceType, resourceID, patientID, reason, m.IP, m.UserAgent)
	return e
}

// AuditPreAuth records events that have no request context yet (login success/failure).
func AuditPreAuth(ctx context.Context, tx pgx.Tx, action, outcome, resourceID, org, user, reason, ip, userAgent string) error {
	_, e := tx.Exec(ctx, `SELECT audit.log($1,'authentication',$2,'session',NULLIF($3,'')::uuid,NULL,NULLIF($4,''),NULLIF($5,'')::inet,NULLIF($6,''),NULL,NULLIF($7,'')::uuid,NULLIF($8,'')::uuid,'staff')`, action, outcome, resourceID, reason, ip, userAgent, org, user)
	return e
}

// Idempotent runs create at most once per (org, user, operation, key); a replay returns the saved response.
func Idempotent[T any](ctx context.Context, tx pgx.Tx, s Session, op, key string, input any, create func() (T, error)) (T, error) {
	var zero T
	if len(key) < 8 || len(key) > 128 {
		return zero, Fail(422, "idempotency_key_required", "Idempotency-Key must be 8–128 characters")
	}
	body, e := json.Marshal(input)
	if e != nil {
		return zero, e
	}
	sum := sha256.Sum256(body)
	hash := hex.EncodeToString(sum[:])
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, s.OrgID+s.UserID+op+key); e != nil {
		return zero, e
	}
	var prior string
	var saved []byte
	e = tx.QueryRow(ctx, `SELECT request_hash,response FROM sec.idempotency WHERE org_id=$1::uuid AND user_id=$2::uuid AND operation=$3 AND key=$4`, s.OrgID, s.UserID, op, key).Scan(&prior, &saved)
	if e == nil {
		if prior != hash {
			return zero, Fail(409, "idempotency_conflict", "Key already used with a different request")
		}
		var out T
		return out, json.Unmarshal(saved, &out)
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return zero, e
	}
	out, e := create()
	if e != nil {
		return zero, e
	}
	resp, e := json.Marshal(out)
	if e != nil {
		return zero, e
	}
	_, e = tx.Exec(ctx, `INSERT INTO sec.idempotency(org_id,user_id,operation,key,request_hash,response) VALUES($1::uuid,$2::uuid,$3,$4,$5,$6::jsonb)`, s.OrgID, s.UserID, op, key, hash, resp)
	return out, e
}
