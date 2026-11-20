package integration

import (
	"bytes"
	"encoding/base32"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"spartial.local/backend/internal/httpapi"
	"spartial.local/backend/internal/security"
)

func (e *testEnv) ensureRoleTokens(roles ...string) {
	e.t.Helper()
	for _, r := range roles {
		if e.tokens[r] == "" {
			e.tokens[r] = e.loginRole(r)
		}
	}
}

func TestWorkflow_VisitClosureGuards(t *testing.T) {
	env := setupEnv(t)
	tag := numTag()
	pid := env.createPatient(tag)

	// Open & start encounter
	enc := env.mustJSON(env.tokens["reception"], "POST", "/api/v1/staff/encounters", map[string]any{
		"patient_id": pid, "facility_id": env.facID, "encounter_type": "outpatient", "chief_complaint": "Closure guard check",
	}, uuid.NewString(), 201)
	eid := enc["id"].(string)
	enc = env.mustJSON(env.tokens["reception"], "POST", "/api/v1/staff/encounters/"+eid+"/status", map[string]any{
		"status": "in_progress", "version": int(enc["version"].(float64)),
	}, "", 200)
	encVer := int(enc["version"].(float64))

	// 1. Draft triage blocks visit closure (even when receptionist has no clinical.read!)
	tr := env.mustJSON(env.tokens["nurse"], "POST", "/api/v1/staff/encounters/"+eid+"/triage", map[string]any{
		"temperature_c": 37.2, "pulse_bpm": 78, "triage_category": "queue", "finalise": false,
	}, uuid.NewString(), 201)
	st, raw := env.do(env.tokens["reception"], "POST", "/api/v1/staff/encounters/"+eid+"/status", map[string]any{
		"status": "completed", "version": encVer,
	}, "")
	if st != 409 || !strings.Contains(string(raw), "open_draft_triage") {
		t.Fatalf("expected 409 open_draft_triage, got %d: %s", st, raw)
	}
	env.mustJSON(env.tokens["nurse"], "POST", "/api/v1/staff/triage/"+tr["id"].(string)+"/finalise", map[string]any{
		"version": int(tr["version"].(float64)),
	}, "", 200)

	// 2. Draft consultation blocks visit closure
	cs := env.mustJSON(env.tokens["doctor"], "POST", "/api/v1/staff/encounters/"+eid+"/consultations", map[string]any{
		"presenting_complaint": "Headache", "examination_findings": "Alert", "assessment": "Tension headache", "treatment_plan": "Rest",
	}, "", 201)
	st, raw = env.do(env.tokens["reception"], "POST", "/api/v1/staff/encounters/"+eid+"/status", map[string]any{
		"status": "completed", "version": encVer,
	}, "")
	if st != 409 || !strings.Contains(string(raw), "open_draft_consultation") {
		t.Fatalf("expected 409 open_draft_consultation, got %d: %s", st, raw)
	}
	env.mustJSON(env.tokens["doctor"], "POST", "/api/v1/staff/consultations/"+cs["id"].(string)+"/sign", map[string]any{
		"version": int(cs["version"].(float64)),
	}, "", 200)

	// 3. Draft prescription blocks visit closure
	drugs := env.mustJSON(env.tokens["doctor"], "GET", "/api/v1/staff/pharmacy/drugs", nil, "", 200)["items"].([]any)
	drugID := drugs[0].(map[string]any)["id"].(string)
	rx := env.mustJSON(env.tokens["doctor"], "POST", "/api/v1/staff/encounters/"+eid+"/prescriptions", map[string]any{
		"sign": false,
		"items": []map[string]any{{
			"drug_id": drugID, "dose_amount": 1, "dose_unit": "tablet", "route": "oral",
			"frequency_code": "STAT", "quantity_prescribed": 1,
		}},
	}, uuid.NewString(), 201)
	st, raw = env.do(env.tokens["reception"], "POST", "/api/v1/staff/encounters/"+eid+"/status", map[string]any{
		"status": "completed", "version": encVer,
	}, "")
	if st != 409 || !strings.Contains(string(raw), "open_draft_prescription") {
		t.Fatalf("expected 409 open_draft_prescription, got %d: %s", st, raw)
	}
	env.mustJSON(env.tokens["doctor"], "POST", "/api/v1/staff/prescriptions/"+rx["id"].(string)+"/sign", map[string]any{
		"version": int(rx["version"].(float64)),
	}, "", 200)

	// 4. Unpaid invoice blocks visit closure unless allow_unpaid + reason supplied
	env.do(env.tokens["cashier"], "POST", "/api/v1/staff/billing/shifts", map[string]any{
		"facility_id": env.facID, "opening_float": 10000,
	}, "")
	inv := env.mustJSON(env.tokens["cashier"], "POST", "/api/v1/staff/billing/invoices", map[string]any{
		"encounter_id": eid,
	}, "", 201)
	svcs := env.mustJSON(env.tokens["cashier"], "GET", "/api/v1/staff/billing/services", nil, "", 200)["items"].([]any)
	var consultSvcID string
	for _, s := range svcs {
		m := s.(map[string]any)
		if m["code"].(string) == "CONSULT" {
			consultSvcID = m["id"].(string)
			break
		}
	}
	inv = env.mustJSON(env.tokens["cashier"], "POST", "/api/v1/staff/billing/invoices/"+inv["id"].(string)+"/lines", map[string]any{
		"service_id": consultSvcID, "source_type": "consultation", "consultation_id": cs["id"].(string), "quantity": 1,
	}, "", 201)
	inv = env.mustJSON(env.tokens["cashier"], "POST", "/api/v1/staff/billing/invoices/"+inv["id"].(string)+"/issue", map[string]any{
		"version": int(inv["version"].(float64)),
	}, "", 200)

	st, raw = env.do(env.tokens["reception"], "POST", "/api/v1/staff/encounters/"+eid+"/status", map[string]any{
		"status": "completed", "version": encVer,
	}, "")
	if st != 409 || !strings.Contains(string(raw), "unpaid_invoice") {
		t.Fatalf("expected 409 unpaid_invoice, got %d: %s", st, raw)
	}
	// allow_unpaid without reason is rejected 422
	st, _ = env.do(env.tokens["reception"], "POST", "/api/v1/staff/encounters/"+eid+"/status", map[string]any{
		"status": "completed", "allow_unpaid": true, "version": encVer,
	}, "")
	if st != 422 {
		t.Fatalf("expected 422 for allow_unpaid without reason, got %d", st)
	}
	// Settle invoice -> visit completion succeeds!
	env.mustJSON(env.tokens["cashier"], "POST", "/api/v1/staff/billing/payments", map[string]any{
		"invoice_id": inv["id"].(string), "amount": inv["total"].(float64), "method": "cash",
	}, uuid.NewString(), 201)
	closed := env.mustJSON(env.tokens["reception"], "POST", "/api/v1/staff/encounters/"+eid+"/status", map[string]any{
		"status": "completed", "version": encVer,
	}, "", 200)
	if closed["status"].(string) != "completed" {
		t.Fatalf("expected completed encounter, got %v", closed)
	}
}

func TestWorkflow_AppointmentCheckInLinksEncounter(t *testing.T) {
	env := setupEnv(t)
	pid := env.createPatient(numTag())

	start := time.Now().UTC().Add(2 * time.Hour).Truncate(time.Second)
	end := start.Add(30 * time.Minute)
	appt := env.mustJSON(env.tokens["reception"], "POST", "/api/v1/staff/appointments", map[string]any{
		"facility_id":     env.facID,
		"department_id":   env.depts["OPD"],
		"patient_id":      pid,
		"scheduled_start": start.Format(time.RFC3339),
		"scheduled_end":   end.Format(time.RFC3339),
		"reason":          "Follow-up hypertension check",
	}, uuid.NewString(), 201)
	aid := appt["id"].(string)

	// Check-in atomically creates & links the encounter
	res := env.mustJSON(env.tokens["reception"], "POST", "/api/v1/staff/appointments/"+aid+"/check-in", map[string]any{
		"version": int(appt["version"].(float64)),
	}, "", 200)
	apptOut := res["appointment"].(map[string]any)
	encOut := res["encounter"].(map[string]any)
	if apptOut["status"].(string) != "checked_in" {
		t.Fatalf("expected appointment checked_in, got %v", apptOut)
	}
	if encOut["appointment_id"].(string) != aid {
		t.Fatalf("expected encounter.appointment_id=%s, got %v", aid, encOut["appointment_id"])
	}
	eid := encOut["id"].(string)

	// Completing the linked encounter also marks the appointment completed
	encOut = env.mustJSON(env.tokens["reception"], "POST", "/api/v1/staff/encounters/"+eid+"/status", map[string]any{
		"status": "in_progress", "version": int(encOut["version"].(float64)),
	}, "", 200)
	env.mustJSON(env.tokens["reception"], "POST", "/api/v1/staff/encounters/"+eid+"/status", map[string]any{
		"status": "completed", "version": int(encOut["version"].(float64)),
	}, "", 200)
	apptDone := env.mustJSON(env.tokens["reception"], "GET", "/api/v1/staff/appointments/"+aid, nil, "", 200)
	if apptDone["status"].(string) != "completed" {
		t.Fatalf("expected linked appointment to be completed, got %v", apptDone["status"])
	}
}

func TestWorkflow_PharmacyAllergyCheckAndDispense(t *testing.T) {
	env := setupEnv(t)
	tag := numTag()
	pid := env.createPatient(tag)

	// Find Paracetamol in drug catalogue and receive stock
	drugs := env.mustJSON(env.tokens["doctor"], "GET", "/api/v1/staff/pharmacy/drugs?q=Paracetamol", nil, "", 200)["items"].([]any)
	if len(drugs) == 0 {
		t.Fatalf("expected Paracetamol in seeded drug catalogue")
	}
	paraID := drugs[0].(map[string]any)["id"].(string)
	env.mustJSON(env.tokens["pharmacy"], "POST", "/api/v1/staff/pharmacy/stock/receipts", map[string]any{
		"facility_id": env.facID, "drug_id": paraID, "batch_no": "ALG-" + tag,
		"expiry_date": "2029-12-31", "quantity": 100, "unit_cost": 50,
	}, "", 201)

	// Record active Paracetamol drug allergy on patient
	env.mustJSON(env.tokens["nurse"], "POST", "/api/v1/staff/patients/"+pid+"/allergies", map[string]any{
		"substance": "Paracetamol", "category": "drug", "severity": "severe", "reaction": "Urticaria rash",
	}, "", 201)

	// Open encounter
	enc := env.mustJSON(env.tokens["reception"], "POST", "/api/v1/staff/encounters", map[string]any{
		"patient_id": pid, "facility_id": env.facID, "encounter_type": "outpatient",
	}, uuid.NewString(), 201)
	eid := enc["id"].(string)

	rxPayload := map[string]any{
		"sign": true,
		"items": []map[string]any{{
			"drug_id": paraID, "dose_amount": 1, "dose_unit": "tablet", "route": "oral",
			"frequency_code": "TDS", "duration_days": 3, "quantity_prescribed": 9,
		}},
	}

	// 1. Prescribing without override MUST be blocked (409 allergy_conflict)
	st, raw := env.do(env.tokens["doctor"], "POST", "/api/v1/staff/encounters/"+eid+"/prescriptions", rxPayload, uuid.NewString())
	if st != 409 || !strings.Contains(string(raw), "allergy_conflict") {
		t.Fatalf("expected 409 allergy_conflict on prescribing allergen, got %d: %s", st, raw)
	}

	// 2. Prescribing WITH explicit clinical override succeeds and is audited
	rxPayload["override_allergy_reason"] = "Senior clinician approved desensitisation protocol"
	rx := env.mustJSON(env.tokens["doctor"], "POST", "/api/v1/staff/encounters/"+eid+"/prescriptions", rxPayload, uuid.NewString(), 201)
	rxID := rx["id"].(string)
	itemID := rx["items"].([]any)[0].(map[string]any)["id"].(string)

	// 3. Dispensing without override MUST ALSO be blocked (409 allergy_conflict)
	st, raw = env.do(env.tokens["pharmacy"], "POST", "/api/v1/staff/prescriptions/"+rxID+"/dispense", map[string]any{
		"facility_id": env.facID, "counselling_given": true,
		"lines": []map[string]any{{"prescription_item_id": itemID, "quantity": 9}},
	}, uuid.NewString())
	if st != 409 || !strings.Contains(string(raw), "allergy_conflict") {
		t.Fatalf("expected 409 allergy_conflict on dispensing allergen, got %d: %s", st, raw)
	}

	// 4. Dispensing WITH override succeeds, then reversal returns stock
	disp := env.mustJSON(env.tokens["pharmacy"], "POST", "/api/v1/staff/prescriptions/"+rxID+"/dispense", map[string]any{
		"facility_id": env.facID, "counselling_given": true,
		"override_allergy_reason": "Verified clinician override order on chart",
		"lines":                   []map[string]any{{"prescription_item_id": itemID, "quantity": 9}},
	}, uuid.NewString(), 201)
	if disp["status"].(string) != "dispensed" {
		t.Fatalf("expected dispensed, got %v", disp["status"])
	}
	dispID := disp["dispenses"].([]any)[0].(map[string]any)["id"].(string)
	rev := env.mustJSON(env.tokens["pharmacy"], "POST", "/api/v1/staff/dispenses/"+dispID+"/reverse", map[string]any{
		"reason": "Patient declined medication after counselling",
	}, "", 200)
	if rev["dispenses"].([]any)[0].(map[string]any)["status"].(string) != "reversed" {
		t.Fatalf("expected reversed dispense, got %v", rev)
	}

	// Verify both override audit events exist
	aud := env.mustJSON(env.tokens["privacy-reviewer"], "GET", "/api/v1/staff/audit?patient_id="+pid+"&action=prescription.allergy_override", nil, "", 200)
	if len(aud["items"].([]any)) == 0 {
		t.Fatalf("expected prescription.allergy_override audit event")
	}
	aud2 := env.mustJSON(env.tokens["privacy-reviewer"], "GET", "/api/v1/staff/audit?patient_id="+pid+"&action=pharmacy.dispense.allergy_override", nil, "", 200)
	if len(aud2["items"].([]any)) == 0 {
		t.Fatalf("expected pharmacy.dispense.allergy_override audit event")
	}
}

func TestWorkflow_LabAndCriticalResultAlert(t *testing.T) {
	env := setupEnv(t)
	env.ensureRoleTokens("laboratory", "lab-supervisor")
	pid := env.createPatient(numTag())

	enc := env.mustJSON(env.tokens["reception"], "POST", "/api/v1/staff/encounters", map[string]any{
		"patient_id": pid, "facility_id": env.facID, "encounter_type": "outpatient",
	}, uuid.NewString(), 201)
	eid := enc["id"].(string)

	// Fetch FBC test and HGB/WBC/PLT components
	tests := env.mustJSON(env.tokens["doctor"], "GET", "/api/v1/staff/diagnostics/tests?category=laboratory", nil, "", 200)["items"].([]any)
	var fbc map[string]any
	for _, raw := range tests {
		m := raw.(map[string]any)
		if m["code"].(string) == "FBC" {
			fbc = m
			break
		}
	}
	if fbc == nil {
		t.Fatalf("FBC test not seeded")
	}
	comps := map[string]string{}
	for _, c := range fbc["components"].([]any) {
		m := c.(map[string]any)
		comps[m["code"].(string)] = m["id"].(string)
	}

	// Order -> collect -> receive -> enter critical low HGB (6.5)
	rq := env.mustJSON(env.tokens["doctor"], "POST", "/api/v1/staff/encounters/"+eid+"/diagnostic-requests", map[string]any{
		"clinical_indication": "Severe pallor, rule out anaemia",
		"priority":            "stat",
		"test_ids":            []string{fbc["id"].(string)},
	}, uuid.NewString(), 201)
	itemID := rq["items"].([]any)[0].(map[string]any)["id"].(string)

	rq = env.mustJSON(env.tokens["laboratory"], "POST", "/api/v1/staff/diagnostic-items/"+itemID+"/specimens", map[string]any{}, "", 201)
	spec := rq["items"].([]any)[0].(map[string]any)["specimens"].([]any)[0].(map[string]any)
	env.mustJSON(env.tokens["laboratory"], "POST", "/api/v1/staff/specimens/"+spec["id"].(string)+"/receive", map[string]any{
		"version": int(spec["version"].(float64)),
	}, "", 200)

	rq = env.mustJSON(env.tokens["laboratory"], "POST", "/api/v1/staff/diagnostic-items/"+itemID+"/results", map[string]any{
		"results": []map[string]any{
			{"component_id": comps["HGB"], "value_numeric": 6.5},
			{"component_id": comps["WBC"], "value_numeric": 7.0},
			{"component_id": comps["PLT"], "value_numeric": 200},
		},
	}, "", 201)
	var hgbRes map[string]any
	for _, r := range rq["items"].([]any)[0].(map[string]any)["results"].([]any) {
		m := r.(map[string]any)
		if m["component_code"].(string) == "HGB" {
			hgbRes = m
			break
		}
	}
	if hgbRes == nil || !hgbRes["is_critical"].(bool) || hgbRes["abnormal_flag"].(string) != "LL" {
		t.Fatalf("expected critical LL HGB result, got %v", hgbRes)
	}
	hgbID := hgbRes["id"].(string)

	// Verify outbox contains diagnostic.critical_result alert and audit contains critical_alert
	outbox := env.mustJSON(env.tokens["privacy-reviewer"], "GET", "/api/v1/staff/comms/outbox?status=pending", nil, "", 200)["items"].([]any)
	foundAlert := false
	for _, raw := range outbox {
		m := raw.(map[string]any)
		if m["event_type"].(string) == "diagnostic.critical_result" && m["aggregate_id"].(string) == hgbID {
			foundAlert = true
			break
		}
	}
	if !foundAlert {
		t.Fatalf("expected diagnostic.critical_result outbox alert for result %s", hgbID)
	}
	aud := env.mustJSON(env.tokens["privacy-reviewer"], "GET", "/api/v1/staff/audit?patient_id="+pid+"&action=diagnostic.result.critical_alert", nil, "", 200)
	if len(aud["items"].([]any)) == 0 {
		t.Fatalf("expected diagnostic.result.critical_alert audit event")
	}

	// Technician cannot verify (403); lab supervisor verifies (200) and enqueues verified critical alert
	if st, _ := env.do(env.tokens["laboratory"], "POST", "/api/v1/staff/results/"+hgbID+"/verify", map[string]any{
		"version": int(hgbRes["version"].(float64)),
	}, ""); st != 403 {
		t.Fatalf("expected 403 for technician verifying result, got %d", st)
	}
	verified := env.mustJSON(env.tokens["lab-supervisor"], "POST", "/api/v1/staff/results/"+hgbID+"/verify", map[string]any{
		"version": int(hgbRes["version"].(float64)),
	}, "", 200)
	if verified["status"].(string) != "final" {
		t.Fatalf("expected final verified result, got %v", verified)
	}
	audV := env.mustJSON(env.tokens["privacy-reviewer"], "GET", "/api/v1/staff/audit?patient_id="+pid+"&action=diagnostic.result.critical_verified", nil, "", 200)
	if len(audV["items"].([]any)) == 0 {
		t.Fatalf("expected diagnostic.result.critical_verified audit event")
	}
}

func TestWorkflow_BillingEndToEnd(t *testing.T) {
	env := setupEnv(t)
	tag := numTag()
	pid := env.createPatient(tag)
	enc := env.mustJSON(env.tokens["reception"], "POST", "/api/v1/staff/encounters", map[string]any{
		"patient_id": pid, "facility_id": env.facID, "encounter_type": "outpatient",
	}, uuid.NewString(), 201)
	eid := enc["id"].(string)

	env.do(env.tokens["cashier"], "POST", "/api/v1/staff/billing/shifts", map[string]any{
		"facility_id": env.facID, "opening_float": 25000,
	}, "")
	inv := env.mustJSON(env.tokens["cashier"], "POST", "/api/v1/staff/billing/invoices", map[string]any{
		"encounter_id": eid,
	}, "", 201)
	svcs := env.mustJSON(env.tokens["cashier"], "GET", "/api/v1/staff/billing/services", nil, "", 200)["items"].([]any)
	var consultSvcID string
	for _, s := range svcs {
		m := s.(map[string]any)
		if m["code"].(string) == "CONSULT" {
			consultSvcID = m["id"].(string)
			break
		}
	}
	inv = env.mustJSON(env.tokens["cashier"], "POST", "/api/v1/staff/billing/invoices/"+inv["id"].(string)+"/lines", map[string]any{
		"service_id": consultSvcID, "source_type": "manual", "quantity": 1,
	}, "", 201)
	total := inv["total"].(float64)
	inv = env.mustJSON(env.tokens["cashier"], "POST", "/api/v1/staff/billing/invoices/"+inv["id"].(string)+"/issue", map[string]any{
		"version": int(inv["version"].(float64)),
	}, "", 200)
	if inv["status"].(string) != "issued" {
		t.Fatalf("expected issued invoice, got %v", inv)
	}

	// Partial payment + over-balance rejected + final payment + dual-control refund
	pay1 := env.mustJSON(env.tokens["cashier"], "POST", "/api/v1/staff/billing/payments", map[string]any{
		"invoice_id": inv["id"].(string), "amount": 5000, "method": "cash",
	}, uuid.NewString(), 201)
	if st, _ := env.do(env.tokens["cashier"], "POST", "/api/v1/staff/billing/payments", map[string]any{
		"invoice_id": inv["id"].(string), "amount": total, "method": "cash",
	}, uuid.NewString()); st != 409 {
		t.Fatalf("expected 409 for overpayment, got %d", st)
	}
	env.mustJSON(env.tokens["cashier"], "POST", "/api/v1/staff/billing/payments", map[string]any{
		"invoice_id": inv["id"].(string), "amount": total - 5000, "method": "mobile_money",
		"provider": "mpesa", "provider_reference": "MPE" + tag,
	}, uuid.NewString(), 201)
	invPaid := env.mustJSON(env.tokens["cashier"], "GET", "/api/v1/staff/billing/invoices/"+inv["id"].(string), nil, "", 200)
	if invPaid["status"].(string) != "paid" {
		t.Fatalf("expected paid invoice, got %v", invPaid["status"])
	}

	rf := env.mustJSON(env.tokens["cashier"], "POST", "/api/v1/staff/billing/payments/"+pay1["id"].(string)+"/refunds", map[string]any{
		"amount": 1000, "reason": "Duplicate manual charge",
	}, "", 201)
	if st, _ := env.do(env.tokens["cashier"], "POST", "/api/v1/staff/billing/refunds/"+rf["id"].(string)+"/decision", map[string]any{
		"approve": true, "version": int(rf["version"].(float64)),
	}, ""); st != 403 {
		t.Fatalf("expected 403 for cashier approving refund, got %d", st)
	}
	rf = env.mustJSON(env.tokens["billing-supervisor"], "POST", "/api/v1/staff/billing/refunds/"+rf["id"].(string)+"/decision", map[string]any{
		"approve": true, "version": int(rf["version"].(float64)),
	}, "", 200)
	if rf["status"].(string) != "approved" {
		t.Fatalf("expected approved refund, got %v", rf)
	}
}

func TestWorkflow_ReportPermissionsAndMetrics(t *testing.T) {
	env := setupEnv(t)
	env.ensureRoleTokens("lab-supervisor")

	// Seed a final diagnosis so diagnosis-counts has data
	pid := env.createPatient(numTag())
	enc := env.mustJSON(env.tokens["reception"], "POST", "/api/v1/staff/encounters", map[string]any{
		"patient_id": pid, "facility_id": env.facID, "encounter_type": "outpatient",
	}, uuid.NewString(), 201)
	eid := enc["id"].(string)
	cs := env.mustJSON(env.tokens["doctor"], "POST", "/api/v1/staff/encounters/"+eid+"/consultations", map[string]any{
		"presenting_complaint": "Fever", "examination_findings": "Febrile", "assessment": "Malaria", "treatment_plan": "ACT",
	}, "", 201)
	env.mustJSON(env.tokens["doctor"], "POST", "/api/v1/staff/consultations/"+cs["id"].(string)+"/sign", map[string]any{
		"version": int(cs["version"].(float64)),
	}, "", 200)
	env.mustJSON(env.tokens["doctor"], "POST", "/api/v1/staff/encounters/"+eid+"/diagnoses", map[string]any{
		"description": "Malaria", "icd10_code": "B50.9", "diagnosis_type": "final", "is_primary": true, "consultation_id": cs["id"].(string),
	}, "", 201)

	// 1. /reports/diagnosis-counts is reachable by doctor (clinical.read + consultation.write), blocked for non-clinical roles
	dc := env.mustJSON(env.tokens["doctor"], "GET", "/api/v1/staff/reports/diagnosis-counts", nil, "", 200)
	if len(dc["items"].([]any)) == 0 {
		t.Fatalf("expected diagnosis-counts to return at least 1 diagnosis row for doctor")
	}
	for _, blocked := range []string{"management", "lab-supervisor", "admin", "reception", "nurse"} {
		if st, _ := env.do(env.tokens[blocked], "GET", "/api/v1/staff/reports/diagnosis-counts", nil, ""); st != 403 {
			t.Fatalf("expected 403 on diagnosis-counts for %s, got %d", blocked, st)
		}
	}

	// 2. Finance reports require report.view + billing.read: allowed for management & billing-supervisor, blocked for lab-supervisor & admin
	for _, path := range []string{
		"/api/v1/staff/reports/daily-revenue",
		"/api/v1/staff/reports/receivables",
		"/api/v1/staff/reports/billing-reconciliation",
	} {
		env.mustJSON(env.tokens["management"], "GET", path, nil, "", 200)
		env.mustJSON(env.tokens["billing-supervisor"], "GET", path, nil, "", 200)
		for _, blocked := range []string{"lab-supervisor", "admin", "privacy-reviewer", "doctor"} {
			if st, _ := env.do(env.tokens[blocked], "GET", path, nil, ""); st != 403 {
				t.Fatalf("expected 403 on %s for %s, got %d", path, blocked, st)
			}
		}
	}

	// 3. /metrics is restricted strictly to admin (admin.org.manage)
	if st, _ := env.do(env.tokens["admin"], "GET", "/api/v1/staff/metrics", nil, ""); st != 200 {
		t.Fatalf("expected 200 on /metrics for admin, got %d", st)
	}
	for _, blocked := range []string{"management", "lab-supervisor", "billing-supervisor", "privacy-reviewer", "reception"} {
		if st, _ := env.do(env.tokens[blocked], "GET", "/api/v1/staff/metrics", nil, ""); st != 403 {
			t.Fatalf("expected 403 on /metrics for %s, got %d", blocked, st)
		}
	}
}

func TestWorkflow_ProductionMFAEnforcement(t *testing.T) {
	env := setupEnv(t)
	tag := numTag()
	mgrEmail := "mfa-mgr-" + tag + "@spartial.test"
	mgrPass := "ProdMFA-Strong-Pass!2026"

	// Create a fresh manager (privileged role)
	env.mustJSON(env.tokens["admin"], "POST", "/api/v1/staff/users", map[string]any{
		"email": mgrEmail, "display_name": "MFA Enforced Manager", "password": mgrPass,
		"role": "manager", "facility_ids": []string{env.facID},
	}, "", 201)

	// Build a second Fiber app with production MFA enforcement enabled
	prodCfg := env.cfg
	prodCfg.MFAEnforcePrivileged = true
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	prodApp, err := httpapi.New(prodCfg, env.db, log)
	if err != nil {
		t.Fatalf("httpapi.New(MFAEnforcePrivileged): %v", err)
	}

	reqJSON := func(tok, method, path string, body any) (int, map[string]any) {
		var r io.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			r = bytes.NewReader(b)
		}
		req := httptest.NewRequest(method, path, r)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
		resp, err := prodApp.Test(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		var out map[string]any
		_ = json.Unmarshal(raw, &out)
		return resp.StatusCode, out
	}

	// 1. Non-privileged receptionist is NOT blocked by MFAEnforcePrivileged
	st, _ := reqJSON(env.tokens["reception"], "GET", "/api/v1/staff/facilities", nil)
	if st != 200 {
		t.Fatalf("expected 200 for non-privileged receptionist, got %d", st)
	}

	// 2. Privileged manager without MFA gets 403 mfa_required on protected /staff/* routes
	st, login := reqJSON("", "POST", "/api/v1/auth/login", map[string]any{
		"email": mgrEmail, "password": mgrPass, "client_type": "mobile",
	})
	if st != 200 {
		t.Fatalf("manager login failed: %d %v", st, login)
	}
	unverifiedTok := login["access_token"].(string)
	st, errBody := reqJSON(unverifiedTok, "GET", "/api/v1/staff/reports/daily-visits", nil)
	if st != 403 || errBody["error"].(map[string]any)["code"].(string) != "mfa_required" {
		t.Fatalf("expected 403 mfa_required for unverified privileged role, got %d: %v", st, errBody)
	}

	// 3. Manager enrolls & confirms TOTP MFA, then signs in with second factor -> succeeds 200!
	st, enroll := reqJSON(unverifiedTok, "POST", "/api/v1/auth/mfa/enroll", map[string]any{
		"current_password": mgrPass,
	})
	if st != 200 {
		t.Fatalf("mfa enroll failed: %d %v", st, enroll)
	}
	secretBytes, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(enroll["secret"].(string))
	if err != nil {
		t.Fatalf("decode secret: %v", err)
	}
	step := security.TOTPStep(time.Now().UTC())
	code1 := security.HOTP(secretBytes, step, security.TOTPDigits)
	st, _ = reqJSON(unverifiedTok, "POST", "/api/v1/auth/mfa/confirm", map[string]any{"code": code1})
	if st != 200 {
		t.Fatalf("mfa confirm failed: %d", st)
	}

	// Re-login returns MFA challenge
	st, ch := reqJSON("", "POST", "/api/v1/auth/login", map[string]any{
		"email": mgrEmail, "password": mgrPass, "client_type": "mobile",
	})
	if st != 200 || ch["mfa_required"] != true {
		t.Fatalf("expected MFA challenge on re-login, got %d %v", st, ch)
	}
	// Verify with +1 TOTP step (within skew window, avoiding replay of enrollment step)
	code2 := security.HOTP(secretBytes, step+1, security.TOTPDigits)
	st, verifiedLogin := reqJSON("", "POST", "/api/v1/auth/mfa/verify", map[string]any{
		"challenge_token": ch["challenge_token"].(string),
		"code":            code2,
		"client_type":     "mobile",
	})
	if st != 200 {
		t.Fatalf("mfa verify failed: %d %v", st, verifiedLogin)
	}
	mfaTok := verifiedLogin["access_token"].(string)

	// Now protected route succeeds 200!
	st, _ = reqJSON(mfaTok, "GET", "/api/v1/staff/reports/daily-visits", nil)
	if st != 200 {
		t.Fatalf("expected 200 on protected report after MFA verification, got %d", st)
	}
}
