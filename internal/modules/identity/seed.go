package identity

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"spartial.local/backend/internal/platform"
	"spartial.local/backend/internal/security"
)

// Seed is for synthetic development databases only. Existing accounts are never reset.
func Seed(ctx context.Context, db *pgxpool.Pool, adminPassword, receptionPassword string) (string, error) {
	if !security.PasswordAllowed(adminPassword) || !security.PasswordAllowed(receptionPassword) {
		return "", errors.New("both seed passwords must contain 12–256 characters")
	}
	ah, e := security.HashPassword(adminPassword)
	if e != nil {
		return "", e
	}
	rh, e := security.HashPassword(receptionPassword)
	if e != nil {
		return "", e
	}
	tx, e := db.Begin(ctx)
	if e != nil {
		return "", e
	}
	defer tx.Rollback(ctx)
	if e = lockStaff(ctx, tx); e != nil {
		return "", e
	}
	var facility string
	e = tx.QueryRow(ctx, `SELECT id::text FROM facilities WHERE name='Synthetic Development Clinic' ORDER BY created_at LIMIT 1`).Scan(&facility)
	if errors.Is(e, pgx.ErrNoRows) {
		e = tx.QueryRow(ctx, `INSERT INTO facilities(name) VALUES('Synthetic Development Clinic') RETURNING id::text`).Scan(&facility)
	}
	if e != nil {
		return "", e
	}
	for _, in := range []struct{ email, name, role, hash string }{{"admin@spartial.test", "Synthetic Administrator", "administrator", ah}, {"reception@spartial.test", "Synthetic Receptionist", "reception", rh}} {
		var exists bool
		if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM staff WHERE email=$1)`, in.email).Scan(&exists); e != nil {
			return "", e
		}
		if exists {
			continue
		}
		u, e := insertUser(ctx, tx, StaffInput{Email: in.email, DisplayName: in.name, Role: in.role, FacilityIDs: []string{facility}}, in.hash)
		if e != nil {
			return "", e
		}
		if e = platform.Audit(ctx, tx, u.ID, "development.seed", u.ID, "", uuid.NewString(), "success"); e != nil {
			return "", e
		}
	}
	if e = tx.Commit(ctx); e != nil {
		return "", e
	}
	return facility, nil
}
