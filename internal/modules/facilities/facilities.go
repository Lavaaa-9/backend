package facilities

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"spartial.local/backend/internal/modules/identity"
	"spartial.local/backend/internal/platform"
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

const sel = `SELECT f.id::text,f.name,o.timezone,f.status='active' FROM org.facility f JOIN org.organization o ON o.id=f.org_id`

func (s Service) List(ctx context.Context, a identity.Actor, request string) ([]Facility, error) {
	tx, _, e := platform.BeginRequest(ctx, s.DB, a.SessionID, request)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	all := a.Can("admin.facility.manage")
	rows, e := tx.Query(ctx, sel+` WHERE ($1 OR f.id=ANY($2::uuid[])) ORDER BY f.name,f.id LIMIT 100`, all, a.FacilityIDs)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Facility{}
	for rows.Next() {
		var f Facility
		if e = rows.Scan(&f.ID, &f.Name, &f.Timezone, &f.Active); e != nil {
			return nil, e
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

var nonCode = regexp.MustCompile(`[^A-Z0-9]+`)

func code(name string) string {
	base := nonCode.ReplaceAllString(strings.ToUpper(name), "")
	if len(base) > 12 {
		base = base[:12]
	}
	if len(base) < 2 {
		base = "FAC"
	}
	b := make([]byte, 4)
	rand.Read(b)
	return fmt.Sprintf("%s-%04d", base, binary.BigEndian.Uint32(b)%10000)
}
func (s Service) Create(ctx context.Context, a identity.Actor, in Input, request string) (Facility, error) {
	if !a.Can("admin.facility.manage") {
		return Facility{}, platform.Forbidden()
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return Facility{}, platform.Fail(422, "validation", "Facility name required")
	}
	if _, e := time.LoadLocation(in.Timezone); e != nil {
		return Facility{}, platform.Fail(422, "validation", "Unknown timezone")
	}
	tx, sess, e := platform.BeginRequest(ctx, s.DB, a.SessionID, request)
	if e != nil {
		return Facility{}, e
	}
	defer tx.Rollback(ctx)
	var id string
	if e = tx.QueryRow(ctx, `INSERT INTO org.facility(org_id,code,name) VALUES($1::uuid,$2,$3) RETURNING id::text`, sess.OrgID, code(in.Name), in.Name).Scan(&id); e != nil {
		return Facility{}, e
	}
	if e = platform.Audit(ctx, tx, "facility.create", "administration", "success", "facility", id, "", ""); e != nil {
		return Facility{}, e
	}
	var f Facility
	if e = tx.QueryRow(ctx, sel+` WHERE f.id=$1::uuid`, id).Scan(&f.ID, &f.Name, &f.Timezone, &f.Active); e != nil {
		return Facility{}, e
	}
	return f, tx.Commit(ctx)
}
