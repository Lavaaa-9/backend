-- Security, tenant isolation, session, portal, break-glass, audit integrity.
\set ON_ERROR_STOP 1
\set ORG '''a0000000-0000-0000-0000-000000000001'''
\set as_recep   'SET LOCAL ROLE spartial_app; SELECT 1 FROM sec.begin_request(''30000000-0000-0000-0000-000000000002'');'
\set as_recepb  'SET LOCAL ROLE spartial_app; SELECT 1 FROM sec.begin_request(''30000000-0000-0000-0000-000000000020'');'
\set as_admin   'SET LOCAL ROLE spartial_app; SELECT 1 FROM sec.begin_request(''30000000-0000-0000-0000-000000000001'');'
\set as_erclerk 'SET LOCAL ROLE spartial_app; SELECT 1 FROM sec.begin_request(''30000000-0000-0000-0000-000000000012'');'
\set as_doctor  'SET LOCAL ROLE spartial_app; SELECT 1 FROM sec.begin_request(''30000000-0000-0000-0000-000000000004'');'
\set as_compliance 'SET LOCAL ROLE spartial_app; SELECT 1 FROM sec.begin_request(''30000000-0000-0000-0000-000000000013'');'
SET test.suite = 'security';

-- Fail-closed without request context.
BEGIN; SET LOCAL ROLE spartial_app;
SELECT test.eq((SELECT count(*) FROM patient.patient), 0::bigint, 'SEC: no request context → zero PHI rows');
SELECT test.throws($$INSERT INTO patient.patient (org_id, mrn, registered_facility_id, first_name, last_name, sex, date_of_birth)
  VALUES ('a0000000-0000-0000-0000-000000000001','SP-2026-000777','a0000000-0000-0000-0000-0000000000f1','No','Context','male','2000-01-01')$$,
  '42501', 'SEC: no request context → writes rejected');
SELECT test.throws($$SELECT sec.begin_request('30000000-0000-0000-0000-0000000000e1')$$, '28000', 'SEC: idle-expired session rejected (automatic logoff)');
SELECT test.throws($$SELECT sec.begin_request('30000000-0000-0000-0000-0000000000e2')$$, '28000', 'SEC: staff session without MFA rejected');
SELECT test.throws($$SELECT sec.begin_request(gen_random_uuid())$$, '28000', 'SEC: unknown session rejected');
SELECT test.throws($$TRUNCATE patient.patient$$, '42501', 'SEC: app role cannot TRUNCATE');
SELECT test.throws($$DELETE FROM patient.patient$$, '42501', 'SEC: app role cannot DELETE patients');
SELECT test.throws($$DROP TABLE billing.invoice$$, '42501', 'SEC: app role cannot run DDL');
SELECT test.throws($$SELECT org.next_value('a0000000-0000-0000-0000-000000000001','invoice','2026')$$, '42501', 'SEC: counters cannot be advanced outside tenant context');
COMMIT;

-- Tenant isolation.
BEGIN; :as_recepb
SELECT test.eq((SELECT count(*) FROM patient.patient), 0::bigint, 'TENANT: org B sees none of org A''s patients');
SELECT test.eq((SELECT count(*) FROM clinical.encounter), 0::bigint, 'TENANT: org B sees none of org A''s visits');
SELECT test.eq((SELECT count(*) FROM billing.invoice), 0::bigint, 'TENANT: org B sees none of org A''s invoices');
SELECT test.eq((SELECT count(*) FROM audit.event), 0::bigint, 'TENANT: org B cannot read org A audit (and lacks audit.read)');
SELECT test.throws($$INSERT INTO patient.contact_point (org_id, patient_id, system, value) VALUES
  ('a0000000-0000-0000-0000-000000000001','90000000-0000-0000-0000-000000000001','phone','+255799999999')$$, '42501', 'TENANT: cannot write rows into another tenant');
SELECT test.throws($$INSERT INTO patient.contact_point (org_id, patient_id, system, value) VALUES
  ('b0000000-0000-0000-0000-000000000001','90000000-0000-0000-0000-000000000001','phone','+255799999999')$$, '23503', 'TENANT: cannot attach own-tenant row to a foreign patient (tenant-aware FK)');
UPDATE patient.patient SET first_name = 'Hacked' WHERE id = '90000000-0000-0000-0000-000000000001';
COMMIT;
BEGIN;
SELECT test.eq((SELECT first_name FROM patient.patient WHERE id = '90000000-0000-0000-0000-000000000001'), 'Asha', 'TENANT: cross-tenant UPDATE affects nothing');
-- even a superuser-level bypass of RLS cannot create a cross-tenant link: composite FK
SELECT test.throws($$INSERT INTO clinical.triage_assessment (org_id, encounter_id, patient_id, recorded_by_membership_id, temperature_c)
  VALUES ('b0000000-0000-0000-0000-000000000001','92000000-0000-0000-0000-000000000001','90000000-0000-0000-0000-000000000001','20000000-0000-0000-0000-000000000020', 37)$$,
  '23503', 'TENANT: composite FK blocks cross-tenant clinical link even when RLS is bypassed');
COMMIT;

-- Administrator: manages users but has no PHI access by default (minimum necessary).
BEGIN; :as_admin
SELECT test.eq((SELECT count(*) FROM patient.patient), 0::bigint, 'RBAC: administrator cannot browse patient records');
SELECT test.ok((SELECT count(*) > 0 FROM iam.membership), 'RBAC: administrator can list staff');
SELECT test.throws($$INSERT INTO iam.membership_role (org_id, membership_id, role_id)
  SELECT 'a0000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001', id FROM iam.role WHERE code = 'doctor' AND org_id IS NULL$$,
  'SPV01', 'RBAC: administrator cannot grant roles to themselves');
COMMIT;
BEGIN; :as_admin
UPDATE iam.role SET name = 'Tampered' WHERE code = 'doctor' AND org_id IS NULL;
COMMIT;
BEGIN;
SELECT test.eq((SELECT name FROM iam.role WHERE code = 'doctor' AND org_id IS NULL), 'Doctor / clinician', 'RBAC: system role templates are read-only to tenants');
COMMIT;

-- Break-glass.
BEGIN; :as_erclerk
SELECT test.eq((SELECT count(*) FROM clinical.triage_assessment), 0::bigint, 'BREAK-GLASS: no clinical access before emergency access');
SELECT test.throws($$SELECT iam.open_break_glass('90000000-0000-0000-0000-000000000001', 'too short')$$, '23514', 'BREAK-GLASS: justification must be meaningful (>=20 chars)');
SELECT iam.open_break_glass('90000000-0000-0000-0000-000000000001', 'Unconscious patient brought in, need allergy history (TEST)', 30);
COMMIT;
BEGIN; :as_erclerk
SELECT test.eq((SELECT count(*) FROM clinical.allergy), 1::bigint, 'BREAK-GLASS: emergency grant exposes that patient''s allergies');
SELECT test.eq((SELECT count(*) FROM clinical.triage_assessment WHERE patient_id <> '90000000-0000-0000-0000-000000000001'), 0::bigint, 'BREAK-GLASS: grant limited to the one patient');
COMMIT;
BEGIN;
SELECT test.ok((SELECT count(*) = 1 FROM audit.event WHERE action = 'iam.break_glass.open' AND category = 'phi_access' AND outcome = 'success'), 'BREAK-GLASS: opening is audited');
SELECT test.eq((SELECT count(*) FROM reporting.v_break_glass_pending_review), 1::bigint, 'BREAK-GLASS: appears in pending-review report');
COMMIT;

-- Patient portal (mobile app).
BEGIN;
SELECT set_config('app.org_id', 'a0000000-0000-0000-0000-000000000001', true);
INSERT INTO iam.user_account (id, account_type, phone_e164, display_name, status, mfa_required, is_test_account)
VALUES ('10000000-0000-0000-0000-000000000030', 'patient', '+255700000001', 'TEST Asha', 'active', false, true);
INSERT INTO patient.portal_account (org_id, patient_id, user_id, status, verification_method, verified_at)
VALUES (:ORG, '90000000-0000-0000-0000-000000000001', '10000000-0000-0000-0000-000000000030', 'active', 'in_person_id_check', now());
INSERT INTO iam.session (id, user_id, org_id, patient_id, token_hash, client_type, auth_method, idle_timeout, idle_expires_at, absolute_expires_at)
VALUES ('30000000-0000-0000-0000-000000000030', '10000000-0000-0000-0000-000000000030', :ORG, '90000000-0000-0000-0000-000000000001',
        sha256('\x30'), 'mobile', 'otp', interval '30 minutes', now() + interval '30 minutes', now() + interval '30 days');
COMMIT;
\set as_patient 'SET LOCAL ROLE spartial_app; SELECT 1 FROM sec.begin_request(''30000000-0000-0000-0000-000000000030'');'
BEGIN; :as_patient
SELECT test.eq((SELECT count(*) FROM patient.patient), 1::bigint, 'PORTAL: patient sees exactly their own record');
SELECT test.eq((SELECT count(*) FROM clinical.appointment WHERE patient_id <> '90000000-0000-0000-0000-000000000001'), 0::bigint, 'PORTAL: patient sees no other patient''s appointments');
SELECT test.eq((SELECT count(*) FROM clinical.appointment), 2::bigint, 'PORTAL: patient sees both own appointments');
SELECT test.eq((SELECT count(*) FROM billing.invoice), 1::bigint, 'PORTAL: patient sees own invoice');
SELECT test.eq((SELECT count(*) FROM clinical.consultation), 0::bigint, 'PORTAL: clinical notes not exposed (scope decision pending)');
SELECT test.eq((SELECT count(*) FROM patient.identifier), 0::bigint, 'PORTAL: identifiers not exposed');
SELECT test.lives($$INSERT INTO clinical.appointment (org_id, facility_id, department_id, patient_id, source, scheduled_start, scheduled_end, status, reason, idempotency_key)
  VALUES ('a0000000-0000-0000-0000-000000000001','a0000000-0000-0000-0000-0000000000f1','a0000000-0000-0000-0000-0000000000d3',
  '90000000-0000-0000-0000-000000000001','patient_app', now() + interval '3 days', now() + interval '3 days 30 minutes', 'requested', 'Review', 'aaaa0000-0000-0000-0000-000000000001')$$,
  'PORTAL: patient can request an appointment for self');
SELECT test.throws($$INSERT INTO clinical.appointment (org_id, facility_id, department_id, patient_id, source, scheduled_start, scheduled_end, status)
  VALUES ('a0000000-0000-0000-0000-000000000001','a0000000-0000-0000-0000-0000000000f1','a0000000-0000-0000-0000-0000000000d3',
  '90000000-0000-0000-0000-000000000002','patient_app', now() + interval '4 days', now() + interval '4 days 30 minutes', 'requested')$$,
  '42501', 'PORTAL: patient cannot book for someone else');
SELECT test.throws($$INSERT INTO clinical.appointment (org_id, facility_id, department_id, patient_id, source, scheduled_start, scheduled_end, status)
  VALUES ('a0000000-0000-0000-0000-000000000001','a0000000-0000-0000-0000-0000000000f1','a0000000-0000-0000-0000-0000000000d3',
  '90000000-0000-0000-0000-000000000001','patient_app', now() + interval '5 days', now() + interval '5 days 30 minutes', 'booked')$$,
  '42501', 'PORTAL: patient cannot self-confirm a booking');
SELECT test.throws($$INSERT INTO clinical.appointment (org_id, facility_id, department_id, patient_id, source, scheduled_start, scheduled_end, status, idempotency_key)
  VALUES ('a0000000-0000-0000-0000-000000000001','a0000000-0000-0000-0000-0000000000f1','a0000000-0000-0000-0000-0000000000d3',
  '90000000-0000-0000-0000-000000000001','patient_app', now() + interval '3 days', now() + interval '3 days 30 minutes', 'requested', 'aaaa0000-0000-0000-0000-000000000001')$$,
  '23505', 'PORTAL: app retry with same idempotency key does not double-book');
SELECT test.throws($$UPDATE clinical.appointment SET scheduled_start = scheduled_start - interval '1 day', scheduled_end = scheduled_end - interval '1 day'
  WHERE idempotency_key = 'aaaa0000-0000-0000-0000-000000000001'$$, '42501', 'PORTAL: patient cannot reschedule (column guard)');
SELECT test.lives($$UPDATE clinical.appointment SET status = 'cancelled', cancel_reason = 'Feeling better (TEST)', cancelled_at = now()
  WHERE idempotency_key = 'aaaa0000-0000-0000-0000-000000000001'$$, 'PORTAL: patient can cancel own requested appointment');
COMMIT;

-- Consent-gated integrations.
BEGIN; :as_doctor
INSERT INTO clinical.appointment (id, org_id, facility_id, department_id, patient_id, practitioner_membership_id, appointment_type, source,
       scheduled_start, scheduled_end, status)
VALUES ('91000000-0000-0000-0000-000000000002', :ORG, 'a0000000-0000-0000-0000-0000000000f1', 'a0000000-0000-0000-0000-0000000000d3',
        '90000000-0000-0000-0000-000000000002', '20000000-0000-0000-0000-000000000004', 'telemedicine', 'reception',
        now() + interval '2 days', now() + interval '2 days 20 minutes', 'booked');
INSERT INTO patient.consent (id, org_id, patient_id, consent_type, capture_method, policy_version, status, withdrawn_at)
VALUES ('9e000000-0000-0000-0000-000000000001', :ORG, '90000000-0000-0000-0000-000000000002', 'telemedicine', 'written', 'v1', 'granted', NULL);
UPDATE patient.consent SET status = 'withdrawn', withdrawn_at = now(), withdrawal_reason = 'TEST' WHERE id = '9e000000-0000-0000-0000-000000000001';
SELECT test.throws($$INSERT INTO telehealth.session (org_id, appointment_id, patient_id, practitioner_membership_id, consent_id, provider, scheduled_start)
  VALUES ('a0000000-0000-0000-0000-000000000001','91000000-0000-0000-0000-000000000002','90000000-0000-0000-0000-000000000002',
  '20000000-0000-0000-0000-000000000004','9e000000-0000-0000-0000-000000000001','jitsi_self_hosted', now() + interval '2 days')$$,
  'SPC01', 'CONSENT: telemedicine session requires active telemedicine consent');
INSERT INTO patient.consent (id, org_id, patient_id, consent_type, capture_method, policy_version)
VALUES ('9e000000-0000-0000-0000-000000000002', :ORG, '90000000-0000-0000-0000-000000000002', 'telemedicine', 'written', 'v1');
SELECT test.lives($$INSERT INTO telehealth.session (org_id, appointment_id, patient_id, practitioner_membership_id, consent_id, provider, scheduled_start)
  VALUES ('a0000000-0000-0000-0000-000000000001','91000000-0000-0000-0000-000000000002','90000000-0000-0000-0000-000000000002',
  '20000000-0000-0000-0000-000000000004','9e000000-0000-0000-0000-000000000002','jitsi_self_hosted', now() + interval '2 days')$$,
  'CONSENT: telemedicine session scheduled once consent captured');
SELECT test.throws($$UPDATE patient.consent SET status = 'granted', withdrawn_at = NULL WHERE id = '9e000000-0000-0000-0000-000000000001'$$,
  'SPT01', 'CONSENT: withdrawn consent cannot be silently re-activated (new grant required)');
COMMIT;
BEGIN;
SET LOCAL ROLE spartial_worker;
SELECT sec.begin_system_request('a0000000-0000-0000-0000-000000000001', NULL, '{comms.read}');
SELECT test.throws($$INSERT INTO comms.message (org_id, patient_id, channel, purpose, recipient_address)
  VALUES ('a0000000-0000-0000-0000-000000000001','90000000-0000-0000-0000-000000000002','sms','appointment_reminder','+255700000003')$$,
  'SPC01', 'CONSENT: SMS reminder blocked without SMS consent');
SELECT test.lives($$INSERT INTO comms.message (org_id, patient_id, channel, purpose, recipient_address)
  VALUES ('a0000000-0000-0000-0000-000000000001','90000000-0000-0000-0000-000000000001','sms','appointment_reminder','+255700000001')$$,
  'CONSENT: SMS reminder allowed with SMS consent');
SELECT test.throws($$SELECT sec.begin_system_request('a0000000-0000-0000-0000-000000000001', NULL, '{clinical.read}')$$, '42501', 'WORKER: cannot grant itself clinical access');
COMMIT;

-- Audit trail integrity.
BEGIN; SET LOCAL ROLE spartial_app;
SELECT test.throws($$UPDATE audit.event SET outcome = 'success'$$, '42501', 'AUDIT: app role has no UPDATE on audit trail');
COMMIT;
BEGIN;
SELECT test.throws($$UPDATE audit.event SET outcome = 'denied' WHERE id = (SELECT min(id) FROM audit.event)$$, 'SPA01', 'AUDIT: even the owner cannot update audit rows (trigger)');
SELECT test.throws($$DELETE FROM audit.event WHERE id = (SELECT min(id) FROM audit.event)$$, 'SPA01', 'AUDIT: audit rows cannot be deleted');
SELECT test.throws($$TRUNCATE audit.event$$, 'SPA01', 'AUDIT: audit table cannot be truncated');
COMMIT;
SELECT audit.seal_pending();
BEGIN;
SELECT test.ok((SELECT row_count > 0 FROM audit.seal ORDER BY seal_no DESC LIMIT 1), 'AUDIT: seal covers committed events');
SELECT test.eq((SELECT count(*) FROM audit.verify_integrity()), 0::bigint, 'AUDIT: integrity verification clean');
COMMIT;
BEGIN; :as_compliance
SELECT test.ok((SELECT count(*) > 20 FROM audit.event), 'AUDIT: compliance officer reads tenant audit trail');
SELECT test.ok((SELECT count(DISTINCT action) >= 10 FROM audit.event WHERE patient_id = '90000000-0000-0000-0000-000000000001'), 'AUDIT: per-patient access/disclosure history available');
COMMIT;
-- simulated tamper by a privileged DB user bypassing triggers → detected
BEGIN;
ALTER TABLE audit.event DISABLE TRIGGER trg_audit_append_only;
UPDATE audit.event SET outcome = 'denied' WHERE id = (SELECT min(id) FROM audit.event);
ALTER TABLE audit.event ENABLE TRIGGER trg_audit_append_only;
SELECT count(*) >= 1 AS tamper_detected FROM audit.verify_integrity() WHERE problem IN ('row_hash_mismatch','seal_content_mismatch') \gset
ROLLBACK;
SELECT test.ok(:'tamper_detected'::boolean, 'AUDIT: tampering by privileged user is detected by hash/seal verification');
SELECT test.eq((SELECT count(*) FROM audit.verify_integrity()), 0::bigint, 'AUDIT: chain clean again after tamper simulation rolled back');
