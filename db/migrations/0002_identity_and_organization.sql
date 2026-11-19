-- =============================================================================
-- 0002_identity_and_organization.sql
-- Global identities, tenant/facility structure, credentials, sessions, RBAC.
-- Conventions: uuid PKs; timestamptz (UTC); every tenant table carries org_id
-- and UNIQUE (org_id, id) so children can use tenant-aware composite FKs.
-- =============================================================================

-- ---------------------------------------------------------------- identities
CREATE TABLE iam.user_account (
  id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  account_type         text NOT NULL CHECK (account_type IN ('staff','patient','service')),
  username             citext CHECK (username ~ '^[a-z0-9._-]{3,40}$'),
  email                citext CHECK (email ~ '^[^@\s]+@[^@\s]+\.[^@\s]+$'),
  phone_e164           text   CHECK (phone_e164 ~ '^\+[1-9][0-9]{7,14}$'),
  display_name         text NOT NULL CHECK (length(btrim(display_name)) BETWEEN 1 AND 150),
  status               text NOT NULL DEFAULT 'pending_activation'
                       CHECK (status IN ('pending_activation','active','suspended','deactivated')),
  failed_login_count   integer NOT NULL DEFAULT 0 CHECK (failed_login_count >= 0),
  locked_until         timestamptz,
  last_login_at        timestamptz,
  must_change_password boolean NOT NULL DEFAULT false,
  mfa_required         boolean NOT NULL DEFAULT true,
  is_test_account      boolean NOT NULL DEFAULT false,
  created_at           timestamptz NOT NULL DEFAULT now(),
  created_by           uuid DEFAULT sec.current_user_id(),
  updated_at           timestamptz NOT NULL DEFAULT now(),
  updated_by           uuid,
  row_version          integer NOT NULL DEFAULT 1,
  CONSTRAINT ck_user_has_login_identifier CHECK (num_nonnulls(username, email, phone_e164) >= 1)
);
COMMENT ON TABLE iam.user_account IS 'Global login identity (staff, patient portal, service). Never hard-deleted: deactivated.';
CREATE UNIQUE INDEX ux_user_username ON iam.user_account (username) WHERE username IS NOT NULL;
CREATE UNIQUE INDEX ux_user_email    ON iam.user_account (account_type, email) WHERE email IS NOT NULL;
CREATE UNIQUE INDEX ux_user_phone    ON iam.user_account (account_type, phone_e164) WHERE phone_e164 IS NOT NULL;
ALTER TABLE iam.user_account ADD CONSTRAINT fk_user_created_by FOREIGN KEY (created_by) REFERENCES iam.user_account(id);
ALTER TABLE iam.user_account ADD CONSTRAINT fk_user_updated_by FOREIGN KEY (updated_by) REFERENCES iam.user_account(id);
CREATE TRIGGER trg_touch BEFORE UPDATE ON iam.user_account FOR EACH ROW EXECUTE FUNCTION sec.touch_row();

-- Argon2id hashes only (PHC string). Plaintext never reaches the database.
CREATE TABLE iam.password_credential (
  user_id        uuid PRIMARY KEY REFERENCES iam.user_account(id) ON DELETE RESTRICT,
  password_hash  text NOT NULL CHECK (password_hash LIKE '$argon2id$%'),
  changed_at     timestamptz NOT NULL DEFAULT now(),
  expires_at     timestamptz
);
CREATE TABLE iam.password_history (
  id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id       uuid NOT NULL REFERENCES iam.user_account(id),
  password_hash text NOT NULL CHECK (password_hash LIKE '$argon2id$%'),
  created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX ix_pwhist_user ON iam.password_history (user_id, created_at DESC);

CREATE TABLE iam.mfa_factor (
  id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id           uuid NOT NULL REFERENCES iam.user_account(id),
  factor_type       text NOT NULL CHECK (factor_type IN ('totp','sms_otp','webauthn')),
  label             text NOT NULL DEFAULT 'default',
  secret_ciphertext bytea,          -- TOTP seed, envelope-encrypted by the API (KMS)
  key_ref           text,           -- KMS key id/version used for secret_ciphertext
  webauthn_credential_id bytea,
  webauthn_public_key    bytea,
  status            text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','active','revoked')),
  verified_at       timestamptz,
  last_used_at      timestamptz,
  last_used_step    bigint,         -- last accepted TOTP time-step: blocks replay
  created_at        timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT ck_mfa_secret CHECK (
    (factor_type = 'totp'     AND secret_ciphertext IS NOT NULL AND key_ref IS NOT NULL) OR
    (factor_type = 'webauthn' AND webauthn_credential_id IS NOT NULL AND webauthn_public_key IS NOT NULL) OR
    (factor_type = 'sms_otp')),
  CONSTRAINT ck_mfa_active_verified CHECK (status <> 'active' OR verified_at IS NOT NULL)
);
CREATE UNIQUE INDEX ux_mfa_user_label ON iam.mfa_factor (user_id, factor_type, label) WHERE status <> 'revoked';

CREATE TABLE iam.mfa_recovery_code (
  id        uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id   uuid NOT NULL REFERENCES iam.user_account(id),
  code_hash bytea NOT NULL UNIQUE CHECK (length(code_hash) = 32),
  used_at   timestamptz,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX ix_recovery_user ON iam.mfa_recovery_code (user_id) WHERE used_at IS NULL;

-- One-time codes (SMS sign-in, phone verification, step-up). Code stored hashed.
CREATE TABLE iam.otp_challenge (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id      uuid REFERENCES iam.user_account(id),
  phone_e164   text NOT NULL CHECK (phone_e164 ~ '^\+[1-9][0-9]{7,14}$'),
  purpose      text NOT NULL CHECK (purpose IN ('portal_sign_in','phone_verification','mfa','password_reset')),
  code_hash    bytea NOT NULL CHECK (length(code_hash) = 32),
  attempts     smallint NOT NULL DEFAULT 0 CHECK (attempts BETWEEN 0 AND 5),
  expires_at   timestamptz NOT NULL,
  consumed_at  timestamptz,
  requested_ip inet,
  created_at   timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT ck_otp_ttl CHECK (expires_at <= created_at + interval '10 minutes')
);
CREATE INDEX ix_otp_phone_recent ON iam.otp_challenge (phone_e164, created_at DESC);

CREATE TABLE iam.password_reset (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id      uuid NOT NULL REFERENCES iam.user_account(id),
  token_hash   bytea NOT NULL UNIQUE CHECK (length(token_hash) = 32),
  requested_at timestamptz NOT NULL DEFAULT now(),
  expires_at   timestamptz NOT NULL,
  consumed_at  timestamptz,
  requested_ip inet,
  CONSTRAINT ck_reset_ttl CHECK (expires_at <= requested_at + interval '1 hour')
);
CREATE INDEX ix_reset_user ON iam.password_reset (user_id, requested_at DESC);

-- ---------------------------------------------------------------- tenant & facilities
CREATE TABLE org.organization (
  id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  code           text NOT NULL UNIQUE CHECK (code ~ '^[A-Z0-9_]{2,20}$'),
  legal_name     text NOT NULL CHECK (length(btrim(legal_name)) BETWEEN 2 AND 200),
  display_name   text NOT NULL,
  status         text NOT NULL DEFAULT 'active' CHECK (status IN ('active','suspended','closed')),
  timezone       text NOT NULL DEFAULT 'Africa/Dar_es_Salaam',
  currency_code  char(3) NOT NULL DEFAULT 'TZS' CHECK (currency_code ~ '^[A-Z]{3}$'),
  mrn_prefix     text NOT NULL DEFAULT 'SP' CHECK (mrn_prefix ~ '^[A-Z]{1,6}$'),
  staff_idle_timeout   interval NOT NULL DEFAULT interval '15 minutes' CHECK (staff_idle_timeout BETWEEN interval '5 minutes' AND interval '60 minutes'),
  staff_session_max    interval NOT NULL DEFAULT interval '12 hours'   CHECK (staff_session_max <= interval '24 hours'),
  lockout_threshold    smallint NOT NULL DEFAULT 5 CHECK (lockout_threshold BETWEEN 3 AND 10),
  lockout_duration     interval NOT NULL DEFAULT interval '15 minutes',
  lab_self_verification_allowed boolean NOT NULL DEFAULT false,
  created_at  timestamptz NOT NULL DEFAULT now(),
  created_by  uuid DEFAULT sec.current_user_id() REFERENCES iam.user_account(id),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  updated_by  uuid REFERENCES iam.user_account(id),
  row_version integer NOT NULL DEFAULT 1
);
COMMENT ON TABLE org.organization IS 'Tenant. Spartial is tenant #1; model supports further clinics (Lavaa resale) without schema change.';
CREATE TRIGGER trg_touch BEFORE UPDATE ON org.organization FOR EACH ROW EXECUTE FUNCTION sec.touch_row();

CREATE TABLE org.facility (
  id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id        uuid NOT NULL REFERENCES org.organization(id),
  code          text NOT NULL CHECK (code ~ '^[A-Z0-9_-]{2,20}$'),
  name          text NOT NULL,
  hfr_code      text,                         -- Tanzania Health Facility Registry code
  phone_e164    text CHECK (phone_e164 ~ '^\+[1-9][0-9]{7,14}$'),
  region        text, district text, street_address text,
  status        text NOT NULL DEFAULT 'active' CHECK (status IN ('active','inactive')),
  created_at  timestamptz NOT NULL DEFAULT now(),
  created_by  uuid DEFAULT sec.current_user_id() REFERENCES iam.user_account(id),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  updated_by  uuid REFERENCES iam.user_account(id),
  row_version integer NOT NULL DEFAULT 1,
  UNIQUE (org_id, id),
  UNIQUE (org_id, code)
);
CREATE TRIGGER trg_touch BEFORE UPDATE ON org.facility FOR EACH ROW EXECUTE FUNCTION sec.touch_row();

CREATE TABLE org.department (
  id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id      uuid NOT NULL,
  facility_id uuid NOT NULL,
  code        text NOT NULL CHECK (code ~ '^[A-Z0-9_-]{2,20}$'),
  name        text NOT NULL,
  dept_type   text NOT NULL CHECK (dept_type IN ('reception','triage','consultation','laboratory','imaging',
                                                 'pharmacy','billing','administration','telemedicine','other')),
  status      text NOT NULL DEFAULT 'active' CHECK (status IN ('active','inactive')),
  created_at  timestamptz NOT NULL DEFAULT now(),
  created_by  uuid DEFAULT sec.current_user_id() REFERENCES iam.user_account(id),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  updated_by  uuid REFERENCES iam.user_account(id),
  row_version integer NOT NULL DEFAULT 1,
  UNIQUE (org_id, id),
  UNIQUE (facility_id, code),
  FOREIGN KEY (org_id, facility_id) REFERENCES org.facility (org_id, id)
);
CREATE INDEX ix_department_facility ON org.department (org_id, facility_id);
CREATE TRIGGER trg_touch BEFORE UPDATE ON org.department FOR EACH ROW EXECUTE FUNCTION sec.touch_row();

CREATE TABLE org.service_point (           -- consultation rooms, lab counters, pharmacy windows
  id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id        uuid NOT NULL,
  department_id uuid NOT NULL,
  code          text NOT NULL,
  name          text NOT NULL,
  status        text NOT NULL DEFAULT 'active' CHECK (status IN ('active','inactive')),
  created_at  timestamptz NOT NULL DEFAULT now(),
  created_by  uuid DEFAULT sec.current_user_id() REFERENCES iam.user_account(id),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  updated_by  uuid REFERENCES iam.user_account(id),
  row_version integer NOT NULL DEFAULT 1,
  UNIQUE (org_id, id),
  UNIQUE (department_id, code),
  FOREIGN KEY (org_id, department_id) REFERENCES org.department (org_id, id)
);
CREATE TRIGGER trg_touch BEFORE UPDATE ON org.service_point FOR EACH ROW EXECUTE FUNCTION sec.touch_row();

-- Gapless, transactional business numbering (MRN, invoice, receipt, queue tickets).
CREATE TABLE org.counter (
  org_id      uuid NOT NULL REFERENCES org.organization(id),
  counter_key text NOT NULL,            -- e.g. 'mrn', 'invoice', 'queue:<department_id>'
  period_key  text NOT NULL DEFAULT '', -- e.g. '2026' or '2026-11-19'
  next_value  bigint NOT NULL CHECK (next_value > 0),
  PRIMARY KEY (org_id, counter_key, period_key)
);

CREATE OR REPLACE FUNCTION org.next_value(p_org uuid, p_key text, p_period text DEFAULT '')
RETURNS bigint LANGUAGE sql SECURITY DEFINER SET search_path = pg_catalog AS $$
  INSERT INTO org.counter AS c (org_id, counter_key, period_key, next_value)
  VALUES (p_org, p_key, p_period, 2)
  ON CONFLICT (org_id, counter_key, period_key) DO UPDATE SET next_value = c.next_value + 1
  RETURNING next_value - 1;
$$;
COMMENT ON FUNCTION org.next_value IS 'Row-locks the counter until commit: gapless on commit, rolled back with the business txn.';

CREATE OR REPLACE FUNCTION org.local_date(p_org uuid, p_ts timestamptz DEFAULT now())
RETURNS date LANGUAGE sql STABLE AS
$$ SELECT (p_ts AT TIME ZONE (SELECT timezone FROM org.organization WHERE id = p_org))::date $$;

-- ---------------------------------------------------------------- RBAC
CREATE TABLE iam.role (
  id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id         uuid REFERENCES org.organization(id),   -- NULL = system template role
  code           text NOT NULL CHECK (code ~ '^[a-z_]{3,40}$'),
  name           text NOT NULL,
  description    text,
  parent_role_id uuid REFERENCES iam.role(id),            -- inherits parent permissions
  is_system      boolean NOT NULL DEFAULT false,
  status         text NOT NULL DEFAULT 'active' CHECK (status IN ('active','retired')),
  created_at  timestamptz NOT NULL DEFAULT now(),
  created_by  uuid DEFAULT sec.current_user_id() REFERENCES iam.user_account(id),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  updated_by  uuid REFERENCES iam.user_account(id),
  row_version integer NOT NULL DEFAULT 1,
  CONSTRAINT ck_role_not_own_parent CHECK (parent_role_id IS DISTINCT FROM id),
  CONSTRAINT ck_system_role_global CHECK (NOT is_system OR org_id IS NULL)
);
CREATE UNIQUE INDEX ux_role_code ON iam.role (org_id, code) NULLS NOT DISTINCT;
CREATE TRIGGER trg_touch BEFORE UPDATE ON iam.role FOR EACH ROW EXECUTE FUNCTION sec.touch_row();

CREATE OR REPLACE FUNCTION iam.check_role_hierarchy() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE v_parent_org uuid;
BEGIN
  IF NEW.parent_role_id IS NULL THEN RETURN NEW; END IF;
  SELECT org_id INTO v_parent_org FROM iam.role WHERE id = NEW.parent_role_id;
  IF v_parent_org IS NOT NULL AND v_parent_org IS DISTINCT FROM NEW.org_id THEN
    RAISE EXCEPTION 'parent role belongs to another organization' USING ERRCODE = 'SPX01';
  END IF;
  IF EXISTS (WITH RECURSIVE up AS (
               SELECT id, parent_role_id, 1 AS depth FROM iam.role WHERE id = NEW.parent_role_id
               UNION ALL
               SELECT r.id, r.parent_role_id, up.depth + 1 FROM iam.role r JOIN up ON r.id = up.parent_role_id
               WHERE up.depth < 20)
             SELECT 1 FROM up WHERE id = NEW.id) THEN
    RAISE EXCEPTION 'role hierarchy cycle detected' USING ERRCODE = 'SPV01';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER trg_role_hierarchy BEFORE INSERT OR UPDATE OF parent_role_id ON iam.role
  FOR EACH ROW EXECUTE FUNCTION iam.check_role_hierarchy();

CREATE TABLE iam.role_permission (
  role_id         uuid NOT NULL REFERENCES iam.role(id) ON DELETE CASCADE,
  permission_code text NOT NULL REFERENCES ref.permission(code),
  granted_at      timestamptz NOT NULL DEFAULT now(),
  granted_by      uuid DEFAULT sec.current_user_id() REFERENCES iam.user_account(id),
  PRIMARY KEY (role_id, permission_code)
);

-- Staff (and service accounts) belong to an organization through a membership.
CREATE TABLE iam.membership (
  id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id              uuid NOT NULL REFERENCES org.organization(id),
  user_id             uuid NOT NULL REFERENCES iam.user_account(id),
  employee_no         text,
  job_title           text,
  professional_body   text CHECK (professional_body IN ('MCT','TNMC','PC','HLPC','other')),  -- Medical Council, Nursing & Midwifery Council, Pharmacy Council, Health Lab Practitioners Council
  professional_reg_no text,
  status              text NOT NULL DEFAULT 'invited' CHECK (status IN ('invited','active','suspended','ended')),
  start_date          date NOT NULL DEFAULT current_date,
  end_date            date,
  created_at  timestamptz NOT NULL DEFAULT now(),
  created_by  uuid DEFAULT sec.current_user_id() REFERENCES iam.user_account(id),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  updated_by  uuid REFERENCES iam.user_account(id),
  row_version integer NOT NULL DEFAULT 1,
  UNIQUE (org_id, id),
  UNIQUE (org_id, user_id),
  CONSTRAINT ck_membership_dates CHECK (end_date IS NULL OR end_date >= start_date),
  CONSTRAINT ck_membership_ended CHECK (status <> 'ended' OR end_date IS NOT NULL),
  CONSTRAINT ck_prof_reg CHECK ((professional_body IS NULL) = (professional_reg_no IS NULL))
);
CREATE UNIQUE INDEX ux_membership_employee_no ON iam.membership (org_id, employee_no) WHERE employee_no IS NOT NULL;
SELECT sec.attach_standard_triggers('iam.membership');

CREATE TABLE iam.membership_role (
  id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id        uuid NOT NULL,
  membership_id uuid NOT NULL,
  role_id       uuid NOT NULL REFERENCES iam.role(id),
  facility_id   uuid,                      -- NULL = all facilities of the org (ABAC scope)
  granted_by    uuid DEFAULT sec.current_user_id() REFERENCES iam.user_account(id),
  granted_at    timestamptz NOT NULL DEFAULT now(),
  expires_at    timestamptz,
  revoked_at    timestamptz,
  revoked_by    uuid REFERENCES iam.user_account(id),
  revoke_reason text,
  UNIQUE (org_id, id),
  FOREIGN KEY (org_id, membership_id) REFERENCES iam.membership (org_id, id),
  FOREIGN KEY (org_id, facility_id)   REFERENCES org.facility (org_id, id),
  CONSTRAINT ck_mr_revoke CHECK ((revoked_at IS NULL) = (revoked_by IS NULL)),
  CONSTRAINT ck_mr_expiry CHECK (expires_at IS NULL OR expires_at > granted_at)
);
CREATE UNIQUE INDEX ux_membership_role_active ON iam.membership_role
  (membership_id, role_id, coalesce(facility_id, '00000000-0000-0000-0000-000000000000'::uuid))
  WHERE revoked_at IS NULL;
CREATE INDEX ix_membership_role_membership ON iam.membership_role (membership_id) WHERE revoked_at IS NULL;

CREATE OR REPLACE FUNCTION iam.check_membership_role() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE v_role_org uuid; v_member_user uuid;
BEGIN
  SELECT org_id INTO v_role_org FROM iam.role WHERE id = NEW.role_id AND status = 'active';
  IF NOT FOUND THEN RAISE EXCEPTION 'role % not active', NEW.role_id USING ERRCODE = 'SPV01'; END IF;
  IF v_role_org IS NOT NULL AND v_role_org <> NEW.org_id THEN
    RAISE EXCEPTION 'role belongs to another organization' USING ERRCODE = 'SPX01';
  END IF;
  SELECT user_id INTO v_member_user FROM iam.membership WHERE id = NEW.membership_id;
  IF TG_OP = 'INSERT' AND NEW.granted_by IS NOT NULL AND NEW.granted_by = v_member_user THEN
    RAISE EXCEPTION 'users cannot grant roles to themselves' USING ERRCODE = 'SPV01';
  END IF;
  IF TG_OP = 'UPDATE' AND (NEW.role_id, NEW.membership_id, NEW.facility_id, NEW.granted_at)
                       IS DISTINCT FROM (OLD.role_id, OLD.membership_id, OLD.facility_id, OLD.granted_at) THEN
    RAISE EXCEPTION 'role assignments are immutable; revoke and re-grant' USING ERRCODE = 'SPI01';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER trg_membership_role_check BEFORE INSERT OR UPDATE ON iam.membership_role
  FOR EACH ROW EXECUTE FUNCTION iam.check_membership_role();

CREATE TABLE org.staff_department (       -- routes queue work & worklists
  org_id        uuid NOT NULL,
  membership_id uuid NOT NULL,
  department_id uuid NOT NULL,
  is_primary    boolean NOT NULL DEFAULT false,
  assigned_at   timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (membership_id, department_id),
  FOREIGN KEY (org_id, membership_id) REFERENCES iam.membership (org_id, id),
  FOREIGN KEY (org_id, department_id) REFERENCES org.department (org_id, id)
);
CREATE UNIQUE INDEX ux_staff_primary_dept ON org.staff_department (membership_id) WHERE is_primary;

-- Effective permissions of a membership (roles + inherited parents, unexpired, unrevoked).
CREATE OR REPLACE FUNCTION iam.effective_permissions(p_membership uuid)
RETURNS text[] LANGUAGE sql STABLE SECURITY DEFINER SET search_path = pg_catalog AS $$
  WITH RECURSIVE granted AS (
    SELECT r.id, r.parent_role_id, 1 AS depth
    FROM iam.membership_role mr JOIN iam.role r ON r.id = mr.role_id AND r.status = 'active'
    WHERE mr.membership_id = p_membership AND mr.revoked_at IS NULL
      AND (mr.expires_at IS NULL OR mr.expires_at > now())
    UNION
    SELECT p.id, p.parent_role_id, g.depth + 1
    FROM iam.role p JOIN granted g ON p.id = g.parent_role_id
    WHERE p.status = 'active' AND g.depth < 20)
  SELECT coalesce(array_agg(DISTINCT rp.permission_code ORDER BY rp.permission_code), '{}')
  FROM granted g JOIN iam.role_permission rp ON rp.role_id = g.id;
$$;

-- ---------------------------------------------------------------- sessions
CREATE TABLE iam.session (
  id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id             uuid NOT NULL REFERENCES iam.user_account(id),
  org_id              uuid REFERENCES org.organization(id),
  membership_id       uuid,
  patient_id          uuid,                                -- portal sessions (FK added in 0004)
  token_hash          bytea NOT NULL UNIQUE CHECK (length(token_hash) = 32),  -- SHA-256 of opaque token
  client_type         text NOT NULL CHECK (client_type IN ('web','mobile','api')),
  auth_method         text NOT NULL CHECK (auth_method IN ('password','password_mfa','otp','api_key','sso')),
  ip_address          inet,
  user_agent          text CHECK (length(user_agent) <= 512),
  idle_timeout        interval NOT NULL,
  created_at          timestamptz NOT NULL DEFAULT now(),
  last_seen_at        timestamptz NOT NULL DEFAULT now(),
  idle_expires_at     timestamptz NOT NULL,
  absolute_expires_at timestamptz NOT NULL,
  mfa_verified_at     timestamptz,
  revoked_at          timestamptz,
  revoked_reason      text CHECK (revoked_reason IN ('logout','idle_timeout','admin_revoked','password_changed',
                                                     'token_reuse_detected','account_disabled','superseded')),
  FOREIGN KEY (org_id, membership_id) REFERENCES iam.membership (org_id, id),
  CONSTRAINT ck_session_expiry CHECK (absolute_expires_at > created_at AND idle_expires_at <= absolute_expires_at),
  CONSTRAINT ck_session_revoked CHECK ((revoked_at IS NULL) = (revoked_reason IS NULL))
);
CREATE INDEX ix_session_user_active ON iam.session (user_id) WHERE revoked_at IS NULL;
CREATE INDEX ix_session_expiry ON iam.session (absolute_expires_at) WHERE revoked_at IS NULL;

-- Rotating refresh tokens for the mobile app, with reuse detection per family.
CREATE TABLE iam.refresh_token (
  id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  session_id     uuid NOT NULL REFERENCES iam.session(id),
  family_id      uuid NOT NULL,
  token_hash     bytea NOT NULL UNIQUE CHECK (length(token_hash) = 32),
  issued_at      timestamptz NOT NULL DEFAULT now(),
  expires_at     timestamptz NOT NULL,
  used_at        timestamptz,
  replaced_by_id uuid REFERENCES iam.refresh_token(id),
  revoked_at     timestamptz,
  CONSTRAINT ck_refresh_ttl CHECK (expires_at > issued_at AND expires_at <= issued_at + interval '30 days')
);
CREATE INDEX ix_refresh_family ON iam.refresh_token (family_id);

-- Service accounts (integrations) authenticate with API keys; permissions come
-- from the service account's membership roles. Only a hash is stored.
CREATE TABLE iam.api_credential (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id       uuid NOT NULL REFERENCES org.organization(id),
  user_id      uuid NOT NULL REFERENCES iam.user_account(id),
  name         text NOT NULL,
  key_prefix   text NOT NULL UNIQUE CHECK (key_prefix ~ '^sp_[a-z]{4}_[A-Za-z0-9]{8}$'),
  key_hash     bytea NOT NULL CHECK (length(key_hash) = 32),
  allowed_cidrs cidr[] NOT NULL DEFAULT '{}',
  expires_at   timestamptz NOT NULL,
  last_used_at timestamptz,
  revoked_at   timestamptz,
  created_at   timestamptz NOT NULL DEFAULT now(),
  created_by   uuid DEFAULT sec.current_user_id() REFERENCES iam.user_account(id),
  UNIQUE (org_id, id),
  CONSTRAINT ck_api_key_max_life CHECK (expires_at <= created_at + interval '366 days')
);

-- SOC 2 CC6.2/6.3 periodic access reviews.
CREATE TABLE iam.access_review (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id       uuid NOT NULL REFERENCES org.organization(id),
  title        text NOT NULL,
  period_start date NOT NULL,
  period_end   date NOT NULL,
  status       text NOT NULL DEFAULT 'open' CHECK (status IN ('open','completed','cancelled')),
  started_by   uuid DEFAULT sec.current_user_id() REFERENCES iam.user_account(id),
  completed_at timestamptz,
  created_at  timestamptz NOT NULL DEFAULT now(),
  created_by  uuid DEFAULT sec.current_user_id() REFERENCES iam.user_account(id),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  updated_by  uuid REFERENCES iam.user_account(id),
  row_version integer NOT NULL DEFAULT 1,
  UNIQUE (org_id, id),
  CONSTRAINT ck_review_period CHECK (period_end >= period_start),
  CONSTRAINT ck_review_completed CHECK ((status = 'completed') = (completed_at IS NOT NULL))
);
SELECT sec.attach_standard_triggers('iam.access_review');

CREATE TABLE iam.access_review_item (
  id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id             uuid NOT NULL,
  review_id          uuid NOT NULL,
  membership_role_id uuid NOT NULL,
  decision           text NOT NULL DEFAULT 'pending' CHECK (decision IN ('pending','keep','revoke')),
  decided_by         uuid REFERENCES iam.user_account(id),
  decided_at         timestamptz,
  note               text,
  FOREIGN KEY (org_id, review_id) REFERENCES iam.access_review (org_id, id),
  FOREIGN KEY (org_id, membership_role_id) REFERENCES iam.membership_role (org_id, id),
  UNIQUE (review_id, membership_role_id),
  CONSTRAINT ck_review_decided CHECK ((decision = 'pending') = (decided_at IS NULL))
);
