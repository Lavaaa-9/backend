package facilities

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"spartial.local/backend/internal/modules/identity"
	"spartial.local/backend/internal/platform"
	"strings"
	"time"
)

type Facility struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Timezone string `json:"timezone"`
	Active   bool   `json:"active"`
}
type Input struct {
	Name     string `json:"name" validate:"required,max=160"`
	Timezone string `json:"timezone" validate:"required,max=100"`
}
type Service struct{ DB *pgxpool.Pool }

func (s Service) List(ctx context.Context, a identity.Actor) ([]Facility, error) {
	if !a.Can("facilities.read") {
		return nil, platform.Forbidden()
	}
	return list(ctx, s.DB, a.Can("facilities.manage"), a.FacilityIDs)
}
func (s Service) Create(ctx context.Context, a identity.Actor, in Input, request string) (Facility, error) {
	if !a.Can("facilities.manage") {
		return Facility{}, platform.Forbidden()
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return Facility{}, platform.Fail(422, "validation", "Facility name required")
	}
	if _, e := time.LoadLocation(in.Timezone); e != nil {
		return Facility{}, platform.Fail(422, "validation", "Unknown timezone")
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return Facility{}, e
	}
	defer tx.Rollback(ctx)
	f, e := create(ctx, tx, in)
	if e != nil {
		return Facility{}, e
	}
	if e = platform.Audit(ctx, tx, a.ID, "facilities.create", f.ID, "", request, "success"); e != nil {
		return Facility{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return Facility{}, e
	}
	return f, nil
}
