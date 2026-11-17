package platform

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Seal is the result of one sealing pass. ChainHash must be shipped off-host
// (structured log to a SIEM or WORM storage) so that database-owner tampering is detectable.
type Seal struct {
	Count       int
	LastEventID int64
	ChainHash   string
}

// SealAudit chains audit events older than settle into a new immutable seal.
func SealAudit(ctx context.Context, db *pgxpool.Pool, settle time.Duration) (Seal, error) {
	var s Seal
	err := db.QueryRow(ctx, `SELECT sealed_count,last_event_id,chain_hash FROM seal_audit_events(make_interval(secs => $1))`, settle.Seconds()).Scan(&s.Count, &s.LastEventID, &s.ChainHash)
	return s, err
}

// ChainReport is the outcome of recomputing the audit chain.
type ChainReport struct {
	Seals, Events int64
	Unsealed      int64
	BrokenSeal    *int64
}

func (r ChainReport) Intact() bool { return r.BrokenSeal == nil }

func VerifyAuditChain(ctx context.Context, db *pgxpool.Pool) (ChainReport, error) {
	var r ChainReport
	err := db.QueryRow(ctx, `SELECT seals_checked,events_checked,first_broken_seal,unsealed_events FROM verify_audit_chain()`).Scan(&r.Seals, &r.Events, &r.BrokenSeal, &r.Unsealed)
	return r, err
}
