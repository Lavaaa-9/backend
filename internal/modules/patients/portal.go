package patients

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"spartial.local/backend/internal/modules/identity"
	"spartial.local/backend/internal/platform"
)

var portalJ = platform.JSON("pa", true)

type PortalAccountInput struct {
	Phone              string `json:"phone" validate:"omitempty,max=40"`
	Email              string `json:"email" validate:"omitempty,email,max=254"`
	DisplayName        string `json:"display_name" validate:"required,min=1,max=150"`
	AccessType         string `json:"access_type" validate:"omitempty,oneof=self guardian"`
	Activate           bool   `json:"activate"`
	VerificationMethod string `json:"verification_method" validate:"omitempty,oneof=in_person_id_check otp_registered_phone staff_assisted"`
}

type PortalAccountStatus struct {
	Status             string `json:"status" validate:"required,oneof=active suspended revoked"`
	VerificationMethod string `json:"verification_method" validate:"omitempty,oneof=in_person_id_check otp_registered_phone staff_assisted"`
	RevokedReason      string `json:"revoked_reason" validate:"max=500"`
	Version            int    `json:"version" validate:"required,min=1"`
}

func (s Service) ListPortalAccounts(ctx context.Context, a identity.Actor, patientID, request string) ([]Raw, error) {
	if e := platform.Need(a.Permissions, "patient.read", "admin.user.manage"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) ([]Raw, error) {
		if e := ensurePatientInScope(ctx, tx, a.FacilityIDs, patientID); e != nil {
			return nil, e
		}
		return platform.Many(ctx, tx, `SELECT `+portalJ+` || jsonb_build_object('email',u.email,'phone_e164',u.phone_e164,'display_name',u.display_name)
 FROM patient.portal_account pa JOIN iam.user_account u ON u.id=pa.user_id
 WHERE pa.patient_id=$1::uuid ORDER BY pa.created_at DESC`, patientID)
	})
}

func (s Service) CreatePortalAccount(ctx context.Context, a identity.Actor, patientID string, in PortalAccountInput, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "patient.update", "admin.user.manage"); e != nil {
		return nil, e
	}
	p, e := phone(in.Phone)
	if e != nil {
		return nil, e
	}
	email := strings.ToLower(strings.TrimSpace(in.Email))
	if p == "" && email == "" {
		return nil, platform.Fail(422, "validation", "Provide phone and/or email for the portal account")
	}
	if in.AccessType == "" {
		in.AccessType = "self"
	}
	status := "pending_verification"
	var method *string
	if in.Activate {
		if in.VerificationMethod == "" {
			return nil, platform.Fail(422, "validation", "verification_method is required when activating a portal account")
		}
		status = "active"
		method = &in.VerificationMethod
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, sess platform.Session) (Raw, error) {
		if e := ensurePatientInScope(ctx, tx, a.FacilityIDs, patientID); e != nil {
			return nil, e
		}
		var uid string
		if p != "" {
			e = tx.QueryRow(ctx, `SELECT id::text FROM iam.user_account WHERE account_type='patient' AND phone_e164=$1`, p).Scan(&uid)
		} else {
			e = tx.QueryRow(ctx, `SELECT id::text FROM iam.user_account WHERE account_type='patient' AND email=$1`, email).Scan(&uid)
		}
		if errors.Is(e, pgx.ErrNoRows) {
			e = tx.QueryRow(ctx, `INSERT INTO iam.user_account(account_type,email,phone_e164,display_name,status,mfa_required)
 VALUES('patient',NULLIF($1,''),NULLIF($2,''),$3,'active',false) RETURNING id::text`,
				email, p, strings.TrimSpace(in.DisplayName)).Scan(&uid)
		}
		if e != nil {
			return nil, e
		}
		var id string
		e = tx.QueryRow(ctx, `INSERT INTO patient.portal_account(org_id,patient_id,user_id,access_type,status,verification_method,verified_by,verified_at)
 VALUES($1::uuid,$2::uuid,$3::uuid,$4,$5,$6,CASE WHEN $5='active' THEN $7::uuid END,CASE WHEN $5='active' THEN now() END) RETURNING id::text`,
			sess.OrgID, patientID, uid, in.AccessType, status, method, sess.UserID).Scan(&id)
		if e != nil {
			return nil, e
		}
		if e = platform.Audit(ctx, tx, "patient.portal.create", "phi_access", "success", "portal_account", id, patientID, status); e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, `SELECT `+portalJ+` || jsonb_build_object('email',u.email,'phone_e164',u.phone_e164,'display_name',u.display_name)
 FROM patient.portal_account pa JOIN iam.user_account u ON u.id=pa.user_id WHERE pa.id=$1::uuid`, id)
	})
}

func (s Service) ChangePortalAccountStatus(ctx context.Context, a identity.Actor, id string, in PortalAccountStatus, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "patient.update", "admin.user.manage"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, sess platform.Session) (Raw, error) {
		tag, e := tx.Exec(ctx, `UPDATE patient.portal_account SET
 status=$3,
 verification_method=CASE WHEN $4<>'' THEN $4 ELSE verification_method END,
 verified_by=CASE WHEN $3='active' THEN coalesce(verified_by,$5::uuid) ELSE verified_by END,
 verified_at=CASE WHEN $3='active' THEN coalesce(verified_at,now()) ELSE verified_at END,
 revoked_reason=CASE WHEN $3='revoked' THEN NULLIF($6,'') ELSE revoked_reason END
 WHERE id=$1::uuid AND row_version=$2`,
			id, in.Version, in.Status, in.VerificationMethod, sess.UserID, strings.TrimSpace(in.RevokedReason))
		if e != nil {
			return nil, e
		}
		if e = platform.Changed(ctx, tx, tag, "patient.portal_account", id); e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, `SELECT `+portalJ+` FROM patient.portal_account pa WHERE pa.id=$1::uuid`, id)
	})
}
