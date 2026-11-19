package main

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"spartial.local/backend/internal/config"
	"spartial.local/backend/internal/security"
)

// rotateKeys re-encrypts any stored ciphertext whose key_ref differs from PHI_ACTIVE_KEY_ID.
func rotateKeys() error {
	cfg, e := config.Load()
	if e != nil {
		return e
	}
	if cfg.Keys == nil {
		return fmt.Errorf("PHI_ENCRYPTION_KEYS and PHI_ACTIVE_KEY_ID are required for key rotation")
	}
	raw, e := adminURL()
	if e != nil {
		return e
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	db, e := pgxpool.New(ctx, raw)
	if e != nil {
		return fmt.Errorf("database connection failed")
	}
	defer db.Close()

	active := cfg.Keys.ActiveKeyID()
	mfaN, e := rotateMFA(ctx, db, cfg.Keys, active)
	if e != nil {
		return e
	}
	idN, e := rotateIdentifiers(ctx, db, cfg.Keys, active)
	if e != nil {
		return e
	}
	covN, e := rotateCoverages(ctx, db, cfg.Keys, active)
	if e != nil {
		return e
	}
	devN, e := rotateDevices(ctx, db, cfg.Keys, active)
	if e != nil {
		return e
	}
	fmt.Printf("Rotated to key %s: mfa=%d identifiers=%d coverages=%d devices=%d\n", active, mfaN, idN, covN, devN)
	return nil
}

func rotateMFA(ctx context.Context, db *pgxpool.Pool, keys *security.Keyring, active string) (int, error) {
	rows, e := db.Query(ctx, `SELECT id::text,user_id::text,secret_ciphertext FROM iam.mfa_factor WHERE factor_type='totp' AND secret_ciphertext IS NOT NULL AND key_ref<>$1`, active)
	if e != nil {
		return 0, e
	}
	type item struct {
		id, uid string
		blob    []byte
	}
	var list []item
	for rows.Next() {
		var it item
		if e = rows.Scan(&it.id, &it.uid, &it.blob); e != nil {
			rows.Close()
			return 0, e
		}
		list = append(list, it)
	}
	rows.Close()
	for _, it := range list {
		aad := []byte("iam_mfa_factor:" + it.uid)
		pt, e := keys.Open(it.blob, aad)
		if e != nil {
			return 0, fmt.Errorf("mfa %s: %w", it.id, e)
		}
		ct, e := keys.Seal(pt, aad)
		if e != nil {
			return 0, e
		}
		if _, e = db.Exec(ctx, `UPDATE iam.mfa_factor SET secret_ciphertext=$2,key_ref=$3 WHERE id=$1::uuid`, it.id, ct, active); e != nil {
			return 0, e
		}
	}
	return len(list), nil
}

func rotateIdentifiers(ctx context.Context, db *pgxpool.Pool, keys *security.Keyring, active string) (int, error) {
	rows, e := db.Query(ctx, `SELECT id::text,org_id::text,patient_id::text,id_type,value_ciphertext FROM patient.identifier WHERE key_ref<>$1`, active)
	if e != nil {
		return 0, e
	}
	type item struct {
		id, org, pid, typ string
		blob              []byte
	}
	var list []item
	for rows.Next() {
		var it item
		if e = rows.Scan(&it.id, &it.org, &it.pid, &it.typ, &it.blob); e != nil {
			rows.Close()
			return 0, e
		}
		list = append(list, it)
	}
	rows.Close()
	for _, it := range list {
		aad := []byte("patient_identifier:" + it.org + ":" + it.pid + ":" + it.typ)
		pt, e := keys.Open(it.blob, aad)
		if e != nil {
			return 0, fmt.Errorf("identifier %s: %w", it.id, e)
		}
		ct, e := keys.Seal(pt, aad)
		if e != nil {
			return 0, e
		}
		if _, e = db.Exec(ctx, `UPDATE patient.identifier SET value_ciphertext=$2,key_ref=$3 WHERE id=$1::uuid`, it.id, ct, active); e != nil {
			return 0, e
		}
	}
	return len(list), nil
}

func rotateCoverages(ctx context.Context, db *pgxpool.Pool, keys *security.Keyring, active string) (int, error) {
	rows, e := db.Query(ctx, `SELECT id::text,org_id::text,patient_id::text,payer_id::text,member_no_ciphertext FROM billing.coverage WHERE key_ref<>$1`, active)
	if e != nil {
		return 0, e
	}
	type item struct {
		id, org, pid, payer string
		blob                []byte
	}
	var list []item
	for rows.Next() {
		var it item
		if e = rows.Scan(&it.id, &it.org, &it.pid, &it.payer, &it.blob); e != nil {
			rows.Close()
			return 0, e
		}
		list = append(list, it)
	}
	rows.Close()
	for _, it := range list {
		aad := []byte("billing_coverage:" + it.org + ":" + it.pid + ":" + it.payer)
		pt, e := keys.Open(it.blob, aad)
		if e != nil {
			return 0, fmt.Errorf("coverage %s: %w", it.id, e)
		}
		ct, e := keys.Seal(pt, aad)
		if e != nil {
			return 0, e
		}
		if _, e = db.Exec(ctx, `UPDATE billing.coverage SET member_no_ciphertext=$2,key_ref=$3 WHERE id=$1::uuid`, it.id, ct, active); e != nil {
			return 0, e
		}
	}
	return len(list), nil
}

func rotateDevices(ctx context.Context, db *pgxpool.Pool, keys *security.Keyring, active string) (int, error) {
	rows, e := db.Query(ctx, `SELECT id::text,user_id::text,push_token_ciphertext FROM comms.device WHERE key_ref<>$1`, active)
	if e != nil {
		return 0, e
	}
	type item struct {
		id, uid string
		blob    []byte
	}
	var list []item
	for rows.Next() {
		var it item
		if e = rows.Scan(&it.id, &it.uid, &it.blob); e != nil {
			rows.Close()
			return 0, e
		}
		list = append(list, it)
	}
	rows.Close()
	for _, it := range list {
		aad := []byte("comms_device:" + it.uid)
		pt, e := keys.Open(it.blob, aad)
		if e != nil {
			return 0, fmt.Errorf("device %s: %w", it.id, e)
		}
		ct, e := keys.Seal(pt, aad)
		if e != nil {
			return 0, e
		}
		if _, e = db.Exec(ctx, `UPDATE comms.device SET push_token_ciphertext=$2,key_ref=$3 WHERE id=$1::uuid`, it.id, ct, active); e != nil {
			return 0, e
		}
	}
	return len(list), nil
}
