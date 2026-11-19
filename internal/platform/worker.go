package platform

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNoJob = pgx.ErrNoRows

type OutboxJob struct {
	ID            int64
	OrgID         string
	EventType     string
	AggregateType string
	AggregateID   string
	Attempts      int
	MaxAttempts   int
	WorkerID      string
}

func ReapStaleOutbox(ctx context.Context, db *pgxpool.Pool) (int64, error) {
	tag, e := db.Exec(ctx, `UPDATE comms.outbox
 SET status=CASE WHEN attempts>=max_attempts THEN 'dead' ELSE 'failed' END,
     locked_by=NULL, locked_at=NULL, last_error='worker_lease_expired',
     available_at=now()+interval '5 seconds'
 WHERE status='processing' AND locked_at < now() - interval '2 minutes'`)
	if e != nil {
		return 0, e
	}
	return tag.RowsAffected(), nil
}

func ClaimOutbox(ctx context.Context, db *pgxpool.Pool, workerID string) (OutboxJob, error) {
	var j OutboxJob
	j.WorkerID = workerID
	e := db.QueryRow(ctx, `WITH candidate AS (
 SELECT id FROM comms.outbox
 WHERE status IN ('pending','failed') AND available_at <= now() AND attempts < max_attempts
 ORDER BY available_at, id FOR UPDATE SKIP LOCKED LIMIT 1
)
UPDATE comms.outbox o
 SET status='processing', locked_by=$1, locked_at=now(), attempts=o.attempts+1
FROM candidate c WHERE o.id=c.id
RETURNING o.id, coalesce(o.org_id::text,''), o.event_type, o.aggregate_type, o.aggregate_id::text, o.attempts, o.max_attempts`, workerID).
		Scan(&j.ID, &j.OrgID, &j.EventType, &j.AggregateType, &j.AggregateID, &j.Attempts, &j.MaxAttempts)
	return j, e
}

func CompleteOutbox(ctx context.Context, db *pgxpool.Pool, j OutboxJob) error {
	tag, e := db.Exec(ctx, `UPDATE comms.outbox
 SET status='succeeded', processed_at=now(), locked_by=NULL, locked_at=NULL, last_error=NULL
 WHERE id=$1 AND locked_by=$2 AND status='processing'`, j.ID, j.WorkerID)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return Conflict()
	}
	return nil
}

func FailOutbox(ctx context.Context, db *pgxpool.Pool, j OutboxJob, reason string) error {
	backoff := int64(1 << min(j.Attempts, 10))
	_, e := db.Exec(ctx, `UPDATE comms.outbox
 SET status=CASE WHEN attempts>=max_attempts THEN 'dead' ELSE 'failed' END,
     available_at=now()+($3 * interval '1 second'),
     locked_by=NULL, locked_at=NULL, last_error=left($4, 1000)
 WHERE id=$1 AND locked_by=$2 AND status='processing'`, j.ID, j.WorkerID, backoff, reason)
	return e
}

// SealAuditPending chains any completed transactions' audit events into a new immutable seal.
func SealAuditPending(ctx context.Context, db *pgxpool.Pool) (sealNo int64, rowCount int64, sealed bool, err error) {
	var sn, rc *int64
	err = db.QueryRow(ctx, `SELECT seal_no, row_count FROM audit.seal_pending()`).Scan(&sn, &rc)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, 0, false, nil
	}
	if err != nil {
		return 0, 0, false, err
	}
	if sn == nil {
		return 0, 0, false, nil
	}
	return *sn, *rc, true, nil
}

// DetectPrivacyAlerts runs the database privacy anomaly detector and returns the number of new alerts created.
func DetectPrivacyAlerts(ctx context.Context, db *pgxpool.Pool, readThreshold, mfaThreshold int) (int, error) {
	var n int
	e := db.QueryRow(ctx, `SELECT gov.detect_privacy_alerts($1,$2)`, readThreshold, mfaThreshold).Scan(&n)
	return n, e
}
