#!/usr/bin/env bash
# Concurrency: two pharmacists dispense the last stock at the same time; two
# cashiers allocate to the same invoice at the same time. Exactly one must win.
set -euo pipefail
DB="$1"
CTX_PHARM="SET LOCAL ROLE spartial_app; SELECT 1 FROM sec.begin_request('30000000-0000-0000-0000-000000000008');"
CTX_DOC="SET LOCAL ROLE spartial_app; SELECT 1 FROM sec.begin_request('30000000-0000-0000-0000-000000000004');"
ORG="a0000000-0000-0000-0000-000000000001"; FAC="a0000000-0000-0000-0000-0000000000f1"
q() { psql -X -q -t -A -v ON_ERROR_STOP=1 -d "$DB" "$@"; }

# Setup: batch with 10 units; two signed prescriptions for 8 each (patient 2 visit).
q <<SQL >/dev/null
BEGIN; $CTX_PHARM
INSERT INTO pharmacy.stock_batch (id, org_id, facility_id, drug_id, batch_no, expiry_date)
VALUES ('98000000-0000-0000-0000-0000000000c1','$ORG','$FAC','70000000-0000-0000-0000-000000000001','ALU-CONC',current_date+200);
INSERT INTO pharmacy.stock_movement (org_id, batch_id, movement_type, quantity) VALUES ('$ORG','98000000-0000-0000-0000-0000000000c1','receipt',10);
COMMIT;
BEGIN; SET LOCAL ROLE spartial_app; SELECT 1 FROM sec.begin_request('30000000-0000-0000-0000-000000000002');
INSERT INTO clinical.encounter (id, org_id, facility_id, patient_id, encounter_no, encounter_type)
VALUES ('92000000-0000-0000-0000-0000000000c1','$ORG','$FAC','90000000-0000-0000-0000-000000000002',clinical.next_encounter_no('$ORG'),'outpatient');
COMMIT;
BEGIN; $CTX_DOC
INSERT INTO pharmacy.prescription (id, org_id, encounter_id, patient_id, prescription_no, prescriber_membership_id) VALUES
 ('99000000-0000-0000-0000-0000000000c1','$ORG','92000000-0000-0000-0000-0000000000c1','90000000-0000-0000-0000-000000000002',org.next_number('prescription'),'20000000-0000-0000-0000-000000000004'),
 ('99000000-0000-0000-0000-0000000000c2','$ORG','92000000-0000-0000-0000-0000000000c1','90000000-0000-0000-0000-000000000002',org.next_number('prescription'),'20000000-0000-0000-0000-000000000004');
INSERT INTO pharmacy.prescription_item (id, org_id, prescription_id, patient_id, drug_id, dose_amount, dose_unit, route, frequency_code, duration_days, quantity_prescribed) VALUES
 ('99100000-0000-0000-0000-0000000000c1','$ORG','99000000-0000-0000-0000-0000000000c1','90000000-0000-0000-0000-000000000002','70000000-0000-0000-0000-000000000001',4,'tablet','oral','BD',1,8),
 ('99100000-0000-0000-0000-0000000000c2','$ORG','99000000-0000-0000-0000-0000000000c2','90000000-0000-0000-0000-000000000002','70000000-0000-0000-0000-000000000001',4,'tablet','oral','BD',1,8);
UPDATE pharmacy.prescription SET status = 'active' WHERE id IN ('99000000-0000-0000-0000-0000000000c1','99000000-0000-0000-0000-0000000000c2');
COMMIT;
SQL

dispense() { # $1 suffix, $2 delay-before-commit
q <<SQL
BEGIN; $CTX_PHARM
INSERT INTO pharmacy.dispense (id, org_id, facility_id, prescription_id, patient_id, dispensed_by_membership_id, idempotency_key)
VALUES ('99200000-0000-0000-0000-0000000000$1','$ORG','$FAC','99000000-0000-0000-0000-0000000000$1','90000000-0000-0000-0000-000000000002','20000000-0000-0000-0000-000000000008',gen_random_uuid());
INSERT INTO pharmacy.dispense_item (org_id, dispense_id, prescription_item_id, batch_id, quantity)
VALUES ('$ORG','99200000-0000-0000-0000-0000000000$1','99100000-0000-0000-0000-0000000000$1','98000000-0000-0000-0000-0000000000c1',8);
SELECT pg_sleep($2);
COMMIT;
SQL
}
( if dispense c1 2 >/tmp/conc1.out 2>&1; then echo 0; else echo 1; fi >/tmp/conc1.rc ) &
sleep 0.5
( if dispense c2 0 >/tmp/conc2.out 2>&1; then echo 0; else echo 1; fi >/tmp/conc2.rc ) &
wait
rc1=$(cat /tmp/conc1.rc); rc2=$(cat /tmp/conc2.rc)
qoh=$(q -c "SELECT quantity_on_hand FROM pharmacy.stock_batch WHERE id='98000000-0000-0000-0000-0000000000c1'")
blocked_err=$(grep -o 'insufficient stock' /tmp/conc2.out || true)
pass=$([[ "$rc1" == 0 && "$rc2" != 0 && "$qoh" == "2.00" && -n "$blocked_err" ]] && echo true || echo false)
q -c "SET test.suite='concurrency'; SELECT test.ok($pass, 'CONCURRENCY: simultaneous dispense of last stock — first commits, second fails with insufficient stock, balance 2')" >/dev/null
echo "dispense: rc1=$rc1 rc2=$rc2 qoh=$qoh err='$blocked_err'"

# Counter gaplessness under parallel issuance: 20 parallel MRN allocations.
for i in $(seq 1 20); do
  ( q -c "BEGIN; SET LOCAL ROLE spartial_app; SELECT 1 FROM sec.begin_request('30000000-0000-0000-0000-000000000002');
          INSERT INTO patient.patient (org_id, mrn, registered_facility_id, first_name, last_name, sex, date_of_birth, is_test_data)
          VALUES ('$ORG', patient.next_mrn('$ORG'), '$FAC', 'Conc$i', 'Test', 'unknown', '2000-01-01', true); COMMIT;" >/dev/null ) &
done
wait
gaps=$(q -c "WITH n AS (SELECT right(mrn,6)::int v FROM patient.patient WHERE org_id='$ORG') SELECT (max(v)-min(v)+1) - count(*) FROM n")
dups=$(q -c "SELECT count(*) - count(DISTINCT mrn) FROM patient.patient")
q -c "SET test.suite='concurrency'; SELECT test.ok($gaps = 0 AND $dups = 0, 'CONCURRENCY: 20 parallel registrations get unique, gapless MRNs')" >/dev/null
echo "mrn: gaps=$gaps dups=$dups"
