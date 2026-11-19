package billing

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"
	"spartial.local/backend/internal/modules/identity"
	"spartial.local/backend/internal/modules/patients"
	"spartial.local/backend/internal/platform"
)

var coverageJ = platform.JSON("c", true, "member_no_ciphertext", "member_no_bidx", "key_ref")

func coverageAAD(orgID, patientID, payerID string) []byte {
	return []byte("billing_coverage:" + orgID + ":" + patientID + ":" + payerID)
}

func last4(v string) *string {
	var b strings.Builder
	for _, r := range v {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	s := b.String()
	if s == "" {
		return nil
	}
	if len(s) > 4 {
		s = s[len(s)-4:]
	}
	return &s
}

type CoverageInput struct {
	PayerID    string  `json:"payer_id" validate:"required,uuid"`
	MemberNo   string  `json:"member_no" validate:"required,min=2,max=100"`
	SchemeName string  `json:"scheme_name" validate:"max=160"`
	ValidFrom  string  `json:"valid_from" validate:"required,datetime=2006-01-02"`
	ValidTo    *string `json:"valid_to" validate:"omitempty,datetime=2006-01-02"`
}

type CoverageUpdate struct {
	SchemeName *string `json:"scheme_name" validate:"omitempty,max=160"`
	ValidTo    *string `json:"valid_to" validate:"omitempty,datetime=2006-01-02"`
	Status     *string `json:"status" validate:"omitempty,oneof=active expired suspended cancelled"`
	Verified   bool    `json:"verified"`
	Version    int     `json:"version" validate:"required,min=1"`
}

type RevealInput struct {
	Reason string `json:"reason" validate:"required,min=5,max=500"`
}

func (s Service) requireCrypto() error {
	if s.Keys == nil || len(s.IndexKey) != 32 {
		return platform.Fail(503, "encryption_unavailable", "Coverage encryption keys are not configured on this server")
	}
	return nil
}

func (s Service) AddCoverage(ctx context.Context, a identity.Actor, patientID string, in CoverageInput, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "billing.invoice.manage", "patient.update"); e != nil {
		return nil, e
	}
	if e := s.requireCrypto(); e != nil {
		return nil, e
	}
	raw := strings.TrimSpace(in.MemberNo)
	norm := patients.NormalizeIdentifier(raw)
	if len(norm) < 2 {
		return nil, platform.Fail(422, "validation", "Member number is too short")
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, sess platform.Session) (Raw, error) {
		var ok bool
		if e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM patient.patient WHERE id=$1::uuid AND registered_facility_id=ANY($2::uuid[]))`, patientID, a.FacilityIDs).Scan(&ok); e != nil {
			return nil, e
		}
		if !ok {
			return nil, platform.NotFound()
		}
		ct, e := s.Keys.Seal([]byte(raw), coverageAAD(sess.OrgID, patientID, in.PayerID))
		if e != nil {
			return nil, e
		}
		bidx := patients.BlindIndex(s.IndexKey, "billing_coverage:"+in.PayerID, norm)
		var id string
		e = tx.QueryRow(ctx, `INSERT INTO billing.coverage(org_id,patient_id,payer_id,member_no_ciphertext,member_no_bidx,member_no_last4,key_ref,scheme_name,valid_from,valid_to)
 VALUES($1::uuid,$2::uuid,$3::uuid,$4,$5,$6,$7,NULLIF($8,''),$9::date,$10::date) RETURNING id::text`,
			sess.OrgID, patientID, in.PayerID, ct, bidx, last4(norm), s.Keys.ActiveKeyID(), strings.TrimSpace(in.SchemeName), in.ValidFrom, in.ValidTo).Scan(&id)
		if e != nil {
			return nil, e
		}
		if e = platform.Audit(ctx, tx, "billing.coverage.create", "phi_access", "success", "coverage", id, patientID, ""); e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, `SELECT `+coverageJ+` || jsonb_build_object('payer_name',p.name,'payer_type',p.payer_type)
 FROM billing.coverage c JOIN billing.payer p ON p.id=c.payer_id WHERE c.id=$1::uuid`, id)
	})
}

func (s Service) ListCoverages(ctx context.Context, a identity.Actor, patientID, request string) ([]Raw, error) {
	if e := platform.Need(a.Permissions, "billing.read", "patient.read"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) ([]Raw, error) {
		var ok bool
		if e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM patient.patient WHERE id=$1::uuid AND registered_facility_id=ANY($2::uuid[]))`, patientID, a.FacilityIDs).Scan(&ok); e != nil {
			return nil, e
		}
		if !ok {
			return nil, platform.NotFound()
		}
		return platform.Many(ctx, tx, `SELECT `+coverageJ+` || jsonb_build_object('payer_name',p.name,'payer_type',p.payer_type)
 FROM billing.coverage c JOIN billing.payer p ON p.id=c.payer_id
 WHERE c.patient_id=$1::uuid ORDER BY (c.status='active') DESC, c.valid_from DESC`, patientID)
	})
}

func (s Service) RevealCoverage(ctx context.Context, a identity.Actor, id string, in RevealInput, request string) (map[string]any, error) {
	if e := platform.Need(a.Permissions, "patient.identifier.reveal"); e != nil {
		return nil, e
	}
	if e := s.requireCrypto(); e != nil {
		return nil, e
	}
	reason, e := platform.Reason(in.Reason, 5)
	if e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, sess platform.Session) (map[string]any, error) {
		var patientID, payerID, status string
		var ct []byte
		e := tx.QueryRow(ctx, `SELECT patient_id::text,payer_id::text,status,member_no_ciphertext FROM billing.coverage WHERE id=$1::uuid`, id).
			Scan(&patientID, &payerID, &status, &ct)
		if e != nil {
			return nil, e
		}
		pt, e := s.Keys.Open(ct, coverageAAD(sess.OrgID, patientID, payerID))
		if e != nil {
			return nil, platform.Fail(500, "decrypt_failed", "Stored member number could not be decrypted")
		}
		if e = platform.Audit(ctx, tx, "billing.coverage.reveal", "phi_access", "success", "coverage", id, patientID, reason); e != nil {
			return nil, e
		}
		return map[string]any{"id": id, "patient_id": patientID, "payer_id": payerID, "status": status, "member_no": string(pt)}, nil
	})
}

func (s Service) UpdateCoverage(ctx context.Context, a identity.Actor, id string, in CoverageUpdate, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "billing.invoice.manage", "patient.update"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) (Raw, error) {
		tag, e := tx.Exec(ctx, `UPDATE billing.coverage SET
 scheme_name=CASE WHEN $3::text IS NOT NULL THEN NULLIF(btrim($3),'') ELSE scheme_name END,
 valid_to=coalesce($4::date,valid_to),
 status=coalesce($5,status),
 last_verified_at=CASE WHEN $6 THEN now() ELSE last_verified_at END
 WHERE id=$1::uuid AND row_version=$2`, id, in.Version, in.SchemeName, in.ValidTo, in.Status, in.Verified)
		if e != nil {
			return nil, e
		}
		if e = platform.Changed(ctx, tx, tag, "billing.coverage", id); e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, `SELECT `+coverageJ+` || jsonb_build_object('payer_name',p.name,'payer_type',p.payer_type)
 FROM billing.coverage c JOIN billing.payer p ON p.id=c.payer_id WHERE c.id=$1::uuid`, id)
	})
}
