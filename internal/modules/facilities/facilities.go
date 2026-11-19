package facilities

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"spartial.local/backend/internal/modules/identity"
	"spartial.local/backend/internal/platform"
)

type Facility struct {
	ID            string    `json:"id"`
	Code          string    `json:"code"`
	Name          string    `json:"name"`
	HFRCode       *string   `json:"hfr_code,omitempty"`
	Phone         *string   `json:"phone,omitempty"`
	Region        *string   `json:"region,omitempty"`
	District      *string   `json:"district,omitempty"`
	StreetAddress *string   `json:"street_address,omitempty"`
	Timezone      string    `json:"timezone"`
	Status        string    `json:"status"`
	Active        bool      `json:"active"`
	Version       int       `json:"version"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type Input struct {
	Code          string `json:"code" validate:"omitempty,min=2,max=20"`
	Name          string `json:"name" validate:"required,min=1,max=160"`
	Timezone      string `json:"timezone" validate:"omitempty,max=100"`
	HFRCode       string `json:"hfr_code" validate:"omitempty,max=64"`
	Phone         string `json:"phone" validate:"omitempty,max=40"`
	Region        string `json:"region" validate:"omitempty,max=100"`
	District      string `json:"district" validate:"omitempty,max=100"`
	StreetAddress string `json:"street_address" validate:"omitempty,max=300"`
}

type UpdateInput struct {
	Name          *string `json:"name" validate:"omitempty,min=1,max=160"`
	HFRCode       *string `json:"hfr_code" validate:"omitempty,max=64"`
	Phone         *string `json:"phone" validate:"omitempty,max=40"`
	Region        *string `json:"region" validate:"omitempty,max=100"`
	District      *string `json:"district" validate:"omitempty,max=100"`
	StreetAddress *string `json:"street_address" validate:"omitempty,max=300"`
	Version       int     `json:"version" validate:"required,min=1"`
}

type StatusInput struct {
	Status  string `json:"status" validate:"required,oneof=active inactive"`
	Version int    `json:"version" validate:"required,min=1"`
}

type Service struct{ DB *pgxpool.Pool }

const sel = `SELECT f.id::text,f.code,f.name,f.hfr_code,f.phone_e164,f.region,f.district,f.street_address,o.timezone,f.status,(f.status='active'),f.row_version,f.created_at,f.updated_at
 FROM org.facility f JOIN org.organization o ON o.id=f.org_id`

func scanFacility(row pgx.Row) (Facility, error) {
	var f Facility
	e := row.Scan(&f.ID, &f.Code, &f.Name, &f.HFRCode, &f.Phone, &f.Region, &f.District, &f.StreetAddress, &f.Timezone, &f.Status, &f.Active, &f.Version, &f.CreatedAt, &f.UpdatedAt)
	return f, e
}

var (
	nonCode = regexp.MustCompile(`[^A-Z0-9]+`)
	e164Re  = regexp.MustCompile(`^\+[1-9][0-9]{7,14}$`)
)

func cleanPhone(v string) (string, error) {
	v = strings.NewReplacer(" ", "", "-", "", "(", "", ")", "").Replace(strings.TrimSpace(v))
	switch {
	case v == "":
		return "", nil
	case strings.HasPrefix(v, "00"):
		v = "+" + v[2:]
	case strings.HasPrefix(v, "0"):
		v = "+255" + v[1:]
	case strings.HasPrefix(v, "255"):
		v = "+" + v
	}
	if !e164Re.MatchString(v) {
		return "", platform.Fail(422, "validation", "Phone must be a valid international number such as +255712345678")
	}
	return v, nil
}

func genCode(name string) string {
	base := nonCode.ReplaceAllString(strings.ToUpper(name), "")
	if len(base) > 12 {
		base = base[:12]
	}
	if len(base) < 2 {
		base = "FAC"
	}
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%s-%04d", base, binary.BigEndian.Uint32(b)%10000)
}

func (s Service) List(ctx context.Context, a identity.Actor, q, status, request string) ([]Facility, error) {
	tx, _, e := platform.BeginRequest(ctx, s.DB, a.SessionID, request)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	all := a.Can("admin.facility.manage")
	q = strings.TrimSpace(q)
	rows, e := tx.Query(ctx, sel+` WHERE ($1 OR f.id=ANY($2::uuid[]))
 AND ($3='' OR strpos(lower(f.name),lower($3))>0 OR starts_with(upper(f.code),upper($3)) OR strpos(lower(coalesce(f.region,'')),lower($3))>0)
 AND ($4='' OR f.status=$4)
 ORDER BY f.name,f.id LIMIT 100`, all, a.FacilityIDs, q, status)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Facility{}
	for rows.Next() {
		f, e := scanFacility(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s Service) Get(ctx context.Context, a identity.Actor, id, request string) (Facility, error) {
	tx, _, e := platform.BeginRequest(ctx, s.DB, a.SessionID, request)
	if e != nil {
		return Facility{}, e
	}
	defer tx.Rollback(ctx)
	all := a.Can("admin.facility.manage")
	return scanFacility(tx.QueryRow(ctx, sel+` WHERE f.id=$1::uuid AND ($2 OR f.id=ANY($3::uuid[]))`, id, all, a.FacilityIDs))
}

func (s Service) Create(ctx context.Context, a identity.Actor, in Input, request string) (Facility, error) {
	if !a.Can("admin.facility.manage") {
		return Facility{}, platform.Forbidden()
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return Facility{}, platform.Fail(422, "validation", "Facility name required")
	}
	if in.Timezone != "" {
		if _, e := time.LoadLocation(in.Timezone); e != nil {
			return Facility{}, platform.Fail(422, "validation", "Unknown timezone")
		}
	}
	ph, e := cleanPhone(in.Phone)
	if e != nil {
		return Facility{}, e
	}
	fcode := strings.ToUpper(strings.TrimSpace(in.Code))
	if fcode == "" {
		fcode = genCode(in.Name)
	}
	tx, sess, e := platform.BeginRequest(ctx, s.DB, a.SessionID, request)
	if e != nil {
		return Facility{}, e
	}
	defer tx.Rollback(ctx)
	var id string
	if e = tx.QueryRow(ctx, `INSERT INTO org.facility(org_id,code,name,hfr_code,phone_e164,region,district,street_address)
 VALUES($1::uuid,$2,$3,NULLIF($4,''),NULLIF($5,''),NULLIF($6,''),NULLIF($7,''),NULLIF($8,'')) RETURNING id::text`,
		sess.OrgID, fcode, in.Name, strings.TrimSpace(in.HFRCode), ph, strings.TrimSpace(in.Region), strings.TrimSpace(in.District), strings.TrimSpace(in.StreetAddress)).Scan(&id); e != nil {
		return Facility{}, e
	}
	if e = platform.Audit(ctx, tx, "facility.create", "administration", "success", "facility", id, "", fcode); e != nil {
		return Facility{}, e
	}
	f, e := scanFacility(tx.QueryRow(ctx, sel+` WHERE f.id=$1::uuid`, id))
	if e != nil {
		return Facility{}, e
	}
	return f, tx.Commit(ctx)
}

func (s Service) Update(ctx context.Context, a identity.Actor, id string, in UpdateInput, request string) (Facility, error) {
	if !a.Can("admin.facility.manage") {
		return Facility{}, platform.Forbidden()
	}
	var ph *string
	if in.Phone != nil {
		v, e := cleanPhone(*in.Phone)
		if e != nil {
			return Facility{}, e
		}
		ph = &v
	}
	tx, _, e := platform.BeginRequest(ctx, s.DB, a.SessionID, request)
	if e != nil {
		return Facility{}, e
	}
	defer tx.Rollback(ctx)
	tag, e := tx.Exec(ctx, `UPDATE org.facility SET
 name=coalesce(NULLIF(btrim($3),''),name),
 hfr_code=CASE WHEN $4::text IS NOT NULL THEN NULLIF(btrim($4),'') ELSE hfr_code END,
 phone_e164=CASE WHEN $5::text IS NOT NULL THEN NULLIF($5,'') ELSE phone_e164 END,
 region=CASE WHEN $6::text IS NOT NULL THEN NULLIF(btrim($6),'') ELSE region END,
 district=CASE WHEN $7::text IS NOT NULL THEN NULLIF(btrim($7),'') ELSE district END,
 street_address=CASE WHEN $8::text IS NOT NULL THEN NULLIF(btrim($8),'') ELSE street_address END
 WHERE id=$1::uuid AND row_version=$2`, id, in.Version, in.Name, in.HFRCode, ph, in.Region, in.District, in.StreetAddress)
	if e != nil {
		return Facility{}, e
	}
	if e = platform.Changed(ctx, tx, tag, "org.facility", id); e != nil {
		return Facility{}, e
	}
	if e = platform.Audit(ctx, tx, "facility.update", "administration", "success", "facility", id, "", ""); e != nil {
		return Facility{}, e
	}
	f, e := scanFacility(tx.QueryRow(ctx, sel+` WHERE f.id=$1::uuid`, id))
	if e != nil {
		return Facility{}, e
	}
	return f, tx.Commit(ctx)
}

func (s Service) SetStatus(ctx context.Context, a identity.Actor, id string, in StatusInput, request string) (Facility, error) {
	if !a.Can("admin.facility.manage") {
		return Facility{}, platform.Forbidden()
	}
	tx, _, e := platform.BeginRequest(ctx, s.DB, a.SessionID, request)
	if e != nil {
		return Facility{}, e
	}
	defer tx.Rollback(ctx)
	tag, e := tx.Exec(ctx, `UPDATE org.facility SET status=$3 WHERE id=$1::uuid AND row_version=$2`, id, in.Version, in.Status)
	if e != nil {
		return Facility{}, e
	}
	if e = platform.Changed(ctx, tx, tag, "org.facility", id); e != nil {
		return Facility{}, e
	}
	act := "facility.activate"
	if in.Status == "inactive" {
		act = "facility.deactivate"
	}
	if e = platform.Audit(ctx, tx, act, "administration", "success", "facility", id, "", in.Status); e != nil {
		return Facility{}, e
	}
	f, e := scanFacility(tx.QueryRow(ctx, sel+` WHERE f.id=$1::uuid`, id))
	if e != nil {
		return Facility{}, e
	}
	return f, tx.Commit(ctx)
}

func (s Service) Departments(ctx context.Context, a identity.Actor, facilityID, request string) ([]platform.Raw, error) {
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) ([]platform.Raw, error) {
		all := a.Can("admin.facility.manage")
		var ok bool
		if e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM org.facility WHERE id=$1::uuid AND ($2 OR id=ANY($3::uuid[])))`, facilityID, all, a.FacilityIDs).Scan(&ok); e != nil {
			return nil, e
		}
		if !ok {
			return nil, platform.NotFound()
		}
		return platform.Many(ctx, tx, `SELECT `+platform.JSON("d", true)+` FROM org.department d WHERE d.facility_id=$1::uuid ORDER BY d.name`, facilityID)
	})
}
