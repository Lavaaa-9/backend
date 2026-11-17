-- +goose Up
CREATE TABLE facilities (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 name text NOT NULL CHECK (length(name) BETWEEN 1 AND 160),
 timezone text NOT NULL DEFAULT 'Africa/Dar_es_Salaam',
 active boolean NOT NULL DEFAULT true,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE roles (name text PRIMARY KEY, privileged boolean NOT NULL DEFAULT false);
CREATE TABLE permissions (name text PRIMARY KEY);
CREATE TABLE role_permissions (
 role_name text NOT NULL REFERENCES roles(name),
 permission_name text NOT NULL REFERENCES permissions(name),
 PRIMARY KEY(role_name, permission_name)
);
INSERT INTO roles(name, privileged) VALUES ('administrator',true),('reception',false),('nurse',false),('doctor',false),('laboratory',false),('pharmacy',false),('cashier',false),('management',false),('patient',false),('privacy-reviewer',true);
INSERT INTO permissions VALUES ('staff.manage'),('staff.read'),('facilities.manage'),('facilities.read'),('patients.read'),('patients.write'),('audit.read');
INSERT INTO role_permissions VALUES
 ('administrator','staff.manage'),('administrator','staff.read'),('administrator','facilities.manage'),('administrator','facilities.read'),('administrator','audit.read'),
 ('reception','patients.read'),('reception','patients.write'),('reception','facilities.read'),
 ('nurse','facilities.read'),('doctor','facilities.read'),('laboratory','facilities.read'),('pharmacy','facilities.read'),('cashier','facilities.read'),('management','facilities.read'),('privacy-reviewer','audit.read');
CREATE TABLE staff (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 email text NOT NULL UNIQUE CHECK(email=lower(email) AND length(email)<=254),
 display_name text NOT NULL CHECK(length(display_name) BETWEEN 1 AND 160),
 password_hash text NOT NULL,
 role_name text NOT NULL REFERENCES roles(name) CHECK(role_name <> 'patient'),
 active boolean NOT NULL DEFAULT true,
 version integer NOT NULL DEFAULT 1 CHECK(version>0),
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE staff_facilities (
 staff_id uuid NOT NULL REFERENCES staff(id),
 facility_id uuid NOT NULL REFERENCES facilities(id),
 PRIMARY KEY(staff_id, facility_id)
);
CREATE TABLE sessions (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 staff_id uuid NOT NULL REFERENCES staff(id),
 client_type text NOT NULL CHECK(client_type IN ('web','mobile')),
 access_hash text NOT NULL UNIQUE,
 access_expires_at timestamptz NOT NULL,
 expires_at timestamptz NOT NULL,
 revoked_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX sessions_staff ON sessions(staff_id);
CREATE TABLE refresh_tokens (
 token_hash text PRIMARY KEY,
 session_id uuid NOT NULL REFERENCES sessions(id),
 used_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX refresh_session ON refresh_tokens(session_id);
CREATE TABLE auth_limits (
 bucket text PRIMARY KEY,
 window_start timestamptz NOT NULL,
 attempts integer NOT NULL CHECK(attempts>0)
);
CREATE SEQUENCE patient_mrn_seq;
CREATE TABLE patients (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 mrn text NOT NULL UNIQUE DEFAULT ('SP-' || lpad(nextval('patient_mrn_seq')::text,10,'0')),
 facility_id uuid NOT NULL REFERENCES facilities(id),
 given_name text NOT NULL CHECK(length(given_name) BETWEEN 1 AND 100),
 family_name text NOT NULL CHECK(length(family_name) BETWEEN 1 AND 100),
 birth_precision text NOT NULL CHECK(birth_precision IN ('exact','year','unknown')),
 birth_date date,
 birth_year integer CHECK(birth_year BETWEEN 1850 AND 9999),
 phone text NOT NULL DEFAULT '' CHECK(length(phone)<=40),
 address text NOT NULL DEFAULT '' CHECK(length(address)<=500),
 version integer NOT NULL DEFAULT 1 CHECK(version>0),
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 CHECK ((birth_precision='exact' AND birth_date IS NOT NULL AND birth_year IS NULL) OR (birth_precision='year' AND birth_year IS NOT NULL AND birth_date IS NULL) OR (birth_precision='unknown' AND birth_date IS NULL AND birth_year IS NULL))
);
CREATE INDEX patients_facility_id ON patients(facility_id,id);
CREATE TABLE audit_events (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 actor_id uuid REFERENCES staff(id),
 action text NOT NULL,
 resource_id uuid,
 patient_id uuid REFERENCES patients(id),
 request_id uuid NOT NULL,
 outcome text NOT NULL CHECK(outcome IN ('success','denied','failure')),
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX audit_patient_time ON audit_events(patient_id,created_at DESC);
-- Application-level audit mutations fail even if privileges were accidentally granted.
-- +goose StatementBegin
CREATE FUNCTION reject_audit_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'audit events are append-only'; END $$;
-- +goose StatementEnd
CREATE TRIGGER audit_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON audit_events FOR EACH STATEMENT EXECUTE FUNCTION reject_audit_mutation();
CREATE TABLE idempotency_records (
 actor_id uuid NOT NULL REFERENCES staff(id),
 operation text NOT NULL,
 key text NOT NULL CHECK(length(key) BETWEEN 8 AND 128),
 request_hash text NOT NULL,
 response jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(actor_id,operation,key)
);
CREATE TABLE outbox (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 event_type text NOT NULL,
 aggregate_id uuid NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 available_at timestamptz NOT NULL DEFAULT now(),
 lease_until timestamptz,
 lease_token uuid,
 attempts integer NOT NULL DEFAULT 0,
 status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','processing','completed','failed')),
 last_error text,
 UNIQUE(event_type,aggregate_id)
);
CREATE INDEX outbox_pending ON outbox(available_at) WHERE status IN ('pending','processing');
-- +goose Down
DROP TABLE outbox, idempotency_records;
DROP TABLE audit_events;
DROP FUNCTION reject_audit_mutation();
DROP TABLE patients;
DROP SEQUENCE patient_mrn_seq;
DROP TABLE auth_limits,refresh_tokens,sessions,staff_facilities,staff,role_permissions,permissions,roles,facilities;
