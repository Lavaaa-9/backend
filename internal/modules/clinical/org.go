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

type DepartmentUpdate struct {
	Name     string `json:"name" validate:"required,min=2,max=160"`
	DeptType string `json:"dept_type" validate:"required,oneof=reception triage consultation laboratory imaging pharmacy billing administration telemedicine other"`
	Version  int    `json:"version" validate:"required,min=1"`
}

type DepartmentStatusInput struct {
	Status  string `json:"status" validate:"required,oneof=active inactive"`
	Version int    `json:"version" validate:"required,min=1"`
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
		var id string
		if e := tx.QueryRow(ctx, `INSERT INTO org.department(org_id,facility_id,code,name,dept_type)
 VALUES($1::uuid,$2::uuid,$3,$4,$5) RETURNING id::text`,
			sess.OrgID, in.FacilityID, strings.ToUpper(strings.TrimSpace(in.Code)), strings.TrimSpace(in.Name), in.DeptType).Scan(&id); e != nil {
			return nil, e
		}
		if e := platform.Audit(ctx, tx, "department.create", "administration", "success", "department", id, "", ""); e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, `SELECT `+platform.JSON("d", true)+` FROM org.department d WHERE d.id=$1::uuid`, id)
	})
}

func (s Service) GetDepartment(ctx context.Context, a identity.Actor, id, request string) (Raw, error) {
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) (Raw, error) {
		if a.Can("admin.facility.manage") {
			return platform.One(ctx, tx, `SELECT `+platform.JSON("d", true)+` FROM org.department d WHERE d.id=$1::uuid`, id)
		}
		return platform.One(ctx, tx, `SELECT `+platform.JSON("d", true)+` FROM org.department d WHERE d.id=$1::uuid AND d.facility_id=ANY($2::uuid[])`, id, a.FacilityIDs)
	})
}

func (s Service) UpdateDepartment(ctx context.Context, a identity.Actor, id string, in DepartmentUpdate, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "admin.facility.manage"); e != nil {
		return nil, e
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, platform.Fail(422, "validation", "Department name is required")
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) (Raw, error) {
		tag, e := tx.Exec(ctx, `UPDATE org.department SET name=$3, dept_type=$4 WHERE id=$1::uuid AND row_version=$2`,
			id, in.Version, name, in.DeptType)
		if e != nil {
			return nil, e
		}
		if e = platform.Changed(ctx, tx, tag, "org.department", id); e != nil {
			return nil, e
		}
		if e = platform.Audit(ctx, tx, "department.update", "administration", "success", "department", id, "", ""); e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, `SELECT `+platform.JSON("d", true)+` FROM org.department d WHERE d.id=$1::uuid`, id)
	})
}

func (s Service) SetDepartmentStatus(ctx context.Context, a identity.Actor, id string, in DepartmentStatusInput, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "admin.facility.manage"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) (Raw, error) {
		tag, e := tx.Exec(ctx, `UPDATE org.department SET status=$3 WHERE id=$1::uuid AND row_version=$2`, id, in.Version, in.Status)
		if e != nil {
			return nil, e
		}
		if e = platform.Changed(ctx, tx, tag, "org.department", id); e != nil {
			return nil, e
		}
		action := "department.activate"
		if in.Status == "inactive" {
			action = "department.deactivate"
		}
		if e = platform.Audit(ctx, tx, action, "administration", "success", "department", id, "", ""); e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, `SELECT `+platform.JSON("d", true)+` FROM org.department d WHERE d.id=$1::uuid`, id)
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
