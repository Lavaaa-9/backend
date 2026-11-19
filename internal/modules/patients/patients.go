package patients

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"spartial.local/backend/internal/modules/identity"
	"spartial.local/backend/internal/platform"
	"spartial.local/backend/internal/security"
)

type Input struct {
	FacilityID string `json:"facility_id" validate:"required,uuid"`
	Demographics
}
type Demographics struct {
	GivenName         string  `json:"given_name" validate:"required,min=1,max=100"`
	MiddleName        string  `json:"middle_name,omitempty" validate:"omitempty,max=100"`
	FamilyName        string  `json:"family_name" validate:"required,min=1,max=100"`
	PreferredName     string  `json:"preferred_name,omitempty" validate:"omitempty,max=100"`
	BirthPrecision    string  `json:"birth_precision" validate:"required,oneof=exact year unknown"`
	BirthDate         *string `json:"birth_date" validate:"omitempty,datetime=2006-01-02"`
	BirthYear         *int    `json:"birth_year" validate:"omitempty,min=1900,max=9999"`
	Phone             string  `json:"phone" validate:"max=40"`
	Address           string  `json:"address" validate:"max=500"`
	Region            string  `json:"region,omitempty" validate:"omitempty,max=100"`
	District          string  `json:"district,omitempty" validate:"omitempty,max=100"`
	Ward              string  `json:"ward,omitempty" validate:"omitempty,max=100"`
	Sex               string  `json:"sex,omitempty" validate:"omitempty,oneof=female male intersex unknown"`
	MaritalStatus     string  `json:"marital_status,omitempty" validate:"omitempty,oneof=single married divorced widowed separated unknown"`
	Occupation        string  `json:"occupation,omitempty" validate:"omitempty,max=120"`
	Nationality       string  `json:"nationality,omitempty" validate:"omitempty,len=2,alpha"`
	PreferredLanguage string  `json:"preferred_language,omitempty" validate:"omitempty,oneof=sw en"`
	BloodGroup        string  `json:"blood_group,omitempty" validate:"omitempty,oneof=A+ A- B+ B- AB+ AB- O+ O- unknown"`
}
type Update struct {
	Demographics
	Version int `json:"version" validate:"required,min=1"`
}
type StatusInput struct {
	Status     string  `json:"status" validate:"required,oneof=active inactive deceased"`
	DeceasedAt *string `json:"deceased_at" validate:"omitempty,datetime=2006-01-02T15:04:05Z07:00"`
	Reason     string  `json:"reason" validate:"max=500"`
	Version    int     `json:"version" validate:"required,min=1"`
}
type ListFilter struct {
	Q          string
	Phone      string
	BirthDate  string
	FacilityID string
	Status     string
	Sort       string
	After      string
	Limit      int
}
type Patient struct {
	ID         string     `json:"id"`
	MRN        string     `json:"mrn"`
	FacilityID string     `json:"facility_id"`
	Status     string     `json:"status,omitempty"`
	DeceasedAt *time.Time `json:"deceased_at,omitempty"`
	Demographics
	Version   int       `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
type Service struct {
	DB       *pgxpool.Pool
	Location *time.Location
	Keys     *security.Keyring
	IndexKey []byte
}

var e164 = regexp.MustCompile(`^\+[1-9][0-9]{7,14}$`)

// phone normalises Tanzanian national numbers and validates E.164; empty is allowed.
func phone(v string) (string, error) {
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
	if !e164.MatchString(v) {
		return "", platform.Fail(422, "validation", "Phone must be a valid international number such as +255712345678")
	}
	return v, nil
}

// dob maps the API's birth precision onto date_of_birth + dob_is_estimated.
func dob(d Demographics, now time.Time) (time.Time, bool, error) {
	bad := platform.Fail(422, "validation", "Invalid birth information")
	switch d.BirthPrecision {
	case "exact":
		if d.BirthDate == nil || d.BirthYear != nil {
			return time.Time{}, false, bad
		}
		t, e := time.Parse("2006-01-02", *d.BirthDate)
		if e != nil || t.After(now) || t.Year() < 1900 {
			return time.Time{}, false, bad
		}
		return t, false, nil
	case "year":
		if d.BirthYear == nil || d.BirthDate != nil || *d.BirthYear > now.Year() {
			return time.Time{}, false, bad
		}
		t := time.Date(*d.BirthYear, 7, 1, 0, 0, 0, 0, time.UTC)
		if t.After(now) {
			t = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		}
		return t, true, nil
	default:
		if d.BirthDate != nil || d.BirthYear != nil {
			return time.Time{}, false, bad
		}
		return time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC), true, nil
	}
}
func (s Service) today() time.Time {
	n := time.Now().In(s.Location)
	return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.UTC)
}
func clean(d *Demographics) error {
	d.GivenName, d.FamilyName = strings.TrimSpace(d.GivenName), strings.TrimSpace(d.FamilyName)
	d.MiddleName = strings.TrimSpace(d.MiddleName)
	d.PreferredName = strings.TrimSpace(d.PreferredName)
	d.Address = strings.TrimSpace(d.Address)
	d.Region = strings.TrimSpace(d.Region)
	d.District = strings.TrimSpace(d.District)
	d.Ward = strings.TrimSpace(d.Ward)
	d.Occupation = strings.TrimSpace(d.Occupation)
	if d.GivenName == "" || d.FamilyName == "" {
		return platform.Fail(422, "validation", "Patient names are required")
	}
	p, e := phone(d.Phone)
	if e != nil {
		return e
	}
	d.Phone = p
	if d.Sex == "" {
		d.Sex = "unknown"
	}
	d.Nationality = strings.ToUpper(strings.TrimSpace(d.Nationality))
	if d.Nationality == "" {
		d.Nationality = "TZ"
	}
	if d.PreferredLanguage == "" {
		d.PreferredLanguage = "sw"
	}
	return nil
}

const sel = `SELECT p.id::text,p.mrn,p.registered_facility_id::text,p.status,p.deceased_at,
 p.first_name,coalesce(p.middle_name,''),p.last_name,coalesce(p.preferred_name,''),
 p.date_of_birth,p.dob_is_estimated,p.sex,
 coalesce((SELECT cp.value FROM patient.contact_point cp WHERE cp.patient_id=p.id AND cp.system='phone' AND cp.is_primary AND cp.status='active'),''),
 coalesce(p.street_address,''),coalesce(p.region,''),coalesce(p.district,''),coalesce(p.ward,''),
 coalesce(p.marital_status,''),coalesce(p.occupation,''),trim(p.nationality),p.preferred_language,coalesce(p.blood_group,''),
 p.row_version,p.created_at,p.updated_at FROM patient.patient p`

func scan(row pgx.Row) (Patient, error) {
	var p Patient
	var d time.Time
	var est bool
	if e := row.Scan(&p.ID, &p.MRN, &p.FacilityID, &p.Status, &p.DeceasedAt,
		&p.GivenName, &p.MiddleName, &p.FamilyName, &p.PreferredName,
		&d, &est, &p.Sex, &p.Phone,
		&p.Address, &p.Region, &p.District, &p.Ward,
		&p.MaritalStatus, &p.Occupation, &p.Nationality, &p.PreferredLanguage, &p.BloodGroup,
		&p.Version, &p.CreatedAt, &p.UpdatedAt); e != nil {
		return p, e
	}
	switch {
	case est && d.Year() == 1900 && d.Month() == 1 && d.Day() == 1:
		p.BirthPrecision = "unknown"
	case est:
		y := d.Year()
		p.BirthPrecision, p.BirthYear = "year", &y
	default:
		s := d.Format("2006-01-02")
		p.BirthPrecision, p.BirthDate = "exact", &s
	}
	return p, nil
}

func (s Service) Create(ctx context.Context, a identity.Actor, in Input, key, request string) (Patient, error) {
	if !a.Can("patient.create") || !a.InFacility(in.FacilityID) {
		return Patient{}, platform.Forbidden()
	}
	if e := clean(&in.Demographics); e != nil {
		return Patient{}, e
	}
	birth, est, e := dob(in.Demographics, s.today())
	if e != nil {
		return Patient{}, e
	}
	tx, sess, e := platform.BeginRequest(ctx, s.DB, a.SessionID, request)
	if e != nil {
		return Patient{}, e
	}
	defer tx.Rollback(ctx)
	p, e := platform.Idempotent(ctx, tx, sess, "patient.create", key, in, func() (Patient, error) {
		var id string
		e := tx.QueryRow(ctx, `INSERT INTO patient.patient(org_id,mrn,registered_facility_id,first_name,middle_name,last_name,preferred_name,sex,date_of_birth,dob_is_estimated,
 marital_status,occupation,nationality,preferred_language,region,district,ward,street_address,blood_group)
 VALUES($1::uuid,patient.next_mrn($1::uuid),$2::uuid,$3,NULLIF($4,''),$5,NULLIF($6,''),$7,$8,$9,
 NULLIF($10,''),NULLIF($11,''),$12,$13,NULLIF($14,''),NULLIF($15,''),NULLIF($16,''),NULLIF($17,''),NULLIF($18,'')) RETURNING id::text`,
			sess.OrgID, in.FacilityID, in.GivenName, in.MiddleName, in.FamilyName, in.PreferredName, in.Sex, birth, est,
			in.MaritalStatus, in.Occupation, in.Nationality, in.PreferredLanguage, in.Region, in.District, in.Ward, in.Address, in.BloodGroup).Scan(&id)
		if e != nil {
			return Patient{}, e
		}
		if in.Phone != "" {
			if _, e = tx.Exec(ctx, `INSERT INTO patient.contact_point(org_id,patient_id,system,value,is_primary) VALUES($1::uuid,$2::uuid,'phone',$3,true)`, sess.OrgID, id, in.Phone); e != nil {
				return Patient{}, e
			}
		}
		if e = platform.Audit(ctx, tx, "patient.record.create", "phi_access", "success", "patient", id, id, ""); e != nil {
			return Patient{}, e
		}
		return scan(tx.QueryRow(ctx, sel+` WHERE p.id=$1::uuid`, id))
	})
	if e != nil {
		return Patient{}, e
	}
	return p, tx.Commit(ctx)
}

func (s Service) List(ctx context.Context, a identity.Actor, f ListFilter, request string) ([]Patient, error) {
	if !a.Can("patient.read") {
		return nil, platform.Forbidden()
	}
	f.Q = strings.TrimSpace(f.Q)
	f.Phone = strings.TrimSpace(f.Phone)
	if len(f.Q) > 100 || len(f.Phone) > 40 {
		return nil, platform.Fail(422, "validation", "Search text is too long")
	}
	phoneQuery := f.Phone
	if phoneQuery != "" {
		if norm, e := phone(phoneQuery); e == nil {
			phoneQuery = norm
		}
	}
	qPhone := ""
	if f.Q != "" {
		if norm, e := phone(f.Q); e == nil {
			qPhone = norm
		}
	}
	orderClause := ` ORDER BY p.id`
	switch f.Sort {
	case "name":
		orderClause = ` ORDER BY lower(p.last_name), lower(p.first_name), p.id`
	case "newest":
		orderClause = ` ORDER BY p.created_at DESC, p.id`
	}
	cursorAfter := f.After
	if f.Sort != "" && f.Sort != "id" {
		cursorAfter = ""
	}
	tx, _, e := platform.BeginRequest(ctx, s.DB, a.SessionID, request)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	rows, e := tx.Query(ctx, sel+` WHERE p.registered_facility_id=ANY($1::uuid[])
 AND ($2='' OR starts_with(upper(p.mrn),upper($2)) OR strpos(patient.name_key(p.first_name,p.middle_name,p.last_name),lower($2))>0
      OR EXISTS(SELECT 1 FROM patient.contact_point cp WHERE cp.patient_id=p.id AND cp.status='active' AND (position(lower($2) IN lower(cp.value))>0 OR ($9<>'' AND cp.value=$9))))
 AND ($3='' OR EXISTS(SELECT 1 FROM patient.contact_point cp WHERE cp.patient_id=p.id AND cp.status='active' AND position(lower($3) IN lower(cp.value))>0))
 AND ($4='' OR p.date_of_birth=NULLIF($4,'')::date)
 AND ($5='' OR p.registered_facility_id=NULLIF($5,'')::uuid)
 AND ($6='' OR p.status=$6)
 AND ($7='' OR p.id>NULLIF($7,'')::uuid)`+orderClause+` LIMIT $8`,
		a.FacilityIDs, f.Q, phoneQuery, f.BirthDate, f.FacilityID, f.Status, cursorAfter, f.Limit, qPhone)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Patient{}
	for rows.Next() {
		p, e := scan(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, p)
	}
	if e = rows.Err(); e != nil {
		return nil, e
	}
	rows.Close()
	if e = platform.Audit(ctx, tx, "patient.record.search", "phi_access", "success", "patient", "", "", ""); e != nil {
		return nil, e
	}
	return out, tx.Commit(ctx)
}

func (s Service) Get(ctx context.Context, a identity.Actor, id, request string) (Patient, error) {
	if !a.Can("patient.read") {
		return Patient{}, platform.Forbidden()
	}
	tx, _, e := platform.BeginRequest(ctx, s.DB, a.SessionID, request)
	if e != nil {
		return Patient{}, e
	}
	defer tx.Rollback(ctx)
	p, e := scan(tx.QueryRow(ctx, sel+` WHERE p.id=$1::uuid AND p.registered_facility_id=ANY($2::uuid[])`, id, a.FacilityIDs))
	if e != nil {
		return Patient{}, e
	}
	if e = platform.Audit(ctx, tx, "patient.record.view", "phi_access", "success", "patient", id, id, ""); e != nil {
		return Patient{}, e
	}
	return p, tx.Commit(ctx)
}

func (s Service) Update(ctx context.Context, a identity.Actor, id string, in Update, request string) (Patient, error) {
	if !a.Can("patient.update") {
		return Patient{}, platform.Forbidden()
	}
	if e := clean(&in.Demographics); e != nil {
		return Patient{}, e
	}
	birth, est, e := dob(in.Demographics, s.today())
	if e != nil {
		return Patient{}, e
	}
	tx, sess, e := platform.BeginRequest(ctx, s.DB, a.SessionID, request)
	if e != nil {
		return Patient{}, e
	}
	defer tx.Rollback(ctx)
	tag, e := tx.Exec(ctx, `UPDATE patient.patient SET first_name=$2,middle_name=NULLIF($3,''),last_name=$4,preferred_name=NULLIF($5,''),sex=$6,date_of_birth=$7,dob_is_estimated=$8,
 marital_status=NULLIF($9,''),occupation=NULLIF($10,''),nationality=$11,preferred_language=$12,region=NULLIF($13,''),district=NULLIF($14,''),ward=NULLIF($15,''),street_address=NULLIF($16,''),blood_group=NULLIF($17,'')
 WHERE id=$1::uuid AND row_version=$18 AND registered_facility_id=ANY($19::uuid[])`,
		id, in.GivenName, in.MiddleName, in.FamilyName, in.PreferredName, in.Sex, birth, est,
		in.MaritalStatus, in.Occupation, in.Nationality, in.PreferredLanguage, in.Region, in.District, in.Ward, in.Address, in.BloodGroup,
		in.Version, a.FacilityIDs)
	if e != nil {
		return Patient{}, e
	}
	if tag.RowsAffected() != 1 {
		var exists bool
		tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM patient.patient WHERE id=$1::uuid AND registered_facility_id=ANY($2::uuid[]))`, id, a.FacilityIDs).Scan(&exists)
		if !exists {
			return Patient{}, platform.NotFound()
		}
		return Patient{}, platform.Conflict()
	}
	var current string
	e = tx.QueryRow(ctx, `SELECT value FROM patient.contact_point WHERE patient_id=$1::uuid AND system='phone' AND is_primary AND status='active'`, id).Scan(&current)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return Patient{}, e
	}
	if current != in.Phone {
		if current != "" {
			if _, e = tx.Exec(ctx, `UPDATE patient.contact_point SET status='inactive',is_primary=false WHERE patient_id=$1::uuid AND system='phone' AND is_primary AND status='active'`, id); e != nil {
				return Patient{}, e
			}
		}
		if in.Phone != "" {
			if _, e = tx.Exec(ctx, `INSERT INTO patient.contact_point(org_id,patient_id,system,value,is_primary) VALUES($1::uuid,$2::uuid,'phone',$3,true)`, sess.OrgID, id, in.Phone); e != nil {
				return Patient{}, e
			}
		}
	}
	if e = platform.Audit(ctx, tx, "patient.record.update", "phi_access", "success", "patient", id, id, ""); e != nil {
		return Patient{}, e
	}
	p, e := scan(tx.QueryRow(ctx, sel+` WHERE p.id=$1::uuid`, id))
	if e != nil {
		return Patient{}, e
	}
	return p, tx.Commit(ctx)
}

func (s Service) ChangeStatus(ctx context.Context, a identity.Actor, id string, in StatusInput, request string) (Patient, error) {
	if !a.Can("patient.update") {
		return Patient{}, platform.Forbidden()
	}
	var deceasedAt *time.Time
	if in.Status == "deceased" {
		if in.DeceasedAt != nil && *in.DeceasedAt != "" {
			t, e := time.Parse(time.RFC3339, *in.DeceasedAt)
			if e != nil || t.After(time.Now().Add(5*time.Minute)) {
				return Patient{}, platform.Fail(422, "validation", "deceased_at must be a valid non-future RFC3339 timestamp")
			}
			deceasedAt = &t
		} else {
			now := time.Now().UTC()
			deceasedAt = &now
		}
	}
	tx, _, e := platform.BeginRequest(ctx, s.DB, a.SessionID, request)
	if e != nil {
		return Patient{}, e
	}
	defer tx.Rollback(ctx)
	tag, e := tx.Exec(ctx, `UPDATE patient.patient SET status=$3, deceased_at=$4 WHERE id=$1::uuid AND row_version=$2 AND registered_facility_id=ANY($5::uuid[])`,
		id, in.Version, in.Status, deceasedAt, a.FacilityIDs)
	if e != nil {
		return Patient{}, e
	}
	if tag.RowsAffected() != 1 {
		var exists bool
		tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM patient.patient WHERE id=$1::uuid AND registered_facility_id=ANY($2::uuid[]))`, id, a.FacilityIDs).Scan(&exists)
		if !exists {
			return Patient{}, platform.NotFound()
		}
		return Patient{}, platform.Conflict()
	}
	if e = platform.Audit(ctx, tx, "patient.status.update", "phi_access", "success", "patient", id, id, strings.TrimSpace(in.Reason)); e != nil {
		return Patient{}, e
	}
	p, e := scan(tx.QueryRow(ctx, sel+` WHERE p.id=$1::uuid`, id))
	if e != nil {
		return Patient{}, e
	}
	return p, tx.Commit(ctx)
}

// ---------------------------------------------------------------- verification history

type VerificationInput struct {
	Scope        []string `json:"scope" validate:"required,min=1,dive,oneof=identity contacts next_of_kin address insurance portal_link"`
	Method       string   `json:"method" validate:"required,oneof=id_document_seen nida_lookup insurance_card_seen otp_registered_phone patient_stated next_of_kin_confirmed"`
	Outcome      string   `json:"outcome" validate:"required,oneof=confirmed updated discrepancy"`
	Note         string   `json:"note" validate:"max=1000"`
	EncounterID  *string  `json:"encounter_id" validate:"omitempty,uuid"`
	IdentifierID *string  `json:"identifier_id" validate:"omitempty,uuid"`
}
type Verification struct {
	ID           string    `json:"id"`
	PatientID    string    `json:"patient_id"`
	EncounterID  *string   `json:"encounter_id"`
	IdentifierID *string   `json:"identifier_id"`
	Scope        []string  `json:"scope"`
	Method       string    `json:"method"`
	Outcome      string    `json:"outcome"`
	Note         string    `json:"note"`
	VerifiedBy   string    `json:"verified_by"`
	VerifiedAt   time.Time `json:"verified_at"`
}

const verSel = `SELECT id::text,patient_id::text,encounter_id::text,identifier_id::text,scope,method,outcome,coalesce(note,''),verified_by::text,verified_at FROM patient.verification`

func scanVer(r pgx.Row) (Verification, error) {
	var v Verification
	e := r.Scan(&v.ID, &v.PatientID, &v.EncounterID, &v.IdentifierID, &v.Scope, &v.Method, &v.Outcome, &v.Note, &v.VerifiedBy, &v.VerifiedAt)
	return v, e
}

// Verify records that staff confirmed the patient's identity or details (append-only).
func (s Service) Verify(ctx context.Context, a identity.Actor, patient string, in VerificationInput, key, request string) (Verification, error) {
	if !a.Can("patient.create") && !a.Can("patient.update") {
		return Verification{}, platform.Forbidden()
	}
	in.Note = strings.TrimSpace(in.Note)
	if in.Outcome == "discrepancy" && in.Note == "" {
		return Verification{}, platform.Fail(422, "validation", "A note is required when a discrepancy is found")
	}
	if (in.Method == "id_document_seen" || in.Method == "nida_lookup") && in.IdentifierID == nil {
		return Verification{}, platform.Fail(422, "validation", "identifier_id is required for document verification")
	}
	tx, sess, e := platform.BeginRequest(ctx, s.DB, a.SessionID, request)
	if e != nil {
		return Verification{}, e
	}
	defer tx.Rollback(ctx)
	var ok bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM patient.patient WHERE id=$1::uuid AND registered_facility_id=ANY($2::uuid[]))`, patient, a.FacilityIDs).Scan(&ok); e != nil {
		return Verification{}, e
	}
	if !ok {
		return Verification{}, platform.NotFound()
	}
	v, e := platform.Idempotent(ctx, tx, sess, "patient.verify:"+patient, key, in, func() (Verification, error) {
		var id string
		e := tx.QueryRow(ctx, `INSERT INTO patient.verification(org_id,patient_id,encounter_id,identifier_id,scope,method,outcome,note)
 VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5,$6,$7,NULLIF($8,'')) RETURNING id::text`,
			sess.OrgID, patient, in.EncounterID, in.IdentifierID, in.Scope, in.Method, in.Outcome, in.Note).Scan(&id)
		if e != nil {
			return Verification{}, e
		}
		if e = platform.Audit(ctx, tx, "patient.verification.create", "phi_access", "success", "patient", patient, patient, ""); e != nil {
			return Verification{}, e
		}
		return scanVer(tx.QueryRow(ctx, verSel+` WHERE id=$1::uuid`, id))
	})
	if e != nil {
		return Verification{}, e
	}
	return v, tx.Commit(ctx)
}

// Verifications returns the verification history, newest first.
func (s Service) Verifications(ctx context.Context, a identity.Actor, patient, request string) ([]Verification, error) {
	if !a.Can("patient.read") {
		return nil, platform.Forbidden()
	}
	tx, _, e := platform.BeginRequest(ctx, s.DB, a.SessionID, request)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	var ok bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM patient.patient WHERE id=$1::uuid AND registered_facility_id=ANY($2::uuid[]))`, patient, a.FacilityIDs).Scan(&ok); e != nil {
		return nil, e
	}
	if !ok {
		return nil, platform.NotFound()
	}
	rows, e := tx.Query(ctx, verSel+` WHERE patient_id=$1::uuid ORDER BY verified_at DESC LIMIT 100`, patient)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Verification{}
	for rows.Next() {
		v, e := scanVer(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	if e = rows.Err(); e != nil {
		return nil, e
	}
	rows.Close()
	if e = platform.Audit(ctx, tx, "patient.verification.list", "phi_access", "success", "patient", patient, patient, ""); e != nil {
		return nil, e
	}
	return out, tx.Commit(ctx)
}
