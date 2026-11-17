package approvals

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"spartial.local/backend/internal/modules/identity"
	"spartial.local/backend/internal/platform"
	"strings"
	"time"
)

type Service struct{ DB *pgxpool.Pool }
type Input struct {
	Kind      string `json:"kind" validate:"required,oneof=admin_role_grant mfa_reset"`
	SubjectID string `json:"subject_id" validate:"required,uuid"`
	Reason    string `json:"reason" validate:"required,min=20,max=1000"`
}
type Decision struct {
	Note string `json:"note" validate:"required,min=20,max=1000"`
}
type Request struct {
	ID             string     `json:"id"`
	Kind           string     `json:"kind"`
	SubjectID      string     `json:"subject_id"`
	SubjectVersion int        `json:"subject_version"`
	RequestedBy    string     `json:"requested_by"`
	Reason         string     `json:"reason"`
	ExpiresAt      time.Time  `json:"expires_at"`
	CreatedAt      time.Time  `json:"created_at"`
	Status         string     `json:"status"`
	ReviewedBy     *string    `json:"reviewed_by"`
	ReviewedAt     *time.Time `json:"reviewed_at"`
	ReviewNote     *string    `json:"review_note"`
}

func authorize(ctx context.Context, tx pgx.Tx, a identity.Actor) error {
	ok, e := liveAdministrator(ctx, tx, a.ID, a.SessionID)
	if e != nil {
		return e
	}
	if !ok {
		return platform.Fail(403, "mfa_required", "An active administrator session verified with MFA is required")
	}
	return nil
}
func (s Service) Create(ctx context.Context, a identity.Actor, in Input, key, request string) (Request, error) {
	if !a.Can("approvals.request") {
		return Request{}, platform.Forbidden()
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if len(in.Reason) < 20 {
		return Request{}, platform.Fail(422, "validation", "Approval requires a reason of at least 20 characters")
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return Request{}, e
	}
	defer tx.Rollback(ctx)
	if e = platform.LockStaffAdministration(ctx, tx); e != nil {
		return Request{}, e
	}
	if e = authorize(ctx, tx, a); e != nil {
		return Request{}, e
	}
	command, e := platform.BeginCommand(ctx, tx, a.ID, "approval.request", key, in)
	if e != nil {
		return Request{}, e
	}
	if command.Replay != nil {
		var prior Request
		e = json.Unmarshal(command.Replay, &prior)
		return prior, e
	}
	version, role, active, mfa, e := target(ctx, tx, in.SubjectID)
	if e != nil {
		return Request{}, e
	}
	if !active {
		return Request{}, platform.Fail(409, "inactive_subject", "Subject account must be active")
	}
	if (in.Kind == "admin_role_grant" && role == "administrator") || (in.Kind == "mfa_reset" && !mfa) {
		return Request{}, platform.Conflict()
	}
	result, e := create(ctx, tx, a.ID, in, version)
	if e != nil {
		return Request{}, e
	}
	if e = platform.AuditReason(ctx, tx, a.ID, "approval.request", result.ID, "", request, "success", in.Kind); e != nil {
		return Request{}, e
	}
	if e = command.Save(ctx, tx, result); e != nil {
		return Request{}, e
	}
	return result, tx.Commit(ctx)
}
func (s Service) Decide(ctx context.Context, a identity.Actor, id string, in Decision, approve bool, request string) (Request, error) {
	if !a.Can("approvals.review") {
		return Request{}, platform.Forbidden()
	}
	in.Note = strings.TrimSpace(in.Note)
	if len(in.Note) < 20 {
		return Request{}, platform.Fail(422, "validation", "A review note is required")
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return Request{}, e
	}
	defer tx.Rollback(ctx)
	if e = platform.LockStaffAdministration(ctx, tx); e != nil {
		return Request{}, e
	}
	if e = authorize(ctx, tx, a); e != nil {
		return Request{}, e
	}
	old, e := lockRequest(ctx, tx, id)
	if e != nil {
		return Request{}, e
	}
	if old.RequestedBy == a.ID {
		if e = platform.AuditReason(ctx, tx, a.ID, "approval.review", id, "", request, "denied", "self_approval"); e != nil {
			return Request{}, e
		}
		if e = tx.Commit(ctx); e != nil {
			return Request{}, e
		}
		return Request{}, platform.Forbidden()
	}
	decision := "rejected"
	if approve {
		decision = "approved"
	}
	if old.Status == decision {
		return old, nil
	}
	if old.Status != "pending" {
		return Request{}, platform.Conflict()
	}
	if !old.ExpiresAt.After(time.Now()) {
		if e = expire(ctx, tx, id); e != nil {
			return Request{}, e
		}
		if e = platform.AuditReason(ctx, tx, a.ID, "approval.expired", id, "", request, "denied", "expired"); e != nil {
			return Request{}, e
		}
		if e = tx.Commit(ctx); e != nil {
			return Request{}, e
		}
		return Request{}, platform.Fail(409, "approval_expired", "Approval request expired")
	}
	eligible, e := requesterStillEligible(ctx, tx, old.RequestedBy)
	if e != nil {
		return Request{}, e
	}
	if !eligible {
		return Request{}, platform.Fail(409, "requester_changed", "Requester is no longer an eligible administrator")
	}
	if approve {
		version, _, active, _, e := target(ctx, tx, old.SubjectID)
		if e != nil {
			return Request{}, e
		}
		if version != old.SubjectVersion || !active {
			return Request{}, platform.Conflict()
		}
		if e = apply(ctx, tx, old); e != nil {
			return Request{}, e
		}
	}
	result, e := decide(ctx, tx, id, a.ID, decision, in.Note)
	if e != nil {
		return Request{}, e
	}
	if e = platform.AuditReason(ctx, tx, a.ID, "approval.review", id, "", request, "success", decision+":"+old.Kind); e != nil {
		return Request{}, e
	}
	return result, tx.Commit(ctx)
}
func (s Service) List(ctx context.Context, a identity.Actor, after, request string, limit int) ([]Request, error) {
	if !a.Can("approvals.review") {
		return nil, platform.Forbidden()
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	if e = authorize(ctx, tx, a); e != nil {
		return nil, e
	}
	out, e := list(ctx, tx, after, limit)
	if e != nil {
		return nil, e
	}
	if e = platform.Audit(ctx, tx, a.ID, "approval.list", "", "", request, "success"); e != nil {
		return nil, e
	}
	return out, tx.Commit(ctx)
}
