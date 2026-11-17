package maintenance

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"spartial.local/backend/internal/modules/identifiers"
	"spartial.local/backend/internal/platform"
	"spartial.local/backend/internal/security"
)

type RotationResult struct{ MFA, Identifiers int }

// RotateKeys is an owner-authorized maintenance operation; it never deletes keys or changes blind indexes.
func RotateKeys(ctx context.Context, db *pgxpool.Pool, keys *security.Keyring) (RotationResult, error) {
	var result RotationResult
	if keys == nil {
		return result, fmt.Errorf("encryption keys are required")
	}
	var bypass bool
	if e := db.QueryRow(ctx, `SELECT rolsuper OR rolbypassrls FROM pg_roles WHERE rolname=current_user`).Scan(&bypass); e != nil {
		return result, e
	}
	if !bypass {
		return result, fmt.Errorf("key rotation requires explicitly authorized maintenance credentials with RLS bypass; never use runtime credentials")
	}
	after := ""
	for {
		rows, e := db.Query(ctx, `SELECT staff_id::text FROM staff_mfa WHERE ($1='' OR staff_id>NULLIF($1,'')::uuid) ORDER BY staff_id LIMIT 100`, after)
		if e != nil {
			return result, e
		}
		ids := []string{}
		for rows.Next() {
			var id string
			if e = rows.Scan(&id); e != nil {
				rows.Close()
				return result, e
			}
			ids = append(ids, id)
		}
		rows.Close()
		if e = rows.Err(); e != nil {
			return result, e
		}
		if len(ids) == 0 {
			break
		}
		for _, id := range ids {
			tx, e := db.Begin(ctx)
			if e != nil {
				return result, e
			}
			var locked string
			e = tx.QueryRow(ctx, `SELECT id::text FROM staff WHERE id=$1::uuid FOR UPDATE`, id).Scan(&locked)
			var blob []byte
			if e == nil {
				e = tx.QueryRow(ctx, `SELECT secret_ciphertext FROM staff_mfa WHERE staff_id=$1::uuid FOR UPDATE`, id).Scan(&blob)
			}
			if e == nil && security.CiphertextKeyID(blob) != keys.ActiveKeyID() {
				var plain, rotated []byte
				plain, e = keys.Open(blob, []byte("staff_mfa:"+id))
				if e == nil {
					rotated, e = keys.Seal(plain, []byte("staff_mfa:"+id))
				}
				if e == nil {
					_, e = tx.Exec(ctx, `UPDATE staff_mfa SET secret_ciphertext=$1 WHERE staff_id=$2::uuid`, rotated, id)
					if e == nil {
						e = platform.AuditReason(ctx, tx, "", "security.key_rotation", id, "", uuid.NewString(), "success", keys.ActiveKeyID())
					}
					result.MFA++
				}
			}
			if e != nil {
				tx.Rollback(ctx)
				return result, fmt.Errorf("MFA key rotation failed; retain all original keys")
			}
			if e = tx.Commit(ctx); e != nil {
				return result, e
			}
		}
		after = ids[len(ids)-1]
	}
	after = ""
	for {
		rows, e := db.Query(ctx, `SELECT id::text FROM patient_identifiers WHERE ($1='' OR id>NULLIF($1,'')::uuid) ORDER BY id LIMIT 100`, after)
		if e != nil {
			return result, e
		}
		ids := []string{}
		for rows.Next() {
			var id string
			if e = rows.Scan(&id); e != nil {
				rows.Close()
				return result, e
			}
			ids = append(ids, id)
		}
		rows.Close()
		if e = rows.Err(); e != nil {
			return result, e
		}
		if len(ids) == 0 {
			break
		}
		for _, id := range ids {
			tx, e := db.Begin(ctx)
			if e != nil {
				return result, e
			}
			var patient, kind, issuer string
			var blob []byte
			e = tx.QueryRow(ctx, `SELECT patient_id::text,kind,issuer,value_ciphertext FROM patient_identifiers WHERE id=$1::uuid FOR UPDATE`, id).Scan(&patient, &kind, &issuer, &blob)
			if e == nil && security.CiphertextKeyID(blob) != keys.ActiveKeyID() {
				aad := identifiers.AAD(id, patient, kind, issuer)
				var plain, rotated []byte
				plain, e = keys.Open(blob, aad)
				if e == nil {
					rotated, e = keys.Seal(plain, aad)
				}
				if e == nil {
					_, e = tx.Exec(ctx, `UPDATE patient_identifiers SET value_ciphertext=$1 WHERE id=$2::uuid`, rotated, id)
					if e == nil {
						e = platform.AuditReason(ctx, tx, "", "security.key_rotation", id, patient, uuid.NewString(), "success", keys.ActiveKeyID())
					}
					result.Identifiers++
				}
			}
			if e != nil {
				tx.Rollback(ctx)
				return result, fmt.Errorf("identifier key rotation failed; retain all original keys")
			}
			if e = tx.Commit(ctx); e != nil {
				return result, e
			}
		}
		after = ids[len(ids)-1]
	}
	var remaining int
	if e := db.QueryRow(ctx, `SELECT (SELECT count(*) FROM staff_mfa WHERE convert_from(substring(secret_ciphertext FROM 3 FOR get_byte(secret_ciphertext,1)),'UTF8')<>$1)+(SELECT count(*) FROM patient_identifiers WHERE convert_from(substring(value_ciphertext FROM 3 FOR get_byte(value_ciphertext,1)),'UTF8')<>$1)`, keys.ActiveKeyID()).Scan(&remaining); e != nil {
		return result, e
	}
	if remaining != 0 {
		return result, fmt.Errorf("old-key ciphertext remains; stop old writers and rerun rotation before retiring any key")
	}
	return result, nil
}
