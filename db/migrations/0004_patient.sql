-- =============================================================================
-- 0004_patient.sql — patient master index, identifiers, contacts, next of kin,
-- consent, portal linkage, duplicate management, break-glass access.   [PHI]
--
-- Sensitive identifiers (NIDA, NHIF, passport) are encrypted by the API with
-- envelope encryption (KMS data keys) → *_ciphertext. Exact-match search and
-- uniqueness use a keyed blind index: HMAC-SHA256(normalised value, index key)
-- → *_bidx. The database never sees the plaintext or the keys.
-- =============================================================================

CREATE TABLE patient.patient (
  id                     uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id                 uuid NOT NULL REFERENCES org.organization(id),
  mrn                    text NOT NULL CHECK (mrn ~ '^[A-Z]{1,6}-[0-9]{4}-[0-9]{6}$'),
  registered_facility_id uuid NOT NULL,
  first_name             text NOT NULL CHECK (length(btrim(first_name)) BETWEEN 1 AND 100),
  middle_name            text CHECK (length(btrim(middle_name)) BETWEEN 1 AND 100),
  last_name              text NOT NULL CHECK (length(btrim(last_name)) BETWEEN 1 AND 100),
  preferred_name         text,
  sex                    text NOT NULL CHECK (sex IN ('female','male','intersex','unknown')),
  date_of_birth          date NOT NULL CHECK (date_of_birth >= DATE '1900-01-01'),
  dob_is_estimated       boolean NOT NULL DEFAULT false,
  marital_status         text CHECK (marital_status IN ('single','married','divorced','widowed','separated','unknown')),
  occupation             text,
  nationality            char(2) NOT NULL DEFAULT 'TZ' CHECK (nationality ~ '^[A-Z]{2}$'),
  preferred_language     text NOT NULL DEFAULT 'sw' CHECK (preferred_language IN ('sw','en')),
  region                 text, district text, ward text, street_address text,
  blood_group            text CHECK (blood_group IN ('A+','A-','B+','B-','AB+','AB-','O+','O-','unknown')),
  status                 text NOT NULL DEFAULT 'active' CHECK (status IN ('active','inactive','deceased','merged')),
  deceased_at            timestamptz,
  merged_into_patient_id uuid,
  is_test_data           boolean NOT NULL DEFAULT false,   -- roadmap: demo/test data clearly labelled
  created_at  timestamptz NOT NULL DEFAULT now(),
  created_by  uuid DEFAULT sec.current_user_id() REFERENCES iam.user_account(id),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  updated_by  uuid REFERENCES iam.user_account(id),
  row_version integer NOT NULL DEFAULT 1,
  UNIQUE (org_id, id),
  UNIQUE (org_id, mrn),
  FOREIGN KEY (org_id, registered_facility_id) REFERENCES org.facility (org_id, id),
  FOREIGN KEY (org_id, merged_into_patient_id) REFERENCES patient.patient (org_id, id),
  CONSTRAINT ck_patient_deceased CHECK (status <> 'deceased' OR deceased_at IS NOT NULL),
  CONSTRAINT ck_patient_merged CHECK ((status = 'merged') = (merged_into_patient_id IS NOT NULL)),
  CONSTRAINT ck_patient_not_self_merge CHECK (merged_into_patient_id IS DISTINCT FROM id)
);
COMMENT ON COLUMN patient.patient.mrn IS 'Medical record number <prefix>-<yyyy>-<seq>; allocated by patient.next_mrn(); immutable.';

CREATE OR REPLACE FUNCTION patient.name_key(p_first text, p_middle text, p_last text)
RETURNS text LANGUAGE sql IMMUTABLE PARALLEL SAFE AS
$$ SELECT lower(regexp_replace(concat_ws(' ', p_first, p_middle, p_last), '\s+', ' ', 'g')) $$;

CREATE INDEX ix_patient_name_trgm ON patient.patient USING gin (patient.name_key(first_name, middle_name, last_name) gin_trgm_ops);
CREATE INDEX ix_patient_dob ON patient.patient (org_id, date_of_birth);
CREATE INDEX ix_patient_last_name ON patient.patient (org_id, lower(last_name), lower(first_name));

CREATE OR REPLACE FUNCTION patient.validate_patient() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.date_of_birth > org.local_date(NEW.org_id) THEN
    RAISE EXCEPTION 'date_of_birth cannot be in the future' USING ERRCODE = 'SPV01';
  END IF;
  IF TG_OP = 'UPDATE' AND NEW.mrn <> OLD.mrn THEN
    RAISE EXCEPTION 'MRN is immutable' USING ERRCODE = 'SPI01';
  END IF;
  IF TG_OP = 'UPDATE' AND OLD.status = 'merged' AND NOT sec.is_internal() THEN
    RAISE EXCEPTION 'merged patient records are read-only' USING ERRCODE = 'SPI01';
  END IF;
  NEW.first_name := btrim(NEW.first_name); NEW.last_name := btrim(NEW.last_name);
  NEW.middle_name := nullif(btrim(NEW.middle_name), '');
  RETURN NEW;
END $$;
CREATE TRIGGER trg_patient_validate BEFORE INSERT OR UPDATE ON patient.patient
  FOR EACH ROW EXECUTE FUNCTION patient.validate_patient();
SELECT sec.attach_standard_triggers('patient.patient');
SELECT audit.attach_capture('patient.patient');

CREATE OR REPLACE FUNCTION patient.next_mrn(p_org uuid) RETURNS text
LANGUAGE sql SECURITY DEFINER SET search_path = pg_catalog AS $$
  SELECT o.mrn_prefix || '-' || to_char(org.local_date(p_org), 'YYYY') || '-' ||
         lpad(org.next_value(p_org, 'mrn', to_char(org.local_date(p_org), 'YYYY'))::text, 6, '0')
  FROM org.organization o WHERE o.id = p_org;
$$;

-- Contacts --------------------------------------------------------------------
CREATE TABLE patient.contact_point (
  id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id      uuid NOT NULL,
  patient_id  uuid NOT NULL,
  system      text NOT NULL CHECK (system IN ('phone','whatsapp','email')),
  value       text NOT NULL,
  is_primary  boolean NOT NULL DEFAULT false,
  verified_at timestamptz,
  status      text NOT NULL DEFAULT 'active' CHECK (status IN ('active','inactive')),
  created_at  timestamptz NOT NULL DEFAULT now(),
  created_by  uuid DEFAULT sec.current_user_id() REFERENCES iam.user_account(id),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  updated_by  uuid REFERENCES iam.user_account(id),
  row_version integer NOT NULL DEFAULT 1,
  UNIQUE (org_id, id),
  FOREIGN KEY (org_id, patient_id) REFERENCES patient.patient (org_id, id),
  CONSTRAINT ck_contact_format CHECK (
    (system IN ('phone','whatsapp') AND value ~ '^\+[1-9][0-9]{7,14}$') OR
    (system = 'email' AND value ~ '^[^@\s]+@[^@\s]+\.[^@\s]+$'))
);
CREATE UNIQUE INDEX ux_contact_primary ON patient.contact_point (patient_id, system) WHERE is_primary AND status = 'active';
CREATE UNIQUE INDEX ux_contact_dupe    ON patient.contact_point (patient_id, system, lower(value)) WHERE status = 'active';
CREATE INDEX ix_contact_value ON patient.contact_point (org_id, lower(value)) WHERE status = 'active';
SELECT sec.attach_standard_triggers('patient.contact_point');
SELECT audit.attach_capture('patient.contact_point');

-- Identifiers (encrypted + blind index) ---------------------------------------
CREATE TABLE patient.identifier (
  id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id           uuid NOT NULL,
  patient_id       uuid NOT NULL,
  id_type          text NOT NULL CHECK (id_type IN ('nida_nin','nhif_member','passport','driving_licence',
                                                    'voter_id','birth_certificate','zanzibar_id','legacy_powerhms','other')),
  value_ciphertext bytea NOT NULL,
  value_bidx       bytea NOT NULL CHECK (length(value_bidx) = 32),
  value_last4      text CHECK (value_last4 ~ '^[A-Za-z0-9]{1,4}$'),   -- masked display only
  key_ref          text NOT NULL,                                      -- KMS key version
  issuing_country  char(2) DEFAULT 'TZ',
  status           text NOT NULL DEFAULT 'active' CHECK (status IN ('active','inactive','entered_in_error')),
  created_at  timestamptz NOT NULL DEFAULT now(),
  created_by  uuid DEFAULT sec.current_user_id() REFERENCES iam.user_account(id),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  updated_by  uuid REFERENCES iam.user_account(id),
  row_version integer NOT NULL DEFAULT 1,
  UNIQUE (org_id, id),
  FOREIGN KEY (org_id, patient_id) REFERENCES patient.patient (org_id, id)
);
-- Hard duplicate prevention: one active patient per identifier value per org.
CREATE UNIQUE INDEX ux_identifier_value ON patient.identifier (org_id, id_type, value_bidx) WHERE status = 'active';
CREATE INDEX ix_identifier_patient ON patient.identifier (patient_id);
SELECT sec.attach_standard_triggers('patient.identifier');
SELECT audit.attach_capture('patient.identifier', ARRAY['value_ciphertext','value_bidx']);

-- Next of kin -----------------------------------------------------------------
CREATE TABLE patient.next_of_kin (
  id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id               uuid NOT NULL,
  patient_id           uuid NOT NULL,
  full_name            text NOT NULL CHECK (length(btrim(full_name)) BETWEEN 2 AND 150),
  relationship         text NOT NULL CHECK (relationship IN ('spouse','parent','child','sibling','guardian',
                                                              'relative','friend','employer','other')),
  phone_e164           text NOT NULL CHECK (phone_e164 ~ '^\+[1-9][0-9]{7,14}$'),
  alt_phone_e164       text CHECK (alt_phone_e164 ~ '^\+[1-9][0-9]{7,14}$'),
  address              text,
  is_emergency_contact boolean NOT NULL DEFAULT true,
  priority             smallint NOT NULL DEFAULT 1 CHECK (priority BETWEEN 1 AND 5),
  status               text NOT NULL DEFAULT 'active' CHECK (status IN ('active','inactive')),
  created_at  timestamptz NOT NULL DEFAULT now(),
  created_by  uuid DEFAULT sec.current_user_id() REFERENCES iam.user_account(id),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  updated_by  uuid REFERENCES iam.user_account(id),
  row_version integer NOT NULL DEFAULT 1,
  UNIQUE (org_id, id),
  FOREIGN KEY (org_id, patient_id) REFERENCES patient.patient (org_id, id)
);
CREATE UNIQUE INDEX ux_nok_priority ON patient.next_of_kin (patient_id, priority) WHERE status = 'active';
SELECT sec.attach_standard_triggers('patient.next_of_kin');
SELECT audit.attach_capture('patient.next_of_kin');

-- Consent ---------------------------------------------------------------------
CREATE TABLE patient.consent (
  id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id              uuid NOT NULL,
  patient_id          uuid NOT NULL,
  consent_type        text NOT NULL CHECK (consent_type IN ('treatment_general','telemedicine','sms_notifications',
                                       'whatsapp_notifications','portal_access','data_sharing_insurer','research')),
  status              text NOT NULL DEFAULT 'granted' CHECK (status IN ('granted','withdrawn')),
  capture_method      text NOT NULL CHECK (capture_method IN ('written','verbal_witnessed','electronic_portal')),
  captured_by         uuid DEFAULT sec.current_user_id() REFERENCES iam.user_account(id),
  witness_membership_id uuid,
  document_id         uuid,                 -- scanned form (FK added in 0006)
  policy_version      text NOT NULL,        -- version of the consent text shown
  granted_at          timestamptz NOT NULL DEFAULT now(),
  withdrawn_at        timestamptz,
  withdrawal_reason   text,
  created_at  timestamptz NOT NULL DEFAULT now(),
  created_by  uuid DEFAULT sec.current_user_id() REFERENCES iam.user_account(id),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  updated_by  uuid REFERENCES iam.user_account(id),
  row_version integer NOT NULL DEFAULT 1,
  UNIQUE (org_id, id),
  FOREIGN KEY (org_id, patient_id) REFERENCES patient.patient (org_id, id),
  FOREIGN KEY (org_id, witness_membership_id) REFERENCES iam.membership (org_id, id),
  CONSTRAINT ck_consent_withdrawn CHECK ((status = 'withdrawn') = (withdrawn_at IS NOT NULL)),
  CONSTRAINT ck_consent_witness CHECK (capture_method <> 'verbal_witnessed' OR witness_membership_id IS NOT NULL)
);
CREATE UNIQUE INDEX ux_consent_active ON patient.consent (patient_id, consent_type) WHERE status = 'granted';
SELECT sec.attach_standard_triggers('patient.consent');
SELECT audit.attach_capture('patient.consent');

CREATE OR REPLACE FUNCTION patient.has_consent(p_patient uuid, p_type text) RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER SET search_path = pg_catalog AS
$$ SELECT EXISTS (SELECT 1 FROM patient.consent WHERE patient_id = p_patient AND consent_type = p_type AND status = 'granted') $$;

-- Portal / mobile-app linkage -------------------------------------------------
CREATE TABLE patient.portal_account (
  id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id              uuid NOT NULL,
  patient_id          uuid NOT NULL,
  user_id             uuid NOT NULL REFERENCES iam.user_account(id),
  access_type         text NOT NULL DEFAULT 'self' CHECK (access_type IN ('self','guardian')),
  status              text NOT NULL DEFAULT 'pending_verification'
                      CHECK (status IN ('pending_verification','active','suspended','revoked')),
  verification_method text CHECK (verification_method IN ('in_person_id_check','otp_registered_phone','staff_assisted')),
  verified_by         uuid REFERENCES iam.user_account(id),
  verified_at         timestamptz,
  revoked_reason      text,
  created_at  timestamptz NOT NULL DEFAULT now(),
  created_by  uuid DEFAULT sec.current_user_id() REFERENCES iam.user_account(id),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  updated_by  uuid REFERENCES iam.user_account(id),
  row_version integer NOT NULL DEFAULT 1,
  UNIQUE (org_id, id),
  UNIQUE (patient_id, user_id),
  FOREIGN KEY (org_id, patient_id) REFERENCES patient.patient (org_id, id),
  CONSTRAINT ck_portal_verified CHECK (status <> 'active' OR (verified_at IS NOT NULL AND verification_method IS NOT NULL)),
  CONSTRAINT ck_guardian_in_person CHECK (access_type <> 'guardian' OR status <> 'active'
                                          OR verification_method IN ('in_person_id_check','staff_assisted'))
);
-- A login may act for exactly one patient as "self".
CREATE UNIQUE INDEX ux_portal_self ON patient.portal_account (user_id) WHERE access_type = 'self' AND status IN ('pending_verification','active');
SELECT sec.attach_standard_triggers('patient.portal_account');
SELECT audit.attach_capture('patient.portal_account');

ALTER TABLE iam.session ADD CONSTRAINT fk_session_patient FOREIGN KEY (org_id, patient_id) REFERENCES patient.patient (org_id, id);

-- Duplicate management --------------------------------------------------------
CREATE TABLE patient.duplicate_review (
  id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id        uuid NOT NULL,
  patient_id_a  uuid NOT NULL,
  patient_id_b  uuid NOT NULL,
  match_score   numeric(5,4) NOT NULL CHECK (match_score BETWEEN 0 AND 1),
  match_reasons text[] NOT NULL DEFAULT '{}',
  status        text NOT NULL DEFAULT 'open' CHECK (status IN ('open','confirmed_distinct','confirmed_duplicate','merged')),
  resolved_by   uuid REFERENCES iam.user_account(id),
  resolved_at   timestamptz,
  resolution_note text,
  created_at  timestamptz NOT NULL DEFAULT now(),
  created_by  uuid DEFAULT sec.current_user_id() REFERENCES iam.user_account(id),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  updated_by  uuid REFERENCES iam.user_account(id),
  row_version integer NOT NULL DEFAULT 1,
  UNIQUE (org_id, id),
  UNIQUE (patient_id_a, patient_id_b),
  FOREIGN KEY (org_id, patient_id_a) REFERENCES patient.patient (org_id, id),
  FOREIGN KEY (org_id, patient_id_b) REFERENCES patient.patient (org_id, id),
  CONSTRAINT ck_dup_ordered CHECK (patient_id_a < patient_id_b),
  CONSTRAINT ck_dup_resolved CHECK ((status = 'open') = (resolved_at IS NULL))
);
SELECT sec.attach_standard_triggers('patient.duplicate_review');

-- Candidate finder used before registration (soft duplicate check).
CREATE OR REPLACE FUNCTION patient.find_duplicates(
  p_first text, p_last text, p_dob date, p_phone text DEFAULT NULL, p_limit int DEFAULT 10)
RETURNS TABLE (patient_id uuid, mrn text, full_name text, date_of_birth date, score numeric, reasons text[])
LANGUAGE sql STABLE AS $$
  WITH c AS (
    SELECT p.id, p.mrn, concat_ws(' ', p.first_name, p.middle_name, p.last_name) AS full_name, p.date_of_birth,
           similarity(patient.name_key(p.first_name, NULL, p.last_name), patient.name_key(p_first, NULL, p_last)) AS name_sim,
           (p.date_of_birth = p_dob) AS dob_match,
           (abs(p.date_of_birth - p_dob) <= 366) AS dob_near,
           EXISTS (SELECT 1 FROM patient.contact_point cp WHERE cp.patient_id = p.id AND cp.status = 'active'
                   AND cp.system IN ('phone','whatsapp') AND cp.value = p_phone) AS phone_match
    FROM patient.patient p
    WHERE p.status IN ('active','inactive')
      AND (patient.name_key(p.first_name, p.middle_name, p.last_name) % patient.name_key(p_first, NULL, p_last)
           OR p.date_of_birth = p_dob
           OR (p_phone IS NOT NULL AND EXISTS (SELECT 1 FROM patient.contact_point cp
                 WHERE cp.patient_id = p.id AND cp.value = p_phone AND cp.status = 'active'))))
  SELECT id, mrn, full_name, date_of_birth,
         round(least(1, name_sim * 0.6 + CASE WHEN dob_match THEN 0.3 WHEN dob_near THEN 0.1 ELSE 0 END
                     + CASE WHEN phone_match THEN 0.3 ELSE 0 END)::numeric, 4) AS score,
         array_remove(ARRAY[CASE WHEN name_sim > 0.6 THEN 'name' END,
                            CASE WHEN dob_match THEN 'dob' END,
                            CASE WHEN phone_match THEN 'phone' END], NULL)
  FROM c
  WHERE name_sim > 0.45 OR (dob_match AND name_sim > 0.25) OR phone_match
  ORDER BY 5 DESC
  LIMIT p_limit;
$$;

CREATE TABLE patient.merge_event (
  id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id              uuid NOT NULL,
  survivor_patient_id uuid NOT NULL,
  merged_patient_id   uuid NOT NULL,
  duplicate_review_id uuid,
  reason              text NOT NULL CHECK (length(reason) >= 10),
  moved_record_counts jsonb NOT NULL DEFAULT '{}',
  performed_by        uuid NOT NULL DEFAULT sec.current_user_id() REFERENCES iam.user_account(id),
  performed_at        timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY (org_id, survivor_patient_id) REFERENCES patient.patient (org_id, id),
  FOREIGN KEY (org_id, merged_patient_id)   REFERENCES patient.patient (org_id, id),
  FOREIGN KEY (org_id, duplicate_review_id) REFERENCES patient.duplicate_review (org_id, id),
  CONSTRAINT ck_merge_distinct CHECK (survivor_patient_id <> merged_patient_id)
);
CREATE TRIGGER trg_merge_append_only BEFORE UPDATE OR DELETE ON patient.merge_event FOR EACH ROW EXECUTE FUNCTION sec.forbid_mutation();

-- Break-glass (HIPAA emergency access procedure §164.312(a)(2)(ii)) -----------
CREATE TABLE iam.break_glass_access (
  id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id         uuid NOT NULL,
  membership_id  uuid NOT NULL,
  patient_id     uuid NOT NULL,
  reason         text NOT NULL CHECK (length(btrim(reason)) >= 20),
  started_at     timestamptz NOT NULL DEFAULT now(),
  expires_at     timestamptz NOT NULL,
  ended_at       timestamptz,
  reviewed_by    uuid REFERENCES iam.user_account(id),
  reviewed_at    timestamptz,
  review_outcome text CHECK (review_outcome IN ('appropriate','inappropriate','needs_follow_up')),
  review_note    text,
  UNIQUE (org_id, id),
  FOREIGN KEY (org_id, membership_id) REFERENCES iam.membership (org_id, id),
  FOREIGN KEY (org_id, patient_id)    REFERENCES patient.patient (org_id, id),
  CONSTRAINT ck_bg_window CHECK (expires_at > started_at AND expires_at <= started_at + interval '4 hours'),
  CONSTRAINT ck_bg_review CHECK ((reviewed_at IS NULL) = (review_outcome IS NULL))
);
CREATE INDEX ix_break_glass_active ON iam.break_glass_access (membership_id, expires_at) WHERE ended_at IS NULL;
CREATE INDEX ix_break_glass_unreviewed ON iam.break_glass_access (org_id, started_at) WHERE reviewed_at IS NULL;
SELECT audit.attach_capture('iam.break_glass_access');
