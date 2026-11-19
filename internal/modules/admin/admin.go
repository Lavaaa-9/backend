// Package admin covers organization settings, staff-department routing, custom RBAC roles,
// integration API credentials, provider configurations, and governance data categories.
package admin

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"spartial.local/backend/internal/modules/identity"
	"spartial.local/backend/internal/platform"
	"spartial.local/backend/internal/security"
)

type Service struct{ DB *pgxpool.Pool }
type Raw = platform.Raw

var (
	orgJ  = platform.JSON("o", true)
	roleJ = platform.JSON("r", true)
	credJ = `(to_jsonb(c) - 'org_id' - 'key_hash')`
	provJ = platform.JSON("p", true)
)

// ---------------------------------------------------------------- organization settings

type OrgUpdate struct {
	LegalName                  *string `json:"legal_name" validate:"omitempty,min=2,max=200"`
	DisplayName                *string `json:"display_name" validate:"omitempty,min=2,max=200"`
	StaffIdleTimeoutMinutes    *int    `json:"staff_idle_timeout_minutes" validate:"omitempty,min=5,max=60"`
	StaffSessionMaxHours       *int    `json:"staff_session_max_hours" validate:"omitempty,min=1,max=24"`
	LockoutThreshold           *int    `json:"lockout_threshold" validate:"omitempty,min=3,max=10"`
	LockoutDurationMinutes     *int    `json:"lockout_duration_minutes" validate:"omitempty,min=1,max=1440"`
	LabSelfVerificationAllowed *bool   `json:"lab_self_verification_allowed"`
	Version                    int     `json:"version" validate:"required,min=1"`
}

func (s Service) GetOrganization(ctx context.Context, a identity.Actor, request string) (Raw, error) {
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, sess platform.Session) (Raw, error) {
		return platform.One(ctx, tx, `SELECT `+orgJ+` FROM org.organization o WHERE o.id=$1::uuid`, sess.OrgID)
	})
}

func (s Service) UpdateOrganization(ctx context.Context, a identity.Actor, in OrgUpdate, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "admin.org.manage"); e != nil {
		return nil, e
	}
	var idleStr, maxStr, lockStr *string
	if in.StaffIdleTimeoutMinutes != nil {
		v := fmt.Sprintf("%d minutes", *in.StaffIdleTimeoutMinutes)
		idleStr = &v
	}
	if in.StaffSessionMaxHours != nil {
		v := fmt.Sprintf("%d hours", *in.StaffSessionMaxHours)
		maxStr = &v
	}
	if in.LockoutDurationMinutes != nil {
		v := fmt.Sprintf("%d minutes", *in.LockoutDurationMinutes)
		lockStr = &v
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, sess platform.Session) (Raw, error) {
		tag, e := tx.Exec(ctx, `UPDATE org.organization SET
 legal_name=coalesce(NULLIF(btrim($3),''),legal_name),
 display_name=coalesce(NULLIF(btrim($4),''),display_name),
 staff_idle_timeout=coalesce($5::interval,staff_idle_timeout),
 staff_session_max=coalesce($6::interval,staff_session_max),
 lockout_threshold=coalesce($7,lockout_threshold),
 lockout_duration=coalesce($8::interval,lockout_duration),
 lab_self_verification_allowed=coalesce($9,lab_self_verification_allowed)
 WHERE id=$1::uuid AND row_version=$2`,
			sess.OrgID, in.Version, in.LegalName, in.DisplayName, idleStr, maxStr, in.LockoutThreshold, lockStr, in.LabSelfVerificationAllowed)
		if e != nil {
			return nil, e
		}
		if e = platform.Changed(ctx, tx, tag, "org.organization", sess.OrgID); e != nil {
			return nil, e
		}
		if e = platform.Audit(ctx, tx, "org.settings.update", "administration", "success", "organization", sess.OrgID, "", ""); e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, `SELECT `+orgJ+` FROM org.organization o WHERE o.id=$1::uuid`, sess.OrgID)
	})
}

// ---------------------------------------------------------------- staff departments

type StaffDepartmentInput struct {
	MembershipID string `json:"membership_id" validate:"required,uuid"`
	DepartmentID string `json:"department_id" validate:"required,uuid"`
	IsPrimary    bool   `json:"is_primary"`
}

func (s Service) ListStaffDepartments(ctx context.Context, a identity.Actor, deptID, membershipID, request string) ([]Raw, error) {
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) ([]Raw, error) {
		return platform.Many(ctx, tx, `SELECT jsonb_build_object(
 'membership_id',sd.membership_id,'department_id',sd.department_id,'is_primary',sd.is_primary,'assigned_at',sd.assigned_at,
 'department_code',d.code,'department_name',d.name)
 FROM org.staff_department sd JOIN org.department d ON d.id=sd.department_id
 WHERE d.facility_id=ANY($1::uuid[])
   AND ($2='' OR sd.department_id=NULLIF($2,'')::uuid)
   AND ($3='' OR sd.membership_id=NULLIF($3,'')::uuid)
 ORDER BY sd.is_primary DESC, d.name LIMIT 200`, a.FacilityIDs, deptID, membershipID)
	})
}

func (s Service) AssignStaffDepartment(ctx context.Context, a identity.Actor, in StaffDepartmentInput, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "admin.user.manage"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, sess platform.Session) (Raw, error) {
		if in.IsPrimary {
			if _, e := tx.Exec(ctx, `UPDATE org.staff_department SET is_primary=false WHERE membership_id=$1::uuid AND is_primary`, in.MembershipID); e != nil {
				return nil, e
			}
		}
		_, e := tx.Exec(ctx, `INSERT INTO org.staff_department(org_id,membership_id,department_id,is_primary)
 VALUES($1::uuid,$2::uuid,$3::uuid,$4)
 ON CONFLICT (membership_id,department_id) DO UPDATE SET is_primary=EXCLUDED.is_primary`,
			sess.OrgID, in.MembershipID, in.DepartmentID, in.IsPrimary)
		if e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, `SELECT jsonb_build_object('membership_id',membership_id,'department_id',department_id,'is_primary',is_primary,'assigned_at',assigned_at)
 FROM org.staff_department WHERE membership_id=$1::uuid AND department_id=$2::uuid`, in.MembershipID, in.DepartmentID)
	})
}

func (s Service) RemoveStaffDepartment(ctx context.Context, a identity.Actor, in StaffDepartmentInput, request string) error {
	if e := platform.Need(a.Permissions, "admin.user.manage"); e != nil {
		return e
	}
	_, e := platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) (struct{}, error) {
		tag, e := tx.Exec(ctx, `DELETE FROM org.staff_department WHERE membership_id=$1::uuid AND department_id=$2::uuid`, in.MembershipID, in.DepartmentID)
		if e != nil {
			return struct{}{}, e
		}
		if tag.RowsAffected() != 1 {
			return struct{}{}, platform.NotFound()
		}
		return struct{}{}, nil
	})
	return e
}

// ---------------------------------------------------------------- roles & permissions

type RoleInput struct {
	Code         string   `json:"code" validate:"required,min=3,max=40"`
	Name         string   `json:"name" validate:"required,min=2,max=120"`
	Description  string   `json:"description" validate:"max=500"`
	ParentRoleID *string  `json:"parent_role_id" validate:"omitempty,uuid"`
	Permissions  []string `json:"permissions" validate:"max=100,dive,max=80"`
}

type RoleUpdate struct {
	Name        *string  `json:"name" validate:"omitempty,min=2,max=120"`
	Description *string  `json:"description" validate:"omitempty,max=500"`
	Status      *string  `json:"status" validate:"omitempty,oneof=active retired"`
	Permissions []string `json:"permissions" validate:"omitempty,max=100,dive,max=80"`
	Version     int      `json:"version" validate:"required,min=1"`
}

func (s Service) ListPermissions(ctx context.Context, a identity.Actor, request string) ([]Raw, error) {
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) ([]Raw, error) {
		return platform.Many(ctx, tx, `SELECT to_jsonb(p) FROM ref.permission p ORDER BY p.code`)
	})
}

func (s Service) ListRoles(ctx context.Context, a identity.Actor, request string) ([]Raw, error) {
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) ([]Raw, error) {
		return platform.Many(ctx, tx, `SELECT `+roleJ+` || jsonb_build_object('permissions',
 coalesce((SELECT jsonb_agg(rp.permission_code ORDER BY rp.permission_code) FROM iam.role_permission rp WHERE rp.role_id=r.id),'[]'::jsonb))
 FROM iam.role r ORDER BY r.is_system DESC, r.code`)
	})
}

func (s Service) CreateRole(ctx context.Context, a identity.Actor, in RoleInput, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "admin.role.manage"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, sess platform.Session) (Raw, error) {
		var id string
		e := tx.QueryRow(ctx, `INSERT INTO iam.role(org_id,code,name,description,parent_role_id,is_system)
 VALUES($1::uuid,$2,$3,NULLIF($4,''),$5::uuid,false) RETURNING id::text`,
			sess.OrgID, strings.ToLower(strings.TrimSpace(in.Code)), strings.TrimSpace(in.Name), strings.TrimSpace(in.Description), in.ParentRoleID).Scan(&id)
		if e != nil {
			return nil, e
		}
		for _, p := range in.Permissions {
			if _, e = tx.Exec(ctx, `INSERT INTO iam.role_permission(role_id,permission_code) VALUES($1::uuid,$2) ON CONFLICT DO NOTHING`, id, strings.TrimSpace(p)); e != nil {
				return nil, e
			}
		}
		if e = platform.Audit(ctx, tx, "admin.role.create", "administration", "success", "role", id, "", in.Code); e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, `SELECT `+roleJ+` || jsonb_build_object('permissions',
 coalesce((SELECT jsonb_agg(rp.permission_code ORDER BY rp.permission_code) FROM iam.role_permission rp WHERE rp.role_id=r.id),'[]'::jsonb))
 FROM iam.role r WHERE r.id=$1::uuid`, id)
	})
}

func (s Service) UpdateRole(ctx context.Context, a identity.Actor, id string, in RoleUpdate, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "admin.role.manage"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) (Raw, error) {
		tag, e := tx.Exec(ctx, `UPDATE iam.role SET
 name=coalesce(NULLIF(btrim($3),''),name),
 description=CASE WHEN $4::text IS NOT NULL THEN NULLIF(btrim($4),'') ELSE description END,
 status=coalesce($5,status)
 WHERE id=$1::uuid AND row_version=$2 AND NOT is_system`, id, in.Version, in.Name, in.Description, in.Status)
		if e != nil {
			return nil, e
		}
		if e = platform.Changed(ctx, tx, tag, "iam.role", id); e != nil {
			return nil, e
		}
		if in.Permissions != nil {
			if _, e = tx.Exec(ctx, `DELETE FROM iam.role_permission WHERE role_id=$1::uuid`, id); e != nil {
				return nil, e
			}
			for _, p := range in.Permissions {
				if _, e = tx.Exec(ctx, `INSERT INTO iam.role_permission(role_id,permission_code) VALUES($1::uuid,$2) ON CONFLICT DO NOTHING`, id, strings.TrimSpace(p)); e != nil {
					return nil, e
				}
			}
		}
		if e = platform.Audit(ctx, tx, "admin.role.update", "administration", "success", "role", id, "", ""); e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, `SELECT `+roleJ+` || jsonb_build_object('permissions',
 coalesce((SELECT jsonb_agg(rp.permission_code ORDER BY rp.permission_code) FROM iam.role_permission rp WHERE rp.role_id=r.id),'[]'::jsonb))
 FROM iam.role r WHERE r.id=$1::uuid`, id)
	})
}

// ---------------------------------------------------------------- API credentials

type APICredentialInput struct {
	UserID        string   `json:"user_id" validate:"required,uuid"`
	Name          string   `json:"name" validate:"required,min=2,max=120"`
	AllowedCIDRs  []string `json:"allowed_cidrs" validate:"max=20,dive,cidr"`
	ExpiresInDays int      `json:"expires_in_days" validate:"omitempty,min=1,max=365"`
}

func randomAlnum(n int) (string, error) {
	const alpha = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	if _, e := rand.Read(b); e != nil {
		return "", e
	}
	for i := range b {
		b[i] = alpha[int(b[i])%len(alpha)]
	}
	return string(b), nil
}

func (s Service) ListAPICredentials(ctx context.Context, a identity.Actor, request string) ([]Raw, error) {
	if e := platform.Need(a.Permissions, "admin.user.manage"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) ([]Raw, error) {
		return platform.Many(ctx, tx, `SELECT `+credJ+` FROM iam.api_credential c ORDER BY c.created_at DESC LIMIT 100`)
	})
}

func (s Service) CreateAPICredential(ctx context.Context, a identity.Actor, in APICredentialInput, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "admin.user.manage"); e != nil {
		return nil, e
	}
	days := in.ExpiresInDays
	if days == 0 {
		days = 90
	}
	prefTail, e := randomAlnum(8)
	if e != nil {
		return nil, e
	}
	secretPart, e := security.Token()
	if e != nil {
		return nil, e
	}
	prefix := "sp_live_" + prefTail
	fullKey := prefix + "." + secretPart
	cidrs := in.AllowedCIDRs
	if cidrs == nil {
		cidrs = []string{}
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, sess platform.Session) (Raw, error) {
		var id string
		e := tx.QueryRow(ctx, `INSERT INTO iam.api_credential(org_id,user_id,name,key_prefix,key_hash,allowed_cidrs,expires_at)
 VALUES($1::uuid,$2::uuid,$3,$4,decode($5,'hex'),$6::cidr[],$7) RETURNING id::text`,
			sess.OrgID, in.UserID, strings.TrimSpace(in.Name), prefix, security.Digest(fullKey), cidrs, time.Now().UTC().AddDate(0, 0, days)).Scan(&id)
		if e != nil {
			return nil, e
		}
		if e = platform.Audit(ctx, tx, "admin.api_credential.create", "administration", "success", "api_credential", id, "", prefix); e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, `SELECT `+credJ+` || jsonb_build_object('api_key',$2::text) FROM iam.api_credential c WHERE c.id=$1::uuid`, id, fullKey)
	})
}

func (s Service) RevokeAPICredential(ctx context.Context, a identity.Actor, id, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "admin.user.manage"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) (Raw, error) {
		tag, e := tx.Exec(ctx, `UPDATE iam.api_credential SET revoked_at=now() WHERE id=$1::uuid AND revoked_at IS NULL`, id)
		if e != nil {
			return nil, e
		}
		if e = platform.Changed(ctx, tx, tag, "iam.api_credential", id); e != nil {
			return nil, e
		}
		if e = platform.Audit(ctx, tx, "admin.api_credential.revoke", "administration", "success", "api_credential", id, "", ""); e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, `SELECT `+credJ+` FROM iam.api_credential c WHERE c.id=$1::uuid`, id)
	})
}

// ---------------------------------------------------------------- integration providers & governance categories

type ProviderConfigInput struct {
	Provider             string         `json:"provider" validate:"required,min=2,max=80"`
	Purpose              string         `json:"purpose" validate:"required,oneof=sms whatsapp mobile_money card telemedicine object_storage email fiscal_receipt insurance_claims"`
	Status               string         `json:"status" validate:"omitempty,oneof=disabled sandbox live"`
	CredentialsSecretRef string         `json:"credentials_secret_ref" validate:"required,min=8,max=300"`
	Config               map[string]any `json:"config"`
	DPASignedAt          *string        `json:"dpa_signed_at" validate:"omitempty,datetime=2006-01-02"`
	DPADocumentRef       string         `json:"dpa_document_ref" validate:"max=200"`
}

type ProviderConfigUpdate struct {
	Status               *string        `json:"status" validate:"omitempty,oneof=disabled sandbox live"`
	CredentialsSecretRef *string        `json:"credentials_secret_ref" validate:"omitempty,min=8,max=300"`
	Config               map[string]any `json:"config"`
	DPASignedAt          *string        `json:"dpa_signed_at" validate:"omitempty,datetime=2006-01-02"`
	DPADocumentRef       *string        `json:"dpa_document_ref" validate:"omitempty,max=200"`
	Version              int            `json:"version" validate:"required,min=1"`
}

func (s Service) ListProviders(ctx context.Context, a identity.Actor, request string) ([]Raw, error) {
	if e := platform.Need(a.Permissions, "admin.org.manage"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) ([]Raw, error) {
		return platform.Many(ctx, tx, `SELECT `+provJ+` FROM integration.provider_config p ORDER BY p.purpose, p.provider`)
	})
}

func (s Service) CreateProvider(ctx context.Context, a identity.Actor, in ProviderConfigInput, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "admin.org.manage"); e != nil {
		return nil, e
	}
	if in.Status == "" {
		in.Status = "disabled"
	}
	if in.Status == "live" && in.DPASignedAt == nil {
		return nil, platform.Fail(422, "validation", "dpa_signed_at is required before activating a live integration provider")
	}
	cfgJSON := []byte("{}")
	if in.Config != nil {
		b, e := json.Marshal(in.Config)
		if e != nil {
			return nil, e
		}
		cfgJSON = b
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, sess platform.Session) (Raw, error) {
		return platform.One(ctx, tx, `WITH p AS (INSERT INTO integration.provider_config(org_id,provider,purpose,status,credentials_secret_ref,config,dpa_signed_at,dpa_document_ref)
 VALUES($1::uuid,$2,$3,$4,$5,$6::jsonb,$7::date,NULLIF($8,'')) RETURNING *) SELECT `+provJ+` FROM p`,
			sess.OrgID, strings.ToLower(strings.TrimSpace(in.Provider)), in.Purpose, in.Status, strings.TrimSpace(in.CredentialsSecretRef), cfgJSON, in.DPASignedAt, strings.TrimSpace(in.DPADocumentRef))
	})
}

func (s Service) UpdateProvider(ctx context.Context, a identity.Actor, id string, in ProviderConfigUpdate, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "admin.org.manage"); e != nil {
		return nil, e
	}
	var cfgJSON []byte
	if in.Config != nil {
		b, e := json.Marshal(in.Config)
		if e != nil {
			return nil, e
		}
		cfgJSON = b
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) (Raw, error) {
		tag, e := tx.Exec(ctx, `UPDATE integration.provider_config SET
 status=coalesce($3,status),
 credentials_secret_ref=coalesce(NULLIF(btrim($4),''),credentials_secret_ref),
 config=coalesce($5::jsonb,config),
 dpa_signed_at=coalesce($6::date,dpa_signed_at),
 dpa_document_ref=CASE WHEN $7::text IS NOT NULL THEN NULLIF(btrim($7),'') ELSE dpa_document_ref END,
 last_reviewed_at=current_date
 WHERE id=$1::uuid AND row_version=$2`, id, in.Version, in.Status, in.CredentialsSecretRef, cfgJSON, in.DPASignedAt, in.DPADocumentRef)
		if e != nil {
			return nil, e
		}
		if e = platform.Changed(ctx, tx, tag, "integration.provider_config", id); e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, `SELECT `+provJ+` FROM integration.provider_config p WHERE p.id=$1::uuid`, id)
	})
}

func (s Service) DataCategories(ctx context.Context, a identity.Actor, request string) ([]Raw, error) {
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) ([]Raw, error) {
		return platform.Many(ctx, tx, `SELECT to_jsonb(c) FROM gov.data_category c ORDER BY c.code`)
	})
}
