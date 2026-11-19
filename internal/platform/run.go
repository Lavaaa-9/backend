package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Raw is a database-produced JSON document returned to the client as is.
type Raw = json.RawMessage

// Run executes fn in one request transaction (begin_request first, commit at the end).
func Run[T any](ctx context.Context, db *pgxpool.Pool, sessionID, request string, fn func(pgx.Tx, Session) (T, error)) (T, error) {
	var zero T
	tx, s, e := BeginRequest(ctx, db, sessionID, request)
	if e != nil {
		return zero, e
	}
	defer tx.Rollback(ctx)
	out, e := fn(tx, s)
	if e != nil {
		return zero, e
	}
	return out, tx.Commit(ctx)
}

// JSON renders a table alias as a client document: internal columns are dropped and
// row_version is exposed as "version" (pass hasVersion=false for tables without it).
func JSON(alias string, hasVersion bool, drop ...string) string {
	s := "to_jsonb(" + alias + ") - 'org_id' - 'created_by' - 'updated_by' - 'row_version'"
	for _, d := range drop {
		s += " - '" + d + "'"
	}
	if hasVersion {
		s += " || jsonb_build_object('version'," + alias + ".row_version)"
	}
	return "(" + s + ")"
}

// One runs a query returning a single jsonb column.
func One(ctx context.Context, tx pgx.Tx, sql string, args ...any) (Raw, error) {
	var out []byte
	if e := tx.QueryRow(ctx, sql, args...).Scan(&out); e != nil {
		return nil, e
	}
	return out, nil
}

// Many runs a query returning one jsonb column per row.
func Many(ctx context.Context, tx pgx.Tx, sql string, args ...any) ([]Raw, error) {
	rows, e := tx.Query(ctx, sql, args...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Raw{}
	for rows.Next() {
		var b []byte
		if e = rows.Scan(&b); e != nil {
			return nil, e
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// Changed turns a zero-row optimistic update into 404 (row missing or hidden) or 409 (stale version).
func Changed(ctx context.Context, tx pgx.Tx, tag pgconn.CommandTag, table, id string) error {
	if tag.RowsAffected() == 1 {
		return nil
	}
	var ok bool
	if e := tx.QueryRow(ctx, fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM %s WHERE id=$1::uuid)`, table), id).Scan(&ok); e != nil {
		return e
	}
	if !ok {
		return NotFound()
	}
	return Conflict()
}

// Encounter resolves the patient and facility of a visit inside the caller's facilities.
func Encounter(ctx context.Context, tx pgx.Tx, facilities []string, id string) (patient, facility string, err error) {
	err = tx.QueryRow(ctx, `SELECT patient_id::text,facility_id::text FROM clinical.encounter WHERE id=$1::uuid AND facility_id=ANY($2::uuid[])`, id, facilities).Scan(&patient, &facility)
	return
}

// Need returns 403 unless the actor holds at least one of the permissions.
func Need(perms []string, any ...string) error {
	for _, w := range any {
		for _, p := range perms {
			if p == w {
				return nil
			}
		}
	}
	return Forbidden()
}

// Reason trims and validates a mandatory free-text reason.
func Reason(v string, min int) (string, error) {
	v = strings.TrimSpace(v)
	if len(v) < min {
		return "", Fail(422, "validation", "A reason is required")
	}
	return v, nil
}

var _ = errors.New
