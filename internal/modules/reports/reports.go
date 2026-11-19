// Package reports exposes management summaries that never reveal record content.
package reports

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"spartial.local/backend/internal/modules/identity"
	"spartial.local/backend/internal/platform"
)

type Service struct{ DB *pgxpool.Pool }

type ActivityRow struct {
	Date            string  `json:"date"`
	UserID          *string `json:"user_id"`
	StaffName       *string `json:"staff_name"`
	Category        string  `json:"category"`
	Action          string  `json:"action"`
	Events          int64   `json:"events"`
	Denied          int64   `json:"denied"`
	PatientsTouched int64   `json:"patients_touched"`
}

// StaffActivity aggregates audit events per day/staff/action (permission report.staff_activity).
func (s Service) StaffActivity(ctx context.Context, a identity.Actor, from, to time.Time, request string) ([]ActivityRow, error) {
	if !a.Can("report.staff_activity") {
		return nil, platform.Forbidden()
	}
	tx, _, e := platform.BeginRequest(ctx, s.DB, a.SessionID, request)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	rows, e := tx.Query(ctx, `SELECT activity_date,user_id::text,staff_name,category,action,events,denied,patients_touched FROM reporting.staff_activity($1::date,$2::date)`, from, to)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []ActivityRow{}
	for rows.Next() {
		var v ActivityRow
		var d time.Time
		if e = rows.Scan(&d, &v.UserID, &v.StaffName, &v.Category, &v.Action, &v.Events, &v.Denied, &v.PatientsTouched); e != nil {
			return nil, e
		}
		v.Date = d.Format("2006-01-02")
		out = append(out, v)
	}
	if e = rows.Err(); e != nil {
		return nil, e
	}
	rows.Close()
	return out, tx.Commit(ctx)
}

type AccessRow struct {
	MembershipID          string     `json:"membership_id"`
	UserID                string     `json:"user_id"`
	StaffName             string     `json:"staff_name"`
	JobTitle              *string    `json:"job_title"`
	Status                string     `json:"membership_status"`
	Roles                 []string   `json:"roles"`
	PrivilegedPermissions []string   `json:"privileged_permissions"`
	MFAEnabled            bool       `json:"mfa_enabled"`
	LastLoginAt           *time.Time `json:"last_login_at"`
	OpenBreakGlass        int64      `json:"open_break_glass"`
}

// StaffAccess lists every staff member with their roles, privileged permissions and MFA state.
func (s Service) StaffAccess(ctx context.Context, a identity.Actor, request string) ([]AccessRow, error) {
	if !a.Can("report.staff_activity") {
		return nil, platform.Forbidden()
	}
	tx, _, e := platform.BeginRequest(ctx, s.DB, a.SessionID, request)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	rows, e := tx.Query(ctx, `SELECT membership_id::text,user_id::text,staff_name,job_title,membership_status,coalesce(roles,'{}'),coalesce(privileged_permissions,'{}'),mfa_enabled,last_login_at,open_break_glass FROM reporting.staff_access()`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []AccessRow{}
	for rows.Next() {
		var v AccessRow
		if e = rows.Scan(&v.MembershipID, &v.UserID, &v.StaffName, &v.JobTitle, &v.Status, &v.Roles, &v.PrivilegedPermissions, &v.MFAEnabled, &v.LastLoginAt, &v.OpenBreakGlass); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	if e = rows.Err(); e != nil {
		return nil, e
	}
	rows.Close()
	if e = platform.Audit(ctx, tx, "report.staff_access.view", "administration", "success", "report", "", "", ""); e != nil {
		return nil, e
	}
	return out, tx.Commit(ctx)
}
