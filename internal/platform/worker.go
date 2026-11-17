package platform

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Job struct {
	ID, Type, Lease string
	Attempts        int
}

func ClaimJob(ctx context.Context, db *pgxpool.Pool) (Job, error) {
	var j Job
	j.Lease = uuid.NewString()
	e := db.QueryRow(ctx, `WITH candidate AS (SELECT id FROM outbox WHERE ((status='pending' AND available_at<=now()) OR (status='processing' AND lease_until<now())) AND attempts<5 ORDER BY created_at,id FOR UPDATE SKIP LOCKED LIMIT 1)
 UPDATE outbox o SET status='processing',lease_until=now()+interval '30 seconds',lease_token=$1::uuid,attempts=attempts+1 FROM candidate c WHERE o.id=c.id RETURNING o.id::text,o.event_type,o.attempts`, j.Lease).Scan(&j.ID, &j.Type, &j.Attempts)
	if e != nil {
		return j, e
	}
	return j, nil
}

// CompleteDevelopmentJob atomically records a sink observation and acknowledges the lease.
func CompleteDevelopmentJob(ctx context.Context, db *pgxpool.Pool, j Job) error {
	tx, e := db.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var id string
	e = tx.QueryRow(ctx, `SELECT id::text FROM outbox WHERE id=$1::uuid AND lease_token=$2::uuid AND status='processing' AND lease_until>now() FOR UPDATE`, j.ID, j.Lease).Scan(&id)
	if e != nil {
		return e
	}
	if j.Type != "patient.registered" {
		return Fail(422, "unsupported_event", "No configured consumer")
	}
	if _, e = tx.Exec(ctx, `INSERT INTO development_event_sink(event_id,event_type) VALUES($1::uuid,$2) ON CONFLICT DO NOTHING`, id, j.Type); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `UPDATE outbox SET status='completed',lease_until=NULL,lease_token=NULL,last_error=NULL WHERE id=$1::uuid`, id); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func FailJob(ctx context.Context, db *pgxpool.Pool, j Job) error {
	_, e := db.Exec(ctx, `UPDATE outbox SET status=CASE WHEN attempts>=5 THEN 'failed' ELSE 'pending' END,available_at=now()+($3*interval '1 second'),lease_until=NULL,lease_token=NULL,last_error='consumer_failed' WHERE id=$1::uuid AND lease_token=$2::uuid AND status='processing'`, j.ID, j.Lease, int64(1<<j.Attempts))
	return e
}
func ReapExhaustedJobs(ctx context.Context, db *pgxpool.Pool) error {
	_, e := db.Exec(ctx, `UPDATE outbox SET status='failed',last_error='lease_expired_at_retry_limit',lease_until=NULL,lease_token=NULL WHERE status='processing' AND lease_until<now() AND attempts>=5`)
	return e
}

var ErrNoJob = pgx.ErrNoRows
