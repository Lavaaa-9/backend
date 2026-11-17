package patients

import (
	"context"
	"github.com/jackc/pgx/v5"
)

const columns = `id::text,mrn,facility_id::text,given_name,family_name,birth_precision,birth_date::text,birth_year,phone,address,version,created_at,updated_at`

func scan(row pgx.Row) (Patient, error) {
	var p Patient
	e := row.Scan(&p.ID, &p.MRN, &p.FacilityID, &p.GivenName, &p.FamilyName, &p.BirthPrecision, &p.BirthDate, &p.BirthYear, &p.Phone, &p.Address, &p.Version, &p.CreatedAt, &p.UpdatedAt)
	return p, e
}

type repository struct{ tx pgx.Tx }

func (r repository) create(ctx context.Context, in Input) (Patient, error) {
	return scan(r.tx.QueryRow(ctx, `INSERT INTO patients(facility_id,given_name,family_name,birth_precision,birth_date,birth_year,phone,address) VALUES($1::uuid,$2,$3,$4,$5::date,$6,$7,$8) RETURNING `+columns, in.FacilityID, in.GivenName, in.FamilyName, in.BirthPrecision, in.BirthDate, in.BirthYear, in.Phone, in.Address))
}
func (r repository) get(ctx context.Context, id string, facilities []string) (Patient, error) {
	return scan(r.tx.QueryRow(ctx, `SELECT `+columns+` FROM patients WHERE id=$1::uuid AND facility_id=ANY($2::uuid[])`, id, facilities))
}
func (r repository) update(ctx context.Context, id string, in Update, facilities []string) (Patient, error) {
	return scan(r.tx.QueryRow(ctx, `UPDATE patients SET given_name=$1,family_name=$2,birth_precision=$3,birth_date=$4::date,birth_year=$5,phone=$6,address=$7,version=version+1,updated_at=now() WHERE id=$8::uuid AND facility_id=ANY($9::uuid[]) AND version=$10 RETURNING `+columns, in.GivenName, in.FamilyName, in.BirthPrecision, in.BirthDate, in.BirthYear, in.Phone, in.Address, id, facilities, in.Version))
}
func (r repository) list(ctx context.Context, facilities []string, q, after string, limit int) ([]Patient, error) {
	rows, e := r.tx.Query(ctx, `SELECT `+columns+` FROM patients WHERE facility_id=ANY($1::uuid[]) AND ($2='' OR mrn ILIKE $2 ESCAPE '\' OR given_name ILIKE $2 ESCAPE '\' OR family_name ILIKE $2 ESCAPE '\' OR phone ILIKE $2 ESCAPE '\') AND ($3='' OR id>NULLIF($3,'')::uuid) ORDER BY id LIMIT $4`, facilities, q, after, limit)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Patient{}
	for rows.Next() {
		p, e := scan(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
