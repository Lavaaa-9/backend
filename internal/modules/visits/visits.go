// Package visits covers encounters (visits) and the records that hang off them:
// nursing observations and procedures, plus the billable service list used by procedures.
package visits

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"spartial.local/backend/internal/modules/identity"
	"spartial.local/backend/internal/platform"
)

type Service struct{ DB *pgxpool.Pool }

// ---------------------------------------------------------------- encounters

type EncounterInput struct {
	PatientID      string  `json:"patient_id" validate:"required,uuid"`
	FacilityID     string  `json:"facility_id" validate:"required,uuid"`
	AppointmentID  *string `json:"appointment_id" validate:"omitempty,uuid"`
	EncounterType  string  `json:"encounter_type" validate:"required,oneof=outpatient emergency telemedicine follow_up"`
	ChiefComplaint string  `json:"chief_complaint" validate:"max=500"`
}
type StatusInput struct {
	Status      string `json:"status" validate:"required,oneof=in_progress completed cancelled entered_in_error"`
	Reason      string `json:"reason" validate:"max=500"`
	AllowUnpaid bool   `json:"allow_unpaid"`
	Version     int    `json:"version" validate:"required,min=1"`
}
type Encounter struct {
	ID             string     `json:"id"`
	EncounterNo    string     `json:"encounter_no"`
	PatientID      string     `json:"patient_id"`
	FacilityID     string     `json:"facility_id"`
	AppointmentID  *string    `json:"appointment_id,omitempty"`
	EncounterType  string     `json:"encounter_type"`
	Status         string     `json:"status"`
	ChiefComplaint string     `json:"chief_complaint"`
	ArrivedAt      time.Time  `json:"arrived_at"`
	StartedAt      *time.Time `json:"started_at"`
	ClosedAt       *time.Time `json:"closed_at"`
	Version        int        `json:"version"`
}

const encSel = `SELECT e.id::text,e.encounter_no,e.patient_id::text,e.facility_id::text,e.appointment_id::text,e.encounter_type,e.status,coalesce(e.chief_complaint,''),
 e.arrived_at,e.started_at,e.closed_at,e.row_version FROM clinical.encounter e`

func scanEnc(r pgx.Row) (Encounter, error) {
	var v Encounter
	e := r.Scan(&v.ID, &v.EncounterNo, &v.PatientID, &v.FacilityID, &v.AppointmentID, &v.EncounterType, &v.Status, &v.ChiefComplaint, &v.ArrivedAt, &v.StartedAt, &v.ClosedAt, &v.Version)
	return v, e
}

func canSee(a identity.Actor) bool {
	return a.Can("encounter.read") || a.Can("clinical.read") || a.Can("encounter.manage")
}

func (s Service) CreateEncounter(ctx context.Context, a identity.Actor, in EncounterInput, key, request string) (Encounter, error) {
	if !a.Can("encounter.manage") || !a.InFacility(in.FacilityID) {
		return Encounter{}, platform.Forbidden()
	}
	tx, sess, e := platform.BeginRequest(ctx, s.DB, a.SessionID, request)
	if e != nil {
		return Encounter{}, e
	}
	defer tx.Rollback(ctx)
	v, e := platform.Idempotent(ctx, tx, sess, "encounter.create", key, in, func() (Encounter, error) {
		if in.AppointmentID != nil && *in.AppointmentID != "" {
			var apptStatus string
			e := tx.QueryRow(ctx, `SELECT status FROM clinical.appointment WHERE id=$1::uuid AND patient_id=$2::uuid AND facility_id=$3::uuid`,
				*in.AppointmentID, in.PatientID, in.FacilityID).Scan(&apptStatus)
			if e != nil {
				return Encounter{}, platform.Fail(422, "invalid_appointment", "Appointment not found for this patient and facility")
			}
			if apptStatus != "booked" && apptStatus != "confirmed" && apptStatus != "checked_in" {
				return Encounter{}, platform.Fail(409, "invalid_appointment_status", "Appointment cannot be linked to a new visit in status "+apptStatus)
			}
			if (apptStatus == "booked" || apptStatus == "confirmed") && a.Can("appointment.manage") {
				if _, e = tx.Exec(ctx, `UPDATE clinical.appointment SET status='checked_in', checked_in_at=coalesce(checked_in_at,now()) WHERE id=$1::uuid`, *in.AppointmentID); e != nil {
					return Encounter{}, e
				}
			}
		}
		var id string
		e := tx.QueryRow(ctx, `INSERT INTO clinical.encounter(org_id,facility_id,patient_id,appointment_id,encounter_no,encounter_type,chief_complaint)
 VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,clinical.next_encounter_no($1::uuid),$5,NULLIF($6,'')) RETURNING id::text`,
			sess.OrgID, in.FacilityID, in.PatientID, in.AppointmentID, in.EncounterType, strings.TrimSpace(in.ChiefComplaint)).Scan(&id)
		if e != nil {
			return Encounter{}, e
		}
		if e = platform.Audit(ctx, tx, "encounter.create", "phi_access", "success", "encounter", id, in.PatientID, ""); e != nil {
			return Encounter{}, e
		}
		return scanEnc(tx.QueryRow(ctx, encSel+` WHERE e.id=$1::uuid`, id))
	})
	if e != nil {
		return Encounter{}, e
	}
	return v, tx.Commit(ctx)
}

func (s Service) ListEncounters(ctx context.Context, a identity.Actor, patient, status, after, request string, limit int) ([]Encounter, error) {
	if !canSee(a) {
		return nil, platform.Forbidden()
	}
	tx, _, e := platform.BeginRequest(ctx, s.DB, a.SessionID, request)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	rows, e := tx.Query(ctx, encSel+` WHERE e.facility_id=ANY($1::uuid[]) AND ($2='' OR e.patient_id=NULLIF($2,'')::uuid)
 AND ($3='' OR e.status=$3) AND ($4='' OR e.id>NULLIF($4,'')::uuid) ORDER BY e.id LIMIT $5`, a.FacilityIDs, patient, status, after, limit)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Encounter{}
	for rows.Next() {
		v, e := scanEnc(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	if e = rows.Err(); e != nil {
		return nil, e
	}
	rows.Close()
	if e = platform.Audit(ctx, tx, "encounter.list", "phi_access", "success", "encounter", "", patient, ""); e != nil {
		return nil, e
	}
	return out, tx.Commit(ctx)
}

func (s Service) GetEncounter(ctx context.Context, a identity.Actor, id, request string) (Encounter, error) {
	if !canSee(a) {
		return Encounter{}, platform.Forbidden()
	}
	tx, _, e := platform.BeginRequest(ctx, s.DB, a.SessionID, request)
	if e != nil {
		return Encounter{}, e
	}
	defer tx.Rollback(ctx)
	v, e := scanEnc(tx.QueryRow(ctx, encSel+` WHERE e.id=$1::uuid AND e.facility_id=ANY($2::uuid[])`, id, a.FacilityIDs))
	if e != nil {
		return Encounter{}, e
	}
	if e = platform.Audit(ctx, tx, "encounter.view", "phi_access", "success", "encounter", id, v.PatientID, ""); e != nil {
		return Encounter{}, e
	}
	return v, tx.Commit(ctx)
}

// ChangeStatus moves a visit along registered → in_progress → completed (or cancelled / entered_in_error).
// The database state-transition trigger rejects illegal moves with 409.
func (s Service) ChangeStatus(ctx context.Context, a identity.Actor, id string, in StatusInput, request string) (Encounter, error) {
	if !a.Can("encounter.manage") {
		return Encounter{}, platform.Forbidden()
	}
	reason := strings.TrimSpace(in.Reason)
	if (in.Status == "cancelled" || in.Status == "entered_in_error") && reason == "" {
		return Encounter{}, platform.Fail(422, "validation", "A reason is required")
	}
	if in.Status == "completed" && in.AllowUnpaid && len(reason) < 5 {
		return Encounter{}, platform.Fail(422, "validation", "A reason (min 5 chars) is required when completing a visit with unpaid invoices")
	}
	tx, sess, e := platform.BeginRequest(ctx, s.DB, a.SessionID, request)
	if e != nil {
		return Encounter{}, e
	}
	defer tx.Rollback(ctx)
	if in.Status == "completed" {
		var draftConsults, draftTriages, draftRx, unpaidInvoices, pendingDiag int
		if e = tx.QueryRow(ctx, `SELECT draft_consultations,draft_triages,draft_prescriptions,unpaid_invoices,pending_diagnostics FROM clinical.encounter_closure_blockers($1::uuid)`, id).
			Scan(&draftConsults, &draftTriages, &draftRx, &unpaidInvoices, &pendingDiag); e != nil {
			return Encounter{}, e
		}
		switch {
		case draftConsults > 0:
			return Encounter{}, platform.Fail(409, "open_draft_consultation", "Cannot complete visit while consultation notes are still in draft; sign or void them first")
		case draftTriages > 0:
			return Encounter{}, platform.Fail(409, "open_draft_triage", "Cannot complete visit while triage assessments are still in draft; finalise or void them first")
		case draftRx > 0:
			return Encounter{}, platform.Fail(409, "open_draft_prescription", "Cannot complete visit while prescriptions are still in draft; sign or cancel them first")
		case unpaidInvoices > 0 && !in.AllowUnpaid:
			return Encounter{}, platform.Fail(409, "unpaid_invoice", "Cannot complete visit with unsettled invoices; settle payment or supply allow_unpaid with a reason")
		}
	}
	tag, e := tx.Exec(ctx, `UPDATE clinical.encounter SET status=$2,
 started_at=CASE WHEN $2='in_progress' THEN now() ELSE started_at END,
 closed_at=CASE WHEN $2 IN ('completed','cancelled','entered_in_error') THEN greatest(now(),arrived_at) END,
 closed_by=CASE WHEN $2 IN ('completed','cancelled','entered_in_error') THEN $5::uuid END,
 cancel_reason=CASE WHEN $2 IN ('cancelled','entered_in_error') THEN $3 ELSE cancel_reason END
 WHERE id=$1::uuid AND row_version=$4 AND facility_id=ANY($6::uuid[])`, id, in.Status, reason, in.Version, sess.UserID, a.FacilityIDs)
	if e != nil {
		return Encounter{}, e
	}
	if tag.RowsAffected() != 1 {
		var exists bool
		tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM clinical.encounter WHERE id=$1::uuid AND facility_id=ANY($2::uuid[]))`, id, a.FacilityIDs).Scan(&exists)
		if !exists {
			return Encounter{}, platform.NotFound()
		}
		return Encounter{}, platform.Conflict()
	}
	v, e := scanEnc(tx.QueryRow(ctx, encSel+` WHERE e.id=$1::uuid`, id))
	if e != nil {
		return Encounter{}, e
	}
	if in.Status == "completed" && v.AppointmentID != nil && *v.AppointmentID != "" && a.Can("appointment.manage") {
		if _, e = tx.Exec(ctx, `UPDATE clinical.appointment SET status='completed' WHERE id=$1::uuid AND status='checked_in'`, *v.AppointmentID); e != nil {
			return Encounter{}, e
		}
	}
	if e = platform.Audit(ctx, tx, "encounter.status."+in.Status, "phi_access", "success", "encounter", id, v.PatientID, reason); e != nil {
		return Encounter{}, e
	}
	return v, tx.Commit(ctx)
}

// encounterPatient resolves the patient of a visit the actor may access.
func encounterPatient(ctx context.Context, tx pgx.Tx, a identity.Actor, id string) (string, error) {
	var p string
	e := tx.QueryRow(ctx, `SELECT patient_id::text FROM clinical.encounter WHERE id=$1::uuid AND facility_id=ANY($2::uuid[])`, id, a.FacilityIDs).Scan(&p)
	return p, e
}

// ---------------------------------------------------------------- nursing observations

type NursingInput struct {
	ObservationType string  `json:"observation_type" validate:"required,oneof=general pain_reassessment wound medication_administered intake_output fall_risk patient_education handover other"`
	Note            string  `json:"note" validate:"required,min=2,max=4000"`
	ObservedAt      *string `json:"observed_at" validate:"omitempty,datetime=2006-01-02T15:04:05Z07:00"`
}
type ErrorInput struct {
	Reason  string `json:"reason" validate:"required,min=2,max=500"`
	Version int    `json:"version" validate:"required,min=1"`
}
type Nursing struct {
	ID              string    `json:"id"`
	EncounterID     string    `json:"encounter_id"`
	PatientID       string    `json:"patient_id"`
	RecordedBy      string    `json:"recorded_by_membership_id"`
	ObservedAt      time.Time `json:"observed_at"`
	ObservationType string    `json:"observation_type"`
	Note            string    `json:"note"`
	Status          string    `json:"status"`
	Version         int       `json:"version"`
}

const nurSel = `SELECT id::text,encounter_id::text,patient_id::text,recorded_by_membership_id::text,observed_at,observation_type,note,status,row_version FROM clinical.nursing_observation`

func scanNur(r pgx.Row) (Nursing, error) {
	var v Nursing
	e := r.Scan(&v.ID, &v.EncounterID, &v.PatientID, &v.RecordedBy, &v.ObservedAt, &v.ObservationType, &v.Note, &v.Status, &v.Version)
	return v, e
}

func (s Service) RecordNursing(ctx context.Context, a identity.Actor, encounter string, in NursingInput, key, request string) (Nursing, error) {
	if !a.Can("nursing.write") {
		return Nursing{}, platform.Forbidden()
	}
	in.Note = strings.TrimSpace(in.Note)
	var observed *time.Time
	if in.ObservedAt != nil {
		t, e := time.Parse(time.RFC3339, *in.ObservedAt)
		if e != nil || t.After(time.Now().Add(5*time.Minute)) {
			return Nursing{}, platform.Fail(422, "validation", "observed_at cannot be in the future")
		}
		observed = &t
	}
	tx, sess, e := platform.BeginRequest(ctx, s.DB, a.SessionID, request)
	if e != nil {
		return Nursing{}, e
	}
	defer tx.Rollback(ctx)
	patient, e := encounterPatient(ctx, tx, a, encounter)
	if e != nil {
		return Nursing{}, e
	}
	v, e := platform.Idempotent(ctx, tx, sess, "nursing.record:"+encounter, key, in, func() (Nursing, error) {
		var id string
		e := tx.QueryRow(ctx, `INSERT INTO clinical.nursing_observation(org_id,encounter_id,patient_id,recorded_by_membership_id,observed_at,observation_type,note)
 VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,coalesce($5::timestamptz,now()),$6,$7) RETURNING id::text`,
			sess.OrgID, encounter, patient, sess.MembershipID, observed, in.ObservationType, in.Note).Scan(&id)
		if e != nil {
			return Nursing{}, e
		}
		if e = platform.Audit(ctx, tx, "nursing.observation.create", "phi_access", "success", "nursing_observation", id, patient, ""); e != nil {
			return Nursing{}, e
		}
		return scanNur(tx.QueryRow(ctx, nurSel+` WHERE id=$1::uuid`, id))
	})
	if e != nil {
		return Nursing{}, e
	}
	return v, tx.Commit(ctx)
}

func (s Service) ListNursing(ctx context.Context, a identity.Actor, encounter, request string) ([]Nursing, error) {
	if !a.Can("clinical.read") && !a.Can("nursing.write") {
		return nil, platform.Forbidden()
	}
	tx, _, e := platform.BeginRequest(ctx, s.DB, a.SessionID, request)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	patient, e := encounterPatient(ctx, tx, a, encounter)
	if e != nil {
		return nil, e
	}
	rows, e := tx.Query(ctx, nurSel+` WHERE encounter_id=$1::uuid ORDER BY observed_at,id`, encounter)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Nursing{}
	for rows.Next() {
		v, e := scanNur(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	if e = rows.Err(); e != nil {
		return nil, e
	}
	rows.Close()
	if e = platform.Audit(ctx, tx, "nursing.observation.list", "phi_access", "success", "encounter", encounter, patient, ""); e != nil {
		return nil, e
	}
	return out, tx.Commit(ctx)
}

func (s Service) VoidNursing(ctx context.Context, a identity.Actor, id string, in ErrorInput, request string) (Nursing, error) {
	if !a.Can("nursing.write") {
		return Nursing{}, platform.Forbidden()
	}
	tx, _, e := platform.BeginRequest(ctx, s.DB, a.SessionID, request)
	if e != nil {
		return Nursing{}, e
	}
	defer tx.Rollback(ctx)
	tag, e := tx.Exec(ctx, `UPDATE clinical.nursing_observation SET status='entered_in_error',error_reason=$2 WHERE id=$1::uuid AND row_version=$3`, id, strings.TrimSpace(in.Reason), in.Version)
	if e != nil {
		return Nursing{}, e
	}
	if tag.RowsAffected() != 1 {
		var exists bool
		tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM clinical.nursing_observation WHERE id=$1::uuid)`, id).Scan(&exists)
		if !exists {
			return Nursing{}, platform.NotFound()
		}
		return Nursing{}, platform.Conflict()
	}
	v, e := scanNur(tx.QueryRow(ctx, nurSel+` WHERE id=$1::uuid`, id))
	if e != nil {
		return Nursing{}, e
	}
	if e = platform.Audit(ctx, tx, "nursing.observation.void", "phi_access", "success", "nursing_observation", id, v.PatientID, in.Reason); e != nil {
		return Nursing{}, e
	}
	return v, tx.Commit(ctx)
}

// ---------------------------------------------------------------- procedures

type ProcedureInput struct {
	Description   string  `json:"description" validate:"required,min=2,max=500"`
	ProcedureCode string  `json:"procedure_code" validate:"max=40"`
	ServiceID     *string `json:"service_id" validate:"omitempty,uuid"`
	Quantity      float64 `json:"quantity" validate:"omitempty,gt=0,lte=9999"`
	Status        string  `json:"status" validate:"required,oneof=planned performed"`
	OutcomeNote   string  `json:"outcome_note" validate:"max=4000"`
}
type ProcedureAction struct {
	Action      string `json:"action" validate:"required,oneof=perform cancel void"`
	Reason      string `json:"reason" validate:"max=500"`
	OutcomeNote string `json:"outcome_note" validate:"max=4000"`
	Version     int    `json:"version" validate:"required,min=1"`
}
type Procedure struct {
	ID            string     `json:"id"`
	EncounterID   string     `json:"encounter_id"`
	PatientID     string     `json:"patient_id"`
	ServiceID     *string    `json:"service_id"`
	ProcedureCode string     `json:"procedure_code"`
	Description   string     `json:"description"`
	Quantity      float64    `json:"quantity"`
	Status        string     `json:"status"`
	PerformedAt   *time.Time `json:"performed_at"`
	PerformedBy   *string    `json:"performed_by_membership_id"`
	OutcomeNote   string     `json:"outcome_note"`
	Version       int        `json:"version"`
}

const procSel = `SELECT id::text,encounter_id::text,patient_id::text,service_id::text,coalesce(procedure_code,''),description,quantity::float8,status,
 performed_at,performed_by_membership_id::text,coalesce(outcome_note,''),row_version FROM clinical.procedure`

func scanProc(r pgx.Row) (Procedure, error) {
	var v Procedure
	e := r.Scan(&v.ID, &v.EncounterID, &v.PatientID, &v.ServiceID, &v.ProcedureCode, &v.Description, &v.Quantity, &v.Status, &v.PerformedAt, &v.PerformedBy, &v.OutcomeNote, &v.Version)
	return v, e
}

func canProc(a identity.Actor) bool { return a.Can("procedure.record") || a.Can("consultation.write") }

func (s Service) RecordProcedure(ctx context.Context, a identity.Actor, encounter string, in ProcedureInput, key, request string) (Procedure, error) {
	if !canProc(a) {
		return Procedure{}, platform.Forbidden()
	}
	if in.Quantity == 0 {
		in.Quantity = 1
	}
	in.Description, in.ProcedureCode = strings.TrimSpace(in.Description), strings.TrimSpace(in.ProcedureCode)
	tx, sess, e := platform.BeginRequest(ctx, s.DB, a.SessionID, request)
	if e != nil {
		return Procedure{}, e
	}
	defer tx.Rollback(ctx)
	patient, e := encounterPatient(ctx, tx, a, encounter)
	if e != nil {
		return Procedure{}, e
	}
	v, e := platform.Idempotent(ctx, tx, sess, "procedure.record:"+encounter, key, in, func() (Procedure, error) {
		var id string
		e := tx.QueryRow(ctx, `INSERT INTO clinical.procedure(org_id,encounter_id,patient_id,service_id,procedure_code,description,quantity,status,ordered_by_membership_id,outcome_note)
 VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,NULLIF($5,''),$6,$7,$8,$9::uuid,NULLIF($10,'')) RETURNING id::text`,
			sess.OrgID, encounter, patient, in.ServiceID, in.ProcedureCode, in.Description, in.Quantity, in.Status, sess.MembershipID, in.OutcomeNote).Scan(&id)
		if e != nil {
			return Procedure{}, e
		}
		if e = platform.Audit(ctx, tx, "procedure.create", "phi_access", "success", "procedure", id, patient, ""); e != nil {
			return Procedure{}, e
		}
		return scanProc(tx.QueryRow(ctx, procSel+` WHERE id=$1::uuid`, id))
	})
	if e != nil {
		return Procedure{}, e
	}
	return v, tx.Commit(ctx)
}

func (s Service) ListProcedures(ctx context.Context, a identity.Actor, encounter, request string) ([]Procedure, error) {
	if !a.Can("clinical.read") && !a.Can("billing.invoice.manage") && !canProc(a) {
		return nil, platform.Forbidden()
	}
	tx, _, e := platform.BeginRequest(ctx, s.DB, a.SessionID, request)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	patient, e := encounterPatient(ctx, tx, a, encounter)
	if e != nil {
		return nil, e
	}
	rows, e := tx.Query(ctx, procSel+` WHERE encounter_id=$1::uuid ORDER BY created_at,id`, encounter)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Procedure{}
	for rows.Next() {
		v, e := scanProc(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	if e = rows.Err(); e != nil {
		return nil, e
	}
	rows.Close()
	if e = platform.Audit(ctx, tx, "procedure.list", "phi_access", "success", "encounter", encounter, patient, ""); e != nil {
		return nil, e
	}
	return out, tx.Commit(ctx)
}

// ActOnProcedure performs a planned procedure, cancels it, or voids a performed one.
func (s Service) ActOnProcedure(ctx context.Context, a identity.Actor, id string, in ProcedureAction, request string) (Procedure, error) {
	if !canProc(a) {
		return Procedure{}, platform.Forbidden()
	}
	reason := strings.TrimSpace(in.Reason)
	if (in.Action == "cancel" || in.Action == "void") && reason == "" {
		return Procedure{}, platform.Fail(422, "validation", "A reason is required")
	}
	tx, _, e := platform.BeginRequest(ctx, s.DB, a.SessionID, request)
	if e != nil {
		return Procedure{}, e
	}
	defer tx.Rollback(ctx)
	var tag pgconn.CommandTag
	switch in.Action {
	case "perform":
		tag, e = tx.Exec(ctx, `UPDATE clinical.procedure SET status='performed',outcome_note=coalesce(NULLIF($2::text,''),outcome_note) WHERE id=$1::uuid AND row_version=$3`, id, in.OutcomeNote, in.Version)
	case "cancel":
		tag, e = tx.Exec(ctx, `UPDATE clinical.procedure SET status='cancelled',cancel_reason=$2::text WHERE id=$1::uuid AND row_version=$3`, id, reason, in.Version)
	default:
		tag, e = tx.Exec(ctx, `UPDATE clinical.procedure SET status='entered_in_error',error_reason=$2::text WHERE id=$1::uuid AND row_version=$3`, id, reason, in.Version)
	}
	if e != nil {
		return Procedure{}, e
	}
	if tag.RowsAffected() != 1 {
		var exists bool
		tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM clinical.procedure WHERE id=$1::uuid)`, id).Scan(&exists)
		if !exists {
			return Procedure{}, platform.NotFound()
		}
		return Procedure{}, platform.Conflict()
	}
	v, e := scanProc(tx.QueryRow(ctx, procSel+` WHERE id=$1::uuid`, id))
	if e != nil {
		return Procedure{}, e
	}
	if e = platform.Audit(ctx, tx, "procedure."+in.Action, "phi_access", "success", "procedure", id, v.PatientID, reason); e != nil {
		return Procedure{}, e
	}
	return v, tx.Commit(ctx)
}

// ---------------------------------------------------------------- billable services

type CatalogService struct {
	ID       string `json:"id"`
	Code     string `json:"code"`
	Name     string `json:"name"`
	Category string `json:"category"`
}

func (s Service) Catalog(ctx context.Context, a identity.Actor, category, request string) ([]CatalogService, error) {
	tx, _, e := platform.BeginRequest(ctx, s.DB, a.SessionID, request)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	rows, e := tx.Query(ctx, `SELECT id::text,code,name,category FROM billing.service_catalog WHERE status='active' AND ($1='' OR category=$1) ORDER BY name LIMIT 200`, category)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []CatalogService{}
	for rows.Next() {
		var v CatalogService
		if e = rows.Scan(&v.ID, &v.Code, &v.Name, &v.Category); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	if e = rows.Err(); e != nil {
		return nil, e
	}
	rows.Close()
	return out, tx.Commit(ctx)
}
