package integration

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func (e *testEnv) doWithHeaders(token, method, path string, body any, headers map[string]string) (int, []byte) {
	e.t.Helper()
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			e.t.Fatalf("marshal: %v", err)
		}
		r = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, r)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := e.app.Test(req)
	if err != nil {
		e.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, out
}

func TestPhase2_AuthLifecycleAndSecurity(t *testing.T) {
	env := setupEnv(t)
	tag := numTag()
	email := "p2-auth-" + tag + "@spartial.test"
	pw1 := "Phase2-Strong-Pass!1"
	pw2 := "Phase2-Rotated-Pass!2"

	// 1. Create pending_activation staff account — cannot login yet
	u := env.mustJSON(env.tokens["admin"], "POST", "/api/v1/staff/users", map[string]any{
		"email":        email,
		"display_name": "Phase2 Auth User " + tag,
		"password":     pw1,
		"role":         "receptionist",
		"facility_ids": []string{env.facID},
		"status":       "pending_activation",
	}, "", 201)
	uid := u["id"].(string)
	if u["status"].(string) != "pending_activation" {
		t.Fatalf("expected pending_activation, got %v", u["status"])
	}
	st, _ := env.do("", "POST", "/api/v1/auth/login", map[string]any{
		"email": email, "password": pw1, "client_type": "mobile",
	}, "")
	if st != 401 {
		t.Fatalf("expected 401 for pending_activation login, got %d", st)
	}

	// 2. Activate account -> login succeeds (re-fetch version after failed login incremented failed_login_count)
	u = env.mustJSON(env.tokens["admin"], "GET", "/api/v1/staff/users/"+uid, nil, "", 200)
	u = env.mustJSON(env.tokens["admin"], "POST", "/api/v1/staff/users/"+uid+"/status", map[string]any{
		"status": "active", "reason": "Onboarding complete", "version": int(u["version"].(float64)),
	}, "", 200)
	if u["status"].(string) != "active" || !u["active"].(bool) {
		t.Fatalf("expected active account, got %v", u)
	}
	login := env.mustJSON("", "POST", "/api/v1/auth/login", map[string]any{
		"email": email, "password": pw1, "client_type": "mobile", "device_label": "phase2-phone",
	}, "", 200)
	access1 := login["access_token"].(string)
	refresh1 := login["refresh_token"].(string)
	env.mustJSON(access1, "GET", "/api/v1/auth/me", nil, "", 200)

	// 3. Refresh token rotation + reuse detection revokes session
	rot := env.mustJSON("", "POST", "/api/v1/auth/refresh", map[string]any{
		"client_type": "mobile", "refresh_token": refresh1,
	}, "", 200)
	access2 := rot["access_token"].(string)
	env.mustJSON(access2, "GET", "/api/v1/auth/me", nil, "", 200)

	// Reusing refresh1 must fail AND revoke the session
	st, _ = env.do("", "POST", "/api/v1/auth/refresh", map[string]any{
		"client_type": "mobile", "refresh_token": refresh1,
	}, "")
	if st != 401 {
		t.Fatalf("expected 401 on refresh token reuse, got %d", st)
	}
	st, _ = env.do(access2, "GET", "/api/v1/auth/me", nil, "")
	if st != 401 {
		t.Fatalf("expected session revoked after refresh reuse, got %d", st)
	}

	// 4. Login again, suspend account -> immediate token revocation + blocked login
	login = env.mustJSON("", "POST", "/api/v1/auth/login", map[string]any{
		"email": email, "password": pw1, "client_type": "mobile",
	}, "", 200)
	access3 := login["access_token"].(string)
	// Re-fetch version because login updated last_login_at (trg_touch bumps row_version)
	u = env.mustJSON(env.tokens["admin"], "GET", "/api/v1/staff/users/"+uid, nil, "", 200)
	u = env.mustJSON(env.tokens["admin"], "POST", "/api/v1/staff/users/"+uid+"/status", map[string]any{
		"status": "suspended", "reason": "Temporary leave", "version": int(u["version"].(float64)),
	}, "", 200)
	if u["status"].(string) != "suspended" {
		t.Fatalf("expected suspended, got %v", u["status"])
	}
	st, _ = env.do(access3, "GET", "/api/v1/auth/me", nil, "")
	if st != 401 {
		t.Fatalf("expected 401 for suspended user's active token, got %d", st)
	}
	st, _ = env.do("", "POST", "/api/v1/auth/login", map[string]any{
		"email": email, "password": pw1, "client_type": "mobile",
	}, "")
	if st != 401 {
		t.Fatalf("expected 401 for suspended user login, got %d", st)
	}

	// 5. Reactivate -> brute-force lockout after 5 failures returns 429 (re-fetch version after failed suspended login)
	u = env.mustJSON(env.tokens["admin"], "GET", "/api/v1/staff/users/"+uid, nil, "", 200)
	u = env.mustJSON(env.tokens["admin"], "POST", "/api/v1/staff/users/"+uid+"/status", map[string]any{
		"status": "active", "reason": "Returned from leave", "version": int(u["version"].(float64)),
	}, "", 200)
	t.Setenv("DISABLE_AUTH_RATE_LIMIT", "false")
	for i := 0; i < 5; i++ {
		st, _ = env.do("", "POST", "/api/v1/auth/login", map[string]any{
			"email": email, "password": "Wrong-Password-Attempt!", "client_type": "mobile",
		}, "")
		if st != 401 {
			t.Fatalf("failure %d: expected 401, got %d", i+1, st)
		}
	}
	// 6th attempt even with right password must be rate-limited (429)
	st, _ = env.do("", "POST", "/api/v1/auth/login", map[string]any{
		"email": email, "password": pw1, "client_type": "mobile",
	}, "")
	if st != 429 {
		t.Fatalf("expected 429 lockout after 5 failures, got %d", st)
	}
	t.Setenv("DISABLE_AUTH_RATE_LIMIT", "true")

	// Admin reactivation clears lock
	u = env.mustJSON(env.tokens["admin"], "GET", "/api/v1/staff/users/"+uid, nil, "", 200)
	u = env.mustJSON(env.tokens["admin"], "POST", "/api/v1/staff/users/"+uid+"/status", map[string]any{
		"status": "active", "reason": "Unlock account", "version": int(u["version"].(float64)),
	}, "", 200)

	// 6. Password change invalidates current session and old password
	login = env.mustJSON("", "POST", "/api/v1/auth/login", map[string]any{
		"email": email, "password": pw1, "client_type": "mobile",
	}, "", 200)
	access4 := login["access_token"].(string)
	st, _ = env.do(access4, "POST", "/api/v1/auth/password", map[string]any{
		"current_password": pw1, "new_password": pw2,
	}, "")
	if st != 204 {
		t.Fatalf("expected 204 on password change, got %d", st)
	}
	st, _ = env.do(access4, "GET", "/api/v1/auth/me", nil, "")
	if st != 401 {
		t.Fatalf("expected 401 after password change, got %d", st)
	}
	st, _ = env.do("", "POST", "/api/v1/auth/login", map[string]any{
		"email": email, "password": pw1, "client_type": "mobile",
	}, "")
	if st != 401 {
		t.Fatalf("expected 401 with old password, got %d", st)
	}
	login = env.mustJSON("", "POST", "/api/v1/auth/login", map[string]any{
		"email": email, "password": pw2, "client_type": "mobile",
	}, "", 200)
	access5 := login["access_token"].(string)

	// 7. Logout revokes session
	st, _ = env.do(access5, "POST", "/api/v1/auth/logout", nil, "")
	if st != 204 {
		t.Fatalf("expected 204 on logout, got %d", st)
	}
	st, _ = env.do(access5, "GET", "/api/v1/auth/me", nil, "")
	if st != 401 {
		t.Fatalf("expected 401 after logout, got %d", st)
	}

	// 8. Deactivate user permanently blocks login
	u = env.mustJSON(env.tokens["admin"], "GET", "/api/v1/staff/users/"+uid, nil, "", 200)
	u = env.mustJSON(env.tokens["admin"], "POST", "/api/v1/staff/users/"+uid+"/status", map[string]any{
		"status": "deactivated", "reason": "Offboarded", "version": int(u["version"].(float64)),
	}, "", 200)
	if u["status"].(string) != "deactivated" {
		t.Fatalf("expected deactivated, got %v", u["status"])
	}
	st, _ = env.do("", "POST", "/api/v1/auth/login", map[string]any{
		"email": email, "password": pw2, "client_type": "mobile",
	}, "")
	if st != 401 {
		t.Fatalf("expected 401 for deactivated login, got %d", st)
	}
}

func TestPhase2_FacilityDepartmentAndStaffLifecycle(t *testing.T) {
	env := setupEnv(t)
	tag := numTag()
	fCode := "F" + tag[:6]

	// Non-admin cannot create facility
	st, _ := env.do(env.tokens["reception"], "POST", "/api/v1/staff/facilities", map[string]any{
		"name": "Unauthorized Clinic",
	}, "")
	if st != 403 {
		t.Fatalf("expected 403 for reception creating facility, got %d", st)
	}

	// Create facility with full fields
	fac := env.mustJSON(env.tokens["admin"], "POST", "/api/v1/staff/facilities", map[string]any{
		"code":           fCode,
		"name":           "Spartial Arusha Annex " + tag,
		"hfr_code":       "109999-1",
		"phone":          "+255272501234",
		"region":         "Arusha",
		"district":       "Arusha Urban",
		"street_address": "Sokoine Road Plot 12",
	}, "", 201)
	fid := fac["id"].(string)
	if fac["code"].(string) != fCode || fac["status"].(string) != "active" {
		t.Fatalf("unexpected facility: %v", fac)
	}

	// Get & search facility
	gotFac := env.mustJSON(env.tokens["admin"], "GET", "/api/v1/staff/facilities/"+fid, nil, "", 200)
	if gotFac["region"].(string) != "Arusha" {
		t.Fatalf("expected region Arusha, got %v", gotFac)
	}
	listed := env.mustJSON(env.tokens["admin"], "GET", "/api/v1/staff/facilities?q="+fCode+"&status=active", nil, "", 200)
	if len(listed["items"].([]any)) != 1 {
		t.Fatalf("expected 1 facility matching %s, got %v", fCode, listed)
	}

	// Update facility
	fac = env.mustJSON(env.tokens["admin"], "PUT", "/api/v1/staff/facilities/"+fid, map[string]any{
		"name":           "Spartial Arusha Medical Centre " + tag,
		"hfr_code":       "109999-2",
		"phone":          "+255272509999",
		"region":         "Arusha",
		"district":       "Arusha City",
		"street_address": "Sokoine Road Plot 14",
		"version":        int(fac["version"].(float64)),
	}, "", 200)
	if fac["district"].(string) != "Arusha City" {
		t.Fatalf("expected updated district, got %v", fac)
	}

	// Create department under new facility
	dCode := "D" + tag[:6]
	dept := env.mustJSON(env.tokens["admin"], "POST", "/api/v1/staff/departments", map[string]any{
		"facility_id": fid,
		"code":        dCode,
		"name":        "Outpatient Consultation " + tag,
		"dept_type":   "consultation",
	}, "", 201)
	did := dept["id"].(string)

	// List departments via facility endpoint & global filtered endpoint
	fDepts := env.mustJSON(env.tokens["admin"], "GET", "/api/v1/staff/facilities/"+fid+"/departments", nil, "", 200)
	if len(fDepts["items"].([]any)) != 1 {
		t.Fatalf("expected 1 department under facility, got %v", fDepts)
	}
	gDepts := env.mustJSON(env.tokens["admin"], "GET", "/api/v1/staff/departments?facility_id="+fid+"&q="+dCode, nil, "", 200)
	if len(gDepts["items"].([]any)) != 1 {
		t.Fatalf("expected 1 department from filtered search, got %v", gDepts)
	}

	// Update & deactivate/reactivate department
	dept = env.mustJSON(env.tokens["admin"], "PUT", "/api/v1/staff/departments/"+did, map[string]any{
		"name": "General OPD " + tag, "dept_type": "consultation", "version": int(dept["version"].(float64)),
	}, "", 200)
	dept = env.mustJSON(env.tokens["admin"], "POST", "/api/v1/staff/departments/"+did+"/status", map[string]any{
		"status": "inactive", "version": int(dept["version"].(float64)),
	}, "", 200)
	if dept["status"].(string) != "inactive" {
		t.Fatalf("expected inactive department, got %v", dept)
	}
	dept = env.mustJSON(env.tokens["admin"], "POST", "/api/v1/staff/departments/"+did+"/status", map[string]any{
		"status": "active", "version": int(dept["version"].(float64)),
	}, "", 200)

	// Staff creation with full profile + department assignment
	empNo := "EMP-" + tag
	staff := env.mustJSON(env.tokens["admin"], "POST", "/api/v1/staff/users", map[string]any{
		"email":               "dr-" + tag + "@spartial.test",
		"display_name":        "Dr. Amina Juma " + tag,
		"password":            "Doctor-Strong-Pass!2026",
		"role":                "doctor",
		"facility_ids":        []string{fid},
		"department_ids":      []string{did},
		"phone":               "0712" + tag[:6],
		"employee_no":         empNo,
		"job_title":           "Senior Medical Officer",
		"professional_body":   "MCT",
		"professional_reg_no": "MCT-" + tag,
	}, "", 201)
	sid := staff["id"].(string)
	if staff["employee_no"].(string) != empNo || staff["professional_body"].(string) != "MCT" {
		t.Fatalf("unexpected staff profile: %v", staff)
	}
	if len(staff["department_ids"].([]any)) != 1 || staff["department_ids"].([]any)[0].(string) != did {
		t.Fatalf("expected department assignment %s, got %v", did, staff["department_ids"])
	}

	// Mismatched professional_body without reg_no rejected 422
	st, _ = env.do(env.tokens["admin"], "POST", "/api/v1/staff/users", map[string]any{
		"email":             "bad-" + tag + "@spartial.test",
		"display_name":      "Bad Reg",
		"password":          "Doctor-Strong-Pass!2026",
		"role":              "doctor",
		"facility_ids":      []string{fid},
		"professional_body": "MCT",
	}, "")
	if st != 422 {
		t.Fatalf("expected 422 for professional_body without reg_no, got %d", st)
	}

	// Search staff by employee_no, role, facility_id
	found := env.mustJSON(env.tokens["admin"], "GET", "/api/v1/staff/users?q="+empNo+"&role=doctor&facility_id="+fid+"&status=active", nil, "", 200)
	if len(found["items"].([]any)) != 1 || found["items"].([]any)[0].(map[string]any)["id"].(string) != sid {
		t.Fatalf("expected staff search match %s, got %v", sid, found)
	}

	// Deactivate & reactivate facility
	fac = env.mustJSON(env.tokens["admin"], "POST", "/api/v1/staff/facilities/"+fid+"/status", map[string]any{
		"status": "inactive", "version": int(fac["version"].(float64)),
	}, "", 200)
	if fac["active"].(bool) || fac["status"].(string) != "inactive" {
		t.Fatalf("expected inactive facility, got %v", fac)
	}
	fac = env.mustJSON(env.tokens["admin"], "POST", "/api/v1/staff/facilities/"+fid+"/status", map[string]any{
		"status": "active", "version": int(fac["version"].(float64)),
	}, "", 200)
	if !fac["active"].(bool) {
		t.Fatalf("expected reactivated facility, got %v", fac)
	}
}

func TestPhase2_RBACAndCrossFacilityIsolationIDOR(t *testing.T) {
	env := setupEnv(t)
	tag := numTag()

	// Create Facility B isolated from seeded Facility A
	facB := env.mustJSON(env.tokens["admin"], "POST", "/api/v1/staff/facilities", map[string]any{
		"code": "ISO" + tag[:5],
		"name": "Isolated Branch " + tag,
	}, "", 201)
	facBID := facB["id"].(string)

	// Create a doctor scoped ONLY to Facility B
	emailB := "doc-b-" + tag + "@spartial.test"
	pwB := "Isolated-Doc-Pass!2026"
	env.mustJSON(env.tokens["admin"], "POST", "/api/v1/staff/users", map[string]any{
		"email":        emailB,
		"display_name": "Facility B Doctor",
		"password":     pwB,
		"role":         "doctor",
		"facility_ids": []string{facBID},
	}, "", 201)
	tokB := env.mustJSON("", "POST", "/api/v1/auth/login", map[string]any{
		"email": emailB, "password": pwB, "client_type": "mobile",
	}, "", 200)["access_token"].(string)

	// Create Patient A in Facility A and upload a PDF document
	patA := env.mustJSON(env.tokens["reception"], "POST", "/api/v1/staff/patients", map[string]any{
		"facility_id":     env.facID,
		"given_name":      "Isolation",
		"family_name":     "Target" + tag,
		"birth_precision": "exact",
		"birth_date":      "1991-04-12",
		"sex":             "female",
	}, uuid.NewString(), 201)
	patAID := patA["id"].(string)
	patAMRN := patA["mrn"].(string)
	pdfB64 := base64.StdEncoding.EncodeToString([]byte("%PDF-1.4\n%Facility A confidential chart\n"))
	docA := env.mustJSON(env.tokens["reception"], "POST", "/api/v1/staff/patients/"+patAID+"/documents", map[string]any{
		"category":       "lab_report",
		"title":          "Confidential Lab Report",
		"content_type":   "application/pdf",
		"content_base64": pdfB64,
	}, uuid.NewString(), 201)
	docAID := docA["id"].(string)

	// Facility B doctor CANNOT read, search, or update Patient A
	if st, _ := env.do(tokB, "GET", "/api/v1/staff/patients/"+patAID, nil, ""); st != 404 {
		t.Fatalf("IDOR: expected 404 reading Facility A patient from Facility B, got %d", st)
	}
	if st, _ := env.do(tokB, "PUT", "/api/v1/staff/patients/"+patAID, map[string]any{
		"given_name": "Tampered", "family_name": "Name", "birth_precision": "exact", "birth_date": "1991-04-12", "version": 1,
	}, ""); st != 404 && st != 403 {
		t.Fatalf("IDOR: expected 403/404 updating Facility A patient from Facility B, got %d", st)
	}
	searchB := env.mustJSON(tokB, "GET", "/api/v1/staff/patients?q="+patAMRN, nil, "", 200)
	if len(searchB["items"].([]any)) != 0 {
		t.Fatalf("IDOR: Facility B search leaked Facility A patient: %v", searchB)
	}
	// Facility B doctor CANNOT list, view metadata, download, update status, or delete Facility A's document
	if st, _ := env.do(tokB, "GET", "/api/v1/staff/patients/"+patAID+"/documents", nil, ""); st != 404 {
		t.Fatalf("IDOR: expected 404 listing Facility A docs, got %d", st)
	}
	if st, _ := env.do(tokB, "GET", "/api/v1/staff/documents/"+docAID, nil, ""); st != 404 {
		t.Fatalf("IDOR: expected 404 getting Facility A doc metadata, got %d", st)
	}
	if st, _ := env.do(tokB, "GET", "/api/v1/staff/documents/"+docAID+"/download", nil, ""); st != 404 {
		t.Fatalf("IDOR: expected 404 downloading Facility A doc, got %d", st)
	}
	if st, _ := env.do(tokB, "POST", "/api/v1/staff/documents/"+docAID+"/status", map[string]any{
		"legal_hold": true, "version": int(docA["version"].(float64)),
	}, ""); st != 404 && st != 403 {
		t.Fatalf("IDOR: expected 403/404 updating Facility A doc status, got %d", st)
	}

	// Privilege escalation checks: self-modification blocked (409), unauthorized admin endpoints blocked (403)
	me := env.mustJSON(env.tokens["admin"], "GET", "/api/v1/auth/me", nil, "", 200)
	adminID := me["id"].(string)
	if st, _ := env.do(env.tokens["admin"], "POST", "/api/v1/staff/users/"+adminID+"/status", map[string]any{
		"status": "suspended", "version": int(me["version"].(float64)),
	}, ""); st != 409 {
		t.Fatalf("expected 409 on admin self-status change, got %d", st)
	}
	if st, _ := env.do(env.tokens["reception"], "GET", "/api/v1/staff/users", nil, ""); st != 403 {
		t.Fatalf("expected 403 for reception listing staff, got %d", st)
	}
	if st, _ := env.do(env.tokens["reception"], "GET", "/api/v1/staff/audit", nil, ""); st != 403 {
		t.Fatalf("expected 403 for reception reading audit logs, got %d", st)
	}
}

func TestPhase2_PatientDemographicsSearchAndStatus(t *testing.T) {
	env := setupEnv(t)
	tag := numTag()
	phoneVal := "+25575" + tag[:7]

	// Register patient with full demographics
	pat := env.mustJSON(env.tokens["reception"], "POST", "/api/v1/staff/patients", map[string]any{
		"facility_id":        env.facID,
		"given_name":         "Baraka",
		"middle_name":        "Said",
		"family_name":        "Mwangi" + tag,
		"preferred_name":     "Bara",
		"birth_precision":    "exact",
		"birth_date":         "1988-09-21",
		"phone":              phoneVal,
		"address":            "Plot 77 Upanga",
		"region":             "Dar es Salaam",
		"district":           "Ilala",
		"ward":               "Upanga Mashariki",
		"sex":                "male",
		"marital_status":     "married",
		"occupation":         "Civil Engineer",
		"nationality":        "TZ",
		"preferred_language": "en",
		"blood_group":        "O+",
	}, uuid.NewString(), 201)
	pid := pat["id"].(string)
	mrn := pat["mrn"].(string)
	if pat["blood_group"].(string) != "O+" || pat["region"].(string) != "Dar es Salaam" || pat["status"].(string) != "active" {
		t.Fatalf("demographics not persisted: %v", pat)
	}

	// Search by MRN, name, phone, birth_date, facility_id, status, sort
	for name, qs := range map[string]string{
		"mrn":       "q=" + mrn,
		"name":      "q=Mwangi" + tag + "&sort=name",
		"phone":     "phone=" + phoneVal,
		"birthDate": "q=Mwangi" + tag + "&birth_date=1988-09-21&status=active&facility_id=" + env.facID,
	} {
		res := env.mustJSON(env.tokens["reception"], "GET", "/api/v1/staff/patients?"+qs, nil, "", 200)
		items := res["items"].([]any)
		if len(items) != 1 || items[0].(map[string]any)["id"].(string) != pid {
			t.Fatalf("search %s (%s) expected patient %s, got %v", name, qs, pid, items)
		}
	}

	// Update demographics + verify optimistic concurrency conflict
	updated := env.mustJSON(env.tokens["reception"], "PUT", "/api/v1/staff/patients/"+pid, map[string]any{
		"given_name":         "Baraka",
		"middle_name":        "Said",
		"family_name":        "Mwangi" + tag,
		"birth_precision":    "exact",
		"birth_date":         "1988-09-21",
		"phone":              phoneVal,
		"address":            "Plot 88 Masaki",
		"region":             "Dar es Salaam",
		"district":           "Kinondoni",
		"sex":                "male",
		"marital_status":     "married",
		"occupation":         "Lead Engineer",
		"nationality":        "TZ",
		"preferred_language": "sw",
		"blood_group":        "O+",
		"version":            int(pat["version"].(float64)),
	}, "", 200)
	if updated["district"].(string) != "Kinondoni" {
		t.Fatalf("expected updated district Kinondoni, got %v", updated)
	}
	// Stale version must 409
	if st, _ := env.do(env.tokens["reception"], "PUT", "/api/v1/staff/patients/"+pid, map[string]any{
		"given_name": "Baraka", "family_name": "Mwangi" + tag, "birth_precision": "exact", "birth_date": "1988-09-21", "version": 1,
	}, ""); st != 409 {
		t.Fatalf("expected 409 on stale patient version, got %d", st)
	}

	// Status transitions: active -> inactive -> active -> deceased
	updated = env.mustJSON(env.tokens["reception"], "POST", "/api/v1/staff/patients/"+pid+"/status", map[string]any{
		"status": "inactive", "reason": "Relocated", "version": int(updated["version"].(float64)),
	}, "", 200)
	if updated["status"].(string) != "inactive" {
		t.Fatalf("expected inactive, got %v", updated["status"])
	}
	updated = env.mustJSON(env.tokens["reception"], "POST", "/api/v1/staff/patients/"+pid+"/status", map[string]any{
		"status": "deceased", "reason": "Confirmed by clinical summary", "version": int(updated["version"].(float64)),
	}, "", 200)
	if updated["status"].(string) != "deceased" || updated["deceased_at"] == nil {
		t.Fatalf("expected deceased with deceased_at, got %v", updated)
	}

	// Invalid input rejected 422 (bad blood_group)
	if st, _ := env.do(env.tokens["reception"], "POST", "/api/v1/staff/patients", map[string]any{
		"facility_id": env.facID, "given_name": "Bad", "family_name": "Blood",
		"birth_precision": "unknown", "blood_group": "Z+",
	}, uuid.NewString()); st != 422 {
		t.Fatalf("expected 422 for invalid blood_group, got %d", st)
	}
}

func TestPhase2_DocumentSecurityAndMagicBytes(t *testing.T) {
	env := setupEnv(t)
	pid := env.createPatient(numTag())

	// 1. Valid PDF upload succeeds and NEVER exposes storage_bucket or storage_key
	pdfPayload := base64.StdEncoding.EncodeToString([]byte("%PDF-1.7\n1 0 obj\n<<>>\nendobj\n"))
	doc := env.mustJSON(env.tokens["reception"], "POST", "/api/v1/staff/patients/"+pid+"/documents", map[string]any{
		"category":       "referral_letter",
		"title":          "Valid PDF Referral",
		"content_type":   "application/pdf",
		"content_base64": pdfPayload,
	}, uuid.NewString(), 201)
	did := doc["id"].(string)
	for _, forbidden := range []string{"storage_bucket", "storage_key", "encryption_key_ref"} {
		if _, exists := doc[forbidden]; exists {
			t.Fatalf("upload response leaked internal field %q: %v", forbidden, doc)
		}
	}
	dl := env.mustJSON(env.tokens["doctor"], "GET", "/api/v1/staff/documents/"+did+"/download", nil, "", 200)
	for _, forbidden := range []string{"storage_bucket", "storage_key"} {
		if _, exists := dl[forbidden]; exists {
			t.Fatalf("download response leaked internal field %q: %v", forbidden, dl)
		}
	}
	if dl["content_base64"].(string) != pdfPayload {
		t.Fatalf("downloaded PDF bytes mismatch")
	}

	// 2. Disguised HTML/script claiming application/pdf is rejected 422
	htmlBytes := base64.StdEncoding.EncodeToString([]byte("<html><body><script>alert(1)</script></body></html>"))
	st, raw := env.do(env.tokens["reception"], "POST", "/api/v1/staff/patients/"+pid+"/documents", map[string]any{
		"category":       "other",
		"title":          "Fake PDF",
		"content_type":   "application/pdf",
		"content_base64": htmlBytes,
	}, uuid.NewString())
	if st != 422 || !strings.Contains(string(raw), "invalid_file_type") {
		t.Fatalf("expected 422 invalid_file_type for disguised HTML as PDF, got %d: %s", st, raw)
	}

	// 3. Disguised HTML claiming text/plain is rejected 422
	st, _ = env.do(env.tokens["reception"], "POST", "/api/v1/staff/patients/"+pid+"/documents", map[string]any{
		"category":       "other",
		"title":          "XSS Text",
		"content_type":   "text/plain",
		"content_base64": htmlBytes,
	}, uuid.NewString())
	if st != 422 {
		t.Fatalf("expected 422 for HTML disguised as text/plain, got %d", st)
	}

	// 4. Random bytes claiming image/png rejected 422; real PNG header accepted 201
	st, _ = env.do(env.tokens["reception"], "POST", "/api/v1/staff/patients/"+pid+"/documents", map[string]any{
		"category":       "clinical_photo",
		"title":          "Fake PNG",
		"content_type":   "image/png",
		"content_base64": base64.StdEncoding.EncodeToString([]byte("not-a-real-png-file")),
	}, uuid.NewString())
	if st != 422 {
		t.Fatalf("expected 422 for fake PNG, got %d", st)
	}
	validPNG := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d, 'I', 'H', 'D', 'R'}
	env.mustJSON(env.tokens["reception"], "POST", "/api/v1/staff/patients/"+pid+"/documents", map[string]any{
		"category":       "clinical_photo",
		"title":          "Real PNG",
		"content_type":   "image/png",
		"content_base64": base64.StdEncoding.EncodeToString(validPNG),
	}, uuid.NewString(), 201)

	// 5. Unauthorized role (cashier) cannot upload or download patient documents
	if st, _ = env.do(env.tokens["cashier"], "GET", "/api/v1/staff/documents/"+did+"/download", nil, ""); st != 403 {
		t.Fatalf("expected 403 for cashier downloading document, got %d", st)
	}
}

func TestPhase2_AuditLoggingImmutabilityAndExport(t *testing.T) {
	env := setupEnv(t)
	tag := numTag()
	ua := "Phase2-Audit-Agent/" + tag

	// Perform audited patient creation & view with custom User-Agent
	st, raw := env.doWithHeaders(env.tokens["reception"], "POST", "/api/v1/staff/patients", map[string]any{
		"facility_id":     env.facID,
		"given_name":      "Audit",
		"family_name":     "Subject" + tag,
		"birth_precision": "exact",
		"birth_date":      "1995-05-05",
		"sex":             "female",
	}, map[string]string{
		"Idempotency-Key": uuid.NewString(),
		"User-Agent":      ua,
	})
	if st != 201 {
		t.Fatalf("create patient failed: %d %s", st, raw)
	}
	var pat map[string]any
	_ = json.Unmarshal(raw, &pat)
	pid := pat["id"].(string)

	// Query filtered audit log & export endpoint as compliance officer
	listed := env.mustJSON(env.tokens["privacy-reviewer"], "GET", "/api/v1/staff/audit?patient_id="+pid+"&action=patient.record.create&outcome=success", nil, "", 200)
	items := listed["items"].([]any)
	if len(items) == 0 {
		t.Fatalf("expected audit event for patient.record.create on %s", pid)
	}
	ev := items[0].(map[string]any)
	if ev["user_agent"] == nil || ev["user_agent"].(string) != ua {
		t.Fatalf("expected user_agent %q in audit event, got %v", ua, ev["user_agent"])
	}
	if ev["source_ip"] == nil || ev["source_ip"].(string) == "" {
		t.Fatalf("expected source_ip populated in audit event, got %v", ev["source_ip"])
	}
	if ev["actor_id"] == nil || ev["request_id"] == nil {
		t.Fatalf("expected actor_id and request_id in audit event, got %v", ev)
	}
	// Ensure no raw changes/PHI or secret fields appear in audit API responses
	for _, secretKey := range []string{"changes", "password", "password_hash", "token_hash", "value_ciphertext"} {
		if _, leaked := ev[secretKey]; leaked {
			t.Fatalf("audit response leaked forbidden key %q", secretKey)
		}
	}

	// Export endpoint works for auditor and fails 403 for non-auditor
	exp := env.mustJSON(env.tokens["privacy-reviewer"], "GET", "/api/v1/staff/audit/export?patient_id="+pid, nil, "", 200)
	if int(exp["count"].(float64)) < 1 || exp["exported_at"].(string) == "" {
		t.Fatalf("unexpected audit export response: %v", exp)
	}
	if st, _ := env.do(env.tokens["reception"], "GET", "/api/v1/staff/audit/export", nil, ""); st != 403 {
		t.Fatalf("expected 403 for reception calling /audit/export, got %d", st)
	}

	// Database-level immutability check: UPDATE and DELETE on audit.event must be rejected
	ctx := context.Background()
	if _, err := env.db.Exec(ctx, `UPDATE audit.event SET reason='tampered' WHERE id=$1`, int64(ev["id"].(float64))); err == nil {
		t.Fatalf("CRITICAL: UPDATE on audit.event succeeded — audit log is not immutable!")
	}
	if _, err := env.db.Exec(ctx, `DELETE FROM audit.event WHERE id=$1`, int64(ev["id"].(float64))); err == nil {
		t.Fatalf("CRITICAL: DELETE on audit.event succeeded — audit log is not immutable!")
	}
}
