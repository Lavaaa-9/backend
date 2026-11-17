package privacy

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"spartial.local/backend/internal/modules/identity"
	"spartial.local/backend/internal/platform"
	"time"
)

type Event struct {
	Reason     *string   `json:"reason"`
	FacilityID *string   `json:"facility_id"`
	ID         int64     `json:"id"`
	ActorID    *string   `json:"actor_id"`
	Action     string    `json:"action"`
	ResourceID *string   `json:"resource_id"`
	PatientID  *string   `json:"patient_id"`
	RequestID  string    `json:"request_id"`
	Outcome    string    `json:"outcome"`
	CreatedAt  time.Time `json:"created_at"`
}
type Service struct{ DB *pgxpool.Pool }

func (s Service) List(ctx context.Context, a identity.Actor, before int64, request string) ([]Event, error) {
	if !a.Can("audit.read") {
		return nil, platform.Forbidden()
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	out, e := list(ctx, tx, before)
	if e != nil {
		return nil, e
	}
	if e = platform.Audit(ctx, tx, a.ID, "audit.read", "", "", request, "success"); e != nil {
		return nil, e
	}
	if e = tx.Commit(ctx); e != nil {
		return nil, e
	}
	return out, nil
}
