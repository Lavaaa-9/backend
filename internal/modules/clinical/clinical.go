// Package clinical covers triage, consultations, diagnoses, follow-up plans and the queue.
// Responses are JSON documents produced by the database (column names are the API names).
package clinical

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"spartial.local/backend/internal/modules/identity"
	"spartial.local/backend/internal/platform"
)

type Service struct{ DB *pgxpool.Pool }

type Raw = platform.Raw

var (
	triageJ  = platform.JSON("t", true, "supersedes_id")
	consultJ = platform.JSON("c", true)
	dxJ      = platform.JSON("d", true)
	fuJ      = platform.JSON("f", true)
	queueJ   = platform.JSON("q", true, "priority_rank")
)

// ---------------------------------------------------------------- triage

type TriageInput struct {
	MeasuredAt       *string  `json:"measured_at" validate:"omitempty,datetime=2006-01-02T15:04:05Z07:00"`
	TemperatureC     *float64 `json:"temperature_c" validate:"omitempty,min=25,max=45"`
	PulseBPM         *int     `json:"pulse_bpm" validate:"omitempty,min=20,max=300"`
	RespiratoryRate  *int     `json:"respiratory_rate" validate:"omitempty,min=4,max=80"`
	SystolicMMHG     *int     `json:"systolic_mmhg" validate:"omitempty,min=40,max=300"`
	DiastolicMMHG    *int     `json:"diastolic_mmhg" validate:"omitempty,min=20,max=200"`
	SpO2Pct          *int     `json:"spo2_pct" validate:"omitempty,min=50,max=100"`
	WeightKg         *float64 `json:"weight_kg" validate:"omitempty,min=0.3,max=400"`
	HeightCm         *float64 `json:"height_cm" validate:"omitempty,min=20,max=260"`
	MUACCm           *float64 `json:"muac_cm" validate:"omitempty,min=5,max=60"`
	BloodGlucoseMMOL *float64 `json:"blood_glucose_mmol" validate:"omitempty,min=0.5,max=60"`
	PainScore        *int     `json:"pain_score" validate:"omitempty,min=0,max=10"`
	TriageCategory   *string  `json:"triage_category" validate:"omitempty,oneof=emergency priority queue"`
	Notes            string   `json:"notes" validate:"max=4000"`
	Finalise         bool     `json:"finalise"`
}

func (s Service) RecordTriage(ctx context.Context, a identity.Actor, encounter string, in TriageInput, key, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "triage.write"); e != nil {
		return nil, e
	}
	var measured *time.Time
	if in.MeasuredAt != nil {
		t, e := time.Parse(time.RFC3339, *in.MeasuredAt)
		if e != nil || t.After(time.Now().Add(5*time.Minute)) {
			return nil, platform.Fail(422, "validation", "measured_at cannot be in the future")
		}
		measured = &t
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, sess platform.Session) (Raw, error) {
		patient, _, e := platform.Encounter(ctx, tx, a.FacilityIDs, encounter)
		if e != nil {
			return nil, e
		}
		return platform.Idempotent(ctx, tx, sess, "triage.record:"+encounter, key, in, func() (Raw, error) {
			status := "draft"
			if in.Finalise {
				status = "final"
			}
			var id string
			e := tx.QueryRow(ctx, `INSERT INTO clinical.triage_assessment(org_id,encounter_id,patient_id,recorded_by_membership_id,measured_at,temperature_c,pulse_bpm,respiratory_rate,
 systolic_mmhg,diastolic_mmhg,spo2_pct,weight_kg,height_cm,muac_cm,blood_glucose_mmol,pain_score,triage_category,notes,status,finalised_at)
 VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,coalesce($5::timestamptz,now()),$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,NULLIF($18,''),$19,CASE WHEN $19='final' THEN now() END) RETURNING id::text`,
				sess.OrgID, encounter, patient, sess.MembershipID, measured, in.TemperatureC, in.PulseBPM, in.RespiratoryRate, in.SystolicMMHG, in.DiastolicMMHG, in.SpO2Pct,
				in.WeightKg, in.HeightCm, in.MUACCm, in.BloodGlucoseMMOL, in.PainScore, in.TriageCategory, strings.TrimSpace(in.Notes), status).Scan(&id)
			if e != nil {
				return nil, e
			}
			if e = platform.Audit(ctx, tx, "triage.create", "phi_access", "success", "triage", id, patient, ""); e != nil {
				return nil, e
			}
			return platform.One(ctx, tx, `SELECT `+triageJ+` FROM clinical.triage_assessment t WHERE id=$1::uuid`, id)
		})
	})
}

func (s Service) ListTriage(ctx context.Context, a identity.Actor, encounter, request string) ([]Raw, error) {
	if e := platform.Need(a.Permissions, "clinical.read"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) ([]Raw, error) {
		patient, _, e := platform.Encounter(ctx, tx, a.FacilityIDs, encounter)
		if e != nil {
			return nil, e
		}
		out, e := platform.Many(ctx, tx, `SELECT `+triageJ+` FROM clinical.triage_assessment t WHERE encounter_id=$1::uuid ORDER BY measured_at DESC`, encounter)
		if e != nil {
			return nil, e
		}
		return out, platform.Audit(ctx, tx, "triage.list", "phi_access", "success", "encounter", encounter, patient, "")
	})
}

type VersionInput struct {
	Version int `json:"version" validate:"required,min=1"`
}
type VoidInput struct {
	Reason  string `json:"reason" validate:"required,min=2,max=500"`
	Version int    `json:"version" validate:"required,min=1"`
}

// FinaliseTriage turns a draft into the final record (needs a category and at least two measurements).
func (s Service) FinaliseTriage(ctx context.Context, a identity.Actor, id string, in VersionInput, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "triage.write"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) (Raw, error) {
		tag, e := tx.Exec(ctx, `UPDATE clinical.triage_assessment SET status='final',finalised_at=now() WHERE id=$1::uuid AND row_version=$2 AND status='draft'`, id, in.Version)
		if e != nil {
			return nil, e
		}
		if e = platform.Changed(ctx, tx, tag, "clinical.triage_assessment", id); e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, `SELECT `+triageJ+` FROM clinical.triage_assessment t WHERE id=$1::uuid`, id)
	})
}

func (s Service) VoidTriage(ctx context.Context, a identity.Actor, id string, in VoidInput, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "triage.write"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) (Raw, error) {
		tag, e := tx.Exec(ctx, `UPDATE clinical.triage_assessment SET status='entered_in_error',error_reason=$3 WHERE id=$1::uuid AND row_version=$2`, id, in.Version, strings.TrimSpace(in.Reason))
		if e != nil {
			return nil, e
		}
		if e = platform.Changed(ctx, tx, tag, "clinical.triage_assessment", id); e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, `SELECT `+triageJ+` FROM clinical.triage_assessment t WHERE id=$1::uuid`, id)
	})
}

// ---------------------------------------------------------------- consultations

type ConsultationInput struct {
	PresentingComplaint   *string `json:"presenting_complaint" validate:"omitempty,max=4000"`
	HistoryPresentIllness *string `json:"history_present_illness" validate:"omitempty,max=8000"`
	ReviewOfSystems       *string `json:"review_of_systems" validate:"omitempty,max=8000"`
	ExaminationFindings   *string `json:"examination_findings" validate:"omitempty,max=8000"`
	Assessment            *string `json:"assessment" validate:"omitempty,max=8000"`
	TreatmentPlan         *string `json:"treatment_plan" validate:"omitempty,max=8000"`
	FollowUpInstructions  *string `json:"follow_up_instructions" validate:"omitempty,max=4000"`
}
type ConsultationUpdate struct {
	ConsultationInput
	Version int `json:"version" validate:"required,min=1"`
}

// CreateConsultation opens a draft note authored by the signed-in practitioner.
func (s Service) CreateConsultation(ctx context.Context, a identity.Actor, encounter string, in ConsultationInput, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "consultation.write"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, sess platform.Session) (Raw, error) {
		patient, _, e := platform.Encounter(ctx, tx, a.FacilityIDs, encounter)
		if e != nil {
			return nil, e
		}
		var id string
		e = tx.QueryRow(ctx, `INSERT INTO clinical.consultation(org_id,encounter_id,patient_id,practitioner_membership_id,presenting_complaint,history_present_illness,review_of_systems,examination_findings,assessment,treatment_plan,follow_up_instructions)
 VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5,$6,$7,$8,$9,$10,$11) RETURNING id::text`, sess.OrgID, encounter, patient, sess.MembershipID,
			in.PresentingComplaint, in.HistoryPresentIllness, in.ReviewOfSystems, in.ExaminationFindings, in.Assessment, in.TreatmentPlan, in.FollowUpInstructions).Scan(&id)
		if e != nil {
			return nil, e
		}
		if e = platform.Audit(ctx, tx, "consultation.create", "phi_access", "success", "consultation", id, patient, ""); e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, `SELECT `+consultJ+` FROM clinical.consultation c WHERE id=$1::uuid`, id)
	})
}

// UpdateConsultation edits a draft (signed notes are immutable in the database).
func (s Service) UpdateConsultation(ctx context.Context, a identity.Actor, id string, in ConsultationUpdate, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "consultation.write"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, sess platform.Session) (Raw, error) {
		tag, e := tx.Exec(ctx, `UPDATE clinical.consultation SET presenting_complaint=$3,history_present_illness=$4,review_of_systems=$5,examination_findings=$6,assessment=$7,treatment_plan=$8,follow_up_instructions=$9
 WHERE id=$1::uuid AND row_version=$2 AND status='draft' AND practitioner_membership_id=$10::uuid`, id, in.Version, in.PresentingComplaint, in.HistoryPresentIllness, in.ReviewOfSystems,
			in.ExaminationFindings, in.Assessment, in.TreatmentPlan, in.FollowUpInstructions, sess.MembershipID)
		if e != nil {
			return nil, e
		}
		if e = platform.Changed(ctx, tx, tag, "clinical.consultation", id); e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, `SELECT `+consultJ+` FROM clinical.consultation c WHERE id=$1::uuid`, id)
	})
}

// SignConsultation signs the author's own draft; it needs complaint, findings, assessment and plan.
func (s Service) SignConsultation(ctx context.Context, a identity.Actor, id string, in VersionInput, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "consultation.write"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) (Raw, error) {
		tag, e := tx.Exec(ctx, `UPDATE clinical.consultation SET status='signed' WHERE id=$1::uuid AND row_version=$2 AND status='draft'`, id, in.Version)
		if e != nil {
			return nil, e
		}
		if e = platform.Changed(ctx, tx, tag, "clinical.consultation", id); e != nil {
			return nil, e
		}
		var patient string
		tx.QueryRow(ctx, `SELECT patient_id::text FROM clinical.consultation WHERE id=$1::uuid`, id).Scan(&patient)
		if e = platform.Audit(ctx, tx, "consultation.sign", "phi_access", "success", "consultation", id, patient, ""); e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, `SELECT `+consultJ+` FROM clinical.consultation c WHERE id=$1::uuid`, id)
	})
}

func (s Service) VoidConsultation(ctx context.Context, a identity.Actor, id string, in VoidInput, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "consultation.write"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) (Raw, error) {
		tag, e := tx.Exec(ctx, `UPDATE clinical.consultation SET status='entered_in_error',error_reason=$3 WHERE id=$1::uuid AND row_version=$2`, id, in.Version, strings.TrimSpace(in.Reason))
		if e != nil {
			return nil, e
		}
		if e = platform.Changed(ctx, tx, tag, "clinical.consultation", id); e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, `SELECT `+consultJ+` FROM clinical.consultation c WHERE id=$1::uuid`, id)
	})
}

func (s Service) ListConsultations(ctx context.Context, a identity.Actor, encounter, request string) ([]Raw, error) {
	if e := platform.Need(a.Permissions, "clinical.read"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) ([]Raw, error) {
		patient, _, e := platform.Encounter(ctx, tx, a.FacilityIDs, encounter)
		if e != nil {
			return nil, e
		}
		out, e := platform.Many(ctx, tx, `SELECT `+consultJ+` FROM clinical.consultation c WHERE encounter_id=$1::uuid ORDER BY created_at`, encounter)
		if e != nil {
			return nil, e
		}
		return out, platform.Audit(ctx, tx, "consultation.list", "phi_access", "success", "encounter", encounter, patient, "")
	})
}

// ---------------------------------------------------------------- diagnoses

type DiagnosisInput struct {
	ICD10Code      *string `json:"icd10_code" validate:"omitempty,max=20"`
	Description    string  `json:"description" validate:"required,min=2,max=500"`
	DiagnosisType  string  `json:"diagnosis_type" validate:"required,oneof=provisional final differential"`
	IsPrimary      bool    `json:"is_primary"`
	ConsultationID *string `json:"consultation_id" validate:"omitempty,uuid"`
}

func (s Service) AddDiagnosis(ctx context.Context, a identity.Actor, encounter string, in DiagnosisInput, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "consultation.write"); e != nil {
		return nil, e
	}
	if in.DiagnosisType == "final" && (in.ICD10Code == nil || *in.ICD10Code == "") {
		return nil, platform.Fail(422, "validation", "A final diagnosis needs an ICD-10 code")
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, sess platform.Session) (Raw, error) {
		patient, _, e := platform.Encounter(ctx, tx, a.FacilityIDs, encounter)
		if e != nil {
			return nil, e
		}
		var id string
		e = tx.QueryRow(ctx, `INSERT INTO clinical.encounter_diagnosis(org_id,encounter_id,patient_id,consultation_id,icd10_code,description,diagnosis_type,is_primary,recorded_by_membership_id)
 VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,NULLIF($5,''),$6,$7,$8,$9::uuid) RETURNING id::text`, sess.OrgID, encounter, patient, in.ConsultationID, deref(in.ICD10Code),
			strings.TrimSpace(in.Description), in.DiagnosisType, in.IsPrimary, sess.MembershipID).Scan(&id)
		if e != nil {
			return nil, e
		}
		if e = platform.Audit(ctx, tx, "diagnosis.create", "phi_access", "success", "diagnosis", id, patient, ""); e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, `SELECT `+dxJ+` FROM clinical.encounter_diagnosis d WHERE id=$1::uuid`, id)
	})
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return strings.TrimSpace(*p)
}

func (s Service) ListDiagnoses(ctx context.Context, a identity.Actor, encounter, request string) ([]Raw, error) {
	if e := platform.Need(a.Permissions, "clinical.read"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) ([]Raw, error) {
		if _, _, e := platform.Encounter(ctx, tx, a.FacilityIDs, encounter); e != nil {
			return nil, e
		}
		return platform.Many(ctx, tx, `SELECT `+dxJ+` FROM clinical.encounter_diagnosis d WHERE encounter_id=$1::uuid ORDER BY is_primary DESC, created_at`, encounter)
	})
}

func (s Service) VoidDiagnosis(ctx context.Context, a identity.Actor, id string, in VoidInput, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "consultation.write"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) (Raw, error) {
		tag, e := tx.Exec(ctx, `UPDATE clinical.encounter_diagnosis SET status='entered_in_error',error_reason=$3,is_primary=false WHERE id=$1::uuid AND row_version=$2`, id, in.Version, strings.TrimSpace(in.Reason))
		if e != nil {
			return nil, e
		}
		if e = platform.Changed(ctx, tx, tag, "clinical.encounter_diagnosis", id); e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, `SELECT `+dxJ+` FROM clinical.encounter_diagnosis d WHERE id=$1::uuid`, id)
	})
}

// ICD10 searches the reference code list by code prefix or title text.
func (s Service) ICD10(ctx context.Context, a identity.Actor, q, request string) ([]Raw, error) {
	q = strings.TrimSpace(q)
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) ([]Raw, error) {
		return platform.Many(ctx, tx, `SELECT jsonb_build_object('code',code,'title',title,'chapter',chapter,'is_billable',is_billable) FROM ref.icd10_code
 WHERE status='active' AND ($1='' OR starts_with(upper(code),upper($1)) OR strpos(lower(title),lower($1))>0) ORDER BY code LIMIT 50`, q)
	})
}

// ---------------------------------------------------------------- follow-up plans

type FollowUpInput struct {
	DueDate      string  `json:"due_date" validate:"required,datetime=2006-01-02"`
	Reason       string  `json:"reason" validate:"required,min=2,max=500"`
	DepartmentID *string `json:"department_id" validate:"omitempty,uuid"`
}

func (s Service) AddFollowUp(ctx context.Context, a identity.Actor, encounter string, in FollowUpInput, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "consultation.write", "appointment.manage"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, sess platform.Session) (Raw, error) {
		patient, _, e := platform.Encounter(ctx, tx, a.FacilityIDs, encounter)
		if e != nil {
			return nil, e
		}
		var id string
		e = tx.QueryRow(ctx, `INSERT INTO clinical.follow_up_plan(org_id,encounter_id,patient_id,department_id,practitioner_membership_id,due_date,reason)
 VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,NULLIF($5,'')::uuid,$6::date,$7) RETURNING id::text`, sess.OrgID, encounter, patient, in.DepartmentID, sess.MembershipID, in.DueDate, strings.TrimSpace(in.Reason)).Scan(&id)
		if e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, `SELECT `+fuJ+` FROM clinical.follow_up_plan f WHERE id=$1::uuid`, id)
	})
}

func (s Service) ListFollowUps(ctx context.Context, a identity.Actor, encounter, request string) ([]Raw, error) {
	if e := platform.Need(a.Permissions, "clinical.read", "appointment.read"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) ([]Raw, error) {
		if _, _, e := platform.Encounter(ctx, tx, a.FacilityIDs, encounter); e != nil {
			return nil, e
		}
		return platform.Many(ctx, tx, `SELECT `+fuJ+` FROM clinical.follow_up_plan f WHERE encounter_id=$1::uuid ORDER BY due_date`, encounter)
	})
}

func (s Service) CancelFollowUp(ctx context.Context, a identity.Actor, id string, in VersionInput, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "consultation.write", "appointment.manage"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) (Raw, error) {
		tag, e := tx.Exec(ctx, `UPDATE clinical.follow_up_plan SET status='cancelled' WHERE id=$1::uuid AND row_version=$2 AND status='planned'`, id, in.Version)
		if e != nil {
			return nil, e
		}
		if e = platform.Changed(ctx, tx, tag, "clinical.follow_up_plan", id); e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, `SELECT `+fuJ+` FROM clinical.follow_up_plan f WHERE id=$1::uuid`, id)
	})
}

// ---------------------------------------------------------------- departments and queue

func (s Service) Departments(ctx context.Context, a identity.Actor, request string) ([]Raw, error) {
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) ([]Raw, error) {
		return platform.Many(ctx, tx, `SELECT `+platform.JSON("d", true)+` FROM org.department d WHERE status='active' AND facility_id=ANY($1::uuid[]) ORDER BY name`, a.FacilityIDs)
	})
}

type QueueInput struct {
	EncounterID    string  `json:"encounter_id" validate:"required,uuid"`
	DepartmentID   string  `json:"department_id" validate:"required,uuid"`
	Priority       string  `json:"priority" validate:"omitempty,oneof=emergency urgent routine"`
	ServicePointID *string `json:"service_point_id" validate:"omitempty,uuid"`
}
type QueueStatus struct {
	Status  string `json:"status" validate:"required,oneof=waiting called in_service completed skipped cancelled"`
	Version int    `json:"version" validate:"required,min=1"`
}

// JoinQueue puts a visit in a department queue; the ticket number is stamped by the database.
func (s Service) JoinQueue(ctx context.Context, a identity.Actor, in QueueInput, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "queue.manage"); e != nil {
		return nil, e
	}
	if in.Priority == "" {
		in.Priority = "routine"
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, sess platform.Session) (Raw, error) {
		patient, facility, e := platform.Encounter(ctx, tx, a.FacilityIDs, in.EncounterID)
		if e != nil {
			return nil, e
		}
		var id string
		e = tx.QueryRow(ctx, `INSERT INTO clinical.queue_ticket(org_id,facility_id,department_id,service_point_id,encounter_id,patient_id,priority,queue_date,ticket_no)
 VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6::uuid,$7,current_date,1) RETURNING id::text`, sess.OrgID, facility, in.DepartmentID, in.ServicePointID, in.EncounterID, patient, in.Priority).Scan(&id)
		if e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, `SELECT `+queueJ+` FROM clinical.queue_ticket q WHERE id=$1::uuid`, id)
	})
}

// Queue returns the active board (waiting, called, in service), most urgent first.
func (s Service) Queue(ctx context.Context, a identity.Actor, department, request string) ([]Raw, error) {
	if e := platform.Need(a.Permissions, "queue.read", "queue.manage"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) ([]Raw, error) {
		return platform.Many(ctx, tx, `SELECT `+queueJ+` FROM clinical.queue_ticket q WHERE q.status IN ('waiting','called','in_service') AND q.facility_id=ANY($1::uuid[])
 AND ($2='' OR q.department_id=NULLIF($2,'')::uuid) ORDER BY q.priority_rank, q.created_at`, a.FacilityIDs, department)
	})
}

func (s Service) QueueStatus(ctx context.Context, a identity.Actor, id string, in QueueStatus, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "queue.manage"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) (Raw, error) {
		tag, e := tx.Exec(ctx, `UPDATE clinical.queue_ticket SET status=$3 WHERE id=$1::uuid AND row_version=$2 AND facility_id=ANY($4::uuid[])`, id, in.Version, in.Status, a.FacilityIDs)
		if e != nil {
			return nil, e
		}
		if e = platform.Changed(ctx, tx, tag, "clinical.queue_ticket", id); e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, `SELECT `+queueJ+` FROM clinical.queue_ticket q WHERE id=$1::uuid`, id)
	})
}
