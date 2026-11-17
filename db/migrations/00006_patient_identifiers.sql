-- +goose Up
CREATE TABLE patient_identifiers (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 patient_id uuid NOT NULL,
 facility_id uuid NOT NULL,
 kind text NOT NULL CHECK(kind IN ('nida','insurance')),
 issuer text NOT NULL CHECK(length(issuer) BETWEEN 1 AND 100),
 value_ciphertext bytea NOT NULL CHECK(octet_length(value_ciphertext) BETWEEN 30 AND 1024),
 blind_index text NOT NULL CHECK(length(blind_index)=64),
 recorded_by uuid NOT NULL REFERENCES staff(id),
 created_at timestamptz NOT NULL DEFAULT now(),
 FOREIGN KEY(patient_id,facility_id) REFERENCES patients(id,facility_id),
 UNIQUE(patient_id,kind,issuer,blind_index)
);
CREATE INDEX patient_identifiers_lookup ON patient_identifiers(kind,issuer,blind_index,patient_id);
ALTER TABLE patient_identifiers ENABLE ROW LEVEL SECURITY;
ALTER TABLE patient_identifiers FORCE ROW LEVEL SECURITY;
CREATE POLICY identifier_read ON patient_identifiers FOR SELECT USING (can_access_patient(NULLIF(current_setting('app.actor_id',true),'')::uuid,patient_id,facility_id,false));
CREATE POLICY identifier_write ON patient_identifiers FOR INSERT WITH CHECK (can_access_patient(NULLIF(current_setting('app.actor_id',true),'')::uuid,patient_id,facility_id,true));
-- Maintenance updates retain scope policy. Runtime roles receive no UPDATE privilege.
CREATE POLICY identifier_maintenance ON patient_identifiers FOR UPDATE USING (can_access_patient(NULLIF(current_setting('app.actor_id',true),'')::uuid,patient_id,facility_id,true));
-- +goose Down
DROP TABLE patient_identifiers;
