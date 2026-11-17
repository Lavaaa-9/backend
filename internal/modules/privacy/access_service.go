package privacy

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"spartial.local/backend/internal/modules/identity"
	"spartial.local/backend/internal/platform"
	"spartial.local/backend/internal/security"
	"strings"
	"time"
)

type Policy struct {
	DB      *pgxpool.Pool
	Enabled bool
}

// CanAccessPatient is also enforced independently by PostgreSQL patient row policies.
func (p Policy) CanAccessPatient(ctx context.Context, tx pgx.Tx, a identity.Actor, id string, write bool) error {
	facility, e := (accessRepository{tx}).facility(ctx, a.ID, id)
	if errors.Is(e, pgx.ErrNoRows) {
		return denyPatient(ctx, tx, a.ID, id, "", "patient_scope_denied")
	}
	if e != nil {
		return e
	}
	ok, e := (accessRepository{tx}).allowed(ctx, a.ID, id, facility, write)
	if e != nil {
		return e
	}
	if !ok {
		return denyPatient(ctx, tx, a.ID, id, id, "care_relationship_required")
	}
	return nil
}
func denyPatient(ctx context.Context, tx pgx.Tx, actor, id, patient, reason string) error {
	if e := platform.AuditReason(ctx, tx, actor, "patients.access", id, patient, requestID(ctx), "denied", reason); e != nil {
		return e
	}
	if e := tx.Commit(ctx); e != nil {
		return e
	}
	return platform.NotFound()
}

// Request IDs are explicitly attached by services, never obtained from request-backed Fiber memory.
type requestKey struct{}

func WithRequest(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestKey{}, id)
}
func requestID(ctx context.Context) string { v, _ := ctx.Value(requestKey{}).(string); return v }

type AssignmentInput struct {
	StaffID    string    `json:"staff_id" validate:"required,uuid"`
	Reason     string    `json:"reason" validate:"required,min=20,max=1000"`
	ValidUntil time.Time `json:"valid_until" validate:"required"`
}
type Assignment struct {
	ID         string     `json:"id"`
	PatientID  string     `json:"patient_id"`
	FacilityID string     `json:"facility_id"`
	StaffID    string     `json:"staff_id"`
	AssignedBy string     `json:"assigned_by"`
	Reason     string     `json:"reason"`
	ValidFrom  time.Time  `json:"valid_from"`
	ValidUntil time.Time  `json:"valid_until"`
	RevokedAt  *time.Time `json:"revoked_at"`
}
type BreakGlassInput struct {
	Reason          string `json:"reason" validate:"required,min=20,max=1000"`
	DurationMinutes int    `json:"duration_minutes" validate:"required,min=1,max=240"`
}
type Grant struct {
	ID         string     `json:"id"`
	PatientID  string     `json:"patient_id"`
	FacilityID string     `json:"facility_id"`
	StaffID    string     `json:"staff_id"`
	Reason     string     `json:"reason"`
	GrantedAt  time.Time  `json:"granted_at"`
	ExpiresAt  time.Time  `json:"expires_at"`
	ReviewedBy *string    `json:"reviewed_by"`
	ReviewedAt *time.Time `json:"reviewed_at"`
	Outcome    *string    `json:"review_outcome"`
	ReviewNote *string    `json:"review_note"`
}
type ReviewInput struct {
	Outcome string `json:"outcome" validate:"required,oneof=appropriate inappropriate"`
	Note    string `json:"note" validate:"required,min=20,max=1000"`
}

func (p Policy) enabled() error {
	if !p.Enabled {
		return platform.Fail(503, "care_policy_not_enabled", "Care access policy is awaiting clinic approval")
	}
	return nil
}
func (p Policy) Assign(ctx context.Context, a identity.Actor, id string, in AssignmentInput, key, request string) (Assignment, error) {
	if !a.Can("care.manage") {
		return Assignment{}, platform.Forbidden()
	}
	if e := p.enabled(); e != nil {
		return Assignment{}, e
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if len(in.Reason) < 20 || !in.ValidUntil.After(time.Now()) {
		return Assignment{}, platform.Fail(422, "validation", "Assignment requires a reason and future expiry")
	}
	tx, e := platform.BeginActor(ctx, p.DB, a.ID, p.Enabled)
	if e != nil {
		return Assignment{}, e
	}
	defer tx.Rollback(ctx)
	if ok, e := (accessRepository{tx}).authorized(ctx, a.ID, a.SessionID, "care.manage"); e != nil {
		return Assignment{}, e
	} else if !ok {
		return Assignment{}, platform.Forbidden()
	}
	r := accessRepository{tx}
	facility, e := r.facility(ctx, a.ID, id)
	if e != nil {
		return Assignment{}, e
	}
	ok, e := r.targetEligible(ctx, in.StaffID, facility)
	if e != nil {
		return Assignment{}, e
	}
	if !ok {
		return Assignment{}, platform.Fail(422, "invalid_reference", "Staff member is not eligible at this facility")
	}
	var prior Assignment
	replay, e := beginCommand(ctx, tx, a.ID, "care.assign", key, struct {
		Patient string
		Input   AssignmentInput
	}{id, in}, &prior)
	if e != nil {
		return Assignment{}, e
	}
	if replay {
		return prior, nil
	}
	result, e := r.createAssignment(ctx, a.ID, id, facility, in)
	if e != nil {
		return Assignment{}, e
	}
	if e = platform.Audit(ctx, tx, a.ID, "care.assign", result.ID, id, request, "success"); e != nil {
		return Assignment{}, e
	}
	return result, finishCommand(ctx, tx, a.ID, "care.assign", key, result)
}
func (p Policy) RevokeAssignment(ctx context.Context, a identity.Actor, id, request string) (Assignment, error) {
	if !a.Can("care.manage") {
		return Assignment{}, platform.Forbidden()
	}

	tx, e := platform.BeginActor(ctx, p.DB, a.ID, p.Enabled)
	if e != nil {
		return Assignment{}, e
	}
	defer tx.Rollback(ctx)
	if ok, e := (accessRepository{tx}).authorized(ctx, a.ID, a.SessionID, "care.manage"); e != nil {
		return Assignment{}, e
	} else if !ok {
		return Assignment{}, platform.Forbidden()
	}
	result, e := (accessRepository{tx}).revokeAssignment(ctx, a.ID, id)
	if e != nil {
		return Assignment{}, e
	}
	if e = platform.Audit(ctx, tx, a.ID, "care.revoke", id, result.PatientID, request, "success"); e != nil {
		return Assignment{}, e
	}
	return result, tx.Commit(ctx)
}
func (p Policy) BreakGlass(ctx context.Context, a identity.Actor, id string, in BreakGlassInput, key, request string) (Grant, error) {
	if !a.Can("break_glass.request") {
		return Grant{}, platform.Forbidden()
	}
	if e := p.enabled(); e != nil {
		return Grant{}, e
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if len(in.Reason) < 20 || len(in.Reason) > 1000 || in.DurationMinutes < 1 || in.DurationMinutes > 240 {
		return Grant{}, platform.Fail(422, "validation", "Provide an emergency reason and a duration of 1–240 minutes")
	}
	tx, e := platform.BeginActor(ctx, p.DB, a.ID, p.Enabled)
	if e != nil {
		return Grant{}, e
	}
	defer tx.Rollback(ctx)
	if ok, e := (accessRepository{tx}).authorized(ctx, a.ID, a.SessionID, "break_glass.request"); e != nil {
		return Grant{}, e
	} else if !ok {
		return Grant{}, platform.Forbidden()
	}
	r := accessRepository{tx}
	facility, e := r.facility(ctx, a.ID, id)
	if e != nil {
		return Grant{}, e
	}
	var prior Grant
	replay, e := beginCommand(ctx, tx, a.ID, "break_glass.request", key, struct {
		Patient string
		Input   BreakGlassInput
	}{id, in}, &prior)
	if e != nil {
		return Grant{}, e
	}
	if replay {
		return prior, nil
	}
	// Business uniqueness survives clients changing their idempotency key.
	if e = platform.LockIdempotency(ctx, tx, a.ID, "break_glass.patient", id); e != nil {
		return Grant{}, e
	}
	active, e := r.activeBreakGlass(ctx, a.ID, id)
	if e != nil {
		return Grant{}, e
	}
	if active {
		return Grant{}, platform.Fail(409, "active_grant", "An active exceptional-access grant already exists")
	}
	result, e := r.createGrant(ctx, a.ID, id, facility, in)
	if e != nil {
		return Grant{}, e
	}
	if e = platform.AuditReason(ctx, tx, a.ID, "break_glass.request", result.ID, id, request, "success", "emergency_grant_reason_in_restricted_record"); e != nil {
		return Grant{}, e
	}
	return result, finishCommand(ctx, tx, a.ID, "break_glass.request", key, result)
}
func (p Policy) Reviews(ctx context.Context, a identity.Actor, after, request string, limit int) ([]Grant, error) {
	if !a.Can("break_glass.review") {
		return nil, platform.Forbidden()
	}
	tx, e := p.DB.Begin(ctx)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	out, e := listGrants(ctx, tx, after, limit)
	if e != nil {
		return nil, e
	}
	if e = platform.Audit(ctx, tx, a.ID, "break_glass.review_queue", "", "", request, "success"); e != nil {
		return nil, e
	}
	return out, tx.Commit(ctx)
}
func (p Policy) Review(ctx context.Context, a identity.Actor, id string, in ReviewInput, request string) (Grant, error) {
	if !a.Can("break_glass.review") {
		return Grant{}, platform.Forbidden()
	}
	tx, e := p.DB.Begin(ctx)
	if e != nil {
		return Grant{}, e
	}
	defer tx.Rollback(ctx)
	if ok, e := (accessRepository{tx}).authorized(ctx, a.ID, a.SessionID, "break_glass.review"); e != nil {
		return Grant{}, e
	} else if !ok {
		return Grant{}, platform.Forbidden()
	}
	r := accessRepository{tx}
	old, e := r.grant(ctx, id)
	if e != nil {
		return Grant{}, e
	}
	if old.StaffID == a.ID {
		if e = platform.AuditReason(ctx, tx, a.ID, "break_glass.review", id, old.PatientID, request, "denied", "self_review"); e != nil {
			return Grant{}, e
		}
		if e = tx.Commit(ctx); e != nil {
			return Grant{}, e
		}
		return Grant{}, platform.Forbidden()
	}
	if old.ReviewedAt != nil {
		return Grant{}, platform.Conflict()
	}
	result, e := r.review(ctx, a.ID, id, in)
	if e != nil {
		return Grant{}, e
	}
	if e = platform.AuditReason(ctx, tx, a.ID, "break_glass.review", id, old.PatientID, request, "success", in.Outcome); e != nil {
		return Grant{}, e
	}
	return result, tx.Commit(ctx)
}

// Command hashes are normalized DTO hashes; the first write and replay response commit together.
func beginCommand(ctx context.Context, tx pgx.Tx, actor, op, key string, input, out any) (bool, error) {
	if len(key) < 8 || len(key) > 128 {
		return false, platform.Fail(422, "idempotency_key_required", "Idempotency-Key must be 8–128 characters")
	}
	body, e := json.Marshal(input)
	if e != nil {
		return false, e
	}
	hash := security.Digest(string(body))
	if e = platform.LockIdempotency(ctx, tx, actor, op, key); e != nil {
		return false, e
	}
	previous, response, e := platform.ReadIdempotency(ctx, tx, actor, op, key)
	if e == nil {
		if previous != hash {
			return false, platform.Fail(409, "idempotency_conflict", "Key already used with a different request")
		}
		return true, json.Unmarshal(response, out)
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return false, e
	}
	// Keep the request hash in the transaction context for finishCommand.
	_, e = tx.Exec(ctx, `SELECT set_config('app.command_hash',$1,true)`, hash)
	return false, e
}
func finishCommand(ctx context.Context, tx pgx.Tx, actor, op, key string, response any) error {
	body, e := json.Marshal(response)
	if e != nil {
		return e
	}
	var hash string
	if e = tx.QueryRow(ctx, `SELECT current_setting('app.command_hash')`).Scan(&hash); e != nil {
		return e
	}
	if e = platform.WriteIdempotency(ctx, tx, actor, op, key, hash, body); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
