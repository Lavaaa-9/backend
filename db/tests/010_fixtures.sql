-- Synthetic fixtures (clearly labelled test data; no real patient information).
-- Run as superuser. Naming: orgs a…/b…, users 1…, memberships 2…, sessions 3….
\set ON_ERROR_STOP 1
BEGIN;
INSERT INTO org.organization (id, code, legal_name, display_name) VALUES
 ('a0000000-0000-0000-0000-000000000001','SPARTIAL','Spartial Specialized Polyclinic (TEST)','Spartial (TEST)'),
 ('b0000000-0000-0000-0000-000000000001','OTHERCLINIC','Other Clinic (TEST)','Other (TEST)');
INSERT INTO org.facility (id, org_id, code, name) VALUES
 ('a0000000-0000-0000-0000-0000000000f1','a0000000-0000-0000-0000-000000000001','MAIN','Main site'),
 ('b0000000-0000-0000-0000-0000000000f1','b0000000-0000-0000-0000-000000000001','MAIN','Other main');
INSERT INTO org.department (id, org_id, facility_id, code, name, dept_type) VALUES
 ('a0000000-0000-0000-0000-0000000000d1','a0000000-0000-0000-0000-000000000001','a0000000-0000-0000-0000-0000000000f1','REC','Reception','reception'),
 ('a0000000-0000-0000-0000-0000000000d2','a0000000-0000-0000-0000-000000000001','a0000000-0000-0000-0000-0000000000f1','TRI','Triage','triage'),
 ('a0000000-0000-0000-0000-0000000000d3','a0000000-0000-0000-0000-000000000001','a0000000-0000-0000-0000-0000000000f1','OPD','Outpatient','consultation'),
 ('a0000000-0000-0000-0000-0000000000d4','a0000000-0000-0000-0000-000000000001','a0000000-0000-0000-0000-0000000000f1','LAB','Laboratory','laboratory'),
 ('a0000000-0000-0000-0000-0000000000d5','a0000000-0000-0000-0000-000000000001','a0000000-0000-0000-0000-0000000000f1','PHA','Pharmacy','pharmacy'),
 ('a0000000-0000-0000-0000-0000000000d6','a0000000-0000-0000-0000-000000000001','a0000000-0000-0000-0000-0000000000f1','BIL','Billing','billing'),
 ('b0000000-0000-0000-0000-0000000000d1','b0000000-0000-0000-0000-000000000001','b0000000-0000-0000-0000-0000000000f1','REC','Reception','reception');

-- staff users + memberships + roles + MFA-verified sessions
CREATE TEMP TABLE fx(suffix text, uname text, org char(1), role text);
INSERT INTO fx VALUES ('01','admin','a','administrator'),('02','recep','a','receptionist'),('03','nurse','a','nurse'),
 ('04','doctor','a','doctor'),('05','doctor2','a','doctor'),('06','labtech','a','lab_technician'),('07','labsup','a','lab_supervisor'),
 ('08','pharm','a','pharmacist'),('09','cashier','a','cashier'),('10','billsup','a','billing_supervisor'),
 ('11','manager','a','manager'),('12','erclerk','a',NULL),('20','recepb','b','receptionist'),('13','compliance','a','compliance_officer');

INSERT INTO iam.user_account (id, account_type, username, display_name, status, is_test_account)
SELECT ('10000000-0000-0000-0000-0000000000'||suffix)::uuid, 'staff', uname, 'TEST '||uname, 'active', true FROM fx;
INSERT INTO iam.password_credential (user_id, password_hash)
SELECT ('10000000-0000-0000-0000-0000000000'||suffix)::uuid, '$argon2id$v=19$m=65536,t=3,p=4$dGVzdHNhbHQ$dGVzdGhhc2g' FROM fx;
INSERT INTO iam.membership (id, org_id, user_id, status, professional_body, professional_reg_no)
SELECT ('20000000-0000-0000-0000-0000000000'||suffix)::uuid,
       CASE org WHEN 'a' THEN 'a0000000-0000-0000-0000-000000000001'::uuid ELSE 'b0000000-0000-0000-0000-000000000001'::uuid END,
       ('10000000-0000-0000-0000-0000000000'||suffix)::uuid, 'active',
       CASE WHEN role = 'doctor' THEN 'MCT' END, CASE WHEN role = 'doctor' THEN 'MCT-TEST-'||suffix END
FROM fx;
INSERT INTO iam.membership_role (org_id, membership_id, role_id)
SELECT m.org_id, m.id, r.id FROM fx JOIN iam.membership m ON m.id = ('20000000-0000-0000-0000-0000000000'||fx.suffix)::uuid
JOIN iam.role r ON r.org_id IS NULL AND r.code = fx.role WHERE fx.role IS NOT NULL;

-- tenant-specific role: ER clerk can register and use break-glass, but has no clinical.read
INSERT INTO iam.role (id, org_id, code, name) VALUES ('40000000-0000-0000-0000-000000000001','a0000000-0000-0000-0000-000000000001','er_clerk','ER clerk (TEST)');
INSERT INTO iam.role_permission (role_id, permission_code) VALUES
 ('40000000-0000-0000-0000-000000000001','patient.read'),('40000000-0000-0000-0000-000000000001','break_glass.use');
INSERT INTO iam.membership_role (org_id, membership_id, role_id)
VALUES ('a0000000-0000-0000-0000-000000000001','20000000-0000-0000-0000-000000000012','40000000-0000-0000-0000-000000000001');

INSERT INTO iam.session (id, user_id, org_id, membership_id, token_hash, client_type, auth_method, idle_timeout,
                         idle_expires_at, absolute_expires_at, mfa_verified_at)
SELECT ('30000000-0000-0000-0000-0000000000'||suffix)::uuid, ('10000000-0000-0000-0000-0000000000'||suffix)::uuid,
       m.org_id, m.id, sha256(convert_to('token-'||suffix, 'UTF8')), 'web', 'password_mfa', interval '15 minutes',
       now() + interval '15 minutes', now() + interval '12 hours', now()
FROM fx JOIN iam.membership m ON m.id = ('20000000-0000-0000-0000-0000000000'||fx.suffix)::uuid;

-- expired session & no-MFA session for negative tests
INSERT INTO iam.session (id, user_id, org_id, membership_id, token_hash, client_type, auth_method, idle_timeout,
                         created_at, last_seen_at, idle_expires_at, absolute_expires_at, mfa_verified_at)
VALUES ('30000000-0000-0000-0000-0000000000e1','10000000-0000-0000-0000-000000000002','a0000000-0000-0000-0000-000000000001',
        '20000000-0000-0000-0000-000000000002', sha256('\x01'), 'web','password_mfa', interval '15 minutes',
        now() - interval '1 hour', now() - interval '1 hour', now() - interval '45 minutes', now() + interval '11 hours', now()),
       ('30000000-0000-0000-0000-0000000000e2','10000000-0000-0000-0000-000000000002','a0000000-0000-0000-0000-000000000001',
        '20000000-0000-0000-0000-000000000002', sha256('\x02'), 'web','password', interval '15 minutes',
        now(), now(), now() + interval '15 minutes', now() + interval '12 hours', NULL);

-- catalogues (org A), set as superuser with tenant context for counters/defaults
SELECT set_config('app.org_id','a0000000-0000-0000-0000-000000000001', true);
INSERT INTO billing.payer (id, org_id, code, name, payer_type) VALUES
 ('50000000-0000-0000-0000-000000000001','a0000000-0000-0000-0000-000000000001','CASH','Self pay','self_pay');
INSERT INTO diag.test_catalog (id, org_id, code, name, category, specimen_type) VALUES
 ('60000000-0000-0000-0000-000000000001','a0000000-0000-0000-0000-000000000001','MRDT','Malaria rapid diagnostic test','laboratory','blood'),
 ('60000000-0000-0000-0000-000000000002','a0000000-0000-0000-0000-000000000001','HB','Haemoglobin','laboratory','blood');
INSERT INTO diag.test_component (id, org_id, test_id, code, name, value_type, unit, allowed_values) VALUES
 ('61000000-0000-0000-0000-000000000001','a0000000-0000-0000-0000-000000000001','60000000-0000-0000-0000-000000000001','MRDT','Result','coded',NULL,'{Positive,Negative}'),
 ('61000000-0000-0000-0000-000000000002','a0000000-0000-0000-0000-000000000001','60000000-0000-0000-0000-000000000002','HB','Haemoglobin','numeric','g/dL',NULL);
INSERT INTO diag.reference_range (org_id, component_id, sex, low, high) VALUES
 ('a0000000-0000-0000-0000-000000000001','61000000-0000-0000-0000-000000000002','female',12.0,15.5);
INSERT INTO pharmacy.drug (id, org_id, code, generic_name, dosage_form, strength, unit_of_issue) VALUES
 ('70000000-0000-0000-0000-000000000001','a0000000-0000-0000-0000-000000000001','ALU','Artemether/Lumefantrine','tablet','20/120 mg','tablet'),
 ('70000000-0000-0000-0000-000000000002','a0000000-0000-0000-0000-000000000001','PCM','Paracetamol','tablet','500 mg','tablet');
INSERT INTO billing.service_catalog (id, org_id, code, name, category, diag_test_id, drug_id) VALUES
 ('80000000-0000-0000-0000-000000000001','a0000000-0000-0000-0000-000000000001','CONS-GP','GP consultation','consultation',NULL,NULL),
 ('80000000-0000-0000-0000-000000000002','a0000000-0000-0000-0000-000000000001','LAB-MRDT','Malaria RDT','laboratory','60000000-0000-0000-0000-000000000001',NULL),
 ('80000000-0000-0000-0000-000000000003','a0000000-0000-0000-0000-000000000001','RX-ALU','Artemether/Lumefantrine tab','pharmacy',NULL,'70000000-0000-0000-0000-000000000001');
INSERT INTO billing.price (org_id, service_id, unit_price, effective) VALUES
 ('a0000000-0000-0000-0000-000000000001','80000000-0000-0000-0000-000000000001',20000, tstzrange('2026-01-01', NULL)),
 ('a0000000-0000-0000-0000-000000000001','80000000-0000-0000-0000-000000000002', 5000, tstzrange('2026-01-01', NULL)),
 ('a0000000-0000-0000-0000-000000000001','80000000-0000-0000-0000-000000000003',  500, tstzrange('2026-01-01', NULL));
COMMIT;
