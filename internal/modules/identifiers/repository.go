package identifiers

import (
	"context"
	"github.com/jackc/pgx/v5"
)

func insert(ctx context.Context, tx pgx.Tx, id, patient, actor string, in Input, index string, blob []byte) (m Metadata, err error) {
	err = tx.QueryRow(ctx, `INSERT INTO patient_identifiers(id,patient_id,facility_id,kind,issuer,value_ciphertext,blind_index,recorded_by) SELECT $1::uuid,id,facility_id,$3,$4,$5,$6,$7::uuid FROM patients WHERE id=$2::uuid RETURNING id::text,patient_id::text,kind,issuer,created_at`, id, patient, in.Kind, in.Issuer, blob, index, actor).Scan(&m.ID, &m.PatientID, &m.Kind, &m.Issuer, &m.CreatedAt)
	return
}

type encryptedRow struct {
	Metadata
	Blob []byte
}

func read(ctx context.Context, tx pgx.Tx, patient string) ([]encryptedRow, error) {
	rows, e := tx.Query(ctx, `SELECT id::text,patient_id::text,kind,issuer,created_at,value_ciphertext FROM patient_identifiers WHERE patient_id=$1::uuid ORDER BY id LIMIT 100`, patient)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []encryptedRow{}
	for rows.Next() {
		var r encryptedRow
		if e = rows.Scan(&r.ID, &r.PatientID, &r.Kind, &r.Issuer, &r.CreatedAt, &r.Blob); e != nil {
			return nil, e
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func lookup(ctx context.Context, tx pgx.Tx, in Input, index string) ([]string, error) {
	rows, e := tx.Query(ctx, `SELECT DISTINCT patient_id::text FROM patient_identifiers WHERE kind=$1 AND issuer=$2 AND blind_index=$3 ORDER BY patient_id::text LIMIT 25`, in.Kind, in.Issuer, index)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			return nil, e
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
