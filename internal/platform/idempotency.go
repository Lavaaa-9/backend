package platform

import (
	"context"
	"github.com/jackc/pgx/v5"
)

func LockIdempotency(ctx context.Context, tx pgx.Tx, actor, operation, key string) error {
	_, e := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, actor+":"+operation+":"+key)
	return e
}
func ReadIdempotency(ctx context.Context, tx pgx.Tx, actor, operation, key string) (hash string, response []byte, err error) {
	err = tx.QueryRow(ctx, `SELECT request_hash,response FROM idempotency_records WHERE actor_id=$1::uuid AND operation=$2 AND key=$3`, actor, operation, key).Scan(&hash, &response)
	return
}
func WriteIdempotency(ctx context.Context, tx pgx.Tx, actor, operation, key, hash string, response []byte) error {
	_, e := tx.Exec(ctx, `INSERT INTO idempotency_records(actor_id,operation,key,request_hash,response) VALUES($1::uuid,$2,$3,$4,$5::jsonb)`, actor, operation, key, hash, string(response))
	return e
}
func Enqueue(ctx context.Context, tx pgx.Tx, event, aggregate string) error {
	_, e := tx.Exec(ctx, `INSERT INTO outbox(event_type,aggregate_id) VALUES($1,$2::uuid)`, event, aggregate)
	return e
}
