package privacy

import (
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type accessRepository struct{ tx pgx.Tx }

func (r accessRepository) facility(ctx context.Context, actor, patient string) (id string, err error) {
	err = r.tx.QueryRow(ctx, `SELECT p.facility_id::text FROM patient_scopes p JOIN staff_facilities sf ON sf.facility_id=p.facility_id AND sf.staff_id=$1::uuid JOIN facilities f ON f.id=p.facility_id AND f.active JOIN staff u ON u.id=sf.staff_id AND u.active WHERE p.patient_id=$2::uuid`, actor, patient).Scan(&id)
	return
}
func (r accessRepository) targetEligible(ctx context.Context, staff, facility string) (ok bool, err error) {
	err = r.tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM staff s JOIN staff_facilities sf ON sf.staff_id=s.id AND sf.facility_id=$2::uuid WHERE s.id=$1::uuid AND s.active AND s.role_name IN ('doctor','nurse','laboratory','pharmacy'))`, staff, facility).Scan(&ok)
	return
}
func (r accessRepository) allowed(ctx context.Context, actor, patient, facility string, write bool) (ok bool, err error) {
	err = r.tx.QueryRow(ctx, `SELECT can_access_patient($1::uuid,$2::uuid,$3::uuid,$4)`, actor, patient, facility, write).Scan(&ok)
	return
}
func (r accessRepository) createAssignment(ctx context.Context, actor, patient, facility string, in AssignmentInput) (a Assignment, err error) {
	err = r.tx.QueryRow(ctx, `INSERT INTO care_assignments(patient_id,facility_id,staff_id,assigned_by,reason,valid_until) VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5,$6) RETURNING id::text,patient_id::text,facility_id::text,staff_id::text,assigned_by::text,reason,valid_from,valid_until,revoked_at`, patient, facility, in.StaffID, actor, in.Reason, in.ValidUntil).Scan(&a.ID, &a.PatientID, &a.FacilityID, &a.StaffID, &a.AssignedBy, &a.Reason, &a.ValidFrom, &a.ValidUntil, &a.RevokedAt)
	return
}
func (r accessRepository) revokeAssignment(ctx context.Context, actor, id string) (a Assignment, err error) {
	err = r.tx.QueryRow(ctx, `UPDATE care_assignments c SET revoked_at=COALESCE(revoked_at,now()),revoked_by=COALESCE(revoked_by,$1::uuid) WHERE c.id=$2::uuid AND EXISTS(SELECT 1 FROM staff_facilities sf JOIN facilities f ON f.id=sf.facility_id AND f.active WHERE sf.staff_id=$1::uuid AND sf.facility_id=c.facility_id) RETURNING c.id::text,c.patient_id::text,c.facility_id::text,c.staff_id::text,c.assigned_by::text,c.reason,c.valid_from,c.valid_until,c.revoked_at`, actor, id).Scan(&a.ID, &a.PatientID, &a.FacilityID, &a.StaffID, &a.AssignedBy, &a.Reason, &a.ValidFrom, &a.ValidUntil, &a.RevokedAt)
	return
}
func (r accessRepository) activeBreakGlass(ctx context.Context, actor, patient string) (ok bool, err error) {
	err = r.tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM break_glass_access WHERE staff_id=$1::uuid AND patient_id=$2::uuid AND expires_at>statement_timestamp() AND (review_outcome IS NULL OR review_outcome<>'inappropriate'))`, actor, patient).Scan(&ok)
	return
}

const grantColumns = `id::text,patient_id::text,facility_id::text,staff_id::text,reason,granted_at,expires_at,reviewed_by::text,reviewed_at,review_outcome,review_note`

func scanGrant(row pgx.Row) (g Grant, err error) {
	err = row.Scan(&g.ID, &g.PatientID, &g.FacilityID, &g.StaffID, &g.Reason, &g.GrantedAt, &g.ExpiresAt, &g.ReviewedBy, &g.ReviewedAt, &g.Outcome, &g.ReviewNote)
	return
}
func (r accessRepository) createGrant(ctx context.Context, actor, patient, facility string, in BreakGlassInput) (Grant, error) {
	return scanGrant(r.tx.QueryRow(ctx, `INSERT INTO break_glass_access(patient_id,facility_id,staff_id,reason,expires_at) VALUES($1::uuid,$2::uuid,$3::uuid,$4,now()+make_interval(mins=>$5)) RETURNING `+grantColumns, patient, facility, actor, in.Reason, in.DurationMinutes))
}
func (r accessRepository) grant(ctx context.Context, id string) (Grant, error) {
	return scanGrant(r.tx.QueryRow(ctx, `SELECT `+grantColumns+` FROM break_glass_access WHERE id=$1::uuid FOR UPDATE`, id))
}
func (r accessRepository) review(ctx context.Context, actor, id string, in ReviewInput) (Grant, error) {
	return scanGrant(r.tx.QueryRow(ctx, `UPDATE break_glass_access SET reviewed_by=$1::uuid,reviewed_at=now(),review_outcome=$2,review_note=$3 WHERE id=$4::uuid AND reviewed_at IS NULL RETURNING `+grantColumns, actor, in.Outcome, in.Note, id))
}
func listGrants(ctx context.Context, tx pgx.Tx, after string, limit int) ([]Grant, error) {
	rows, e := tx.Query(ctx, `SELECT `+grantColumns+` FROM break_glass_access WHERE reviewed_at IS NULL AND ($1='' OR id>NULLIF($1,'')::uuid) ORDER BY id LIMIT $2`, after, limit)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Grant{}
	for rows.Next() {
		g, e := scanGrant(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, g)
	}
	return out, rows.Err()
}
func scopedPatient(ctx context.Context, db *pgxpool.Pool, actor, patient string) (facility string, err error) {
	err = db.QueryRow(ctx, `SELECT p.facility_id::text FROM patient_scopes p JOIN staff_facilities sf ON sf.staff_id=$1::uuid AND sf.facility_id=p.facility_id JOIN facilities f ON f.id=sf.facility_id AND f.active WHERE p.patient_id=$2::uuid`, actor, patient).Scan(&facility)
	return
}

func (r accessRepository) authorized(ctx context.Context, actor, session, permission string) (ok bool, err error) {
	err = r.tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM staff u JOIN role_permissions rp ON rp.role_name=u.role_name AND rp.permission_name=$3 JOIN sessions s ON s.staff_id=u.id WHERE u.id=$1::uuid AND u.active AND s.id=$2::uuid AND s.revoked_at IS NULL AND s.access_expires_at>statement_timestamp() AND s.expires_at>statement_timestamp() AND (s.client_type<>'web' OR s.last_seen_at>statement_timestamp()-interval '15 minutes'))`, actor, session, permission).Scan(&ok)
	return
}
