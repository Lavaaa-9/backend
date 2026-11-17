package privacy

import (
	"context"
	"github.com/jackc/pgx/v5"
)

func list(ctx context.Context, tx pgx.Tx, before int64) ([]Event, error) {
	rows, e := tx.Query(ctx, `SELECT id,actor_id::text,action,resource_id::text,patient_id::text,request_id::text,outcome,created_at FROM audit_events WHERE id<$1 ORDER BY id DESC LIMIT 100`, before)
	if e != nil {
		return nil, e
	}
	out := []Event{}
	for rows.Next() {
		var v Event
		if e = rows.Scan(&v.ID, &v.ActorID, &v.Action, &v.ResourceID, &v.PatientID, &v.RequestID, &v.Outcome, &v.CreatedAt); e != nil {
			rows.Close()
			return nil, e
		}
		out = append(out, v)
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		return nil, e
	}
	return out, nil
}
