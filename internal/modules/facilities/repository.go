package facilities

import (
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func list(ctx context.Context, db *pgxpool.Pool, all bool, ids []string) ([]Facility, error) {
	rows, e := db.Query(ctx, `SELECT id::text,name,timezone,active FROM facilities WHERE ($1 OR id=ANY($2::uuid[])) ORDER BY id LIMIT 100`, all, ids)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Facility{}
	for rows.Next() {
		var f Facility
		if e = rows.Scan(&f.ID, &f.Name, &f.Timezone, &f.Active); e != nil {
			return nil, e
		}
		out = append(out, f)
	}
	return out, rows.Err()
}
func create(ctx context.Context, tx pgx.Tx, in Input) (Facility, error) {
	var f Facility
	e := tx.QueryRow(ctx, `INSERT INTO facilities(name,timezone) VALUES($1,$2) RETURNING id::text,name,timezone,active`, in.Name, in.Timezone).Scan(&f.ID, &f.Name, &f.Timezone, &f.Active)
	return f, e
}
