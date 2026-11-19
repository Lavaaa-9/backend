package audit

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"spartial.local/backend/internal/modules/identity"
	"spartial.local/backend/internal/platform"
)

type Event struct {
	ID         int64     `json:"id"`
	ActorID    *string   `json:"actor_id"`
	Action     string    `json:"action"`
	ResourceID *string   `json:"resource_id"`
	PatientID  *string   `json:"patient_id"`
	RequestID  *string   `json:"request_id"`
	Reason     *string   `json:"reason"`
	FacilityID *string   `json:"facility_id"`
	Outcome    string    `json:"outcome"`
	CreatedAt  time.Time `json:"created_at"`
}
type Service struct{ DB *pgxpool.Pool }

// List returns audit metadata only (no change payloads), newest first.
func (s Service) List(ctx context.Context, a identity.Actor, before int64, request string) ([]Event, error) {
	if !a.Can("audit.read") {
		return nil, platform.Forbidden()
	}
	tx, _, e := platform.BeginRequest(ctx, s.DB, a.SessionID, request)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	rows, e := tx.Query(ctx, `SELECT id,actor_user_id::text,action,resource_id::text,patient_id::text,request_id::text,reason,outcome,occurred_at FROM audit.event WHERE id<$1 ORDER BY id DESC LIMIT 50`, before)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var v Event
		if e = rows.Scan(&v.ID, &v.ActorID, &v.Action, &v.ResourceID, &v.PatientID, &v.RequestID, &v.Reason, &v.Outcome, &v.CreatedAt); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	if e = rows.Err(); e != nil {
		return nil, e
	}
	rows.Close()
	if e = platform.Audit(ctx, tx, "audit.log.view", "security", "success", "audit_log", "", "", ""); e != nil {
		return nil, e
	}
	return out, tx.Commit(ctx)
}
