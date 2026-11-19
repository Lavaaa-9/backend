#!/usr/bin/env python3
"""End-to-end API smoke test against a running dev API (needs the dev seed).
Usage: SEED_ADMIN_PASSWORD=... SEED_RECEPTION_PASSWORD=... python3 scripts/smoke_api.py [base_url]"""
import base64, datetime, json, os, sys, uuid, urllib.request, urllib.error

BASE = (sys.argv[1] if len(sys.argv) > 1 else "http://127.0.0.1:8080") + "/api/v1"
PW = os.environ.get("SEED_TEST_PASSWORD", "Spartial-Test-2026!")
PASSWORDS = {"admin": os.environ.get("SEED_ADMIN_PASSWORD", PW), "reception": os.environ.get("SEED_RECEPTION_PASSWORD", PW)}
fails = []

def call(token, method, path, body=None, key=None, expect=None):
    h = {"Content-Type": "application/json", "Origin": "http://localhost:5173"}
    if token: h["Authorization"] = "Bearer " + token
    if key: h["Idempotency-Key"] = key
    req = urllib.request.Request(BASE + path, method=method, headers=h, data=json.dumps(body).encode() if body is not None else None)
    try:
        r = urllib.request.urlopen(req); code, raw = r.status, r.read()
    except urllib.error.HTTPError as e:
        code, raw = e.code, e.read()
    data = json.loads(raw) if raw else None
    if expect is not None and code != expect:
        fails.append(f"{method} {path}: expected {expect}, got {code} {raw[:200]!r}")
        print("FAIL", method, path, code, raw[:200])
    return code, data

def login(role):
    c, d = call(None, "POST", "/auth/login", {"email": f"{role}@spartial.test", "password": PASSWORDS.get(role, PW), "client_type": "web"}, expect=200)
    return d["access_token"]

def uid(): return str(uuid.uuid4())
def check(name, cond):
    print(("ok   " if cond else "FAIL ") + name)
    if not cond: fails.append(name)

T = {r: login(r) for r in ["admin", "reception", "nurse", "doctor", "laboratory", "lab-supervisor", "pharmacy", "cashier", "billing-supervisor", "management", "privacy-reviewer"]}
fac = call(T["reception"], "GET", "/staff/facilities")[1]["items"][0]["id"]
tag = uid()[:8]
_, pt = call(T["reception"], "POST", "/staff/patients", {"facility_id": fac, "given_name": "Smoke", "family_name": "Test" + tag, "birth_precision": "exact", "birth_date": "1990-05-01", "sex": "female", "phone": "0712000111"}, key=uid(), expect=201)
pid = pt["id"]
_, enc = call(T["reception"], "POST", "/staff/encounters", {"patient_id": pid, "facility_id": fac, "encounter_type": "outpatient", "chief_complaint": "Fever and cough"}, key=uid(), expect=201)
eid = enc["id"]
depts = {d["code"]: d["id"] for d in call(T["reception"], "GET", "/staff/departments", expect=200)[1]["items"]}
check("departments seeded", "OPD" in depts and "LAB" in depts)

# queue
_, q = call(T["reception"], "POST", "/staff/queue", {"encounter_id": eid, "department_id": depts["TRIAGE"], "priority": "urgent"}, expect=201)
check("queue ticket numbered", q["ticket_no"] >= 1)
call(T["nurse"], "POST", f"/staff/queue/{q['id']}/status", {"status": "called", "version": q["version"]}, expect=200)
check("queue board lists ticket", any(t["id"] == q["id"] for t in call(T["nurse"], "GET", "/staff/queue", expect=200)[1]["items"]))
call(T["reception"], "POST", f"/staff/encounters/{eid}/status", {"status": "in_progress", "version": 1}, expect=200)

# triage
_, tr = call(T["nurse"], "POST", f"/staff/encounters/{eid}/triage", {"temperature_c": 38.6, "pulse_bpm": 104, "spo2_pct": 97, "weight_kg": 62, "height_cm": 165, "triage_category": "priority", "finalise": True}, key=uid(), expect=201)
check("triage final with BMI", tr["status"] == "final" and tr["bmi"] is not None)
call(T["reception"], "POST", f"/staff/encounters/{eid}/triage", {"pulse_bpm": 80}, key=uid(), expect=403)

# consultation + diagnosis + follow-up
_, cs = call(T["doctor"], "POST", f"/staff/encounters/{eid}/consultations", {"presenting_complaint": "Fever 3 days", "examination_findings": "Temp 38.6, clear chest", "assessment": "Probable malaria", "treatment_plan": "Test, treat"}, expect=201)
_, cs = call(T["doctor"], "PUT", f"/staff/consultations/{cs['id']}", {"presenting_complaint": "Fever 3 days", "examination_findings": "Temp 38.6, clear chest", "assessment": "Probable malaria", "treatment_plan": "RDT then treat", "version": cs["version"]}, expect=200)
_, cs = call(T["doctor"], "POST", f"/staff/consultations/{cs['id']}/sign", {"version": cs["version"]}, expect=200)
check("consultation signed", cs["status"] == "signed")
call(T["doctor"], "PUT", f"/staff/consultations/{cs['id']}", {"presenting_complaint": "x", "version": cs["version"]}, expect=409)
codes = call(T["doctor"], "GET", "/staff/icd10?q=malaria", expect=200)[1]["items"]
call(T["doctor"], "POST", f"/staff/encounters/{eid}/diagnoses", {"description": "Fever", "diagnosis_type": "final"}, expect=422)
if codes:
    _, dx = call(T["doctor"], "POST", f"/staff/encounters/{eid}/diagnoses", {"description": "Malaria", "icd10_code": codes[0]["code"], "diagnosis_type": "final", "is_primary": True, "consultation_id": cs["id"]}, expect=201)
call(T["doctor"], "POST", f"/staff/encounters/{eid}/follow-ups", {"due_date": "2026-12-15", "reason": "Review results"}, expect=201)

# laboratory
tests = {t["code"]: t for t in call(T["doctor"], "GET", "/staff/diagnostics/tests?category=laboratory", expect=200)[1]["items"]}
_, rq = call(T["doctor"], "POST", f"/staff/encounters/{eid}/diagnostic-requests", {"clinical_indication": "Fever, suspected malaria", "priority": "urgent", "test_ids": [tests["MRDT"]["id"], tests["FBC"]["id"]]}, key=uid(), expect=201)
items_ = {i["test_code"]: i for i in rq["items"]}
_, rq = call(T["laboratory"], "POST", f"/staff/diagnostic-items/{items_['MRDT']['id']}/specimens", {}, expect=201)
sp = next(i for i in rq["items"] if i["test_code"] == "MRDT")["specimens"][0]
check("specimen barcode", sp["barcode"].startswith("SPC-"))
call(T["laboratory"], "POST", f"/staff/specimens/{sp['id']}/receive", {"version": sp["version"]}, expect=200)
mr = tests["MRDT"]["components"][0]
call(T["laboratory"], "POST", f"/staff/diagnostic-items/{items_['MRDT']['id']}/results", {"results": [{"component_id": mr["id"], "value_coded": "Maybe"}]}, expect=422)
_, rq = call(T["laboratory"], "POST", f"/staff/diagnostic-items/{items_['MRDT']['id']}/results", {"results": [{"component_id": mr["id"], "value_coded": "Positive"}]}, expect=201)
res = next(i for i in rq["items"] if i["test_code"] == "MRDT")["results"][0]
call(T["laboratory"], "POST", f"/staff/results/{res['id']}/verify", {"version": res["version"]}, expect=403)   # technicians cannot verify
_, v = call(T["lab-supervisor"], "POST", f"/staff/results/{res['id']}/verify", {"version": res["version"]}, expect=200)
check("result final", v["status"] == "final")
fbc = {c["code"]: c for c in tests["FBC"]["components"]}
_, rq = call(T["laboratory"], "POST", f"/staff/diagnostic-items/{items_['FBC']['id']}/results", {"results": [{"component_id": fbc["HGB"]["id"], "value_numeric": 6.5}, {"component_id": fbc["WBC"]["id"], "value_numeric": 7.2}, {"component_id": fbc["PLT"]["id"], "value_numeric": 210}]}, expect=201)
hgb = next(r for r in next(i for i in rq["items"] if i["test_code"] == "FBC")["results"] if r["component_code"] == "HGB")
check("HGB flagged critical low", hgb["is_critical"] and hgb["abnormal_flag"] == "LL")
check("worklist", any(r["id"] == rq["id"] for r in call(T["laboratory"], "GET", f"/staff/diagnostic-requests?encounter_id={eid}", expect=200)[1]["items"]))

# pharmacy
drugs = {d["code"]: d for d in call(T["doctor"], "GET", "/staff/pharmacy/drugs", expect=200)[1]["items"]}
para = drugs["PARA500"]
_, batch = call(T["pharmacy"], "POST", "/staff/pharmacy/stock/receipts", {"facility_id": fac, "drug_id": para["id"], "batch_no": "B" + tag, "expiry_date": "2028-12-31", "quantity": 500, "unit_cost": 40}, expect=201)
check("stock received", float(batch["quantity_on_hand"]) >= 500)
_, rx = call(T["doctor"], "POST", f"/staff/encounters/{eid}/prescriptions", {"sign": True, "items": [{"drug_id": para["id"], "dose_amount": 1, "dose_unit": "tablet", "route": "oral", "frequency_code": "TDS", "duration_days": 5, "quantity_prescribed": 15, "instructions": "After food"}]}, key=uid(), expect=201)
check("prescription active", rx["status"] == "active")
dkey = uid()
_, rx2 = call(T["pharmacy"], "POST", f"/staff/prescriptions/{rx['id']}/dispense", {"facility_id": fac, "counselling_given": True, "lines": [{"prescription_item_id": rx["items"][0]["id"], "quantity": 15}]}, key=dkey, expect=201)
check("prescription dispensed", rx2["status"] == "dispensed")
_, rx3 = call(T["pharmacy"], "POST", f"/staff/prescriptions/{rx['id']}/dispense", {"facility_id": fac, "counselling_given": True, "lines": [{"prescription_item_id": rx["items"][0]["id"], "quantity": 15}]}, key=dkey, expect=201)
check("dispense replay is idempotent", len(rx3["dispenses"]) == 1)
call(T["pharmacy"], "POST", f"/staff/prescriptions/{rx['id']}/dispense", {"facility_id": fac, "lines": [{"prescription_item_id": rx["items"][0]["id"], "quantity": 1}]}, key=uid(), expect=422)
call(T["nurse"], "POST", f"/staff/prescriptions/{rx['id']}/dispense", {"facility_id": fac, "lines": [{"prescription_item_id": rx["items"][0]["id"], "quantity": 1}]}, key=uid(), expect=403)

# procedure to bill
procs = call(T["nurse"], "GET", "/staff/services?category=procedure", expect=200)[1]["items"]
call(T["nurse"], "POST", f"/staff/encounters/{eid}/procedures", {"description": "Wound dressing", "status": "performed", "service_id": procs[0]["id"]}, key=uid(), expect=201)

# billing
call(T["cashier"], "POST", "/staff/billing/shifts", {"facility_id": fac, "opening_float": 50000}, expect=None)
_, inv = call(T["cashier"], "POST", "/staff/billing/invoices", {"encounter_id": eid}, expect=201)
_, inv = call(T["cashier"], "POST", f"/staff/billing/invoices/{inv['id']}/collect-charges", expect=200)
check("charges collected", len(inv["lines"]) >= 3)
svc = {s["code"]: s for s in call(T["cashier"], "GET", "/staff/billing/services", expect=200)[1]["items"]}
_, inv = call(T["cashier"], "POST", f"/staff/billing/invoices/{inv['id']}/lines", {"service_id": svc["CONSULT"]["id"], "source_type": "consultation", "consultation_id": cs["id"], "quantity": 1}, expect=201)
total = float(inv["total"])
_, inv = call(T["cashier"], "POST", f"/staff/billing/invoices/{inv['id']}/issue", {"version": inv["version"]}, expect=200)
check("invoice issued with number", inv["status"] == "issued" and inv["invoice_no"].startswith("INV-"))
call(T["cashier"], "POST", f"/staff/billing/invoices/{inv['id']}/lines", {"service_id": svc["CONSULT"]["id"], "source_type": "manual", "quantity": 1}, expect=409)
pkey = uid()
_, pay = call(T["cashier"], "POST", "/staff/billing/payments", {"invoice_id": inv["id"], "amount": 10000, "method": "cash"}, key=pkey, expect=201)
check("receipt issued", pay["receipt"]["receipt_no"].startswith("RCT-"))
_, pay2 = call(T["cashier"], "POST", "/staff/billing/payments", {"invoice_id": inv["id"], "amount": 10000, "method": "cash"}, key=pkey, expect=201)
check("payment replay returns same payment", pay2["id"] == pay["id"])
_, inv2 = call(T["cashier"], "GET", f"/staff/billing/invoices/{inv['id']}", expect=200)
check("invoice partially paid", inv2["status"] == "partially_paid" and abs(float(inv2["balance"]) - (total - 10000)) < 0.01)
call(T["cashier"], "POST", "/staff/billing/payments", {"invoice_id": inv["id"], "amount": total, "method": "cash"}, key=uid(), expect=409)   # over balance
_, pay3 = call(T["cashier"], "POST", "/staff/billing/payments", {"invoice_id": inv["id"], "amount": total - 10000, "method": "mobile_money", "provider": "mpesa", "provider_reference": "MP" + tag}, key=uid(), expect=201)
_, inv3 = call(T["cashier"], "GET", f"/staff/billing/invoices/{inv['id']}", expect=200)
check("invoice paid", inv3["status"] == "paid")
_, rf = call(T["cashier"], "POST", f"/staff/billing/payments/{pay['id']}/refunds", {"amount": 2000, "reason": "Overcharged consultation"}, expect=201)
call(T["cashier"], "POST", f"/staff/billing/refunds/{rf['id']}/decision", {"approve": True, "version": rf["version"]}, expect=403)
_, rf = call(T["billing-supervisor"], "POST", f"/staff/billing/refunds/{rf['id']}/decision", {"approve": True, "version": rf["version"]}, expect=200)
check("refund approved", rf["status"] == "approved")
call(T["reception"], "GET", "/staff/billing/invoices", expect=200)
call(T["nurse"], "GET", "/staff/billing/invoices", expect=403)

# visit completion and reports
call(T["reception"], "POST", f"/staff/encounters/{eid}/status", {"status": "completed", "version": 2}, expect=200)
call(T["management"], "GET", "/staff/reports/staff-activity", expect=200)
for rpt in ("daily-visits", "daily-revenue", "receivables", "billing-reconciliation", "queue-wait-times", "stock-expiry"):
    call(T["management"], "GET", f"/staff/reports/{rpt}", expect=200)

# identifiers, contacts, next-of-kin, consents
nida = f"19900501-{tag[:5]}-00012"
_, ident = call(T["reception"], "POST", f"/staff/patients/{pid}/identifiers", {"id_type": "nida_nin", "value": nida}, key=uid(), expect=201)
check("identifier masked", "value_ciphertext" not in ident and ident["value_last4"] == "0012")
_, srch = call(T["reception"], "POST", "/staff/patient-identifiers/search", {"id_type": "nida_nin", "value": nida}, expect=200)
check("identifier blind search", pid in srch["patient_ids"])
_, rev = call(T["reception"], "POST", f"/staff/patients/{pid}/identifiers/{ident['id']}/reveal", {"reason": "Smoke test identity check"}, expect=200)
check("identifier revealed", rev["value"] == nida)
call(T["reception"], "POST", f"/staff/patients/{pid}/contacts", {"system": "email", "value": f"smoke-{tag}@example.com", "is_primary": True}, expect=201)
call(T["reception"], "POST", f"/staff/patients/{pid}/next-of-kin", {"full_name": "Smoke Kin", "relationship": "spouse", "phone": "0712555666"}, expect=201)
call(T["reception"], "POST", f"/staff/patients/{pid}/consents", {"consent_type": "telemedicine", "capture_method": "written", "policy_version": "v1"}, expect=201)

# clinical history: allergy, problem, history entry
call(T["nurse"], "POST", f"/staff/patients/{pid}/allergies", {"substance": "Sulfonamides", "category": "drug", "severity": "moderate"}, expect=201)
call(T["doctor"], "POST", f"/staff/patients/{pid}/problems", {"icd10_code": "I10", "description": "Hypertension"}, expect=201)
call(T["nurse"], "POST", f"/staff/patients/{pid}/history", {"history_type": "past_medical", "description": "Childhood asthma"}, expect=201)

# appointment + telehealth
st = (datetime.datetime.now(datetime.timezone.utc) + datetime.timedelta(days=2)).replace(microsecond=0)
en = st + datetime.timedelta(minutes=30)
_, appt = call(T["reception"], "POST", "/staff/appointments", {"facility_id": fac, "department_id": depts["OPD"], "patient_id": pid, "appointment_type": "telemedicine", "scheduled_start": st.isoformat().replace("+00:00", "Z"), "scheduled_end": en.isoformat().replace("+00:00", "Z")}, key=uid(), expect=201)
_, tses = call(T["doctor"], "POST", "/staff/telehealth/sessions", {"appointment_id": appt["id"], "provider": "jitsi_self_hosted"}, expect=201)
_, tses = call(T["doctor"], "POST", f"/staff/telehealth/sessions/{tses['id']}/status", {"status": "in_progress", "version": tses["version"]}, expect=200)
call(T["doctor"], "POST", f"/staff/telehealth/sessions/{tses['id']}/events", {"event_type": "participant_joined", "participant_role": "practitioner"}, expect=201)

# document + download + coverage
b64 = base64.b64encode(b"smoke test lab summary").decode()
_, doc = call(T["reception"], "POST", f"/staff/patients/{pid}/documents", {"category": "lab_report", "title": "Lab Summary", "content_type": "text/plain", "content_base64": b64}, key=uid(), expect=201)
_, dl = call(T["doctor"], "GET", f"/staff/documents/{doc['id']}/download", expect=200)
check("document download matches", dl.get("content_base64") == b64)
payers = call(T["cashier"], "GET", "/staff/billing/payers", expect=200)[1]["items"]
call(T["cashier"], "POST", f"/staff/patients/{pid}/coverages", {"payer_id": payers[0]["id"], "member_no": "MEM-" + tag, "valid_from": "2026-01-01"}, expect=201)

# break-glass, privacy access, approvals
_, bg = call(T["doctor"], "POST", f"/staff/patients/{pid}/break-glass", {"reason": "Emergency clinical review during smoke test"}, key=uid(), expect=201)
call(T["privacy-reviewer"], "POST", f"/staff/privacy/break-glass/{bg['id']}/review", {"outcome": "appropriate", "note": "Smoke test reviewed"}, expect=200)
_, acc = call(T["privacy-reviewer"], "GET", f"/staff/privacy/patients/{pid}/access", expect=200)
check("patient PHI access report populated", len(acc["items"]) >= 1)
_, apr = call(T["reception"], "POST", "/staff/approvals", {"action_type": "other", "resource_type": "patient", "resource_id": pid, "reason": "Dual-control verification smoke test"}, key=uid(), expect=201)
call(T["privacy-reviewer"], "POST", f"/staff/approvals/{apr['id']}/approve", {"note": "Approved", "version": apr["version"]}, expect=200)
print("\n%d failure(s)" % len(fails))
sys.exit(1 if fails else 0)
