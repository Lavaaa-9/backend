package patients

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

type Input struct {
	FacilityID string `json:"facility_id" validate:"required,uuid"`
	Demographics
}
type Demographics struct {
	GivenName      string  `json:"given_name" validate:"required,min=1,max=100"`
	FamilyName     string  `json:"family_name" validate:"required,min=1,max=100"`
	BirthPrecision string  `json:"birth_precision" validate:"required,oneof=exact year unknown"`
	BirthDate      *string `json:"birth_date" validate:"omitempty,datetime=2006-01-02"`
	BirthYear      *int    `json:"birth_year" validate:"omitempty,min=1850,max=9999"`
	Phone          string  `json:"phone" validate:"max=40"`
	Address        string  `json:"address" validate:"max=500"`
}
type Update struct {
	Demographics
	Version int `json:"version" validate:"required,min=1"`
}
type Patient struct {
	ID         string `json:"id"`
	MRN        string `json:"mrn"`
	FacilityID string `json:"facility_id"`
	Demographics
	Version   int       `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
type Service struct {
	DB       *pgxpool.Pool
	Location *time.Location
}

func clean(d *Demographics, now time.Time) error {
	d.GivenName = strings.TrimSpace(d.GivenName)
	d.FamilyName = strings.TrimSpace(d.FamilyName)
	if d.GivenName == "" || d.FamilyName == "" {
		return platform.Fail(422, "validation", "Patient names are required")
	}
	switch d.BirthPrecision {
	case "exact":
		if d.BirthDate == nil || d.BirthYear != nil {
			return platform.Fail(422, "validation", "Exact birth requires only birth_date")
		}
		b, e := time.Parse("2006-01-02", *d.BirthDate)
		if e != nil || *d.BirthDate > now.Format("2006-01-02") || b.Year() < 1850 {
			return platform.Fail(422, "validation", "Invalid birth date")
		}
	case "year":
		if d.BirthYear == nil || d.BirthDate != nil || *d.BirthYear > now.Year() || *d.BirthYear < 1850 {
			return platform.Fail(422, "validation", "Approximate birth requires only a valid birth_year")
		}
	case "unknown":
		if d.BirthDate != nil || d.BirthYear != nil {
			return platform.Fail(422, "validation", "Unknown birth must omit birth_date and birth_year")
		}
	default:
		return platform.Fail(422, "validation", "Invalid birth_precision")
	}
	return nil
}
func scope(a identity.Actor, facility string) bool {
	for _, f := range a.FacilityIDs {
		if f == facility {
			return true
		}
	}
	return false
}
func (s Service) Create(ctx context.Context, a identity.Actor, in Input, key, request string) (Patient, error) {
	if !a.Can("patients.write") || !scope(a, in.FacilityID) {
		return Patient{}, platform.Forbidden()
	}
	if len(key) < 8 || len(key) > 128 {
		return Patient{}, platform.Fail(422, "idempotency_key_required", "Idempotency-Key must be 8–128 characters")
	}
	if e := clean(&in.Demographics, s.now()); e != nil {
		return Patient{}, e
	}
	body, e := json.Marshal(in)
	if e != nil {
		return Patient{}, e
	}
	hash := security.Digest(string(body))
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return Patient{}, e
	}
	defer tx.Rollback(ctx)
	if e = platform.LockIdempotency(ctx, tx, a.ID, "patients.create", key); e != nil {
		return Patient{}, e
	}
	priorHash, response, e := platform.ReadIdempotency(ctx, tx, a.ID, "patients.create", key)
	if e == nil {
		if priorHash != hash {
			return Patient{}, platform.Fail(409, "idempotency_conflict", "Key already used with a different request")
		}
		var p Patient
		if e = json.Unmarshal(response, &p); e != nil {
			return Patient{}, e
		}
		return p, nil
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return Patient{}, e
	}
	p, e := (repository{tx}).create(ctx, in)
	if e != nil {
		return Patient{}, e
	}
	if e = platform.Audit(ctx, tx, a.ID, "patients.create", p.ID, p.ID, request, "success"); e != nil {
		return Patient{}, e
	}
	if e = platform.Enqueue(ctx, tx, "patient.registered", p.ID); e != nil {
		return Patient{}, e
	}
	response, e = json.Marshal(p)
	if e != nil {
		return Patient{}, e
	}
	if e = platform.WriteIdempotency(ctx, tx, a.ID, "patients.create", key, hash, response); e != nil {
		return Patient{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return Patient{}, e
	}
	return p, nil
}
func (s Service) Get(ctx context.Context, a identity.Actor, id, request string) (Patient, error) {
	if !a.Can("patients.read") {
		return Patient{}, platform.Forbidden()
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return Patient{}, e
	}
	defer tx.Rollback(ctx)
	p, e := (repository{tx}).get(ctx, id, a.FacilityIDs)
	if e != nil {
		return Patient{}, e
	}
	if e = platform.Audit(ctx, tx, a.ID, "patients.read", id, id, request, "success"); e != nil {
		return Patient{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return Patient{}, e
	}
	return p, nil
}
func (s Service) List(ctx context.Context, a identity.Actor, q, after, request string, limit int) ([]Patient, error) {
	if !a.Can("patients.read") {
		return nil, platform.Forbidden()
	}
	if len(q) > 100 {
		return nil, platform.Fail(422, "validation", "Search exceeds 100 characters")
	}
	if q != "" {
		q = "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(q) + "%"
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	out, e := (repository{tx}).list(ctx, a.FacilityIDs, q, after, limit)
	if e != nil {
		return nil, e
	}
	if e = platform.Audit(ctx, tx, a.ID, "patients.search", "", "", request, "success"); e != nil {
		return nil, e
	}
	if e = tx.Commit(ctx); e != nil {
		return nil, e
	}
	return out, nil
}
func (s Service) Update(ctx context.Context, a identity.Actor, id string, in Update, request string) (Patient, error) {
	if !a.Can("patients.write") {
		return Patient{}, platform.Forbidden()
	}
	if e := clean(&in.Demographics, s.now()); e != nil {
		return Patient{}, e
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return Patient{}, e
	}
	defer tx.Rollback(ctx)
	r := repository{tx}
	// Check visibility before distinguishing stale versions from absent records.
	if _, e = r.get(ctx, id, a.FacilityIDs); e != nil {
		return Patient{}, e
	}
	p, e := r.update(ctx, id, in, a.FacilityIDs)
	if errors.Is(e, pgx.ErrNoRows) {
		return Patient{}, platform.Conflict()
	}
	if e != nil {
		return Patient{}, e
	}
	if e = platform.Audit(ctx, tx, a.ID, "patients.update", id, id, request, "success"); e != nil {
		return Patient{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return Patient{}, e
	}
	return p, nil
}

func (s Service) now() time.Time {
	if s.Location != nil {
		return time.Now().In(s.Location)
	}
	return time.Now().UTC()
}
