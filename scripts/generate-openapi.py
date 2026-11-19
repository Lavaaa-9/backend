#!/usr/bin/env python3
"""Brings api/openapi/openapi.json in line with the spartial_hms-backed API.

Idempotent: drops routes/schemas of modules that were removed in the schema switch and
(re)writes the visit, nursing, procedure, verification and report operations.
"""
import json, pathlib

P = pathlib.Path(__file__).resolve().parent.parent / "api/openapi/openapi.json"
d = json.loads(P.read_text())
S = d["components"]["schemas"]
d["info"]["version"] = "1.0.0"
d["info"]["description"] = ("Full Spartial HMS API backed by the spartial_hms PostgreSQL schema (migrations 0001–0018). "
    "Every request runs in a transaction starting with sec.begin_request so row-level security and audit capture apply. "
    "Covers authentication and TOTP MFA, facilities/departments/service points, staff accounts, patients (identifiers, contacts, "
    "next of kin, consents, verifications, duplicate reviews and merge), clinical history (allergies, problems, history), "
    "appointments, telehealth, documents, encounters, queue, triage, nursing, consultations, diagnoses, procedures, follow-ups, "
    "laboratory/diagnostics, pharmacy, billing and insurance coverages, break-glass, privacy/governance, access reviews, "
    "dual-control approvals, management reports, communications and the audit trail. Timestamps are RFC3339 UTC.")

# --- remove obsolete pre-HMS routes only
for path in list(d["paths"]):
    if "care-assignments" in path:
        del d["paths"][path]
for name in ("AssignmentInput", "Assignment"):
    S.pop(name, None)

def ref(n): return {"$ref": f"#/components/schemas/{n}"}
def obj(props, required=None):
    o = {"type": "object", "additionalProperties": False, "properties": props}
    if required: o["required"] = required
    return o
STR, INT, BOOL = {"type": "string"}, {"type": "integer"}, {"type": "boolean"}
UUID = {"type": "string", "format": "uuid"}
TS = {"type": "string", "format": "date-time"}
def nul(t): return {**t, "nullable": True}
def arr(t): return {"type": "array", "items": t}
def enum(*v): return {"type": "string", "enum": list(v)}

S["EncounterInput"] = obj({"patient_id": UUID, "facility_id": UUID,
    "encounter_type": enum("outpatient", "emergency", "telemedicine", "follow_up"), "chief_complaint": {"type": "string", "maxLength": 500}},
    ["patient_id", "facility_id", "encounter_type"])
S["Encounter"] = obj({"id": UUID, "encounter_no": STR, "patient_id": UUID, "facility_id": UUID, "encounter_type": STR,
    "status": enum("registered", "in_progress", "completed", "cancelled", "entered_in_error"), "chief_complaint": STR,
    "arrived_at": TS, "started_at": nul(TS), "closed_at": nul(TS), "version": INT})
S["EncounterStatusInput"] = obj({"status": enum("in_progress", "completed", "cancelled", "entered_in_error"), "reason": STR, "version": INT},
    ["status", "version"])
S["NursingInput"] = obj({"observation_type": enum("general", "pain_reassessment", "wound", "medication_administered", "intake_output",
    "fall_risk", "patient_education", "handover", "other"), "note": {"type": "string", "minLength": 2, "maxLength": 4000},
    "observed_at": TS}, ["observation_type", "note"])
S["NursingObservation"] = obj({"id": UUID, "encounter_id": UUID, "patient_id": UUID, "recorded_by_membership_id": UUID,
    "observed_at": TS, "observation_type": STR, "note": STR, "status": enum("final", "entered_in_error"), "version": INT})
S["VoidInput"] = obj({"reason": {"type": "string", "minLength": 2, "maxLength": 500}, "version": INT}, ["reason", "version"])
S["ProcedureInput"] = obj({"description": {"type": "string", "minLength": 2, "maxLength": 500}, "procedure_code": STR,
    "service_id": nul(UUID), "quantity": {"type": "number", "exclusiveMinimum": 0}, "status": enum("planned", "performed"),
    "outcome_note": STR}, ["description", "status"])
S["ProcedureAction"] = obj({"action": enum("perform", "cancel", "void"), "reason": STR, "outcome_note": STR, "version": INT},
    ["action", "version"])
S["Procedure"] = obj({"id": UUID, "encounter_id": UUID, "patient_id": UUID, "service_id": nul(UUID), "procedure_code": STR,
    "description": STR, "quantity": {"type": "number"}, "status": enum("planned", "performed", "cancelled", "entered_in_error"),
    "performed_at": nul(TS), "performed_by_membership_id": nul(UUID), "outcome_note": STR, "version": INT})
S["CatalogService"] = obj({"id": UUID, "code": STR, "name": STR, "category": STR})
S["VerificationInput"] = obj({"scope": arr(enum("identity", "contacts", "next_of_kin", "address", "insurance", "portal_link")),
    "method": enum("id_document_seen", "nida_lookup", "insurance_card_seen", "otp_registered_phone", "patient_stated", "next_of_kin_confirmed"),
    "outcome": enum("confirmed", "updated", "discrepancy"), "note": {"type": "string", "maxLength": 1000},
    "encounter_id": nul(UUID), "identifier_id": nul(UUID)}, ["scope", "method", "outcome"])
S["Verification"] = obj({"id": UUID, "patient_id": UUID, "encounter_id": nul(UUID), "identifier_id": nul(UUID), "scope": arr(STR),
    "method": STR, "outcome": STR, "note": STR, "verified_by": UUID, "verified_at": TS})
S["StaffActivityRow"] = obj({"date": {"type": "string", "format": "date"}, "user_id": nul(UUID), "staff_name": nul(STR),
    "category": STR, "action": STR, "events": INT, "denied": INT, "patients_touched": INT})
S["StaffAccessRow"] = obj({"membership_id": UUID, "user_id": UUID, "staff_name": STR, "job_title": nul(STR), "membership_status": STR,
    "roles": arr(STR), "privileged_permissions": arr(STR), "mfa_enabled": BOOL, "last_login_at": nul(TS), "open_break_glass": INT})

IDEM = {"name": "Idempotency-Key", "in": "header", "required": True, "schema": {"type": "string", "minLength": 8, "maxLength": 128}}
def idp(n): return {"name": n, "in": "path", "required": True, "schema": UUID}
def body(n): return {"required": True, "content": {"application/json": {"schema": ref(n)}}}
def ok(code, schema): return {str(code): {"description": "OK" if code == 200 else "Created", "content": {"application/json": {"schema": schema}}}}
def items(n): return obj({"items": arr(ref(n))})
ERR = {c: {"description": m, "content": {"application/json": {"schema": ref("Error")}}} for c, m in
       {"401": "Authentication required", "403": "Permission denied", "404": "Not found", "409": "Conflict or illegal state change", "422": "Validation failed"}.items()}
def op(summary, perm, resp, params=None, req=None, idem=False):
    o = {"summary": summary, "description": f"Requires permission: {perm}." if perm else "Any authenticated staff member.",
         "security": [{"bearerAuth": []}] if "bearerAuth" in d["components"]["securitySchemes"] else [{"bearer": []}],
         "parameters": (params or []) + ([IDEM] if idem else []), "responses": {**resp, **ERR}}
    if req: o["requestBody"] = body(req)
    return o

# detect the name of the bearer scheme already in the document
sec = list(d["components"]["securitySchemes"])[0]
def op2(*a, **k):
    o = op(*a, **k); o["security"] = [{sec: []}]; return o

GEN = {"type": "object", "additionalProperties": True}
B = "/api/v1/staff"
d["paths"].update({
 f"{B}/facilities/{{id}}": {
   "get": op2("Get facility details", "", ok(200, GEN), [idp("id")]),
   "put": op2("Update facility details", "admin.facility.manage", ok(200, GEN), [idp("id")], None)},
 f"{B}/facilities/{{id}}/status": {
   "post": op2("Activate or deactivate a facility", "admin.facility.manage", ok(200, GEN), [idp("id")], None)},
 f"{B}/facilities/{{id}}/departments": {
   "get": op2("List departments belonging to a facility", "", ok(200, GEN), [idp("id")])},
 f"{B}/users/{{id}}": {
   "get": op2("Get a staff account profile", "admin.user.manage", ok(200, GEN), [idp("id")]),
   "put": d["paths"].get(f"{B}/users/{{id}}", {}).get("put") or op2("Update a staff account", "admin.user.manage", ok(200, GEN), [idp("id")], None)},
 f"{B}/users/{{id}}/status": {
   "post": op2("Activate, suspend, or deactivate a staff account", "admin.user.manage", ok(200, GEN), [idp("id")], None)},
 f"{B}/patients/{{id}}/status": {
   "post": op2("Change patient status (active, inactive, deceased)", "patient.update", ok(200, GEN), [idp("id")], None)},
 f"{B}/audit/export": {
   "get": op2("Export filtered audit metadata for compliance review", "audit.read", ok(200, GEN),
     [{"name": n, "in": "query", "schema": STR} for n in ("actor_id", "patient_id", "action", "outcome", "from", "to", "before", "limit")])},
 f"{B}/patients/{{id}}/verifications": {
   "post": op2("Record a patient verification", "patient.create or patient.update", ok(201, ref("Verification")), [idp("id")], "VerificationInput", True),
   "get": op2("Verification history", "patient.read", ok(200, items("Verification")), [idp("id")])},
 f"{B}/services": {"get": op2("Billable service catalogue", "", ok(200, items("CatalogService")),
   [{"name": "category", "in": "query", "schema": STR}])},
 f"{B}/encounters": {
   "post": op2("Open a visit", "encounter.manage", ok(201, ref("Encounter")), None, "EncounterInput", True),
   "get": op2("List visits", "encounter.read or clinical.read", ok(200, obj({"items": arr(ref("Encounter")), "next_cursor": STR})),
     [{"name": "patient_id", "in": "query", "schema": UUID}, {"name": "status", "in": "query", "schema": STR},
      {"name": "cursor", "in": "query", "schema": UUID}, {"name": "limit", "in": "query", "schema": INT}])},
 f"{B}/encounters/{{id}}": {"get": op2("Get a visit", "encounter.read or clinical.read", ok(200, ref("Encounter")), [idp("id")])},
 f"{B}/encounters/{{id}}/status": {"post": op2("Change visit status", "encounter.manage", ok(200, ref("Encounter")), [idp("id")], "EncounterStatusInput")},
 f"{B}/encounters/{{id}}/nursing": {
   "post": op2("Record a nursing observation", "nursing.write", ok(201, ref("NursingObservation")), [idp("id")], "NursingInput", True),
   "get": op2("List nursing observations", "clinical.read or nursing.write", ok(200, items("NursingObservation")), [idp("id")])},
 f"{B}/nursing/{{id}}/void": {"post": op2("Mark an observation entered in error", "nursing.write", ok(200, ref("NursingObservation")), [idp("id")], "VoidInput")},
 f"{B}/encounters/{{id}}/procedures": {
   "post": op2("Record or plan a procedure", "procedure.record or consultation.write", ok(201, ref("Procedure")), [idp("id")], "ProcedureInput", True),
   "get": op2("List procedures", "clinical.read, billing.invoice.manage or procedure.record", ok(200, items("Procedure")), [idp("id")])},
 f"{B}/procedures/{{id}}/action": {"post": op2("Perform, cancel or void a procedure", "procedure.record or consultation.write", ok(200, ref("Procedure")), [idp("id")], "ProcedureAction")},
 f"{B}/reports/staff-activity": {"get": op2("Staff activity summary (counts only)", "report.staff_activity",
   ok(200, obj({"items": arr(ref("StaffActivityRow")), "from": STR, "to": STR})),
   [{"name": "from", "in": "query", "schema": {"type": "string", "format": "date"}}, {"name": "to", "in": "query", "schema": {"type": "string", "format": "date"}}])},
 f"{B}/reports/staff-access": {"get": op2("Staff roles, privileged permissions and MFA state", "report.staff_activity", ok(200, items("StaffAccessRow")))},
})

# ensure requestBody is set on new PUT/POST paths added directly above
for p, verb in [
    (f"{B}/facilities/{{id}}", "put"),
    (f"{B}/facilities/{{id}}/status", "post"),
    (f"{B}/users/{{id}}/status", "post"),
    (f"{B}/patients/{{id}}/status", "post"),
]:
    d["paths"][p][verb]["requestBody"] = {"required": True, "content": {"application/json": {"schema": GEN}}}

# --- MFA authentication endpoints
S["Challenge"] = obj({"mfa_required": BOOL, "challenge_token": STR, "expires_in": INT}, ["mfa_required", "challenge_token", "expires_in"])
S["MFAVerify"] = obj({"challenge_token": STR, "code": STR, "recovery_code": STR, "client_type": enum("web", "mobile")}, ["challenge_token", "client_type"])
S["Enrollment"] = obj({"secret": STR, "provisioning_uri": STR}, ["secret", "provisioning_uri"])
S["RecoveryCodes"] = obj({"recovery_codes": arr(STR)}, ["recovery_codes"])
S["MFAEnrollInput"] = obj({"current_password": STR}, ["current_password"])
S["MFAConfirmInput"] = obj({"code": STR}, ["code"])
S["MFAStepUpInput"] = obj({"current_password": STR, "code": STR, "recovery_code": STR}, ["current_password"])

AUTH = "/api/v1/auth"
d["paths"].update({
 f"{AUTH}/mfa/verify": {"post": {"summary": "Verify TOTP or recovery code for an MFA login challenge",
   "description": "Exchanges a login MFA challenge token plus TOTP/recovery code for session tokens.",
   "requestBody": body("MFAVerify"), "responses": {**ok(200, GEN), **ERR}}},
 f"{AUTH}/mfa/enroll": {"post": op2("Start TOTP MFA enrollment", "", ok(200, ref("Enrollment")), None, "MFAEnrollInput")},
 f"{AUTH}/mfa/confirm": {"post": op2("Confirm TOTP MFA enrollment and issue one-time recovery codes", "", ok(200, ref("RecoveryCodes")), None, "MFAConfirmInput")},
 f"{AUTH}/mfa/disable": {"post": op2("Disable TOTP MFA and revoke all sessions", "", {"204": {"description": "MFA disabled"}}, None, "MFAStepUpInput")},
 f"{AUTH}/mfa/recovery-codes/regenerate": {"post": op2("Regenerate MFA recovery codes", "", ok(200, ref("RecoveryCodes")), None, "MFAStepUpInput")},
 f"{AUTH}/password-reset/request": {"post": {"summary": "Request a password reset token (non-enumerating)",
   "description": "Always returns 202 Accepted without revealing whether the email exists.",
   "requestBody": {"required": True, "content": {"application/json": {"schema": GEN}}}, "responses": {**ok(202, GEN), **ERR}}},
 f"{AUTH}/password-reset/confirm": {"post": {"summary": "Confirm a password reset token and set a new password",
   "description": "Consumes the one-time reset token, updates password, and revokes all active sessions.",
   "requestBody": {"required": True, "content": {"application/json": {"schema": GEN}}}, "responses": {"204": {"description": "Password reset"}, **ERR}}},
 f"{AUTH}/devices": {
   "get": op2("List caller's push notification devices", "", ok(200, GEN)),
   "post": op2("Register a push notification device", "", ok(201, GEN), None, None)},
 f"{AUTH}/devices/{{id}}/revoke": {"post": op2("Revoke a push notification device", "", ok(200, GEN), [idp("id")])},
})

# --- routes of routes_ext.go and routes_complete.go
import re
root = pathlib.Path(__file__).resolve().parent.parent / "internal/httpapi"
pat = re.compile(r'staff\.(Get|Post|Put)\("([^"]+)", api\.require\("([^"]*)"\), (withBody|noBody)\((?:\d+, )?func(.*?)\n\t\}\)\)', re.S)
for fname in ("routes_ext.go", "routes_complete.go"):
    src = (root / fname).read_text()
    for m in pat.finditer(src):
        verb, path, perm, kind, body_src = m.groups()
        full = B + re.sub(r":(\w+)", r"{\1}", path)
        params = [idp(n) for n in re.findall(r"\{(\w+)\}", full)]
        code = 200
        if kind == "withBody" and re.search(r"withBody\(201", m.group(0)): code = 201
        query = [{"name": n, "in": "query", "schema": STR} for n in sorted(set(re.findall(r'c\.Query\("(\w+)"\)', body_src)))]
        fn_names = re.findall(r"\.(\w+)\(c\.Context", body_src)
        summary = re.sub(r"([a-z])([A-Z])", r"\1 \2", fn_names[0]) if fn_names else path
        o = op2(summary, perm, ok(code, GEN), params + query, None, "Idempotency-Key" in body_src)
        if kind == "withBody":
            o["requestBody"] = {"required": True, "content": {"application/json": {"schema": GEN}}}
        d["paths"].setdefault(full, {})[verb.lower()] = o
d["paths"] = dict(sorted(d["paths"].items()))
P.write_text(json.dumps(d, indent=1) + "\n")
print("paths:", len(d["paths"]), "schemas:", len(S))
