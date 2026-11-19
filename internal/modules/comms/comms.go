// Package comms manages notification templates, delivery logs, and transactional outbox enqueueing.
package comms

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"spartial.local/backend/internal/modules/identity"
	"spartial.local/backend/internal/platform"
)

type Service struct{ DB *pgxpool.Pool }
type Raw = platform.Raw

var (
	tplJ = platform.JSON("t", true)
	msgJ = platform.JSON("m", true)
	outJ = `(to_jsonb(o) - 'org_id')`
)

type TemplateInput struct {
	Code     string `json:"code" validate:"required,min=3,max=50"`
	Channel  string `json:"channel" validate:"required,oneof=sms whatsapp email push"`
	Language string `json:"language" validate:"required,oneof=sw en"`
	Body     string `json:"body" validate:"required,min=1,max=1000"`
}

type VersionInput struct {
	Version int `json:"version" validate:"required,min=1"`
}

type OutboxInput struct {
	EventType     string         `json:"event_type" validate:"required,min=3,max=100"`
	AggregateType string         `json:"aggregate_type" validate:"required,min=2,max=80"`
	AggregateID   string         `json:"aggregate_id" validate:"required,uuid"`
	DedupeKey     string         `json:"dedupe_key" validate:"max=160"`
	Payload       map[string]any `json:"payload"`
}

func (s Service) Templates(ctx context.Context, a identity.Actor, channel, request string) ([]Raw, error) {
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) ([]Raw, error) {
		return platform.Many(ctx, tx, `SELECT `+tplJ+` FROM comms.notification_template t
 WHERE ($1='' OR t.channel=$1) ORDER BY t.code, t.channel, t.language, t.version DESC LIMIT 200`, channel)
	})
}

func (s Service) CreateTemplate(ctx context.Context, a identity.Actor, in TemplateInput, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "comms.template.manage"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, sess platform.Session) (Raw, error) {
		var nextVer int
		_ = tx.QueryRow(ctx, `SELECT coalesce(max(version),0)+1 FROM comms.notification_template WHERE code=$1 AND channel=$2 AND language=$3`,
			strings.ToLower(strings.TrimSpace(in.Code)), in.Channel, in.Language).Scan(&nextVer)
		return platform.One(ctx, tx, `WITH t AS (INSERT INTO comms.notification_template(org_id,code,channel,language,body,version)
 VALUES($1::uuid,$2,$3,$4,$5,$6) RETURNING *) SELECT `+tplJ+` FROM t`,
			sess.OrgID, strings.ToLower(strings.TrimSpace(in.Code)), in.Channel, in.Language, in.Body, nextVer)
	})
}

func (s Service) ActivateTemplate(ctx context.Context, a identity.Actor, id string, in VersionInput, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "comms.template.manage"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, sess platform.Session) (Raw, error) {
		var code, ch, lang string
		if e := tx.QueryRow(ctx, `SELECT code,channel,language FROM comms.notification_template WHERE id=$1::uuid FOR UPDATE`, id).Scan(&code, &ch, &lang); e != nil {
			return nil, e
		}
		// Retire any currently active version for the same (code, channel, language).
		if _, e := tx.Exec(ctx, `UPDATE comms.notification_template SET status='retired' WHERE code=$1 AND channel=$2 AND language=$3 AND status='active' AND id<>$4::uuid`,
			code, ch, lang, id); e != nil {
			return nil, e
		}
		tag, e := tx.Exec(ctx, `UPDATE comms.notification_template SET status='active',approved_by=$3::uuid WHERE id=$1::uuid AND row_version=$2 AND status='draft'`,
			id, in.Version, sess.UserID)
		if e != nil {
			return nil, e
		}
		if e = platform.Changed(ctx, tx, tag, "comms.notification_template", id); e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, `SELECT `+tplJ+` FROM comms.notification_template t WHERE t.id=$1::uuid`, id)
	})
}

func (s Service) Messages(ctx context.Context, a identity.Actor, patientID, status, request string) ([]Raw, error) {
	if e := platform.Need(a.Permissions, "comms.read"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) ([]Raw, error) {
		return platform.Many(ctx, tx, `SELECT `+msgJ+` FROM comms.message m
 WHERE ($1='' OR m.patient_id=NULLIF($1,'')::uuid) AND ($2='' OR m.status=$2)
 ORDER BY m.created_at DESC LIMIT 100`, patientID, status)
	})
}

func (s Service) Outbox(ctx context.Context, a identity.Actor, status, request string) ([]Raw, error) {
	if e := platform.Need(a.Permissions, "comms.read"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) ([]Raw, error) {
		return platform.Many(ctx, tx, `SELECT `+outJ+` FROM comms.outbox o
 WHERE ($1='' OR o.status=$1) ORDER BY o.id DESC LIMIT 100`, status)
	})
}

func (s Service) EnqueueOutbox(ctx context.Context, a identity.Actor, in OutboxInput, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "comms.template.manage", "appointment.manage", "admin.org.manage"); e != nil {
		return nil, e
	}
	payload := []byte("{}")
	if in.Payload != nil {
		b, e := json.Marshal(in.Payload)
		if e != nil || len(b) >= 8192 {
			return nil, platform.Fail(422, "validation", "Payload must be a JSON object under 8 KB")
		}
		payload = b
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, sess platform.Session) (Raw, error) {
		ev := strings.TrimSpace(in.EventType)
		agg := strings.TrimSpace(in.AggregateType)
		dk := strings.TrimSpace(in.DedupeKey)
		if _, e := tx.Exec(ctx, `INSERT INTO comms.outbox(org_id,event_type,aggregate_type,aggregate_id,dedupe_key,payload)
 VALUES($1::uuid,$2,$3,$4::uuid,NULLIF($5,''),$6::jsonb)`,
			sess.OrgID, ev, agg, in.AggregateID, dk, payload); e != nil {
			return nil, e
		}
		out, e := json.Marshal(map[string]any{
			"event_type":     ev,
			"aggregate_type": agg,
			"aggregate_id":   in.AggregateID,
			"status":         "pending",
		})
		return out, e
	})
}
