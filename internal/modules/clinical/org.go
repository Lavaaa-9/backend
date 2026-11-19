package clinical

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"
	"spartial.local/backend/internal/modules/identity"
	"spartial.local/backend/internal/platform"
)

type DepartmentInput struct {
	FacilityID string `json:"facility_id" validate:"required,uuid"`
	Code       string `json:"code" validate:"required,min=2,max=20"`
	Name       string `json:"name" validate:"required,min=2,max=160"`
	DeptType   string `json:"dept_type" validate:"required,oneof=reception triage consultation laboratory imaging pharmacy billing administration telemedicine other"`
}

type ServicePointInput struct {
	DepartmentID string `json:"department_id" validate:"required,uuid"`
	Code         string `json:"code" validate:"required,min=1,max=30"`
	Name         string `json:"name" validate:"required,min=2,max=160"`
}

func (s Service) CreateDepartment(ctx context.Context, a identity.Actor, in DepartmentInput, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "admin.facility.manage"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, sess platform.Session) (Raw, error) {
		return platform.One(ctx, tx, `WITH d AS (INSERT INTO org.department(org_id,facility_id,code,name,dept_type)
 VALUES($1::uuid,$2::uuid,$3,$4,$5) RETURNING *) SELECT `+platform.JSON("d", true)+` FROM d`,
			sess.OrgID, in.FacilityID, strings.ToUpper(strings.TrimSpace(in.Code)), strings.TrimSpace(in.Name), in.DeptType)
	})
}

func (s Service) ServicePoints(ctx context.Context, a identity.Actor, deptID, request string) ([]Raw, error) {
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) ([]Raw, error) {
		return platform.Many(ctx, tx, `SELECT `+platform.JSON("sp", true)+` FROM org.service_point sp
 JOIN org.department d ON d.id=sp.department_id
 WHERE sp.status='active' AND d.facility_id=ANY($1::uuid[]) AND ($2='' OR sp.department_id=NULLIF($2,'')::uuid)
 ORDER BY sp.name LIMIT 200`, a.FacilityIDs, deptID)
	})
}

func (s Service) CreateServicePoint(ctx context.Context, a identity.Actor, in ServicePointInput, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "admin.facility.manage"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, sess platform.Session) (Raw, error) {
		return platform.One(ctx, tx, `WITH sp AS (INSERT INTO org.service_point(org_id,department_id,code,name)
 VALUES($1::uuid,$2::uuid,$3,$4) RETURNING *) SELECT `+platform.JSON("sp", true)+` FROM sp`,
			sess.OrgID, in.DepartmentID, strings.ToUpper(strings.TrimSpace(in.Code)), strings.TrimSpace(in.Name))
	})
}
