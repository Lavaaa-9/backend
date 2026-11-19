package audit

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"spartial.local/backend/internal/modules/identity"
	"spartial.local/backend/internal/platform"
)

type Event struct {
	ID           int64     `json:"id"`
	ActorID      *string   `json:"actor_id"`
	Action       string    `json:"action"`
	Category     string    `json:"category,omitempty"`
	ResourceType *string   `json:"resource_type,omitempty"`
	ResourceID   *string   `json:"resource_id"`
	PatientID    *string   `json:"patient_id"`
	RequestID    *string   `json:"request_id"`
	Reason       *string   `json:"reason"`
	FacilityID   *string   `json:"facility_id"`
	SourceIP     *string   `json:"source_ip,omitempty"`
	UserAgent    *string   `json:"user_agent,omitempty"`
	Outcome      string    `json:"outcome"`
	CreatedAt    time.Time `json:"created_at"`
}

type Filter struct {
	ActorID   string
	PatientID string
	Action    string
	Outcome   string
	From      *time.Time
	To        *time.Time
	Before    int64
	Limit     int
}

type Service struct{ DB *pgxpool.Pool }

func (s Service) query(ctx context.Context, a identity.Actor, f Filter, maxLimit int, auditAction, request string) ([]Event, error) {
	if !a.Can("audit.read") {
		return nil, platform.Forbidden()
	}
	if f.Before <= 0 {
		f.Before = 9223372036854775807
	}
	if f.Limit <= 0 || f.Limit > maxLimit {
		f.Limit = maxLimit
	}
	f.Action = strings.TrimSpace(f.Action)
	tx, _, e := platform.BeginRequest(ctx, s.DB, a.SessionID, request)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	rows, e := tx.Query(ctx, `SELECT id,actor_user_id::text,action,category,resource_type,resource_id::text,patient_id::text,request_id::text,reason,host(source_ip),user_agent,outcome,occurred_at
 FROM audit.event
 WHERE id<$1
   AND ($2='' OR actor_user_id=NULLIF($2,'')::uuid)
   AND ($3='' OR patient_id=NULLIF($3,'')::uuid)
   AND ($4='' OR action=$4 OR starts_with(action,$4 || '.'))
   AND ($5='' OR outcome=$5)
   AND ($6::timestamptz IS NULL OR occurred_at>=$6::timestamptz)
   AND ($7::timestamptz IS NULL OR occurred_at<=$7::timestamptz)
 ORDER BY id DESC LIMIT $8`,
		f.Before, f.ActorID, f.PatientID, f.Action, f.Outcome, f.From, f.To, f.Limit)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var v Event
		if e = rows.Scan(&v.ID, &v.ActorID, &v.Action, &v.Category, &v.ResourceType, &v.ResourceID, &v.PatientID, &v.RequestID, &v.Reason, &v.SourceIP, &v.UserAgent, &v.Outcome, &v.CreatedAt); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	if e = rows.Err(); e != nil {
		return nil, e
	}
	rows.Close()
	if e = platform.Audit(ctx, tx, auditAction, "security", "success", "audit_log", "", "", ""); e != nil {
		return nil, e
	}
	return out, tx.Commit(ctx)
}

// List returns audit metadata only (no change payloads), newest first.
func (s Service) List(ctx context.Context, a identity.Actor, f Filter, request string) ([]Event, error) {
	if f.Limit <= 0 {
		f.Limit = 50
	}
	return s.query(ctx, a, f, 100, "audit.log.view", request)
}

// Export returns up to 1000 audit metadata events for compliance review, auditing the export.
func (s Service) Export(ctx context.Context, a identity.Actor, f Filter, request string) ([]Event, error) {
	if f.Limit <= 0 {
		f.Limit = 500
	}
	return s.query(ctx, a, f, 1000, "audit.log.export", request)
}
