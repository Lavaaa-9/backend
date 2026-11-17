package integration

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"spartial.local/backend/internal/config"
	"spartial.local/backend/internal/httpapi"
	"spartial.local/backend/internal/maintenance"
	"spartial.local/backend/internal/security"
	"strings"
	"testing"
	"time"
)

func TestEncryptedIdentifiersAndKeyRotation(t *testing.T) {
	oldMaterial, e := security.NewKeyMaterial()
	if e != nil {
		t.Fatal(e)
	}
	newMaterial, e := security.NewKeyMaterial()
	if e != nil {
		t.Fatal(e)
	}
	old, _ := security.ParseKeyring("old:"+oldMaterial, "old")
	index := []byte("0123456789abcdef0123456789abcdef")
	cfg := config.Config{Env: "test", Origins: []string{"http://localhost:3000"}, Keys: old, IdentifierIndexKey: index, CarePolicyEnabled: true}
	f := setupWith(t, cfg)
	ctx := context.Background()
	admin, secret, _ := enrollMFA(t, f, "admin@spartial.test")
	reception := token(f.login(t, "reception@spartial.test"))
	p := f.request(t, "POST", "/api/v1/staff/patients", reception, f.patientInput(), 201, map[string]string{"Idempotency-Key": "identifier-patient"})
	pid := p.body["id"].(string)
	value := "SYNTHETIC-NIDA-00000001"
	body := map[string]any{"kind": "nida", "issuer": "Synthetic identity authority", "value": value}
	added := f.request(t, "POST", "/api/v1/staff/patients/"+pid+"/identifiers", reception, body, 201, map[string]string{"Idempotency-Key": "synthetic-identifier"})
	if _, ok := added.body["value"]; ok {
		t.Fatal("create response would persist plaintext inside idempotency storage")
	}
	raw, e := json.Marshal(added.body)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(raw), value) {
		t.Fatal("plaintext identifier in metadata")
	}
	replay := f.request(t, "POST", "/api/v1/staff/patients/"+pid+"/identifiers", reception, body, 201, map[string]string{"Idempotency-Key": "synthetic-identifier"})
	if replay.body["id"] != added.body["id"] {
		t.Fatal("identifier was duplicated")
	}
	var stored []byte
	var storedResponse string
	if e = f.owner.QueryRow(ctx, `SELECT value_ciphertext FROM patient_identifiers WHERE id=$1::uuid`, added.body["id"]).Scan(&stored); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(stored), value) || security.CiphertextKeyID(stored) != "old" {
		t.Fatal("identifier is not encrypted with old key")
	}
	if e = f.owner.QueryRow(ctx, `SELECT response::text FROM idempotency_records WHERE key='synthetic-identifier'`).Scan(&storedResponse); e != nil || strings.Contains(storedResponse, value) {
		t.Fatal("plaintext identifier persisted in replay cache")
	}
	found := f.request(t, "POST", "/api/v1/staff/patient-identifiers/search", reception, body, 200, nil)
	if len(found.body["patient_ids"].([]any)) != 1 {
		t.Fatal("exact encrypted search failed")
	}
	f.request(t, "GET", "/api/v1/staff/patients/"+pid+"/identifiers", admin, nil, 403, nil)
	rotating, _ := security.ParseKeyring("old:"+oldMaterial+",new:"+newMaterial, "new")
	result, e := maintenance.RotateKeys(ctx, f.owner, rotating)
	if e != nil || result.MFA != 1 || result.Identifiers != 1 {
		t.Fatalf("rotation failed: %+v %v", result, e)
	}
	if _, e = maintenance.RotateKeys(ctx, f.runtime, rotating); e == nil {
		t.Fatal("runtime role performed owner key maintenance")
	}
	onlyNew, _ := security.ParseKeyring("new:"+newMaterial, "new")
	cfg.Keys = onlyNew
	app, e := httpapi.New(cfg, f.runtime, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if e != nil {
		t.Fatal(e)
	}
	f.app = app
	read := f.request(t, "GET", "/api/v1/staff/patients/"+pid+"/identifiers", reception, nil, 200, nil)
	if read.body["items"].([]any)[0].(map[string]any)["value"] != value {
		t.Fatal("retired-key removal lost identifier")
	}
	ch := loginChallenge(t, f, "mobile")
	code := security.HOTP(secret, security.TOTPStep(time.Now()), 6)
	verified := f.request(t, "POST", "/api/v1/auth/mfa/verify", "", map[string]any{"challenge_token": ch, "client_type": "mobile", "code": code}, 200, nil)
	f.request(t, "GET", "/api/v1/staff/users", token(verified), nil, 200, nil)
	found = f.request(t, "POST", "/api/v1/staff/patient-identifiers/search", reception, body, 200, nil)
	if len(found.body["patient_ids"].([]any)) != 1 {
		t.Fatal("blind-index search changed after key rotation")
	}
}
