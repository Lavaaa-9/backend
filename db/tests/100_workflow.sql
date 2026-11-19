-- End-to-end clinical journey (roadmap "End-to-end workflow to demonstrate"):
-- registration → appointment → queue → triage → consultation → lab → pharmacy
-- → billing → follow-up → reports, with permission and audit checks at each step.
\set ON_ERROR_STOP 1
\set ORG '''a0000000-0000-0000-0000-000000000001'''
\set FAC '''a0000000-0000-0000-0000-0000000000f1'''
\set as_admin   'SET LOCAL ROLE spartial_app; SELECT 1 FROM sec.begin_request(''30000000-0000-0000-0000-000000000001'');'
\set as_recep   'SET LOCAL ROLE spartial_app; SELECT 1 FROM sec.begin_request(''30000000-0000-0000-0000-000000000002'');'
\set as_nurse   'SET LOCAL ROLE spartial_app; SELECT 1 FROM sec.begin_request(''30000000-0000-0000-0000-000000000003'');'
\set as_doctor  'SET LOCAL ROLE spartial_app; SELECT 1 FROM sec.begin_request(''30000000-0000-0000-0000-000000000004'');'
\set as_doctor2 'SET LOCAL ROLE spartial_app; SELECT 1 FROM sec.begin_request(''30000000-0000-0000-0000-000000000005'');'
\set as_labtech 'SET LOCAL ROLE spartial_app; SELECT 1 FROM sec.begin_request(''30000000-0000-0000-0000-000000000006'');'
\set as_labsup  'SET LOCAL ROLE spartial_app; SELECT 1 FROM sec.begin_request(''30000000-0000-0000-0000-000000000007'');'
\set as_pharm   'SET LOCAL ROLE spartial_app; SELECT 1 FROM sec.begin_request(''30000000-0000-0000-0000-000000000008'');'
\set as_cashier 'SET LOCAL ROLE spartial_app; SELECT 1 FROM sec.begin_request(''30000000-0000-0000-0000-000000000009'');'
\set as_billsup 'SET LOCAL ROLE spartial_app; SELECT 1 FROM sec.begin_request(''30000000-0000-0000-0000-000000000010'');'
\set as_manager 'SET LOCAL ROLE spartial_app; SELECT 1 FROM sec.begin_request(''30000000-0000-0000-0000-000000000011'');'
SET test.suite = 'workflow';

-- ===================================================================== Phase 2
-- Patient registration with MRN, contact, encrypted identifier.
BEGIN; :as_recep
INSERT INTO patient.patient (id, org_id, mrn, registered_facility_id, first_name, last_name, sex, date_of_birth, is_test_data)
VALUES ('90000000-0000-0000-0000-000000000001', :ORG, patient.next_mrn(:ORG), :FAC, 'Asha', 'Mwakyusa', 'female', '1990-04-12', true);
INSERT INTO patient.contact_point (org_id, patient_id, system, value, is_primary)
VALUES (:ORG, '90000000-0000-0000-0000-000000000001', 'phone', '+255700000001', true);
INSERT INTO patient.identifier (org_id, patient_id, id_type, value_ciphertext, value_bidx, value_last4, key_ref)
VALUES (:ORG, '90000000-0000-0000-0000-000000000001', 'nida_nin', '\xdeadbeef', sha256('NIN-TEST-1'), '0001', 'kms://test/v1');
INSERT INTO patient.next_of_kin (org_id, patient_id, full_name, relationship, phone_e164)
VALUES (:ORG, '90000000-0000-0000-0000-000000000001', 'Juma Mwakyusa', 'spouse', '+255700000002');
INSERT INTO patient.consent (org_id, patient_id, consent_type, capture_method, policy_version)
VALUES (:ORG, '90000000-0000-0000-0000-000000000001', 'treatment_general', 'written', 'v1'),
       (:ORG, '90000000-0000-0000-0000-000000000001', 'sms_notifications', 'written', 'v1');
SELECT test.ok((SELECT mrn ~ '^SP-[0-9]{4}-000001$' FROM patient.patient WHERE id = '90000000-0000-0000-0000-000000000001'), 'P2: MRN allocated SP-YYYY-000001');
SELECT test.eq((SELECT count(*) FROM patient.find_duplicates('Asha', 'Mwakyusa', '1990-04-12')), 1::bigint, 'P2: soft duplicate finder returns the existing patient');
SELECT test.eq((SELECT count(*) FROM patient.find_duplicates('Ashah', 'Mwakusa', '1990-04-12')), 1::bigint, 'P2: duplicate finder tolerates misspelling');
SELECT test.throws($$INSERT INTO patient.patient (org_id, mrn, registered_facility_id, first_name, last_name, sex, date_of_birth)
  VALUES ('a0000000-0000-0000-0000-000000000001', 'SP-2026-999999', 'a0000000-0000-0000-0000-0000000000f1', 'Future', 'Baby', 'male', current_date + 1)$$,
  'SPV01', 'P2: future date of birth rejected');
-- second patient for cross-checks
INSERT INTO patient.patient (id, org_id, mrn, registered_facility_id, first_name, last_name, sex, date_of_birth, is_test_data)
VALUES ('90000000-0000-0000-0000-000000000002', :ORG, patient.next_mrn(:ORG), :FAC, 'Baraka', 'Kimaro', 'male', '1985-01-01', true);
SELECT test.throws($$INSERT INTO patient.identifier (org_id, patient_id, id_type, value_ciphertext, value_bidx, key_ref)
  VALUES ('a0000000-0000-0000-0000-000000000001','90000000-0000-0000-0000-000000000002','nida_nin','\x00',sha256('NIN-TEST-1'),'kms://test/v1')$$,
  '23505', 'P2: same national ID on a second patient rejected (blind-index uniqueness)');
SELECT test.throws($$UPDATE patient.patient SET mrn = 'SP-2026-000500' WHERE id = '90000000-0000-0000-0000-000000000001'$$,
  'SPI01', 'P2: MRN immutable');
SELECT test.throws($$INSERT INTO patient.contact_point (org_id, patient_id, system, value) VALUES
  ('a0000000-0000-0000-0000-000000000001','90000000-0000-0000-0000-000000000001','phone','0712 345 678')$$,
  '23514', 'P2: phone must be E.164');
UPDATE patient.patient SET occupation = 'Teacher' WHERE id = '90000000-0000-0000-0000-000000000001';
SELECT test.eq((SELECT row_version FROM patient.patient WHERE id = '90000000-0000-0000-0000-000000000001'), 2, 'P2: optimistic-lock row_version incremented on update');
COMMIT;

-- Audit entries for creation and edit.
BEGIN;
SELECT test.ok((SELECT count(*) >= 1 FROM audit.event WHERE resource_type = 'patient.patient' AND action = 'patient_patient.insert'
               AND patient_id = '90000000-0000-0000-0000-000000000001' AND actor_user_id = '10000000-0000-0000-0000-000000000002'), 'P2: audit records patient creation with actor');
SELECT test.ok((SELECT changes ? 'occupation' FROM audit.event WHERE action = 'patient_patient.update'
               AND patient_id = '90000000-0000-0000-0000-000000000001' ORDER BY id DESC LIMIT 1), 'P2: audit stores before/after diff for edits');
SELECT test.ok((SELECT NOT (changes::text LIKE '%deadbeef%') FROM audit.event WHERE resource_type = 'patient.identifier' LIMIT 1), 'P2: encrypted identifier excluded from audit payload');
COMMIT;

-- ===================================================================== Phase 3
-- Appointment, check-in, encounter, queue.
BEGIN; :as_recep
INSERT INTO clinical.appointment (id, org_id, facility_id, department_id, patient_id, practitioner_membership_id, source,
       scheduled_start, scheduled_end, status, reason)
VALUES ('91000000-0000-0000-0000-000000000001', :ORG, :FAC, 'a0000000-0000-0000-0000-0000000000d3', '90000000-0000-0000-0000-000000000001',
        '20000000-0000-0000-0000-000000000004', 'reception', date_trunc('hour', now()) + interval '1 hour', date_trunc('hour', now()) + interval '90 minutes', 'booked', 'Fever 3 days');
SELECT test.throws($$INSERT INTO clinical.appointment (org_id, facility_id, department_id, patient_id, practitioner_membership_id, source,
       scheduled_start, scheduled_end, status) VALUES ('a0000000-0000-0000-0000-000000000001','a0000000-0000-0000-0000-0000000000f1',
       'a0000000-0000-0000-0000-0000000000d3','90000000-0000-0000-0000-000000000002','20000000-0000-0000-0000-000000000004','reception',
       date_trunc('hour', now()) + interval '70 minutes', date_trunc('hour', now()) + interval '100 minutes', 'booked')$$,
  '23P01', 'P3: practitioner double-booking blocked by exclusion constraint');
SELECT test.throws($$INSERT INTO clinical.appointment (org_id, facility_id, department_id, patient_id, source, scheduled_start, scheduled_end, status)
  VALUES ('a0000000-0000-0000-0000-000000000001','a0000000-0000-0000-0000-0000000000f1','a0000000-0000-0000-0000-0000000000d3',
  '90000000-0000-0000-0000-000000000002','reception', now(), now() + interval '30 minutes', 'completed')$$,
  'SPT01', 'P3: appointment cannot be created directly as completed');
UPDATE clinical.appointment SET status = 'checked_in', checked_in_at = now() WHERE id = '91000000-0000-0000-0000-000000000001';
INSERT INTO clinical.encounter (id, org_id, facility_id, patient_id, appointment_id, encounter_no, encounter_type, payer_id, chief_complaint)
VALUES ('92000000-0000-0000-0000-000000000001', :ORG, :FAC, '90000000-0000-0000-0000-000000000001', '91000000-0000-0000-0000-000000000001',
        clinical.next_encounter_no(:ORG), 'outpatient', '50000000-0000-0000-0000-000000000001', 'Fever');
SELECT test.throws($$INSERT INTO clinical.encounter (org_id, facility_id, patient_id, encounter_no, encounter_type)
  VALUES ('a0000000-0000-0000-0000-000000000001','a0000000-0000-0000-0000-0000000000f1','90000000-0000-0000-0000-000000000001','V-2026-999998','outpatient')$$,
  '23505', 'P3: second open visit for same patient blocked (duplicate check-in)');
INSERT INTO clinical.appointment (id, org_id, facility_id, department_id, patient_id, source, scheduled_start, scheduled_end, status)
VALUES ('91000000-0000-0000-0000-0000000000a9', :ORG, :FAC, 'a0000000-0000-0000-0000-0000000000d3', '90000000-0000-0000-0000-000000000001',
        'reception', now() + interval '10 days', now() + interval '10 days 30 minutes', 'requested');
SELECT test.throws($$INSERT INTO clinical.encounter (org_id, facility_id, patient_id, appointment_id, encounter_no, encounter_type)
  VALUES ('a0000000-0000-0000-0000-000000000001','a0000000-0000-0000-0000-0000000000f1','90000000-0000-0000-0000-000000000002',
  '91000000-0000-0000-0000-0000000000a9','V-2026-999997','outpatient')$$,
  '23503', 'P3: visit cannot reference another patient''s appointment (composite FK)');
INSERT INTO clinical.queue_ticket (id, org_id, facility_id, department_id, encounter_id, patient_id, priority)
VALUES ('93000000-0000-0000-0000-000000000001', :ORG, :FAC, 'a0000000-0000-0000-0000-0000000000d2', '92000000-0000-0000-0000-000000000001', '90000000-0000-0000-0000-000000000001', 'routine');
SELECT test.eq((SELECT ticket_no FROM clinical.queue_ticket WHERE id = '93000000-0000-0000-0000-000000000001'), 1, 'P3: first ticket of the day is #1');
SELECT test.throws($$INSERT INTO clinical.queue_ticket (org_id, facility_id, department_id, encounter_id, patient_id)
  VALUES ('a0000000-0000-0000-0000-000000000001','a0000000-0000-0000-0000-0000000000f1','a0000000-0000-0000-0000-0000000000d2',
  '92000000-0000-0000-0000-000000000001','90000000-0000-0000-0000-000000000001')$$, '23505', 'P3: one active ticket per visit per department');
SELECT test.throws($$INSERT INTO clinical.triage_assessment (org_id, encounter_id, patient_id, recorded_by_membership_id, temperature_c)
  VALUES ('a0000000-0000-0000-0000-000000000001','92000000-0000-0000-0000-000000000001','90000000-0000-0000-0000-000000000001','20000000-0000-0000-0000-000000000002', 38.5)$$,
  '42501', 'P3: receptionist cannot record triage (RLS)');
SELECT test.eq((SELECT count(*) FROM clinical.consultation), 0::bigint, 'P3: receptionist sees no clinical notes');
COMMIT;

-- Triage by nurse.
BEGIN; :as_nurse
UPDATE clinical.queue_ticket SET status = 'called' WHERE id = '93000000-0000-0000-0000-000000000001';
UPDATE clinical.queue_ticket SET status = 'in_service' WHERE id = '93000000-0000-0000-0000-000000000001';
UPDATE clinical.encounter SET status = 'in_progress' WHERE id = '92000000-0000-0000-0000-000000000001';
SELECT test.throws($$INSERT INTO clinical.triage_assessment (org_id, encounter_id, patient_id, recorded_by_membership_id, systolic_mmhg, diastolic_mmhg)
  VALUES ('a0000000-0000-0000-0000-000000000001','92000000-0000-0000-0000-000000000001','90000000-0000-0000-0000-000000000001','20000000-0000-0000-0000-000000000003', 80, 120)$$,
  '23514', 'P3: diastolic above systolic rejected');
SELECT test.throws($$INSERT INTO clinical.triage_assessment (org_id, encounter_id, patient_id, recorded_by_membership_id, temperature_c)
  VALUES ('a0000000-0000-0000-0000-000000000001','92000000-0000-0000-0000-000000000001','90000000-0000-0000-0000-000000000002','20000000-0000-0000-0000-000000000003', 37)$$,
  '23503', 'P3: triage for patient B on patient A''s visit rejected');
SELECT test.throws($$INSERT INTO clinical.triage_assessment (org_id, encounter_id, patient_id, recorded_by_membership_id, temperature_c, status, finalised_at)
  VALUES ('a0000000-0000-0000-0000-000000000001','92000000-0000-0000-0000-000000000001','90000000-0000-0000-0000-000000000001','20000000-0000-0000-0000-000000000003', 37, 'final', now())$$,
  '23514', 'P3: final triage requires category and >=2 vitals');
INSERT INTO clinical.triage_assessment (id, org_id, encounter_id, patient_id, recorded_by_membership_id, temperature_c, pulse_bpm,
       respiratory_rate, systolic_mmhg, diastolic_mmhg, spo2_pct, weight_kg, height_cm, triage_category, status, finalised_at)
VALUES ('94000000-0000-0000-0000-000000000001', :ORG, '92000000-0000-0000-0000-000000000001', '90000000-0000-0000-0000-000000000001',
        '20000000-0000-0000-0000-000000000003', 38.6, 104, 20, 118, 76, 97, 62.5, 165, 'priority', 'final', now());
SELECT test.eq((SELECT bmi FROM clinical.triage_assessment WHERE id = '94000000-0000-0000-0000-000000000001'), 22.96::numeric(5,2), 'P3: BMI derived');
SELECT test.throws($$UPDATE clinical.triage_assessment SET temperature_c = 37.0 WHERE id = '94000000-0000-0000-0000-000000000001'$$,
  'SPI01', 'P3: finalised triage cannot be edited');
INSERT INTO clinical.allergy (org_id, patient_id, substance, category, severity) VALUES (:ORG, '90000000-0000-0000-0000-000000000001', 'Sulfonamides', 'drug', 'severe');
UPDATE clinical.queue_ticket SET status = 'completed' WHERE id = '93000000-0000-0000-0000-000000000001';
INSERT INTO clinical.queue_ticket (org_id, facility_id, department_id, encounter_id, patient_id, priority)
VALUES (:ORG, :FAC, 'a0000000-0000-0000-0000-0000000000d3', '92000000-0000-0000-0000-000000000001', '90000000-0000-0000-0000-000000000001', 'urgent');
SELECT test.throws($$UPDATE clinical.queue_ticket SET status = 'waiting' WHERE id = '93000000-0000-0000-0000-000000000001'$$,
  'SPT01', 'P3: completed queue ticket cannot return to waiting');
COMMIT;

-- Consultation by doctor.
BEGIN; :as_doctor
SELECT test.eq((SELECT count(*) FROM clinical.triage_assessment WHERE patient_id = '90000000-0000-0000-0000-000000000001'), 1::bigint, 'P3: doctor sees triage history');
INSERT INTO clinical.consultation (id, org_id, encounter_id, patient_id, practitioner_membership_id, presenting_complaint)
VALUES ('95000000-0000-0000-0000-000000000001', :ORG, '92000000-0000-0000-0000-000000000001', '90000000-0000-0000-0000-000000000001',
        '20000000-0000-0000-0000-000000000004', 'Fever, headache x3 days');
SELECT test.throws($$UPDATE clinical.consultation SET status = 'signed' WHERE id = '95000000-0000-0000-0000-000000000001'$$,
  '23514', 'P3: consultation cannot be signed with missing required sections');
UPDATE clinical.consultation SET examination_findings = 'Febrile, no neck stiffness', assessment = 'Suspected malaria',
       treatment_plan = 'MRDT; ALu if positive; paracetamol', follow_up_instructions = 'Review in 3 days'
 WHERE id = '95000000-0000-0000-0000-000000000001';
COMMIT;
BEGIN; :as_doctor2
SELECT test.throws($$UPDATE clinical.consultation SET status = 'signed' WHERE id = '95000000-0000-0000-0000-000000000001'$$,
  'SPV01', 'P3: another doctor cannot sign the author''s note');
COMMIT;
BEGIN; :as_nurse
UPDATE clinical.consultation SET assessment = 'tampered by nurse' WHERE id = '95000000-0000-0000-0000-000000000001';
SELECT test.eq((SELECT assessment FROM clinical.consultation WHERE id = '95000000-0000-0000-0000-000000000001'), 'Suspected malaria', 'P3: nurse cannot modify consultation (RLS filters update)');
COMMIT;
BEGIN; :as_doctor
UPDATE clinical.consultation SET status = 'signed' WHERE id = '95000000-0000-0000-0000-000000000001';
SELECT test.ok((SELECT signed_at IS NOT NULL FROM clinical.consultation WHERE id = '95000000-0000-0000-0000-000000000001'), 'P3: signed_at stamped');
SELECT test.throws($$UPDATE clinical.consultation SET treatment_plan = 'changed' WHERE id = '95000000-0000-0000-0000-000000000001'$$,
  'SPI01', 'P3: signed consultation immutable');
SELECT test.throws($$INSERT INTO clinical.encounter_diagnosis (org_id, encounter_id, patient_id, consultation_id, description, diagnosis_type, recorded_by_membership_id)
  VALUES ('a0000000-0000-0000-0000-000000000001','92000000-0000-0000-0000-000000000001','90000000-0000-0000-0000-000000000001',
  '95000000-0000-0000-0000-000000000001','Malaria','final','20000000-0000-0000-0000-000000000004')$$, '23514', 'P3: final diagnosis requires ICD-10 code');
INSERT INTO clinical.encounter_diagnosis (org_id, encounter_id, patient_id, consultation_id, icd10_code, description, diagnosis_type, is_primary, recorded_by_membership_id)
VALUES (:ORG, '92000000-0000-0000-0000-000000000001', '90000000-0000-0000-0000-000000000001', '95000000-0000-0000-0000-000000000001',
        'B50.9', 'Falciparum malaria', 'final', true, '20000000-0000-0000-0000-000000000004');
-- lab order
INSERT INTO diag.request (id, org_id, facility_id, encounter_id, patient_id, request_no, ordered_by_membership_id, priority, clinical_indication)
VALUES ('96000000-0000-0000-0000-000000000001', :ORG, :FAC, '92000000-0000-0000-0000-000000000001', '90000000-0000-0000-0000-000000000001',
        org.next_number('lab_request'), '20000000-0000-0000-0000-000000000004', 'urgent', 'Fever ?malaria');
INSERT INTO diag.request_item (id, org_id, request_id, patient_id, test_id) VALUES
 ('96100000-0000-0000-0000-000000000001', :ORG, '96000000-0000-0000-0000-000000000001', '90000000-0000-0000-0000-000000000001', '60000000-0000-0000-0000-000000000001');
-- amendment workflow: new version supersedes signed note
INSERT INTO clinical.consultation (id, org_id, encounter_id, patient_id, practitioner_membership_id, presenting_complaint, examination_findings,
       assessment, treatment_plan, supersedes_id, amendment_reason)
SELECT '95000000-0000-0000-0000-000000000002', org_id, encounter_id, patient_id, practitioner_membership_id, presenting_complaint, examination_findings,
       assessment || ' (sulfa allergy noted)', treatment_plan, id, 'Added allergy consideration'
  FROM clinical.consultation WHERE id = '95000000-0000-0000-0000-000000000001';
SELECT test.throws($$INSERT INTO clinical.consultation (org_id, encounter_id, patient_id, practitioner_membership_id) VALUES
  ('a0000000-0000-0000-0000-000000000001','92000000-0000-0000-0000-000000000001','90000000-0000-0000-0000-000000000001','20000000-0000-0000-0000-000000000004')$$,
  '23505', 'P3: only one draft note per practitioner per visit');
COMMIT;
-- (amendment: retire v1 then sign v2)
BEGIN; :as_doctor
UPDATE clinical.consultation SET status = 'amended' WHERE id = '95000000-0000-0000-0000-000000000001';
UPDATE clinical.consultation SET status = 'signed' WHERE id = '95000000-0000-0000-0000-000000000002';
SELECT test.eq((SELECT string_agg(status, ',' ORDER BY id) FROM clinical.consultation WHERE encounter_id = '92000000-0000-0000-0000-000000000001'), 'amended,signed', 'P3: amendment chain v1 amended, v2 signed');
COMMIT;

-- Laboratory.
BEGIN; :as_labtech
INSERT INTO diag.specimen (org_id, request_item_id, patient_id, barcode, specimen_type, status, received_at)
VALUES (:ORG, '96100000-0000-0000-0000-000000000001', '90000000-0000-0000-0000-000000000001', 'BC-TEST-0001', 'blood', 'collected', NULL);
UPDATE diag.request_item SET status = 'specimen_collected' WHERE id = '96100000-0000-0000-0000-000000000001';
UPDATE diag.request SET status = 'in_progress' WHERE id = '96000000-0000-0000-0000-000000000001';
SELECT test.throws($$INSERT INTO diag.result (org_id, request_item_id, patient_id, component_id, value_coded, status)
  VALUES ('a0000000-0000-0000-0000-000000000001','96100000-0000-0000-0000-000000000001','90000000-0000-0000-0000-000000000001',
  '61000000-0000-0000-0000-000000000001','Positive','final')$$, '42501', 'P3: lab technician lacks permission to verify results');
INSERT INTO diag.result (id, org_id, request_item_id, patient_id, component_id, value_coded, abnormal_flag)
VALUES ('97000000-0000-0000-0000-000000000001', :ORG, '96100000-0000-0000-0000-000000000001', '90000000-0000-0000-0000-000000000001',
        '61000000-0000-0000-0000-000000000001', 'Positive', 'A');
SELECT test.eq((SELECT status FROM diag.request WHERE id = '96000000-0000-0000-0000-000000000001'), 'partially_resulted', 'P3: request rolls up to partially_resulted');
SELECT test.eq((SELECT count(*) FROM clinical.consultation), 0::bigint, 'P3: lab tech cannot read consultation notes (minimum necessary)');
COMMIT;
BEGIN; :as_labsup
SELECT test.throws($$INSERT INTO diag.result (org_id, request_item_id, patient_id, component_id, value_numeric, status)
  VALUES ('a0000000-0000-0000-0000-000000000001','96100000-0000-0000-0000-000000000001','90000000-0000-0000-0000-000000000001',
  '61000000-0000-0000-0000-000000000002', 11.2, 'final')$$, 'SPV01', 'P3: supervisor cannot verify a result they entered (four-eyes)');
UPDATE diag.result SET status = 'final' WHERE id = '97000000-0000-0000-0000-000000000001';
SELECT test.eq((SELECT status FROM diag.request WHERE id = '96000000-0000-0000-0000-000000000001'), 'completed', 'P3: verified result completes the request');
SELECT test.throws($$UPDATE diag.result SET value_coded = 'Negative' WHERE id = '97000000-0000-0000-0000-000000000001'$$, 'SPI01', 'P3: final result immutable');
COMMIT;

-- Pharmacy: stock receipt, prescribe, dispense.
BEGIN; :as_pharm
INSERT INTO pharmacy.stock_batch (id, org_id, facility_id, drug_id, batch_no, expiry_date, unit_cost)
VALUES ('98000000-0000-0000-0000-000000000001', :ORG, :FAC, '70000000-0000-0000-0000-000000000001', 'ALU-TEST-1', current_date + 365, 300),
       ('98000000-0000-0000-0000-000000000002', :ORG, :FAC, '70000000-0000-0000-0000-000000000002', 'PCM-TEST-EXP', current_date - 1, 20);
INSERT INTO pharmacy.stock_movement (org_id, batch_id, movement_type, quantity, supplier_ref) VALUES
 (:ORG, '98000000-0000-0000-0000-000000000001', 'receipt', 30, 'GRN-TEST-1'),
 (:ORG, '98000000-0000-0000-0000-000000000002', 'receipt', 100, 'GRN-TEST-2');
SELECT test.eq((SELECT quantity_on_hand FROM pharmacy.stock_batch WHERE id = '98000000-0000-0000-0000-000000000001'), 30.00::numeric(12,2), 'P3: receipt movement updates stock balance');
SELECT test.throws($$UPDATE pharmacy.stock_batch SET quantity_on_hand = 999 WHERE id = '98000000-0000-0000-0000-000000000001'$$, 'SPI01', 'P3: stock balance cannot be edited directly');
SELECT test.throws($$INSERT INTO pharmacy.stock_movement (org_id, batch_id, movement_type, quantity, reason)
  VALUES ('a0000000-0000-0000-0000-000000000001','98000000-0000-0000-0000-000000000001','adjustment_down',-50,'count')$$, 'SPS01', 'P3: stock cannot go negative');
SELECT test.throws($$UPDATE pharmacy.stock_movement SET quantity = 1$$, '42501', 'P3: stock ledger not updatable by the API (no grant)');
SELECT test.throws($$INSERT INTO pharmacy.prescription (org_id, encounter_id, patient_id, prescription_no, prescriber_membership_id)
  VALUES ('a0000000-0000-0000-0000-000000000001','92000000-0000-0000-0000-000000000001','90000000-0000-0000-0000-000000000001','RX-X','20000000-0000-0000-0000-000000000008')$$,
  '42501', 'P3: pharmacist cannot prescribe');
COMMIT;
BEGIN;
SELECT test.throws($$UPDATE pharmacy.stock_movement SET quantity = 1$$, 'SPA01', 'P3: stock ledger append-only even for the owner (trigger)');
COMMIT;
BEGIN; :as_doctor
INSERT INTO pharmacy.prescription (id, org_id, encounter_id, patient_id, prescription_no, prescriber_membership_id)
VALUES ('99000000-0000-0000-0000-000000000001', :ORG, '92000000-0000-0000-0000-000000000001', '90000000-0000-0000-0000-000000000001',
        org.next_number('prescription'), '20000000-0000-0000-0000-000000000004');
SELECT test.throws($$UPDATE pharmacy.prescription SET status = 'active' WHERE id = '99000000-0000-0000-0000-000000000001'$$, 'SPV01', 'P3: empty prescription cannot be signed');
INSERT INTO pharmacy.prescription_item (id, org_id, prescription_id, patient_id, drug_id, dose_amount, dose_unit, route, frequency_code, duration_days, quantity_prescribed)
VALUES ('99100000-0000-0000-0000-000000000001', :ORG, '99000000-0000-0000-0000-000000000001', '90000000-0000-0000-0000-000000000001',
        '70000000-0000-0000-0000-000000000001', 4, 'tablet', 'oral', 'BD', 3, 24),
       ('99100000-0000-0000-0000-000000000002', :ORG, '99000000-0000-0000-0000-000000000001', '90000000-0000-0000-0000-000000000001',
        '70000000-0000-0000-0000-000000000002', 2, 'tablet', 'oral', 'TDS', 3, 18);
UPDATE pharmacy.prescription SET status = 'active' WHERE id = '99000000-0000-0000-0000-000000000001';
SELECT test.throws($$UPDATE pharmacy.prescription_item SET quantity_prescribed = 48 WHERE id = '99100000-0000-0000-0000-000000000001'$$, 'SPI01', 'P3: signed prescription items immutable');
COMMIT;
BEGIN; :as_pharm
INSERT INTO pharmacy.dispense (id, org_id, facility_id, prescription_id, patient_id, dispensed_by_membership_id, idempotency_key)
VALUES ('99200000-0000-0000-0000-000000000001', :ORG, :FAC, '99000000-0000-0000-0000-000000000001', '90000000-0000-0000-0000-000000000001',
        '20000000-0000-0000-0000-000000000008', '99900000-0000-0000-0000-000000000001');
SELECT test.throws($$INSERT INTO pharmacy.dispense_item (org_id, dispense_id, prescription_item_id, batch_id, quantity) VALUES
  ('a0000000-0000-0000-0000-000000000001','99200000-0000-0000-0000-000000000001','99100000-0000-0000-0000-000000000001','98000000-0000-0000-0000-000000000001', 25)$$,
  'SPV01', 'P3: cannot dispense more than prescribed');
SELECT test.throws($$INSERT INTO pharmacy.dispense_item (org_id, dispense_id, prescription_item_id, batch_id, quantity) VALUES
  ('a0000000-0000-0000-0000-000000000001','99200000-0000-0000-0000-000000000001','99100000-0000-0000-0000-000000000001','98000000-0000-0000-0000-000000000002', 5)$$,
  'SPX01', 'P3: batch of a different drug rejected');
SELECT test.throws($$INSERT INTO pharmacy.dispense_item (org_id, dispense_id, prescription_item_id, batch_id, quantity) VALUES
  ('a0000000-0000-0000-0000-000000000001','99200000-0000-0000-0000-000000000001','99100000-0000-0000-0000-000000000002','98000000-0000-0000-0000-000000000002', 5)$$,
  'SPV01', 'P3: expired batch cannot be dispensed');
INSERT INTO pharmacy.dispense_item (id, org_id, dispense_id, prescription_item_id, batch_id, quantity) VALUES
 ('99300000-0000-0000-0000-000000000001', :ORG, '99200000-0000-0000-0000-000000000001', '99100000-0000-0000-0000-000000000001', '98000000-0000-0000-0000-000000000001', 24);
SELECT test.eq((SELECT quantity_on_hand FROM pharmacy.stock_batch WHERE id = '98000000-0000-0000-0000-000000000001'), 6.00::numeric(12,2), 'P3: dispensing decrements stock via ledger');
SELECT test.eq((SELECT status FROM pharmacy.prescription WHERE id = '99000000-0000-0000-0000-000000000001'), 'partially_dispensed', 'P3: prescription partially dispensed');
SELECT test.throws($$INSERT INTO pharmacy.dispense (org_id, facility_id, prescription_id, patient_id, dispensed_by_membership_id, idempotency_key)
  VALUES ('a0000000-0000-0000-0000-000000000001','a0000000-0000-0000-0000-0000000000f1','99000000-0000-0000-0000-000000000001',
  '90000000-0000-0000-0000-000000000001','20000000-0000-0000-0000-000000000008','99900000-0000-0000-0000-000000000001')$$,
  '23505', 'P3: duplicate dispense submission rejected (idempotency key)');
COMMIT;

-- ===================================================================== Phase 4
-- Billing: invoice from consultation, lab and dispensing; cash + mobile money.
BEGIN; :as_cashier
INSERT INTO billing.cashier_shift (id, org_id, facility_id, membership_id, opening_float)
VALUES ('9a000000-0000-0000-0000-000000000001', :ORG, :FAC, '20000000-0000-0000-0000-000000000009', 50000);
INSERT INTO billing.invoice (id, org_id, facility_id, patient_id, encounter_id, payer_id)
VALUES ('9b000000-0000-0000-0000-000000000001', :ORG, :FAC, '90000000-0000-0000-0000-000000000001', '92000000-0000-0000-0000-000000000001', '50000000-0000-0000-0000-000000000001');
SELECT test.throws($$INSERT INTO billing.invoice (org_id, facility_id, patient_id, encounter_id, payer_id)
  VALUES ('a0000000-0000-0000-0000-000000000001','a0000000-0000-0000-0000-0000000000f1','90000000-0000-0000-0000-000000000001',
  '92000000-0000-0000-0000-000000000001','50000000-0000-0000-0000-000000000001')$$, '23505', 'P4: one draft invoice per visit per payer');
INSERT INTO billing.invoice_line (org_id, invoice_id, service_id, source_type, consultation_id, description, quantity, unit_price) VALUES
 (:ORG, '9b000000-0000-0000-0000-000000000001', '80000000-0000-0000-0000-000000000001', 'consultation', '95000000-0000-0000-0000-000000000002', 'GP consultation', 1,
  billing.price_for('80000000-0000-0000-0000-000000000001', '50000000-0000-0000-0000-000000000001'));
INSERT INTO billing.invoice_line (org_id, invoice_id, service_id, source_type, diag_request_item_id, description, quantity, unit_price) VALUES
 (:ORG, '9b000000-0000-0000-0000-000000000001', '80000000-0000-0000-0000-000000000002', 'diagnostic', '96100000-0000-0000-0000-000000000001', 'Malaria RDT', 1, 5000);
INSERT INTO billing.invoice_line (org_id, invoice_id, service_id, source_type, dispense_item_id, description, quantity, unit_price, discount_amount, discount_reason) VALUES
 (:ORG, '9b000000-0000-0000-0000-000000000001', '80000000-0000-0000-0000-000000000003', 'pharmacy', '99300000-0000-0000-0000-000000000001', 'ALu tablets', 24, 500, 2000, 'Staff-approved discount (TEST)');
SELECT test.throws($$INSERT INTO billing.invoice_line (org_id, invoice_id, service_id, source_type, diag_request_item_id, description, quantity, unit_price)
  VALUES ('a0000000-0000-0000-0000-000000000001','9b000000-0000-0000-0000-000000000001','80000000-0000-0000-0000-000000000002','diagnostic',
  '96100000-0000-0000-0000-000000000001','Malaria RDT again',1,5000)$$, '23505', 'P4: same lab item cannot be billed twice');
SELECT test.throws($$UPDATE billing.invoice SET total = 1 WHERE id = '9b000000-0000-0000-0000-000000000001'$$, 'SPI01', 'P4: invoice totals cannot be set by the API');
SELECT test.eq((SELECT total FROM billing.invoice WHERE id = '9b000000-0000-0000-0000-000000000001'), 35000.00::numeric(14,2), 'P4: invoice total = 20000 + 5000 + 12000 - 2000');
UPDATE billing.invoice SET status = 'issued' WHERE id = '9b000000-0000-0000-0000-000000000001';
SELECT test.ok((SELECT invoice_no ~ '^INV-[0-9]{4}-000001$' FROM billing.invoice WHERE id = '9b000000-0000-0000-0000-000000000001'), 'P4: invoice number allocated on issue');
SELECT test.throws($$INSERT INTO billing.invoice_line (org_id, invoice_id, service_id, source_type, description, quantity, unit_price)
  VALUES ('a0000000-0000-0000-0000-000000000001','9b000000-0000-0000-0000-000000000001','80000000-0000-0000-0000-000000000001','manual','extra',1,100)$$,
  'SPB01', 'P4: issued invoice lines locked');
-- cash payment 20,000
INSERT INTO billing.payment (id, org_id, facility_id, patient_id, method, amount, status, idempotency_key, cashier_shift_id, received_by_membership_id)
VALUES ('9c000000-0000-0000-0000-000000000001', :ORG, :FAC, '90000000-0000-0000-0000-000000000001', 'cash', 20000, 'completed',
        '9c900000-0000-0000-0000-000000000001', '9a000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000009');
SELECT test.ok((SELECT receipt_no ~ '^RCT-[0-9]{4}-0000001$' FROM billing.receipt WHERE payment_id = '9c000000-0000-0000-0000-000000000001'), 'P4: receipt auto-issued for completed payment');
INSERT INTO billing.payment_allocation (org_id, payment_id, invoice_id, amount) VALUES (:ORG, '9c000000-0000-0000-0000-000000000001', '9b000000-0000-0000-0000-000000000001', 20000);
SELECT test.eq((SELECT status || ':' || balance FROM billing.invoice WHERE id = '9b000000-0000-0000-0000-000000000001'), 'partially_paid:15000.00', 'P4: partial payment leaves balance 15000');
SELECT test.throws($$INSERT INTO billing.payment (org_id, facility_id, patient_id, method, amount, status, idempotency_key, cashier_shift_id, received_by_membership_id)
  VALUES ('a0000000-0000-0000-0000-000000000001','a0000000-0000-0000-0000-0000000000f1','90000000-0000-0000-0000-000000000001','cash',20000,'completed',
  '9c900000-0000-0000-0000-000000000001','9a000000-0000-0000-0000-000000000001','20000000-0000-0000-0000-000000000009')$$,
  '23505', 'P4: double-submitted payment rejected (idempotency key)');
-- mobile money: pending then failed, then a successful one
INSERT INTO billing.payment (id, org_id, facility_id, patient_id, method, provider, amount, status, idempotency_key, received_by_membership_id)
VALUES ('9c000000-0000-0000-0000-000000000002', :ORG, :FAC, '90000000-0000-0000-0000-000000000001', 'mobile_money', 'mpesa', 15000, 'pending',
        '9c900000-0000-0000-0000-000000000002', '20000000-0000-0000-0000-000000000009');
SELECT test.throws($$INSERT INTO billing.payment_allocation (org_id, payment_id, invoice_id, amount) VALUES
  ('a0000000-0000-0000-0000-000000000001','9c000000-0000-0000-0000-000000000002','9b000000-0000-0000-0000-000000000001',15000)$$,
  'SPB01', 'P4: pending mobile-money payment cannot be allocated');
UPDATE billing.payment SET status = 'failed', failure_reason = 'Subscriber cancelled (TEST)' WHERE id = '9c000000-0000-0000-0000-000000000002';
SELECT test.ok((SELECT NOT EXISTS (SELECT 1 FROM billing.receipt WHERE payment_id = '9c000000-0000-0000-0000-000000000002')), 'P4: failed payment produces no receipt');
SELECT test.eq((SELECT balance FROM billing.invoice WHERE id = '9b000000-0000-0000-0000-000000000001'), 15000.00::numeric(14,2), 'P4: failed payment does not change balance');
INSERT INTO billing.payment (id, org_id, facility_id, patient_id, method, provider, provider_reference, amount, status, idempotency_key, received_by_membership_id)
VALUES ('9c000000-0000-0000-0000-000000000003', :ORG, :FAC, '90000000-0000-0000-0000-000000000001', 'mobile_money', 'mpesa', 'MPX-TEST-001', 20000, 'completed',
        '9c900000-0000-0000-0000-000000000003', '20000000-0000-0000-0000-000000000009');
SELECT test.throws($$INSERT INTO billing.payment_allocation (org_id, payment_id, invoice_id, amount) VALUES
  ('a0000000-0000-0000-0000-000000000001','9c000000-0000-0000-0000-000000000003','9b000000-0000-0000-0000-000000000001',20000)$$,
  'SPB01', 'P4: allocation above invoice balance rejected');
INSERT INTO billing.payment_allocation (org_id, payment_id, invoice_id, amount) VALUES (:ORG, '9c000000-0000-0000-0000-000000000003', '9b000000-0000-0000-0000-000000000001', 15000);
SELECT test.eq((SELECT status || ':' || balance FROM billing.invoice WHERE id = '9b000000-0000-0000-0000-000000000001'), 'paid:0.00', 'P4: invoice paid in full');
SELECT test.throws($$UPDATE billing.invoice SET status = 'void', void_reason = 'test' WHERE id = '9b000000-0000-0000-0000-000000000001'$$, 'SPT01', 'P4: paid invoice cannot be voided');
SELECT test.throws($$UPDATE billing.payment SET amount = 1 WHERE id = '9c000000-0000-0000-0000-000000000001'$$, 'SPI01', 'P4: payment amount immutable');
SELECT test.throws($$UPDATE billing.payment SET status = 'reversed', reversal_reason = 'x' WHERE id = '9c000000-0000-0000-0000-000000000003'$$, '42501', 'P4: cashier cannot reverse payments');
SELECT test.eq((SELECT count(*) FROM clinical.consultation), 0::bigint, 'P4: cashier cannot read consultation notes');
COMMIT;

-- Supervisor: reversal, shift close/reconcile, refunds four-eyes.
BEGIN; :as_billsup
UPDATE billing.payment SET status = 'reversed', reversal_reason = 'Duplicate M-Pesa confirmation (TEST)' WHERE id = '9c000000-0000-0000-0000-000000000003';
SELECT test.eq((SELECT status || ':' || balance FROM billing.invoice WHERE id = '9b000000-0000-0000-0000-000000000001'), 'partially_paid:15000.00', 'P4: payment reversal reopens invoice balance');
COMMIT;
BEGIN; :as_cashier
UPDATE billing.cashier_shift SET status = 'closed', counted_cash = 70000 WHERE id = '9a000000-0000-0000-0000-000000000001';
SELECT test.eq((SELECT expected_cash || '/' || variance FROM billing.cashier_shift WHERE id = '9a000000-0000-0000-0000-000000000001'), '70000.00/0.00', 'P4: shift expected cash = float + cash takings');
SELECT test.throws($$UPDATE billing.cashier_shift SET status = 'reconciled', reconciled_by = '10000000-0000-0000-0000-000000000009' WHERE id = '9a000000-0000-0000-0000-000000000001'$$,
  '42501', 'P4: cashier lacks permission to reconcile shifts');
INSERT INTO billing.refund (id, org_id, payment_id, amount, reason) VALUES ('9d000000-0000-0000-0000-000000000001', :ORG, '9c000000-0000-0000-0000-000000000001', 1000, 'Overcharged consumable (TEST)');
SELECT test.throws($$UPDATE billing.refund SET status = 'approved', approved_by = '10000000-0000-0000-0000-000000000009' WHERE id = '9d000000-0000-0000-0000-000000000001'$$,
  '42501', 'P4: cashier cannot approve refunds');
COMMIT;
BEGIN; :as_billsup
UPDATE billing.cashier_shift SET status = 'reconciled', reconciled_by = '10000000-0000-0000-0000-000000000010' WHERE id = '9a000000-0000-0000-0000-000000000001';
UPDATE billing.refund SET status = 'approved', approved_by = '10000000-0000-0000-0000-000000000010' WHERE id = '9d000000-0000-0000-0000-000000000001';
INSERT INTO billing.refund (id, org_id, payment_id, amount, reason) VALUES ('9d000000-0000-0000-0000-000000000002', :ORG, '9c000000-0000-0000-0000-000000000001', 500, 'Supervisor-raised refund (TEST)');
SELECT test.throws($$UPDATE billing.refund SET status = 'approved', approved_by = '10000000-0000-0000-0000-000000000010' WHERE id = '9d000000-0000-0000-0000-000000000002'$$,
  '23514', 'P4: four-eyes — a supervisor cannot approve a refund they requested');
SELECT test.throws($$INSERT INTO billing.refund (org_id, payment_id, amount, reason) VALUES ('a0000000-0000-0000-0000-000000000001','9c000000-0000-0000-0000-000000000001',19000,'too much refund TEST')$$,
  'SPB01', 'P4: refunds cannot exceed the payment');
INSERT INTO billing.cashier_shift (id, org_id, facility_id, membership_id) VALUES ('9a000000-0000-0000-0000-000000000002', :ORG, :FAC, '20000000-0000-0000-0000-000000000010');
UPDATE billing.cashier_shift SET status = 'closed', counted_cash = 0 WHERE id = '9a000000-0000-0000-0000-000000000002';
SELECT test.throws($$UPDATE billing.cashier_shift SET status = 'reconciled', reconciled_by = '10000000-0000-0000-0000-000000000010' WHERE id = '9a000000-0000-0000-0000-000000000002'$$,
  'SPV01', 'P4: a supervisor cannot reconcile their own shift');
SELECT test.eq((SELECT count(*) FROM reporting.v_billing_reconciliation_exceptions), 0::bigint, 'P4 gate: billing reconciliation report has no exceptions');
COMMIT;
BEGIN;
SELECT test.throws($$DELETE FROM billing.payment_allocation$$, 'SPA01', 'P4: payment allocations append-only even for the owner');
SELECT test.throws($$UPDATE billing.receipt SET receipt_no = 'RCT-X'$$, 'SPI01', 'P4: receipts immutable');
COMMIT;

-- Management report matches underlying records.
BEGIN; :as_manager
SELECT test.eq((SELECT sum(amount) FROM reporting.v_daily_revenue), (SELECT sum(amount) FROM billing.payment WHERE status = 'completed'), 'P4 gate: revenue report equals completed payments');
SELECT test.eq((SELECT sum(amount) FROM reporting.v_daily_revenue), 20000.00::numeric, 'P4 gate: revenue = 20000 after failed+reversed payments excluded');
SELECT test.eq((SELECT sum(visits) FROM reporting.v_daily_visits), 1::numeric, 'P4: visits report counts the test visit');
SELECT test.eq((SELECT count(*) FROM reporting.v_diagnosis_counts), 0::bigint, 'P4: manager without clinical.read sees no diagnosis-level report rows');
SELECT test.eq((SELECT count(*) FROM patient.patient), 0::bigint, 'P4: manager cannot browse patient demographics');
COMMIT;

-- Follow-up and visit close.
BEGIN; :as_doctor
INSERT INTO clinical.follow_up_plan (org_id, encounter_id, patient_id, due_date, reason)
VALUES (:ORG, '92000000-0000-0000-0000-000000000001', '90000000-0000-0000-0000-000000000001', current_date + 3, 'Review after antimalarial');
COMMIT;
BEGIN; :as_recep
UPDATE clinical.encounter SET status = 'completed', closed_at = now() WHERE id = '92000000-0000-0000-0000-000000000001';
UPDATE clinical.appointment SET status = 'completed' WHERE id = '91000000-0000-0000-0000-000000000001';
SELECT test.throws($$UPDATE clinical.encounter SET status = 'registered', closed_at = NULL WHERE id = '92000000-0000-0000-0000-000000000001'$$, 'SPT01', 'P3: closed visit cannot be reopened');
COMMIT;

-- Dispense reversal restores stock.
BEGIN; :as_pharm
SELECT pharmacy.reverse_dispense('99200000-0000-0000-0000-000000000001', 'Wrong patient selected (TEST)');
SELECT test.eq((SELECT quantity_on_hand FROM pharmacy.stock_batch WHERE id = '98000000-0000-0000-0000-000000000001'), 30.00::numeric(12,2), 'P3: dispense reversal restores stock');
SELECT test.eq((SELECT status FROM pharmacy.prescription WHERE id = '99000000-0000-0000-0000-000000000001'), 'active', 'P3: prescription returns to active after reversal');
COMMIT;
