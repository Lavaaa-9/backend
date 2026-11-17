package privacy

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"spartial.local/backend/internal/modules/identity"
	"spartial.local/backend/internal/platform"
	"time"
)

type Alert struct {
	ID          string    `json:"id"`
	ActorID     string    `json:"actor_id"`
	Rule        string    `json:"rule"`
	WindowStart time.Time `json:"window_start"`
	WindowEnd   time.Time `json:"window_end"`
	Count       int       `json:"event_count"`
}

// DetectAlerts uses explicit operational thresholds. Zero disables a rule; no clinical thresholds exist.
func DetectAlerts(ctx context.Context, db *pgxpool.Pool, readLimit, mfaLimit int) error {
	if readLimit <= 0 && mfaLimit <= 0 {
		return nil
	}
	_, e := db.Exec(ctx, `WITH counted AS (
 SELECT actor_id,CASE WHEN outcome='success' AND action IN ('patients.read','patients.search_hit') THEN 'patient_read_volume' ELSE 'mfa_denial_volume' END AS rule,
 date_bin(interval '15 minutes',created_at,timestamptz '2000-01-01 00:00:00+00') AS started,count(*)::integer AS n
 FROM audit_events WHERE actor_id IS NOT NULL AND created_at>=now()-interval '30 minutes'
 AND ((outcome='success' AND action IN ('patients.read','patients.search_hit')) OR (outcome='denied' AND reason='mfa_required'))
 GROUP BY actor_id,rule,started)
 INSERT INTO privacy_alerts(actor_id,rule,window_start,window_end,event_count)
 SELECT actor_id,rule,started,started+interval '15 minutes',n FROM counted
 WHERE (rule='patient_read_volume' AND $1>0 AND n>=$1) OR (rule='mfa_denial_volume' AND $2>0 AND n>=$2)
 ON CONFLICT(actor_id,rule,window_start) DO UPDATE SET event_count=GREATEST(privacy_alerts.event_count,EXCLUDED.event_count)`, readLimit, mfaLimit)
	return e
}
func (s Service) Alerts(ctx context.Context, a identity.Actor, after, request string, limit int) ([]Alert, error) {
	if !a.Can("privacy.report") {
		return nil, platform.Forbidden()
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	rows, e := tx.Query(ctx, `SELECT id::text,actor_id::text,rule,window_start,window_end,event_count FROM privacy_alerts WHERE ($1='' OR id>NULLIF($1,'')::uuid) ORDER BY id LIMIT $2`, after, limit)
	if e != nil {
		return nil, e
	}
	out := []Alert{}
	for rows.Next() {
		var v Alert
		if e = rows.Scan(&v.ID, &v.ActorID, &v.Rule, &v.WindowStart, &v.WindowEnd, &v.Count); e != nil {
			rows.Close()
			return nil, e
		}
		out = append(out, v)
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		return nil, e
	}
	if e = platform.Audit(ctx, tx, a.ID, "privacy.alerts.read", "", "", request, "success"); e != nil {
		return nil, e
	}
	return out, tx.Commit(ctx)
}
func (s Service) PatientAccess(ctx context.Context, a identity.Actor, patient, request string, before int64) ([]Event, error) {
	if !a.Can("privacy.report") {
		return nil, platform.Forbidden()
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	rows, e := tx.Query(ctx, `SELECT id,actor_id::text,action,resource_id::text,patient_id::text,request_id::text,outcome,created_at,reason,facility_id::text FROM audit_events WHERE (patient_id=$1::uuid OR resource_id=$1::uuid) AND id<$2 ORDER BY id DESC LIMIT 100`, patient, before)
	if e != nil {
		return nil, e
	}
	out := []Event{}
	for rows.Next() {
		var v Event
		if e = rows.Scan(&v.ID, &v.ActorID, &v.Action, &v.ResourceID, &v.PatientID, &v.RequestID, &v.Outcome, &v.CreatedAt, &v.Reason, &v.FacilityID); e != nil {
			rows.Close()
			return nil, e
		}
		out = append(out, v)
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		return nil, e
	}
	if e = platform.Audit(ctx, tx, a.ID, "privacy.patient_access.read", patient, "", request, "success"); e != nil {
		return nil, e
	}
	return out, tx.Commit(ctx)
}
