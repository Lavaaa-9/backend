-- Tests for migration 0013: verification history, nursing observations,
-- procedures (and billing them once), staff activity/access reports.
\set ON_ERROR_STOP 1
\set ORG '''a0000000-0000-0000-0000-000000000001'''
\set FAC '''a0000000-0000-0000-0000-0000000000f1'''
\set as_recep   'SET LOCAL ROLE spartial_app; SELECT 1 FROM sec.begin_request(''30000000-0000-0000-0000-000000000002'');'
\set as_nurse   'SET LOCAL ROLE spartial_app; SELECT 1 FROM sec.begin_request(''30000000-0000-0000-0000-000000000003'');'
\set as_doctor  'SET LOCAL ROLE spartial_app; SELECT 1 FROM sec.begin_request(''30000000-0000-0000-0000-000000000004'');'
\set as_labtech 'SET LOCAL ROLE spartial_app; SELECT 1 FROM sec.begin_request(''30000000-0000-0000-0000-000000000006'');'
\set as_cashier 'SET LOCAL ROLE spartial_app; SELECT 1 FROM sec.begin_request(''30000000-0000-0000-0000-000000000009'');'
\set as_manager 'SET LOCAL ROLE spartial_app; SELECT 1 FROM sec.begin_request(''30000000-0000-0000-0000-000000000011'');'
SET test.suite = 'scope_gaps';

-- Setup: a procedure service + price, and a fresh visit for patient 2.
BEGIN;
SELECT set_config('app.org_id', 'a0000000-0000-0000-0000-000000000001', true);
INSERT INTO billing.service_catalog (id, org_id, code, name, category) VALUES
 ('80000000-0000-0000-0000-000000000010', :ORG, 'PROC-DRESS', 'Wound dressing', 'procedure');
INSERT INTO billing.price (org_id, service_id, unit_price, effective) VALUES
 (:ORG, '80000000-0000-0000-0000-000000000010', 8000, tstzrange('2026-01-01', NULL));
COMMIT;
BEGIN; :as_recep
INSERT INTO clinical.encounter (id, org_id, facility_id, patient_id, encounter_no, encounter_type, payer_id)
VALUES ('92000000-0000-0000-0000-0000000000d1', :ORG, :FAC, '90000000-0000-0000-0000-000000000001',
        clinical.next_encounter_no(:ORG), 'outpatient', '50000000-0000-0000-0000-000000000001');

-- 1. Verification history
SELECT test.lives($$INSERT INTO patient.verification (org_id, patient_id, encounter_id, identifier_id, scope, method, outcome)
  SELECT 'a0000000-0000-0000-0000-000000000001', '90000000-0000-0000-0000-000000000001', '92000000-0000-0000-0000-0000000000d1',
         id, '{identity,contacts}', 'id_document_seen', 'confirmed'
  FROM patient.identifier WHERE patient_id = '90000000-0000-0000-0000-000000000001' LIMIT 1$$,
  'VERIFY: reception records identity check against the patient''s NIDA identifier');
SELECT test.throws($$INSERT INTO patient.verification (org_id, patient_id, scope, method, outcome)
  VALUES ('a0000000-0000-0000-0000-000000000001','90000000-0000-0000-0000-000000000001','{identity}','id_document_seen','confirmed')$$,
  '23514', 'VERIFY: document-based verification must name the identifier checked');
SELECT test.throws($$INSERT INTO patient.verification (org_id, patient_id, scope, method, outcome)
  VALUES ('a0000000-0000-0000-0000-000000000001','90000000-0000-0000-0000-000000000001','{contacts}','patient_stated','discrepancy')$$,
  '23514', 'VERIFY: a discrepancy requires a note');
SELECT test.throws($$INSERT INTO patient.verification (org_id, patient_id, scope, method, outcome)
  VALUES ('a0000000-0000-0000-0000-000000000001','90000000-0000-0000-0000-000000000001','{shoe_size}','patient_stated','confirmed')$$,
  '23514', 'VERIFY: scope limited to known detail groups');
SELECT test.throws($$INSERT INTO patient.verification (org_id, patient_id, identifier_id, scope, method, outcome)
  SELECT 'a0000000-0000-0000-0000-000000000001','90000000-0000-0000-0000-000000000002', id, '{identity}','id_document_seen','confirmed'
  FROM patient.identifier WHERE patient_id = '90000000-0000-0000-0000-000000000001' LIMIT 1$$,
  'SPX01', 'VERIFY: cannot cite another patient''s ID document');
SELECT test.throws($$UPDATE patient.verification SET outcome = 'updated'$$, '42501', 'VERIFY: history not editable by the API');
SELECT test.eq((SELECT outcome FROM patient.v_last_verification WHERE patient_id = '90000000-0000-0000-0000-000000000001'), 'confirmed', 'VERIFY: last-verification view returns latest');
COMMIT;
BEGIN;
SELECT test.throws($$DELETE FROM patient.verification$$, 'SPA01', 'VERIFY: append-only even for the owner');
COMMIT;

-- 2. Nursing observations
BEGIN; :as_nurse
SELECT test.lives($$INSERT INTO clinical.nursing_observation (id, org_id, encounter_id, patient_id, recorded_by_membership_id, observation_type, note)
  VALUES ('9f000000-0000-0000-0000-000000000001','a0000000-0000-0000-0000-000000000001','92000000-0000-0000-0000-0000000000d1',
  '90000000-0000-0000-0000-000000000001','20000000-0000-0000-0000-000000000003','pain_reassessment','Pain 3/10 after paracetamol (TEST)')$$,
  'NURSING: nurse records an observation during the visit');
SELECT test.throws($$INSERT INTO clinical.nursing_observation (org_id, encounter_id, patient_id, recorded_by_membership_id, observation_type, note)
  VALUES ('a0000000-0000-0000-0000-000000000001','92000000-0000-0000-0000-0000000000d1','90000000-0000-0000-0000-000000000001',
  '20000000-0000-0000-0000-000000000004','general','Written in someone else''s name')$$,
  'SPV01', 'NURSING: cannot record an observation under another staff member');
SELECT test.throws($$INSERT INTO clinical.nursing_observation (org_id, encounter_id, patient_id, recorded_by_membership_id, observation_type, note)
  VALUES ('a0000000-0000-0000-0000-000000000001','92000000-0000-0000-0000-0000000000d1','90000000-0000-0000-0000-000000000002',
  '20000000-0000-0000-0000-000000000003','general','Wrong patient for this visit')$$,
  '23503', 'NURSING: observation patient must match the visit');
SELECT test.throws($$UPDATE clinical.nursing_observation SET note = 'edited' WHERE id = '9f000000-0000-0000-0000-000000000001'$$,
  'SPI01', 'NURSING: observations are immutable once recorded');
SELECT test.lives($$UPDATE clinical.nursing_observation SET status = 'entered_in_error', error_reason = 'Wrong patient chart (TEST)'
  WHERE id = '9f000000-0000-0000-0000-000000000001'$$, 'NURSING: observation can be marked entered-in-error with reason');
COMMIT;
BEGIN; :as_labtech
SELECT test.eq((SELECT count(*) FROM clinical.nursing_observation), 0::bigint, 'NURSING: lab staff cannot read nursing notes');
COMMIT;

-- 3. Procedures performed and billed once
BEGIN; :as_doctor
INSERT INTO clinical.procedure (id, org_id, encounter_id, patient_id, service_id, description, status, ordered_by_membership_id)
VALUES ('9f100000-0000-0000-0000-000000000001', :ORG, '92000000-0000-0000-0000-0000000000d1', '90000000-0000-0000-0000-000000000001',
        '80000000-0000-0000-0000-000000000010', 'Dressing of left forearm laceration', 'planned', '20000000-0000-0000-0000-000000000004');
SELECT test.throws($$INSERT INTO clinical.procedure (org_id, encounter_id, patient_id, service_id, description, status)
  VALUES ('a0000000-0000-0000-0000-000000000001','92000000-0000-0000-0000-0000000000d1','90000000-0000-0000-0000-000000000001',
  '80000000-0000-0000-0000-000000000001','Consultation is not a procedure','planned')$$, 'SPX01', 'PROCEDURE: must link a procedure-category service');
COMMIT;
BEGIN; :as_cashier
INSERT INTO billing.invoice (id, org_id, facility_id, patient_id, encounter_id, payer_id)
VALUES ('9b000000-0000-0000-0000-0000000000d1', :ORG, :FAC, '90000000-0000-0000-0000-000000000001', '92000000-0000-0000-0000-0000000000d1', '50000000-0000-0000-0000-000000000001');
SELECT test.throws($$INSERT INTO billing.invoice_line (org_id, invoice_id, service_id, source_type, procedure_id, description, quantity, unit_price)
  VALUES ('a0000000-0000-0000-0000-000000000001','9b000000-0000-0000-0000-0000000000d1','80000000-0000-0000-0000-000000000010','procedure',
  '9f100000-0000-0000-0000-000000000001','Wound dressing',1,8000)$$, 'SPB01', 'PROCEDURE: a planned (not yet performed) procedure cannot be billed');
COMMIT;
BEGIN; :as_nurse
UPDATE clinical.procedure SET status = 'performed', outcome_note = 'Clean, dressed, no complications (TEST)' WHERE id = '9f100000-0000-0000-0000-000000000001';
SELECT test.ok((SELECT performed_by_membership_id = '20000000-0000-0000-0000-000000000003' AND performed_at IS NOT NULL
                FROM clinical.procedure WHERE id = '9f100000-0000-0000-0000-000000000001'), 'PROCEDURE: nurse performs it; performer and time stamped');
SELECT test.throws($$UPDATE clinical.procedure SET description = 'changed' WHERE id = '9f100000-0000-0000-0000-000000000001'$$, 'SPI01', 'PROCEDURE: performed procedure immutable');
COMMIT;
BEGIN; :as_cashier
SELECT test.eq((SELECT count(*) FROM clinical.procedure WHERE status = 'performed'), 1::bigint, 'PROCEDURE: cashier can see performed procedures to bill');
INSERT INTO billing.invoice_line (org_id, invoice_id, service_id, source_type, procedure_id, description, quantity, unit_price)
VALUES (:ORG, '9b000000-0000-0000-0000-0000000000d1', '80000000-0000-0000-0000-000000000010', 'procedure', '9f100000-0000-0000-0000-000000000001', 'Wound dressing', 1,
        billing.price_for('80000000-0000-0000-0000-000000000010'));
SELECT test.throws($$INSERT INTO billing.invoice_line (org_id, invoice_id, service_id, source_type, procedure_id, description, quantity, unit_price)
  VALUES ('a0000000-0000-0000-0000-000000000001','9b000000-0000-0000-0000-0000000000d1','80000000-0000-0000-0000-000000000010','procedure',
  '9f100000-0000-0000-0000-000000000001','Wound dressing again',1,8000)$$, '23505', 'PROCEDURE: same procedure cannot be billed twice');
SELECT test.throws($$INSERT INTO billing.invoice_line (org_id, invoice_id, service_id, source_type, description, quantity, unit_price)
  VALUES ('a0000000-0000-0000-0000-000000000001','9b000000-0000-0000-0000-0000000000d1','80000000-0000-0000-0000-000000000010','procedure','No source',1,8000)$$,
  '23514', 'PROCEDURE: procedure line must reference the procedure performed');
SELECT test.eq((SELECT total FROM billing.invoice WHERE id = '9b000000-0000-0000-0000-0000000000d1'), 8000.00::numeric(14,2), 'PROCEDURE: invoice total includes the procedure');
UPDATE clinical.procedure SET status = 'entered_in_error', error_reason = 'cashier attempt' WHERE id = '9f100000-0000-0000-0000-000000000001';
SELECT test.eq((SELECT status FROM clinical.procedure WHERE id = '9f100000-0000-0000-0000-000000000001'), 'performed', 'PROCEDURE: cashier cannot change clinical procedure records (RLS filters update)');
COMMIT;

-- 4. Staff activity and access reports
BEGIN; :as_manager
SELECT test.ok((SELECT count(*) > 0 FROM reporting.staff_activity(current_date - 1, current_date)), 'STAFF: manager sees staff activity summary');
SELECT test.ok((SELECT bool_or(staff_name = 'TEST recep') FROM reporting.staff_activity(current_date - 1, current_date)), 'STAFF: activity attributed to named staff');
SELECT test.ok((SELECT count(*) = 13 FROM reporting.staff_access()), 'STAFF: access report lists every staff member');
SELECT test.ok((SELECT 'billing.payment.reverse' = ANY (privileged_permissions) FROM reporting.staff_access() WHERE staff_name = 'TEST billsup'),
               'STAFF: access report shows privileged permissions per person');
SELECT test.eq((SELECT count(*) FROM audit.event), 0::bigint, 'STAFF: manager still cannot read raw audit rows (PHI diffs)');
SELECT test.throws($$SELECT * FROM reporting.staff_activity(current_date - 400, current_date)$$, 'SPV01', 'STAFF: range limited to a year');
COMMIT;
BEGIN; :as_recep
SELECT test.throws($$SELECT * FROM reporting.staff_activity(current_date, current_date)$$, '42501', 'STAFF: reception cannot view staff activity');
COMMIT;
BEGIN;
SELECT test.ok((SELECT count(*) >= 1 FROM audit.event WHERE action = 'report.staff_activity.view'), 'STAFF: viewing the activity report is itself audited');
COMMIT;
