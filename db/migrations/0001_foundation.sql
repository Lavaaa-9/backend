-- =============================================================================
-- 0001_foundation.sql — extensions, schemas, security-context functions,
-- generic integrity triggers, reference tables.
-- Executed as spartial_owner (extensions below are "trusted" in PG13+).
-- =============================================================================

CREATE EXTENSION IF NOT EXISTS pgcrypto;     -- gen_random_bytes, hmac (blind-index verification in tests)
CREATE EXTENSION IF NOT EXISTS citext;       -- case-insensitive email / username
CREATE EXTENSION IF NOT EXISTS pg_trgm;      -- fuzzy patient-name search & duplicate detection
CREATE EXTENSION IF NOT EXISTS btree_gist;   -- exclusion constraints (double-booking, price overlap)

-- Domain-separated schemas. PHI lives in patient/clinical/diag/pharmacy/document/telehealth.
CREATE SCHEMA IF NOT EXISTS sec;         -- request-context & integrity functions
CREATE SCHEMA IF NOT EXISTS ref;         -- global reference data (ICD-10, permissions, state machines)
CREATE SCHEMA IF NOT EXISTS iam;         -- identities, credentials, sessions, roles
CREATE SCHEMA IF NOT EXISTS org;         -- tenant, facilities, departments, counters
CREATE SCHEMA IF NOT EXISTS audit;       -- tamper-evident audit trail
CREATE SCHEMA IF NOT EXISTS patient;     -- demographics, identifiers, consent       [PHI]
CREATE SCHEMA IF NOT EXISTS clinical;    -- appointments, encounters, notes          [PHI]
CREATE SCHEMA IF NOT EXISTS diag;        -- laboratory & diagnostics                 [PHI]
CREATE SCHEMA IF NOT EXISTS pharmacy;    -- prescriptions, dispensing, stock         [PHI]
CREATE SCHEMA IF NOT EXISTS billing;     -- invoices, payments, receipts             [PHI-linked financial]
CREATE SCHEMA IF NOT EXISTS telehealth;  -- telemedicine sessions                    [PHI]
CREATE SCHEMA IF NOT EXISTS document;    -- document metadata (bytes in object storage) [PHI]
CREATE SCHEMA IF NOT EXISTS comms;       -- notifications, outbox, devices
CREATE SCHEMA IF NOT EXISTS integration; -- inbound webhooks, provider configuration
CREATE SCHEMA IF NOT EXISTS gov;         -- retention, legal hold, privacy requests
CREATE SCHEMA IF NOT EXISTS reporting;   -- security_invoker reporting views

-- public holds only extension objects (citext, pg_trgm, btree_gist); nobody but
-- the owner may create objects there.
REVOKE ALL ON SCHEMA public FROM PUBLIC;
GRANT USAGE ON SCHEMA public TO spartial_app, spartial_worker, spartial_auditor;
ALTER DEFAULT PRIVILEGES REVOKE EXECUTE ON FUNCTIONS FROM PUBLIC;

-- -----------------------------------------------------------------------------
-- Request context. Values are set transaction-locally by sec.begin_request()
-- (SET LOCAL semantics → safe with PgBouncer transaction pooling).
-- Every accessor returns NULL when unset, so RLS predicates FAIL CLOSED.
-- -----------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION sec.setting_uuid(p_name text) RETURNS uuid
LANGUAGE sql STABLE PARALLEL SAFE AS
$$ SELECT nullif(current_setting(p_name, true), '')::uuid $$;

CREATE OR REPLACE FUNCTION sec.current_org_id()        RETURNS uuid LANGUAGE sql STABLE AS $$ SELECT sec.setting_uuid('app.org_id') $$;
CREATE OR REPLACE FUNCTION sec.current_user_id()       RETURNS uuid LANGUAGE sql STABLE AS $$ SELECT sec.setting_uuid('app.user_id') $$;
CREATE OR REPLACE FUNCTION sec.current_membership_id() RETURNS uuid LANGUAGE sql STABLE AS $$ SELECT sec.setting_uuid('app.membership_id') $$;
CREATE OR REPLACE FUNCTION sec.current_patient_id()    RETURNS uuid LANGUAGE sql STABLE AS $$ SELECT sec.setting_uuid('app.patient_id') $$;
CREATE OR REPLACE FUNCTION sec.current_session_id()    RETURNS uuid LANGUAGE sql STABLE AS $$ SELECT sec.setting_uuid('app.session_id') $$;
CREATE OR REPLACE FUNCTION sec.current_request_id()    RETURNS uuid LANGUAGE sql STABLE AS $$ SELECT sec.setting_uuid('app.request_id') $$;

CREATE OR REPLACE FUNCTION sec.actor_type() RETURNS text
LANGUAGE sql STABLE AS $$ SELECT nullif(current_setting('app.actor_type', true), '') $$;

CREATE OR REPLACE FUNCTION sec.is_staff() RETURNS boolean
LANGUAGE sql STABLE AS $$ SELECT coalesce(sec.actor_type() IN ('staff','service'), false) $$;

CREATE OR REPLACE FUNCTION sec.has_perm(p_perm text) RETURNS boolean
LANGUAGE sql STABLE AS
$$ SELECT coalesce(p_perm = ANY (string_to_array(nullif(current_setting('app.perms', true), ''), ',')), false) $$;

CREATE OR REPLACE FUNCTION sec.has_any_perm(p_perms text[]) RETURNS boolean
LANGUAGE sql STABLE AS
$$ SELECT coalesce(p_perms && string_to_array(nullif(current_setting('app.perms', true), ''), ','), false) $$;

-- Patients the current staff member holds an active break-glass grant for.
CREATE OR REPLACE FUNCTION sec.break_glass_patient_ids() RETURNS uuid[]
LANGUAGE sql STABLE AS
$$ SELECT coalesce(string_to_array(nullif(current_setting('app.break_glass', true), ''), ',')::uuid[], '{}'::uuid[]) $$;

-- True only inside SECURITY DEFINER functions owned by spartial_owner (internal
-- derived-field maintenance). Cannot be spoofed by the app role.
CREATE OR REPLACE FUNCTION sec.is_internal() RETURNS boolean
LANGUAGE sql STABLE AS $$ SELECT current_user = 'spartial_owner' $$;

-- -----------------------------------------------------------------------------
-- Error codes (custom SQLSTATE class "SP"); the Go layer maps them to HTTP.
--   SPT01 invalid state transition        → 409
--   SPI01 record immutable                → 409
--   SPA01 append-only violation           → 403
--   SPV01 business validation failed      → 422
--   SPS01 insufficient stock              → 409
--   SPB01 billing rule violated           → 409
--   SPC01 consent missing                 → 422
--   SPX01 cross-entity mismatch           → 422
--   28000 invalid / expired session       → 401
-- -----------------------------------------------------------------------------

-- Standard bookkeeping trigger: updated_* and optimistic-lock row_version.
CREATE OR REPLACE FUNCTION sec.touch_row() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  NEW.created_at  := OLD.created_at;
  NEW.created_by  := OLD.created_by;
  NEW.updated_at  := now();
  NEW.updated_by  := coalesce(sec.current_user_id(), NEW.updated_by);
  NEW.row_version := OLD.row_version + 1;
  RETURN NEW;
END $$;

-- Append-only enforcement (UPDATE/DELETE/TRUNCATE rejected for everyone).
CREATE OR REPLACE FUNCTION sec.forbid_mutation() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION '% on %.% is not permitted (append-only)', TG_OP, TG_TABLE_SCHEMA, TG_TABLE_NAME
    USING ERRCODE = 'SPA01';
END $$;

-- Finalised clinical/financial records: once OLD.status is in TG_ARGV[0] (a
-- comma list of locked statuses) only the lifecycle columns may change.
CREATE OR REPLACE FUNCTION sec.protect_finalised() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
  v_locked text[] := string_to_array(TG_ARGV[0], ',');
  v_allowed text[] := ARRAY['status','updated_at','updated_by','row_version',
                            'error_reason','superseded_at'];
BEGIN
  IF OLD.status = ANY (v_locked) AND NOT sec.is_internal() THEN
    IF (to_jsonb(NEW) - v_allowed) IS DISTINCT FROM (to_jsonb(OLD) - v_allowed) THEN
      RAISE EXCEPTION '%.% % is % and cannot be edited; create an amendment', TG_TABLE_SCHEMA, TG_TABLE_NAME, OLD.id, OLD.status
        USING ERRCODE = 'SPI01';
    END IF;
  END IF;
  RETURN NEW;
END $$;

-- -----------------------------------------------------------------------------
-- Reference: state machines (data-driven, enforced by trigger)
-- -----------------------------------------------------------------------------
CREATE TABLE ref.state_transition (
  entity      text NOT NULL,                 -- 'schema.table'
  from_status text NOT NULL,                 -- '_init' = allowed initial status
  to_status   text NOT NULL,
  required_perm text,                         -- permission needed to perform this transition (NULL = table write perm suffices)
  description text,
  PRIMARY KEY (entity, from_status, to_status)
);
COMMENT ON TABLE ref.state_transition IS 'Allowed lifecycle transitions per entity; from_status=_init lists valid initial states.';

CREATE OR REPLACE FUNCTION sec.enforce_transition() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE v_entity text := TG_TABLE_SCHEMA || '.' || TG_TABLE_NAME; v_from text; v_perm text;
BEGIN
  IF TG_OP = 'UPDATE' AND NEW.status IS NOT DISTINCT FROM OLD.status THEN RETURN NEW; END IF;
  v_from := CASE WHEN TG_OP = 'INSERT' THEN '_init' ELSE OLD.status END;
  SELECT required_perm INTO v_perm FROM ref.state_transition
   WHERE entity = v_entity AND from_status = v_from AND to_status = NEW.status;
  IF NOT FOUND THEN
    IF TG_OP = 'INSERT' THEN
      RAISE EXCEPTION '% cannot be created in status %', v_entity, NEW.status USING ERRCODE = 'SPT01';
    END IF;
    RAISE EXCEPTION 'invalid % transition % -> %', v_entity, OLD.status, NEW.status USING ERRCODE = 'SPT01';
  END IF;
  IF v_perm IS NOT NULL AND NOT sec.has_perm(v_perm) AND NOT sec.is_internal() AND sec.actor_type() IS NOT NULL THEN
    RAISE EXCEPTION '% -> % on % requires permission %', v_from, NEW.status, v_entity, v_perm USING ERRCODE = '42501';
  END IF;
  RETURN NEW;
END $$;

-- Helper used by later migrations to attach standard triggers.
CREATE OR REPLACE FUNCTION sec.attach_standard_triggers(p_table regclass, p_has_status boolean DEFAULT true)
RETURNS void LANGUAGE plpgsql AS $$
BEGIN
  EXECUTE format('CREATE TRIGGER trg_touch BEFORE UPDATE ON %s FOR EACH ROW EXECUTE FUNCTION sec.touch_row()', p_table);
  IF p_has_status THEN
    -- trg_0_state sorts first: lifecycle/permission checks run before table-specific rules.
    EXECUTE format('CREATE TRIGGER trg_0_state BEFORE INSERT OR UPDATE OF status ON %s FOR EACH ROW EXECUTE FUNCTION sec.enforce_transition()', p_table);
  END IF;
END $$;

-- -----------------------------------------------------------------------------
-- Reference: ICD-10 (WHO). Full code set is loaded by a data migration; a
-- small demo subset is seeded in 0014.
-- -----------------------------------------------------------------------------
CREATE TABLE ref.icd10_code (
  code        text PRIMARY KEY CHECK (code ~ '^[A-Z][0-9]{2}(\.[0-9A-Z]{1,4})?$'),
  title       text NOT NULL,
  chapter     text,
  is_billable boolean NOT NULL DEFAULT true,
  status      text NOT NULL DEFAULT 'active' CHECK (status IN ('active','retired'))
);
CREATE INDEX ix_icd10_title_trgm ON ref.icd10_code USING gin (lower(title) gin_trgm_ops);

-- Permission catalogue (global). Codes are referenced by RLS policies.
CREATE TABLE ref.permission (
  code          text PRIMARY KEY CHECK (code ~ '^[a-z_]+(\.[a-z_]+){1,3}$'),
  resource      text NOT NULL,
  action        text NOT NULL,
  description   text NOT NULL,
  is_phi        boolean NOT NULL DEFAULT false,   -- grants access to PHI
  is_privileged boolean NOT NULL DEFAULT false    -- use is always audited & reviewed quarterly
);
