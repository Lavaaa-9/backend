package clinical

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"
	"spartial.local/backend/internal/modules/identity"
	"spartial.local/backend/internal/platform"
)

var (
	allergyJ = platform.JSON("a", true)
	problemJ = platform.JSON("p", true)
	histJ    = platform.JSON("h", true)
)

func ensurePatient(ctx context.Context, tx pgx.Tx, facilities []string, patientID string) error {
	var ok bool
	if e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM patient.patient WHERE id=$1::uuid AND registered_facility_id=ANY($2::uuid[]))`, patientID, facilities).Scan(&ok); e != nil {
		return e
	}
	if !ok {
		return platform.NotFound()
	}
	return nil
}

// ---------------------------------------------------------------- allergies

type AllergyInput struct {
	Substance    string  `json:"substance" validate:"required,min=2,max=200"`
	Category     string  `json:"category" validate:"required,oneof=drug food environment other"`
	Reaction     string  `json:"reaction" validate:"max=500"`
	Severity     string  `json:"severity" validate:"omitempty,oneof=mild moderate severe life_threatening unknown"`
	Verification string  `json:"verification" validate:"omitempty,oneof=unconfirmed confirmed refuted"`
	OnsetDate    *string `json:"onset_date" validate:"omitempty,datetime=2006-01-02"`
}

type AllergyUpdate struct {
	Reaction     *string `json:"reaction" validate:"omitempty,max=500"`
	Severity     *string `json:"severity" validate:"omitempty,oneof=mild moderate severe life_threatening unknown"`
	Verification *string `json:"verification" validate:"omitempty,oneof=unconfirmed confirmed refuted"`
	Status       *string `json:"status" validate:"omitempty,oneof=active inactive resolved entered_in_error"`
	ErrorReason  *string `json:"error_reason" validate:"omitempty,max=500"`
	Version      int     `json:"version" validate:"required,min=1"`
}

func (s Service) ListAllergies(ctx context.Context, a identity.Actor, patientID, request string) ([]Raw, error) {
	if e := platform.Need(a.Permissions, "clinical.read", "pharmacy.read"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) ([]Raw, error) {
		if e := ensurePatient(ctx, tx, a.FacilityIDs, patientID); e != nil {
			return nil, e
		}
		out, e := platform.Many(ctx, tx, `SELECT `+allergyJ+` FROM clinical.allergy a WHERE a.patient_id=$1::uuid ORDER BY (a.status='active') DESC, a.created_at DESC`, patientID)
		if e != nil {
			return nil, e
		}
		return out, platform.Audit(ctx, tx, "allergy.list", "phi_access", "success", "patient", patientID, patientID, "")
	})
}

func (s Service) AddAllergy(ctx context.Context, a identity.Actor, patientID string, in AllergyInput, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "clinical.history.write", "triage.write", "consultation.write"); e != nil {
		return nil, e
	}
	sev := in.Severity
	if sev == "" {
		sev = "unknown"
	}
	ver := in.Verification
	if ver == "" {
		ver = "unconfirmed"
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, sess platform.Session) (Raw, error) {
		if e := ensurePatient(ctx, tx, a.FacilityIDs, patientID); e != nil {
			return nil, e
		}
		var id string
		e := tx.QueryRow(ctx, `INSERT INTO clinical.allergy(org_id,patient_id,substance,category,reaction,severity,verification,onset_date)
 VALUES($1::uuid,$2::uuid,$3,$4,NULLIF($5,''),$6,$7,$8::date) RETURNING id::text`,
			sess.OrgID, patientID, strings.TrimSpace(in.Substance), in.Category, strings.TrimSpace(in.Reaction), sev, ver, in.OnsetDate).Scan(&id)
		if e != nil {
			return nil, e
		}
		if e = platform.Audit(ctx, tx, "allergy.create", "phi_access", "success", "allergy", id, patientID, ""); e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, `SELECT `+allergyJ+` FROM clinical.allergy a WHERE id=$1::uuid`, id)
	})
}

func (s Service) UpdateAllergy(ctx context.Context, a identity.Actor, id string, in AllergyUpdate, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "clinical.history.write", "triage.write", "consultation.write"); e != nil {
		return nil, e
	}
	if in.Status != nil && *in.Status == "entered_in_error" && (in.ErrorReason == nil || strings.TrimSpace(*in.ErrorReason) == "") {
		return nil, platform.Fail(422, "validation", "error_reason is required when marking entered_in_error")
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) (Raw, error) {
		tag, e := tx.Exec(ctx, `UPDATE clinical.allergy SET
 reaction=CASE WHEN $3::text IS NOT NULL THEN NULLIF(btrim($3),'') ELSE reaction END,
 severity=coalesce($4,severity),
 verification=coalesce($5,verification),
 status=coalesce($6,status),
 error_reason=CASE WHEN $7::text IS NOT NULL THEN NULLIF(btrim($7),'') ELSE error_reason END
 WHERE id=$1::uuid AND row_version=$2`, id, in.Version, in.Reaction, in.Severity, in.Verification, in.Status, in.ErrorReason)
		if e != nil {
			return nil, e
		}
		if e = platform.Changed(ctx, tx, tag, "clinical.allergy", id); e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, `SELECT `+allergyJ+` FROM clinical.allergy a WHERE id=$1::uuid`, id)
	})
}

// ---------------------------------------------------------------- problems

type ProblemInput struct {
	ICD10Code   *string `json:"icd10_code" validate:"omitempty,max=20"`
	Description string  `json:"description" validate:"required,min=2,max=500"`
	OnsetDate   *string `json:"onset_date" validate:"omitempty,datetime=2006-01-02"`
}

type ProblemUpdate struct {
	Description  *string `json:"description" validate:"omitempty,min=2,max=500"`
	Status       *string `json:"status" validate:"omitempty,oneof=active inactive resolved entered_in_error"`
	ResolvedDate *string `json:"resolved_date" validate:"omitempty,datetime=2006-01-02"`
	ErrorReason  *string `json:"error_reason" validate:"omitempty,max=500"`
	Version      int     `json:"version" validate:"required,min=1"`
}

func (s Service) ListProblems(ctx context.Context, a identity.Actor, patientID, request string) ([]Raw, error) {
	if e := platform.Need(a.Permissions, "clinical.read"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) ([]Raw, error) {
		if e := ensurePatient(ctx, tx, a.FacilityIDs, patientID); e != nil {
			return nil, e
		}
		out, e := platform.Many(ctx, tx, `SELECT `+problemJ+` FROM clinical.problem p WHERE p.patient_id=$1::uuid ORDER BY (p.status='active') DESC, p.created_at DESC`, patientID)
		if e != nil {
			return nil, e
		}
		return out, platform.Audit(ctx, tx, "problem.list", "phi_access", "success", "patient", patientID, patientID, "")
	})
}

func (s Service) AddProblem(ctx context.Context, a identity.Actor, patientID string, in ProblemInput, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "clinical.history.write", "consultation.write"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, sess platform.Session) (Raw, error) {
		if e := ensurePatient(ctx, tx, a.FacilityIDs, patientID); e != nil {
			return nil, e
		}
		var id string
		e := tx.QueryRow(ctx, `INSERT INTO clinical.problem(org_id,patient_id,icd10_code,description,onset_date)
 VALUES($1::uuid,$2::uuid,NULLIF($3,''),$4,$5::date) RETURNING id::text`,
			sess.OrgID, patientID, deref(in.ICD10Code), strings.TrimSpace(in.Description), in.OnsetDate).Scan(&id)
		if e != nil {
			return nil, e
		}
		if e = platform.Audit(ctx, tx, "problem.create", "phi_access", "success", "problem", id, patientID, ""); e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, `SELECT `+problemJ+` FROM clinical.problem p WHERE id=$1::uuid`, id)
	})
}

func (s Service) UpdateProblem(ctx context.Context, a identity.Actor, id string, in ProblemUpdate, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "clinical.history.write", "consultation.write"); e != nil {
		return nil, e
	}
	if in.Status != nil && *in.Status == "resolved" && in.ResolvedDate == nil {
		return nil, platform.Fail(422, "validation", "resolved_date is required when resolving a problem")
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) (Raw, error) {
		tag, e := tx.Exec(ctx, `UPDATE clinical.problem SET
 description=coalesce(NULLIF(btrim($3),''),description),
 status=coalesce($4,status),
 resolved_date=coalesce($5::date,resolved_date),
 error_reason=CASE WHEN $6::text IS NOT NULL THEN NULLIF(btrim($6),'') ELSE error_reason END
 WHERE id=$1::uuid AND row_version=$2`, id, in.Version, in.Description, in.Status, in.ResolvedDate, in.ErrorReason)
		if e != nil {
			return nil, e
		}
		if e = platform.Changed(ctx, tx, tag, "clinical.problem", id); e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, `SELECT `+problemJ+` FROM clinical.problem p WHERE id=$1::uuid`, id)
	})
}

// ---------------------------------------------------------------- history entries

type HistoryInput struct {
	HistoryType string  `json:"history_type" validate:"required,oneof=past_medical surgical family social obstetric immunization other"`
	Description string  `json:"description" validate:"required,min=2,max=4000"`
	OccurredOn  *string `json:"occurred_on" validate:"omitempty,datetime=2006-01-02"`
}

func (s Service) ListHistory(ctx context.Context, a identity.Actor, patientID, historyType, request string) ([]Raw, error) {
	if e := platform.Need(a.Permissions, "clinical.read"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) ([]Raw, error) {
		if e := ensurePatient(ctx, tx, a.FacilityIDs, patientID); e != nil {
			return nil, e
		}
		out, e := platform.Many(ctx, tx, `SELECT `+histJ+` FROM clinical.history_entry h WHERE h.patient_id=$1::uuid AND ($2='' OR h.history_type=$2) ORDER BY h.created_at DESC`, patientID, historyType)
		if e != nil {
			return nil, e
		}
		return out, platform.Audit(ctx, tx, "history.list", "phi_access", "success", "patient", patientID, patientID, "")
	})
}

func (s Service) AddHistory(ctx context.Context, a identity.Actor, patientID string, in HistoryInput, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "clinical.history.write", "consultation.write"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, sess platform.Session) (Raw, error) {
		if e := ensurePatient(ctx, tx, a.FacilityIDs, patientID); e != nil {
			return nil, e
		}
		var id string
		e := tx.QueryRow(ctx, `INSERT INTO clinical.history_entry(org_id,patient_id,history_type,description,occurred_on)
 VALUES($1::uuid,$2::uuid,$3,$4,$5::date) RETURNING id::text`,
			sess.OrgID, patientID, in.HistoryType, strings.TrimSpace(in.Description), in.OccurredOn).Scan(&id)
		if e != nil {
			return nil, e
		}
		if e = platform.Audit(ctx, tx, "history.create", "phi_access", "success", "history_entry", id, patientID, ""); e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, `SELECT `+histJ+` FROM clinical.history_entry h WHERE id=$1::uuid`, id)
	})
}

func (s Service) VoidHistory(ctx context.Context, a identity.Actor, id string, in VoidInput, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "clinical.history.write", "consultation.write"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) (Raw, error) {
		tag, e := tx.Exec(ctx, `UPDATE clinical.history_entry SET status='entered_in_error',error_reason=$3 WHERE id=$1::uuid AND row_version=$2 AND status='active'`,
			id, in.Version, strings.TrimSpace(in.Reason))
		if e != nil {
			return nil, e
		}
		if e = platform.Changed(ctx, tx, tag, "clinical.history_entry", id); e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, `SELECT `+histJ+` FROM clinical.history_entry h WHERE id=$1::uuid`, id)
	})
}
