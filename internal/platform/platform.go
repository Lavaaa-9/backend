package platform

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"spartial.local/backend/db"
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
		case "23505", "40001", "40P01":
			return Conflict()
		case "23503", "23514", "22P02":
			return Fail(422, "invalid_reference", "Invalid value or reference")
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
func Migrate(ctx context.Context, url string) error {
	conn, err := sql.Open("pgx", url)
	if err != nil {
		return err
	}
	defer conn.Close()
	goose.SetBaseFS(db.Migrations)
	if err = goose.SetDialect("postgres"); err != nil {
		return err
	}
	return goose.UpContext(ctx, conn, "migrations")
}
func Audit(ctx context.Context, tx pgx.Tx, actor, action, resource, patient, request, outcome string) error {
	_, err := tx.Exec(ctx, `INSERT INTO audit_events(actor_id,action,resource_id,patient_id,request_id,outcome) VALUES(NULLIF($1,'')::uuid,$2,NULLIF($3,'')::uuid,NULLIF($4,'')::uuid,$5::uuid,$6)`, actor, action, resource, patient, request, outcome)
	return err
}

const SchemaVersion = 2

func CheckSchema(ctx context.Context, db *pgxpool.Pool) error {
	var version int
	if e := db.QueryRow(ctx, `SELECT COALESCE(max(version_id),0) FROM goose_db_version WHERE is_applied`).Scan(&version); e != nil || version != SchemaVersion {
		return Fail(503, "not_ready", "Database schema version does not match this API")
	}
	return nil
}
func CheckRuntimeRole(ctx context.Context, db *pgxpool.Pool) error {
	var unsafe bool
	e := db.QueryRow(ctx, `SELECT rolsuper OR rolcreaterole OR rolcreatedb OR has_table_privilege(current_user,'audit_events','UPDATE,DELETE,TRUNCATE') FROM pg_roles WHERE rolname=current_user`).Scan(&unsafe)
	if e != nil {
		return e
	}
	if unsafe {
		return fmt.Errorf("API requires a restricted runtime database role; run manage grant-runtime and use DATABASE_URL")
	}
	return nil
}
