package platform

import (
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// BeginActor binds actor context only for this transaction; pooled connections never retain it.
func BeginActor(ctx context.Context, db *pgxpool.Pool, actor string, clinical bool) (pgx.Tx, error) {
	tx, e := db.Begin(ctx)
	if e != nil {
		return nil, e
	}
	if e = SetActor(ctx, tx, actor, clinical); e != nil {
		tx.Rollback(ctx)
		return nil, e
	}
	return tx, nil
}
func SetActor(ctx context.Context, tx pgx.Tx, actor string, clinical bool) error {
	setting := "false"
	if clinical {
		setting = "true"
	}
	_, e := tx.Exec(ctx, `SELECT set_config('app.actor_id',$1,true),set_config('app.care_policy_enabled',$2,true)`, actor, setting)
	return e
}
