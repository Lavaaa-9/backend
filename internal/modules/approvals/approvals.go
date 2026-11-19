// Package approvals implements dual-control (four-eyes) approval requests backed by gov.approval_request.
package approvals

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

var appJ = platform.JSON("a", true)

type Input struct {
	FacilityID   *string        `json:"facility_id" validate:"omitempty,uuid"`
	ActionType   string         `json:"action_type" validate:"required,oneof=patient_merge document_delete billing_adjustment role_grant data_export other"`
	ResourceType string         `json:"resource_type" validate:"required,min=2,max=80"`
	ResourceID   string         `json:"resource_id" validate:"required,uuid"`
	Reason       string         `json:"reason" validate:"required,min=10,max=1000"`
	Payload      map[string]any `json:"payload"`
}

type Decision struct {
	Note    string `json:"note" validate:"max=1000"`
	Version int    `json:"version" validate:"required,min=1"`
}

func (s Service) Create(ctx context.Context, a identity.Actor, in Input, key, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "approval.request"); e != nil {
		return nil, e
	}
	reason, e := platform.Reason(in.Reason, 10)
	if e != nil {
		return nil, platform.Fail(422, "validation", "Approval reason must be at least 10 characters")
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
		create := func() (Raw, error) {
			var id string
			e := tx.QueryRow(ctx, `INSERT INTO gov.approval_request(org_id,facility_id,action_type,resource_type,resource_id,reason,payload)
 VALUES($1::uuid,$2::uuid,$3,$4,$5::uuid,$6,$7::jsonb) RETURNING id::text`,
				sess.OrgID, in.FacilityID, in.ActionType, strings.TrimSpace(in.ResourceType), in.ResourceID, reason, payload).Scan(&id)
			if e != nil {
				return nil, e
			}
			if e = platform.Audit(ctx, tx, "approval.request", "administration", "success", "approval_request", id, "", in.ActionType); e != nil {
				return nil, e
			}
			return platform.One(ctx, tx, `SELECT `+appJ+` FROM gov.approval_request a WHERE a.id=$1::uuid`, id)
		}
		if key != "" {
			return platform.Idempotent(ctx, tx, sess, "approval.create", key, in, create)
		}
		return create()
	})
}

func (s Service) List(ctx context.Context, a identity.Actor, status, after, request string, limit int) ([]Raw, error) {
	if e := platform.Need(a.Permissions, "approval.review", "approval.request", "audit.read"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, sess platform.Session) ([]Raw, error) {
		all := a.Can("approval.review") || a.Can("audit.read")
		return platform.Many(ctx, tx, `SELECT `+appJ+` FROM gov.approval_request a
 WHERE ($1 OR a.requested_by=$2::uuid)
   AND ($3='' OR a.status=$3)
   AND ($4='' OR a.id>NULLIF($4,'')::uuid)
 ORDER BY a.created_at DESC, a.id LIMIT $5`, all, sess.UserID, status, after, limit)
	})
}

func (s Service) Decide(ctx context.Context, a identity.Actor, id string, in Decision, approve bool, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "approval.review"); e != nil {
		return nil, e
	}
	target := "rejected"
	if approve {
		target = "approved"
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, sess platform.Session) (Raw, error) {
		tag, e := tx.Exec(ctx, `UPDATE gov.approval_request SET status=$3,decided_by=$4::uuid,decided_at=now(),decision_note=NULLIF($5,'')
 WHERE id=$1::uuid AND row_version=$2 AND status='pending'`, id, in.Version, target, sess.UserID, strings.TrimSpace(in.Note))
		if e != nil {
			return nil, e
		}
		if e = platform.Changed(ctx, tx, tag, "gov.approval_request", id); e != nil {
			return nil, e
		}
		if e = platform.Audit(ctx, tx, "approval."+target, "administration", "success", "approval_request", id, "", strings.TrimSpace(in.Note)); e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, `SELECT `+appJ+` FROM gov.approval_request a WHERE a.id=$1::uuid`, id)
	})
}
