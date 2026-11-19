package integration

import (
	"bytes"
	"context"
	"encoding/base32"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	"spartial.local/backend/internal/config"
	"spartial.local/backend/internal/httpapi"
	"spartial.local/backend/internal/platform"
	"spartial.local/backend/internal/security"
)

type testEnv struct {
	t        *testing.T
	app      *fiber.App
	db       *pgxpool.Pool
	workerDB *pgxpool.Pool
	cfg      config.Config
	tokens   map[string]string
	facID    string
	depts    map[string]string
}

func setupEnv(t *testing.T) *testEnv {
	t.Helper()
	_ = godotenv.Load("../../.env")
	if os.Getenv("DATABASE_URL") == "" {
		t.Skip("DATABASE_URL not set; skipping integration tests")
	}
	os.Setenv("DISABLE_AUTH_RATE_LIMIT", "true")
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	db, err := platform.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		t.Fatalf("platform.Open: %v", err)
	}
	t.Cleanup(db.Close)
	if err := platform.CheckSchema(ctx, db); err != nil {
		t.Fatalf("CheckSchema: %v", err)
	}
	var workerDB *pgxpool.Pool
	if wurl := os.Getenv("DATABASE_WORKER_URL"); wurl != "" {
		workerDB, err = platform.Open(ctx, wurl)
		if err != nil {
			t.Fatalf("platform.Open(worker): %v", err)
		}
		t.Cleanup(workerDB.Close)
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	app, err := httpapi.New(cfg, db, log)
	if err != nil {
		t.Fatalf("httpapi.New: %v", err)
	}
	env := &testEnv{t: t, app: app, db: db, workerDB: workerDB, cfg: cfg, tokens: map[string]string{}, depts: map[string]string{}}
	roles := []string{"admin", "reception", "nurse", "doctor", "pharmacy", "cashier", "billing-supervisor", "management", "privacy-reviewer"}
	for _, r := range roles {
		env.tokens[r] = env.loginRole(r)
	}
	facs := env.mustJSON(env.tokens["reception"], "GET", "/api/v1/staff/facilities", nil, "", 200)
	env.facID = facs["items"].([]any)[0].(map[string]any)["id"].(string)
	depts := env.mustJSON(env.tokens["reception"], "GET", "/api/v1/staff/departments", nil, "", 200)
	for _, raw := range depts["items"].([]any) {
		m := raw.(map[string]any)
		env.depts[m["code"].(string)] = m["id"].(string)
	}
	return env
}

func (e *testEnv) passwordFor(role string) string {
	pw := os.Getenv("SEED_TEST_PASSWORD")
	if pw == "" {
		pw = "Spartial-Test-2026!"
	}
	if role == "admin" && os.Getenv("SEED_ADMIN_PASSWORD") != "" {
		return os.Getenv("SEED_ADMIN_PASSWORD")
	}
	if role == "reception" && os.Getenv("SEED_RECEPTION_PASSWORD") != "" {
		return os.Getenv("SEED_RECEPTION_PASSWORD")
	}
	return pw
}

func (e *testEnv) loginRole(role string) string {
	res := e.mustJSON("", "POST", "/api/v1/auth/login", map[string]any{
		"email":       role + "@spartial.test",
		"password":    e.passwordFor(role),
		"client_type": "mobile",
	}, "", 200)
	return res["access_token"].(string)
}

func (e *testEnv) do(token, method, path string, body any, idemKey string) (int, []byte) {
	e.t.Helper()
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			e.t.Fatalf("marshal: %v", err)
		}
		reader = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if idemKey != "" {
		req.Header.Set("Idempotency-Key", idemKey)
	}
	resp, err := e.app.Test(req)
	if err != nil {
		e.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, raw
}

func (e *testEnv) mustJSON(token, method, path string, body any, idemKey string, want int) map[string]any {
	e.t.Helper()
	code, raw := e.do(token, method, path, body, idemKey)
	if code != want {
		e.t.Fatalf("%s %s: want %d, got %d: %s", method, path, want, code, string(raw))
	}
	if len(raw) == 0 {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		e.t.Fatalf("%s %s: unmarshal %q: %v", method, path, string(raw), err)
	}
	return out
}

func (e *testEnv) createPatient(tag string) string {
	e.t.Helper()
	pt := e.mustJSON(e.tokens["reception"], "POST", "/api/v1/staff/patients", map[string]any{
		"facility_id":     e.facID,
		"given_name":      "IntTest",
		"family_name":     "User" + tag,
		"birth_precision": "exact",
		"birth_date":      "1991-06-15",
		"sex":             "female",
		"phone":           "+255712" + tag[:6],
	}, uuid.NewString(), 201)
	return pt["id"].(string)
}

func numTag() string {
	u := uuid.NewString()
	out := make([]byte, 0, 8)
	for i := 0; i < len(u) && len(out) < 8; i++ {
		if u[i] >= '0' && u[i] <= '9' {
			out = append(out, u[i])
		}
	}
	for len(out) < 8 {
		out = append(out, '7')
	}
	return string(out)
}

func TestMFALifecycle(t *testing.T) {
	env := setupEnv(t)
	tag := uuid.NewString()[:8]
	email := "mfa-" + tag + "@spartial.test"
	pw := "MfaTest-Strong-Pass-" + tag + "!"

	// Create temporary user
	env.mustJSON(env.tokens["admin"], "POST", "/api/v1/staff/users", map[string]any{
		"email":        email,
		"display_name": "MFA Test User " + tag,
		"password":     pw,
		"role":         "receptionist",
		"facility_ids": []string{env.facID},
	}, "", 201)

	// Password login before MFA enrollment
	toks := env.mustJSON("", "POST", "/api/v1/auth/login", map[string]any{
		"email": email, "password": pw, "client_type": "mobile",
	}, "", 200)
	tok := toks["access_token"].(string)

	// Start enrollment
	enr := env.mustJSON(tok, "POST", "/api/v1/auth/mfa/enroll", map[string]any{
		"current_password": pw,
	}, "", 200)
	secretStr := enr["secret"].(string)
	if secretStr == "" || enr["provisioning_uri"].(string) == "" {
		t.Fatalf("expected secret and provisioning_uri, got %v", enr)
	}
	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secretStr)
	if err != nil {
		t.Fatalf("decode secret: %v", err)
	}

	// Confirm enrollment with current TOTP code
	step := security.TOTPStep(time.Now())
	code := security.HOTP(secret, step, security.TOTPDigits)
	conf := env.mustJSON(tok, "POST", "/api/v1/auth/mfa/confirm", map[string]any{"code": code}, "", 200)
	rcodes := conf["recovery_codes"].([]any)
	if len(rcodes) != 10 {
		t.Fatalf("expected 10 recovery codes, got %d", len(rcodes))
	}

	// Confirm /auth/me reflects mfa_verified and mfa_enabled
	me := env.mustJSON(tok, "GET", "/api/v1/auth/me", nil, "", 200)
	if !me["mfa_verified"].(bool) || !me["mfa_enabled"].(bool) {
		t.Fatalf("expected mfa_verified=true & mfa_enabled=true, got %v", me)
	}

	// Subsequent login returns an MFA challenge (not tokens)
	ch := env.mustJSON("", "POST", "/api/v1/auth/login", map[string]any{
		"email": email, "password": pw, "client_type": "mobile",
	}, "", 200)
	if !ch["mfa_required"].(bool) || ch["challenge_token"].(string) == "" {
		t.Fatalf("expected MFA challenge, got %v", ch)
	}
	chTok := ch["challenge_token"].(string)

	// Replay of the exact same TOTP code used during confirm must fail (replay protection)
	status, _ := env.do("", "POST", "/api/v1/auth/mfa/verify", map[string]any{
		"challenge_token": chTok, "code": code, "client_type": "mobile",
	}, "")
	if status != 401 {
		t.Fatalf("expected 401 on replayed TOTP step, got %d", status)
	}

	// Verify challenge using a single-use recovery code
	rec1 := rcodes[0].(string)
	verified := env.mustJSON("", "POST", "/api/v1/auth/mfa/verify", map[string]any{
		"challenge_token": chTok, "recovery_code": rec1, "client_type": "mobile",
	}, "", 200)
	tok2 := verified["access_token"].(string)

	// Reusing the same recovery code on a new challenge must fail
	ch2 := env.mustJSON("", "POST", "/api/v1/auth/login", map[string]any{
		"email": email, "password": pw, "client_type": "mobile",
	}, "", 200)
	status, _ = env.do("", "POST", "/api/v1/auth/mfa/verify", map[string]any{
		"challenge_token": ch2["challenge_token"].(string), "recovery_code": rec1, "client_type": "mobile",
	}, "")
	if status != 401 {
		t.Fatalf("expected 401 on reused recovery code, got %d", status)
	}

	// Regenerate recovery codes using a second valid recovery code
	regen := env.mustJSON(tok2, "POST", "/api/v1/auth/mfa/recovery-codes/regenerate", map[string]any{
		"current_password": pw, "recovery_code": rcodes[1].(string),
	}, "", 200)
	newCodes := regen["recovery_codes"].([]any)
	if len(newCodes) != 10 {
		t.Fatalf("expected 10 regenerated recovery codes, got %d", len(newCodes))
	}

	// Disable MFA using one of the new recovery codes
	status, raw := env.do(tok2, "POST", "/api/v1/auth/mfa/disable", map[string]any{
		"current_password": pw, "recovery_code": newCodes[0].(string),
	}, "")
	if status != 204 {
		t.Fatalf("expected 204 on MFA disable, got %d: %s", status, string(raw))
	}

	// Old session must now be revoked
	status, _ = env.do(tok2, "GET", "/api/v1/auth/me", nil, "")
	if status != 401 {
		t.Fatalf("expected 401 after MFA disable revoked sessions, got %d", status)
	}

	// Login now succeeds directly with password
	post := env.mustJSON("", "POST", "/api/v1/auth/login", map[string]any{
		"email": email, "password": pw, "client_type": "mobile",
	}, "", 200)
	if post["access_token"] == nil {
		t.Fatalf("expected direct access_token after disabling MFA, got %v", post)
	}
}

func TestPatientExtendedAndMerge(t *testing.T) {
	env := setupEnv(t)
	p1 := env.createPatient(numTag())
	p2 := env.createPatient(numTag())
	nida := "19910615-" + numTag()[:5] + "-" + numTag()[:5] + "-12"

	// Add encrypted identifier
	idObj := env.mustJSON(env.tokens["reception"], "POST", "/api/v1/staff/patients/"+p1+"/identifiers", map[string]any{
		"id_type": "nida_nin", "value": nida, "issuing_country": "TZ",
	}, uuid.NewString(), 201)
	if idObj["value_ciphertext"] != nil || idObj["value_bidx"] != nil {
		t.Fatalf("ciphertext/blind index must never leak in API response")
	}
	if idObj["value_last4"].(string) == "" {
		t.Fatalf("expected value_last4")
	}
	identID := idObj["id"].(string)

	// Duplicate active identifier on another patient is blocked
	status, _ := env.do(env.tokens["reception"], "POST", "/api/v1/staff/patients/"+p2+"/identifiers", map[string]any{
		"id_type": "nida_nin", "value": nida,
	}, uuid.NewString())
	if status != 409 {
		t.Fatalf("expected 409 on duplicate identifier, got %d", status)
	}

	// Blind-index search finds p1
	srch := env.mustJSON(env.tokens["reception"], "POST", "/api/v1/staff/patient-identifiers/search", map[string]any{
		"id_type": "nida_nin", "value": nida,
	}, "", 200)
	pids := srch["patient_ids"].([]any)
	if len(pids) != 1 || pids[0].(string) != p1 {
		t.Fatalf("expected blind search to find %s, got %v", p1, pids)
	}

	// Reveal identifier with mandatory reason
	rev := env.mustJSON(env.tokens["reception"], "POST", "/api/v1/staff/patients/"+p1+"/identifiers/"+identID+"/reveal", map[string]any{
		"reason": "Verify NIDA card at reception desk",
	}, "", 200)
	if rev["value"].(string) != nida {
		t.Fatalf("expected revealed value %q, got %q", nida, rev["value"])
	}

	// Contacts, Next of Kin, Consents
	c := env.mustJSON(env.tokens["reception"], "POST", "/api/v1/staff/patients/"+p1+"/contacts", map[string]any{
		"system": "email", "value": "patient-" + numTag() + "@example.com", "is_primary": true,
	}, "", 201)
	env.mustJSON(env.tokens["reception"], "PUT", "/api/v1/staff/patients/"+p1+"/contacts/"+c["id"].(string), map[string]any{
		"status": "inactive", "version": int(c["version"].(float64)),
	}, "", 200)
	nok := env.mustJSON(env.tokens["reception"], "POST", "/api/v1/staff/patients/"+p1+"/next-of-kin", map[string]any{
		"full_name": "Amina Juma", "relationship": "spouse", "phone": "0712999888", "priority": 1,
	}, "", 201)
	env.mustJSON(env.tokens["reception"], "PUT", "/api/v1/staff/patients/"+p1+"/next-of-kin/"+nok["id"].(string), map[string]any{
		"address": "Mikocheni, Dar es Salaam", "version": int(nok["version"].(float64)),
	}, "", 200)
	cs := env.mustJSON(env.tokens["reception"], "POST", "/api/v1/staff/patients/"+p1+"/consents", map[string]any{
		"consent_type": "treatment_general", "capture_method": "written", "policy_version": "v1.0",
	}, "", 201)
	env.mustJSON(env.tokens["reception"], "POST", "/api/v1/staff/consents/"+cs["id"].(string)+"/withdraw", map[string]any{
		"reason": "Patient withdrew general consent", "version": int(cs["version"].(float64)),
	}, "", 200)

	// Duplicate review & merge
	env.mustJSON(env.tokens["reception"], "POST", "/api/v1/staff/patients/duplicates/check", map[string]any{
		"given_name": "IntTest", "family_name": "User", "birth_date": "1991-06-15",
	}, "", 200)
	dr := env.mustJSON(env.tokens["reception"], "POST", "/api/v1/staff/patients/duplicates", map[string]any{
		"patient_id_a": p1, "patient_id_b": p2, "match_score": 0.91, "match_reasons": []string{"name", "dob"},
	}, "", 201)
	dr = env.mustJSON(env.tokens["reception"], "POST", "/api/v1/staff/patients/duplicates/"+dr["id"].(string)+"/resolve", map[string]any{
		"status": "confirmed_duplicate", "note": "Same person registered twice", "version": int(dr["version"].(float64)),
	}, "", 200)
	merged := env.mustJSON(env.tokens["privacy-reviewer"], "POST", "/api/v1/staff/patients/merge", map[string]any{
		"survivor_patient_id": p1,
		"merged_patient_id":   p2,
		"duplicate_review_id": dr["id"].(string),
		"reason":              "Verified duplicate registration during audit",
	}, "", 200)
	if merged["survivor_patient_id"].(string) != p1 || merged["merged_patient_id"].(string) != p2 {
		t.Fatalf("unexpected merge response: %v", merged)
	}
}

func TestClinicalHistory(t *testing.T) {
	env := setupEnv(t)
	pid := env.createPatient(numTag())

	// Allergies
	al := env.mustJSON(env.tokens["nurse"], "POST", "/api/v1/staff/patients/"+pid+"/allergies", map[string]any{
		"substance": "Penicillin", "category": "drug", "reaction": "Urticaria", "severity": "severe", "verification": "confirmed",
	}, "", 201)
	// Pharmacist can read allergies (pharmacy.read)
	alList := env.mustJSON(env.tokens["pharmacy"], "GET", "/api/v1/staff/patients/"+pid+"/allergies", nil, "", 200)
	if len(alList["items"].([]any)) != 1 {
		t.Fatalf("expected pharmacist to see 1 allergy")
	}
	env.mustJSON(env.tokens["doctor"], "PUT", "/api/v1/staff/allergies/"+al["id"].(string), map[string]any{
		"status": "inactive", "version": int(al["version"].(float64)),
	}, "", 200)

	// Problems
	pr := env.mustJSON(env.tokens["doctor"], "POST", "/api/v1/staff/patients/"+pid+"/problems", map[string]any{
		"icd10_code": "I10", "description": "Essential hypertension", "onset_date": "2024-01-10",
	}, "", 201)
	// Resolving without resolved_date fails 422
	status, _ := env.do(env.tokens["doctor"], "PUT", "/api/v1/staff/problems/"+pr["id"].(string), map[string]any{
		"status": "resolved", "version": int(pr["version"].(float64)),
	}, "")
	if status != 422 {
		t.Fatalf("expected 422 without resolved_date, got %d", status)
	}
	env.mustJSON(env.tokens["doctor"], "PUT", "/api/v1/staff/problems/"+pr["id"].(string), map[string]any{
		"status": "resolved", "resolved_date": "2026-06-01", "version": int(pr["version"].(float64)),
	}, "", 200)

	// History entries
	he := env.mustJSON(env.tokens["nurse"], "POST", "/api/v1/staff/patients/"+pid+"/history", map[string]any{
		"history_type": "surgical", "description": "Appendectomy", "occurred_on": "2018-03-14",
	}, "", 201)
	env.mustJSON(env.tokens["doctor"], "GET", "/api/v1/staff/patients/"+pid+"/history?history_type=surgical", nil, "", 200)
	env.mustJSON(env.tokens["doctor"], "POST", "/api/v1/staff/history/"+he["id"].(string)+"/void", map[string]any{
		"reason": "Entered on wrong chart", "version": int(he["version"].(float64)),
	}, "", 200)
}

func TestAppointmentsAndTelehealth(t *testing.T) {
	env := setupEnv(t)
	pid := env.createPatient(numTag())
	start := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Minute)
	end := start.Add(30 * time.Minute)

	// Book telemedicine appointment
	appt := env.mustJSON(env.tokens["reception"], "POST", "/api/v1/staff/appointments", map[string]any{
		"facility_id":      env.facID,
		"department_id":    env.depts["OPD"],
		"patient_id":       pid,
		"appointment_type": "telemedicine",
		"source":           "reception",
		"scheduled_start":  start.Format(time.RFC3339),
		"scheduled_end":    end.Format(time.RFC3339),
		"reason":           "Telemedicine follow-up",
	}, uuid.NewString(), 201)
	aid := appt["id"].(string)

	// Overlapping appointment for the same patient must fail with 409 (GiST exclusion constraint)
	status, _ := env.do(env.tokens["reception"], "POST", "/api/v1/staff/appointments", map[string]any{
		"facility_id":      env.facID,
		"department_id":    env.depts["OPD"],
		"patient_id":       pid,
		"appointment_type": "in_person",
		"scheduled_start":  start.Add(10 * time.Minute).Format(time.RFC3339),
		"scheduled_end":    end.Add(10 * time.Minute).Format(time.RFC3339),
	}, uuid.NewString())
	if status != 409 {
		t.Fatalf("expected 409 on overlapping appointment, got %d", status)
	}

	// Confirm appointment
	appt = env.mustJSON(env.tokens["reception"], "POST", "/api/v1/staff/appointments/"+aid+"/status", map[string]any{
		"status": "confirmed", "version": int(appt["version"].(float64)),
	}, "", 200)

	// Telehealth session without telemedicine consent fails with 422
	status, _ = env.do(env.tokens["doctor"], "POST", "/api/v1/staff/telehealth/sessions", map[string]any{
		"appointment_id": aid, "provider": "jitsi_self_hosted",
	}, "")
	if status != 422 {
		t.Fatalf("expected 422 without telemedicine consent, got %d", status)
	}

	// Grant telemedicine consent, then create session
	env.mustJSON(env.tokens["reception"], "POST", "/api/v1/staff/patients/"+pid+"/consents", map[string]any{
		"consent_type": "telemedicine", "capture_method": "written", "policy_version": "tele-v1",
	}, "", 201)
	sess := env.mustJSON(env.tokens["doctor"], "POST", "/api/v1/staff/telehealth/sessions", map[string]any{
		"appointment_id": aid, "provider": "jitsi_self_hosted",
	}, "", 201)
	sid := sess["id"].(string)
	sess = env.mustJSON(env.tokens["doctor"], "POST", "/api/v1/staff/telehealth/sessions/"+sid+"/status", map[string]any{
		"status": "in_progress", "version": int(sess["version"].(float64)),
	}, "", 200)
	env.mustJSON(env.tokens["doctor"], "POST", "/api/v1/staff/telehealth/sessions/"+sid+"/events", map[string]any{
		"event_type": "participant_joined", "participant_role": "practitioner", "detail": map[string]any{"client": "web"},
	}, "", 201)
	sess = env.mustJSON(env.tokens["doctor"], "POST", "/api/v1/staff/telehealth/sessions/"+sid+"/status", map[string]any{
		"status": "completed", "version": int(sess["version"].(float64)),
	}, "", 200)
	detail := env.mustJSON(env.tokens["doctor"], "GET", "/api/v1/staff/telehealth/sessions/"+sid, nil, "", 200)
	if len(detail["events"].([]any)) != 1 {
		t.Fatalf("expected 1 session event, got %v", detail["events"])
	}
}

func TestDocumentsAndCoverages(t *testing.T) {
	env := setupEnv(t)
	pid := env.createPatient(numTag())

	// Upload inline document
	payload := base64.StdEncoding.EncodeToString([]byte("Synthetic clinical referral letter"))
	doc := env.mustJSON(env.tokens["reception"], "POST", "/api/v1/staff/patients/"+pid+"/documents", map[string]any{
		"category":       "referral_letter",
		"title":          "Referral from Muhimbili",
		"content_type":   "text/plain",
		"content_base64": payload,
	}, uuid.NewString(), 201)
	did := doc["id"].(string)
	if doc["status"].(string) != "available" {
		t.Fatalf("expected inline document to become available, got %v", doc["status"])
	}

	// Download returns original content
	dl := env.mustJSON(env.tokens["doctor"], "GET", "/api/v1/staff/documents/"+did+"/download", nil, "", 200)
	if dl["content_base64"].(string) != payload {
		t.Fatalf("downloaded content mismatch")
	}

	// Legal hold blocks deletion
	doc = env.mustJSON(env.tokens["reception"], "POST", "/api/v1/staff/documents/"+did+"/status", map[string]any{
		"legal_hold": true, "version": int(doc["version"].(float64)),
	}, "", 200)
	status, _ := env.do(env.tokens["privacy-reviewer"], "POST", "/api/v1/staff/documents/"+did+"/delete", map[string]any{
		"reason": "Duplicate upload", "version": int(doc["version"].(float64)),
	}, "")
	if status != 409 {
		t.Fatalf("expected 409 deleting document under legal hold, got %d", status)
	}
	// Release legal hold and delete
	doc = env.mustJSON(env.tokens["privacy-reviewer"], "POST", "/api/v1/staff/documents/"+did+"/status", map[string]any{
		"legal_hold": false, "version": int(doc["version"].(float64)),
	}, "", 200)
	deleted := env.mustJSON(env.tokens["privacy-reviewer"], "POST", "/api/v1/staff/documents/"+did+"/delete", map[string]any{
		"reason": "Duplicate upload removed after hold release", "version": int(doc["version"].(float64)),
	}, "", 200)
	if deleted["status"].(string) != "deleted" {
		t.Fatalf("expected deleted status, got %v", deleted["status"])
	}

	// Insurance coverages
	payerCode := "NHIF" + numTag()[:4]
	payer := env.mustJSON(env.tokens["billing-supervisor"], "POST", "/api/v1/staff/billing/payers", map[string]any{
		"code": payerCode, "name": "NHIF Test " + payerCode, "payer_type": "insurance",
	}, "", 201)
	memberNo := "NHIF-" + numTag()
	cov := env.mustJSON(env.tokens["cashier"], "POST", "/api/v1/staff/patients/"+pid+"/coverages", map[string]any{
		"payer_id": payer["id"].(string), "member_no": memberNo, "scheme_name": "Standard", "valid_from": "2026-01-01",
	}, "", 201)
	if cov["member_no_ciphertext"] != nil || cov["member_no_bidx"] != nil {
		t.Fatalf("coverage ciphertext/bidx must not leak")
	}
	cid := cov["id"].(string)
	rev := env.mustJSON(env.tokens["billing-supervisor"], "POST", "/api/v1/staff/coverages/"+cid+"/reveal", map[string]any{
		"reason": "Confirm NHIF membership number for claim",
	}, "", 200)
	if rev["member_no"].(string) != memberNo {
		t.Fatalf("expected %q, got %q", memberNo, rev["member_no"])
	}
	env.mustJSON(env.tokens["cashier"], "PUT", "/api/v1/staff/coverages/"+cid, map[string]any{
		"verified": true, "status": "active", "version": int(cov["version"].(float64)),
	}, "", 200)
}

func TestPrivacyGovernanceApprovalsReportsAndWorker(t *testing.T) {
	env := setupEnv(t)
	pid := env.createPatient(numTag())

	// Break-glass emergency access + compliance review + patient access report
	bg := env.mustJSON(env.tokens["doctor"], "POST", "/api/v1/staff/patients/"+pid+"/break-glass", map[string]any{
		"reason": "Emergency trauma stabilization requiring immediate full chart", "duration_minutes": 30,
	}, uuid.NewString(), 201)
	bgID := bg["id"].(string)
	pending := env.mustJSON(env.tokens["privacy-reviewer"], "GET", "/api/v1/staff/reports/break-glass-pending", nil, "", 200)
	if len(pending["items"].([]any)) == 0 {
		t.Fatalf("expected pending break-glass grant")
	}
	env.mustJSON(env.tokens["privacy-reviewer"], "POST", "/api/v1/staff/privacy/break-glass/"+bgID+"/review", map[string]any{
		"outcome": "appropriate", "note": "Verified emergency encounter",
	}, "", 200)
	env.mustJSON(env.tokens["doctor"], "POST", "/api/v1/staff/break-glass/"+bgID+"/end", nil, "", 200)
	acc := env.mustJSON(env.tokens["privacy-reviewer"], "GET", "/api/v1/staff/privacy/patients/"+pid+"/access", nil, "", 200)
	if len(acc["items"].([]any)) == 0 {
		t.Fatalf("expected patient access audit entries")
	}

	// Privacy request + legal hold
	prq := env.mustJSON(env.tokens["privacy-reviewer"], "POST", "/api/v1/staff/privacy/requests", map[string]any{
		"patient_id": pid, "request_type": "access", "received_via": "in_person",
	}, "", 201)
	prq = env.mustJSON(env.tokens["privacy-reviewer"], "POST", "/api/v1/staff/privacy/requests/"+prq["id"].(string)+"/status", map[string]any{
		"status": "identity_verified", "version": int(prq["version"].(float64)),
	}, "", 200)
	prq = env.mustJSON(env.tokens["privacy-reviewer"], "POST", "/api/v1/staff/privacy/requests/"+prq["id"].(string)+"/status", map[string]any{
		"status": "in_progress", "version": int(prq["version"].(float64)),
	}, "", 200)
	env.mustJSON(env.tokens["privacy-reviewer"], "POST", "/api/v1/staff/privacy/requests/"+prq["id"].(string)+"/status", map[string]any{
		"status": "fulfilled", "outcome_note": "Provided record copy", "version": int(prq["version"].(float64)),
	}, "", 200)

	lh := env.mustJSON(env.tokens["privacy-reviewer"], "POST", "/api/v1/staff/privacy/legal-holds", map[string]any{
		"scope": "patient", "patient_id": pid, "reason": "Pending medico-legal inquiry", "reference": "CASE-2026-01",
	}, "", 201)
	env.mustJSON(env.tokens["privacy-reviewer"], "POST", "/api/v1/staff/privacy/legal-holds/"+lh["id"].(string)+"/release", nil, "", 200)

	// Access review (create, decide an item, cancel so seeded roles stay untouched)
	ar := env.mustJSON(env.tokens["privacy-reviewer"], "POST", "/api/v1/staff/access-reviews", map[string]any{
		"title": "Q4 Access Review " + numTag(), "period_start": "2026-10-01", "period_end": "2026-12-31",
	}, "", 201)
	arItems := ar["items"].([]any)
	if len(arItems) == 0 {
		t.Fatalf("expected access review to populate active role items")
	}
	firstItem := arItems[0].(map[string]any)["id"].(string)
	env.mustJSON(env.tokens["privacy-reviewer"], "POST", "/api/v1/staff/access-reviews/"+ar["id"].(string)+"/items/"+firstItem+"/decide", map[string]any{
		"decision": "keep", "note": "Role verified",
	}, "", 200)
	env.mustJSON(env.tokens["privacy-reviewer"], "POST", "/api/v1/staff/access-reviews/"+ar["id"].(string)+"/complete", map[string]any{
		"status": "cancelled", "version": int(ar["version"].(float64)),
	}, "", 200)

	// Dual-control approvals (four-eyes)
	ap := env.mustJSON(env.tokens["reception"], "POST", "/api/v1/staff/approvals", map[string]any{
		"action_type": "patient_merge", "resource_type": "patient", "resource_id": pid,
		"reason": "Requesting approval to merge duplicate record",
	}, uuid.NewString(), 201)
	env.mustJSON(env.tokens["privacy-reviewer"], "POST", "/api/v1/staff/approvals/"+ap["id"].(string)+"/approve", map[string]any{
		"note": "Verified documentation", "version": int(ap["version"].(float64)),
	}, "", 200)
	// Self-approval must fail (422)
	selfAp := env.mustJSON(env.tokens["privacy-reviewer"], "POST", "/api/v1/staff/approvals", map[string]any{
		"action_type": "data_export", "resource_type": "patient", "resource_id": pid,
		"reason": "Self-requested export should require another reviewer",
	}, uuid.NewString(), 201)
	status, _ := env.do(env.tokens["privacy-reviewer"], "POST", "/api/v1/staff/approvals/"+selfAp["id"].(string)+"/approve", map[string]any{
		"note": "Trying to self-approve", "version": int(selfAp["version"].(float64)),
	}, "")
	if status != 422 {
		t.Fatalf("expected 422 on self-approval, got %d", status)
	}

	// Reporting views
	for _, rpath := range []string{
		"/api/v1/staff/reports/daily-visits",
		"/api/v1/staff/reports/daily-revenue",
		"/api/v1/staff/reports/receivables",
		"/api/v1/staff/reports/billing-reconciliation",
		"/api/v1/staff/reports/queue-wait-times",
		"/api/v1/staff/reports/stock-expiry",
	} {
		env.mustJSON(env.tokens["management"], "GET", rpath, nil, "", 200)
	}

	// Comms templates + outbox + worker dispatch & audit sealing
	alphaTag := func() string {
		s := numTag()
		b := make([]byte, len(s))
		for i := range s {
			b[i] = 'a' + (s[i] - '0')
		}
		return string(b)
	}()
	tpl := env.mustJSON(env.tokens["admin"], "POST", "/api/v1/staff/comms/templates", map[string]any{
		"code": "appt_confirm_" + alphaTag, "channel": "sms", "language": "en", "body": "Your appointment is confirmed.",
	}, "", 201)
	env.mustJSON(env.tokens["admin"], "POST", "/api/v1/staff/comms/templates/"+tpl["id"].(string)+"/activate", map[string]any{
		"version": int(tpl["version"].(float64)),
	}, "", 200)
	env.mustJSON(env.tokens["admin"], "POST", "/api/v1/staff/comms/outbox", map[string]any{
		"event_type": "appointment.reminder", "aggregate_type": "patient", "aggregate_id": pid, "payload": map[string]any{"channel": "sms"},
	}, "", 201)

	if env.workerDB != nil {
		ctx := context.Background()
		if _, err := platform.ReapStaleOutbox(ctx, env.workerDB); err != nil {
			t.Fatalf("ReapStaleOutbox: %v", err)
		}
		j, err := platform.ClaimOutbox(ctx, env.workerDB, "test-worker")
		if err != nil {
			t.Fatalf("ClaimOutbox: %v", err)
		}
		if err := platform.CompleteOutbox(ctx, env.workerDB, j); err != nil {
			t.Fatalf("CompleteOutbox: %v", err)
		}
		if _, _, _, err := platform.SealAuditPending(ctx, env.workerDB); err != nil {
			t.Fatalf("SealAuditPending: %v", err)
		}
		if _, err := platform.DetectPrivacyAlerts(ctx, env.workerDB, 1000, 1000); err != nil {
			t.Fatalf("DetectPrivacyAlerts: %v", err)
		}
	}
}

func TestAdminPortalResetAndDevices(t *testing.T) {
	env := setupEnv(t)
	pid := env.createPatient(numTag())

	// Organization settings read & update
	org := env.mustJSON(env.tokens["admin"], "GET", "/api/v1/staff/organization", nil, "", 200)
	env.mustJSON(env.tokens["admin"], "PUT", "/api/v1/staff/organization", map[string]any{
		"display_name": org["display_name"].(string),
		"version":      int(org["version"].(float64)),
	}, "", 200)

	// Permissions & custom RBAC role
	perms := env.mustJSON(env.tokens["admin"], "GET", "/api/v1/staff/permissions", nil, "", 200)
	if len(perms["items"].([]any)) < 40 {
		t.Fatalf("expected full permission catalogue")
	}
	alphaTag := func() string {
		s := numTag()
		b := make([]byte, len(s))
		for i := range s {
			b[i] = 'a' + (s[i] - '0')
		}
		return string(b)
	}()
	role := env.mustJSON(env.tokens["admin"], "POST", "/api/v1/staff/roles", map[string]any{
		"code": "custom_" + alphaTag, "name": "Custom Auditor", "permissions": []string{"report.view"},
	}, "", 201)
	env.mustJSON(env.tokens["admin"], "PUT", "/api/v1/staff/roles/"+role["id"].(string), map[string]any{
		"name": "Custom Auditor Updated", "status": "retired", "permissions": []string{"report.view"}, "version": int(role["version"].(float64)),
	}, "", 200)

	// API credential create & revoke
	me := env.mustJSON(env.tokens["admin"], "GET", "/api/v1/auth/me", nil, "", 200)
	cred := env.mustJSON(env.tokens["admin"], "POST", "/api/v1/staff/api-credentials", map[string]any{
		"user_id": me["id"].(string), "name": "Integration Key " + alphaTag, "expires_in_days": 30,
	}, "", 201)
	if cred["api_key"].(string) == "" || cred["key_hash"] != nil {
		t.Fatalf("expected api_key once and no key_hash")
	}
	env.mustJSON(env.tokens["admin"], "POST", "/api/v1/staff/api-credentials/"+cred["id"].(string)+"/revoke", nil, "", 200)

	// Integration provider config & governance data categories
	prov := env.mustJSON(env.tokens["admin"], "POST", "/api/v1/staff/integrations/providers", map[string]any{
		"provider": "provider_" + alphaTag, "purpose": "sms", "status": "sandbox",
		"credentials_secret_ref": "vault://spartial/sms/" + alphaTag,
	}, "", 201)
	env.mustJSON(env.tokens["admin"], "PUT", "/api/v1/staff/integrations/providers/"+prov["id"].(string), map[string]any{
		"status": "live", "dpa_signed_at": "2026-01-15", "version": int(prov["version"].(float64)),
	}, "", 200)
	cats := env.mustJSON(env.tokens["admin"], "GET", "/api/v1/staff/governance/data-categories", nil, "", 200)
	if len(cats["items"].([]any)) == 0 {
		t.Fatalf("expected governance data categories")
	}

	// Patient portal account link & status transition
	pa := env.mustJSON(env.tokens["reception"], "POST", "/api/v1/staff/patients/"+pid+"/portal-accounts", map[string]any{
		"phone": "+255713" + numTag()[:6], "display_name": "Portal Patient", "access_type": "self",
		"activate": true, "verification_method": "in_person_id_check",
	}, "", 201)
	env.mustJSON(env.tokens["reception"], "POST", "/api/v1/staff/portal-accounts/"+pa["id"].(string)+"/status", map[string]any{
		"status": "suspended", "version": int(pa["version"].(float64)),
	}, "", 200)

	// Password reset request + confirm on a temporary staff user
	tmpEmail := "reset-" + alphaTag + "@spartial.test"
	oldPw := "OldResetPass-" + alphaTag + "-2026!"
	newPw := "NewResetPass-" + alphaTag + "-2026!"
	env.mustJSON(env.tokens["admin"], "POST", "/api/v1/staff/users", map[string]any{
		"email": tmpEmail, "display_name": "Reset User", "password": oldPw,
		"role": "receptionist", "facility_ids": []string{env.facID},
	}, "", 201)
	rst := env.mustJSON("", "POST", "/api/v1/auth/password-reset/request", map[string]any{"email": tmpEmail}, "", 202)
	rtok := rst["dev_reset_token"].(string)
	status, raw := env.do("", "POST", "/api/v1/auth/password-reset/confirm", map[string]any{
		"token": rtok, "new_password": newPw,
	}, "")
	if status != 204 {
		t.Fatalf("expected 204 on password reset confirm, got %d: %s", status, string(raw))
	}
	logged := env.mustJSON("", "POST", "/api/v1/auth/login", map[string]any{
		"email": tmpEmail, "password": newPw, "client_type": "mobile",
	}, "", 200)
	tmpTok := logged["access_token"].(string)

	// Push device registration & revoke
	dev := env.mustJSON(tmpTok, "POST", "/api/v1/auth/devices", map[string]any{
		"platform": "android", "push_token": "fcm-token-" + uuid.NewString() + "-device", "app_version": "1.0.0",
	}, "", 201)
	if dev["push_token_ciphertext"] != nil || dev["push_token_bidx"] != nil {
		t.Fatalf("device token ciphertext must not leak")
	}
	env.mustJSON(tmpTok, "GET", "/api/v1/auth/devices", nil, "", 200)
	env.mustJSON(tmpTok, "POST", "/api/v1/auth/devices/"+dev["id"].(string)+"/revoke", nil, "", 200)
}
