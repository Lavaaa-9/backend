-- =============================================================================
-- 0008_telehealth_comms_integration_gov.sql
-- =============================================================================

-- Telemedicine ---------------------------------------------------------------
-- Media never touches this database; only scheduling/session metadata. The
-- video provider is an open decision (must sign a BAA/DPA; see spec §F).
CREATE TABLE telehealth.session (
  id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id              uuid NOT NULL,
  appointment_id      uuid NOT NULL UNIQUE,
  encounter_id        uuid,
  patient_id          uuid NOT NULL,
  practitioner_membership_id uuid NOT NULL,
  consent_id          uuid NOT NULL,
  provider            text NOT NULL CHECK (provider IN ('jitsi_self_hosted','daily','twilio','agora','other')),
  provider_room_ref   text,               -- opaque room id; join tokens are minted per participant, never stored
  scheduled_start     timestamptz NOT NULL,
  status              text NOT NULL DEFAULT 'scheduled'
                      CHECK (status IN ('scheduled','waiting_room','in_progress','completed','cancelled','failed','no_show')),
  started_at          timestamptz,
  ended_at            timestamptz,
  end_reason          text,
  recording_enabled   boolean NOT NULL DEFAULT false,
  created_at  timestamptz NOT NULL DEFAULT now(),
  created_by  uuid DEFAULT sec.current_user_id() REFERENCES iam.user_account(id),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  updated_by  uuid REFERENCES iam.user_account(id),
  row_version integer NOT NULL DEFAULT 1,
  UNIQUE (org_id, id),
  FOREIGN KEY (org_id, appointment_id, patient_id) REFERENCES clinical.appointment (org_id, id, patient_id),
  FOREIGN KEY (org_id, encounter_id, patient_id) REFERENCES clinical.encounter (org_id, id, patient_id),
  FOREIGN KEY (org_id, practitioner_membership_id) REFERENCES iam.membership (org_id, id),
  FOREIGN KEY (org_id, consent_id) REFERENCES patient.consent (org_id, id),
  CONSTRAINT ck_tele_times CHECK (ended_at IS NULL OR (started_at IS NOT NULL AND ended_at >= started_at)),
  CONSTRAINT ck_tele_recording CHECK (NOT recording_enabled)   -- recording out of scope until policy approved
);
CREATE INDEX ix_tele_practitioner ON telehealth.session (practitioner_membership_id, scheduled_start);
CREATE INDEX ix_tele_patient ON telehealth.session (patient_id, scheduled_start DESC);
SELECT sec.attach_standard_triggers('telehealth.session');
SELECT audit.attach_capture('telehealth.session');

CREATE OR REPLACE FUNCTION telehealth.session_rules() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE v_consent patient.consent; v_appt clinical.appointment;
BEGIN
  SELECT * INTO v_consent FROM patient.consent WHERE id = NEW.consent_id;
  IF v_consent.patient_id <> NEW.patient_id OR v_consent.consent_type <> 'telemedicine' OR v_consent.status <> 'granted' THEN
    RAISE EXCEPTION 'active telemedicine consent for this patient is required' USING ERRCODE = 'SPC01'; END IF;
  IF TG_OP = 'INSERT' THEN
    SELECT * INTO v_appt FROM clinical.appointment WHERE id = NEW.appointment_id;
    IF v_appt.appointment_type <> 'telemedicine' THEN
      RAISE EXCEPTION 'appointment is not a telemedicine appointment' USING ERRCODE = 'SPX01'; END IF;
  END IF;
  IF NEW.status = 'in_progress' AND NEW.started_at IS NULL THEN NEW.started_at := now(); END IF;
  IF NEW.status IN ('completed','failed') AND NEW.ended_at IS NULL THEN NEW.ended_at := now(); END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER trg_tele_rules BEFORE INSERT OR UPDATE ON telehealth.session FOR EACH ROW EXECUTE FUNCTION telehealth.session_rules();

CREATE TABLE telehealth.session_event (
  id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  org_id        uuid NOT NULL,
  session_id    uuid NOT NULL,
  event_type    text NOT NULL CHECK (event_type IN ('participant_joined','participant_left','connection_lost',
                                                   'reconnected','provider_error','ended')),
  participant_role text CHECK (participant_role IN ('patient','practitioner','interpreter','guardian')),
  actor_user_id uuid REFERENCES iam.user_account(id),
  occurred_at   timestamptz NOT NULL DEFAULT now(),
  detail        jsonb CHECK (detail IS NULL OR (jsonb_typeof(detail) = 'object' AND pg_column_size(detail) < 2048)),
  FOREIGN KEY (org_id, session_id) REFERENCES telehealth.session (org_id, id)
);
CREATE INDEX ix_tele_event_session ON telehealth.session_event (session_id, occurred_at);
CREATE TRIGGER trg_tele_event_append_only BEFORE UPDATE OR DELETE ON telehealth.session_event FOR EACH ROW EXECUTE FUNCTION sec.forbid_mutation();

-- Communications ---------------------------------------------------------------
CREATE TABLE comms.notification_template (
  id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id      uuid NOT NULL REFERENCES org.organization(id),
  code        text NOT NULL CHECK (code ~ '^[a-z_]{3,50}$'),
  channel     text NOT NULL CHECK (channel IN ('sms','whatsapp','email','push')),
  language    text NOT NULL CHECK (language IN ('sw','en')),
  body        text NOT NULL CHECK (length(body) <= 1000),
  version     integer NOT NULL DEFAULT 1,
  status      text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','active','retired')),
  approved_by uuid REFERENCES iam.user_account(id),
  created_at  timestamptz NOT NULL DEFAULT now(),
  created_by  uuid DEFAULT sec.current_user_id() REFERENCES iam.user_account(id),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  updated_by  uuid REFERENCES iam.user_account(id),
  row_version integer NOT NULL DEFAULT 1,
  UNIQUE (org_id, id),
  UNIQUE (org_id, code, channel, language, version),
  CONSTRAINT ck_template_approved CHECK (status <> 'active' OR approved_by IS NOT NULL)
);
CREATE UNIQUE INDEX ux_template_active ON comms.notification_template (org_id, code, channel, language) WHERE status = 'active';
SELECT sec.attach_standard_triggers('comms.notification_template');

-- Transactional outbox: business transactions enqueue events atomically; the
-- worker delivers them (SMS, WhatsApp, push, webhooks) with retry/backoff.
CREATE TABLE comms.outbox (
  id             bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  org_id         uuid REFERENCES org.organization(id),
  event_type     text NOT NULL CHECK (event_type ~ '^[a-z_]+(\.[a-z_]+){1,3}$'),
  aggregate_type text NOT NULL,
  aggregate_id   uuid NOT NULL,
  dedupe_key     text,
  payload        jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(payload) = 'object' AND pg_column_size(payload) < 8192),
  status         text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','processing','succeeded','failed','dead')),
  attempts       smallint NOT NULL DEFAULT 0 CHECK (attempts >= 0),
  max_attempts   smallint NOT NULL DEFAULT 8,
  available_at   timestamptz NOT NULL DEFAULT now(),
  locked_by      text,
  locked_at      timestamptz,
  last_error     text CHECK (length(last_error) <= 1000),   -- sanitised: no PHI
  created_at     timestamptz NOT NULL DEFAULT now(),
  processed_at   timestamptz,
  request_id     uuid DEFAULT sec.current_request_id()
);
CREATE UNIQUE INDEX ux_outbox_dedupe ON comms.outbox (org_id, dedupe_key) WHERE dedupe_key IS NOT NULL;
CREATE INDEX ix_outbox_ready ON comms.outbox (available_at) WHERE status IN ('pending','failed');
CREATE INDEX ix_outbox_dead ON comms.outbox (created_at) WHERE status = 'dead';

CREATE TABLE comms.message (
  id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id              uuid NOT NULL,
  patient_id          uuid,
  user_id             uuid REFERENCES iam.user_account(id),
  outbox_id           bigint REFERENCES comms.outbox(id),
  template_id         uuid,
  channel             text NOT NULL CHECK (channel IN ('sms','whatsapp','email','push')),
  purpose             text NOT NULL CHECK (purpose IN ('otp','appointment_confirmation','appointment_reminder',
                                         'results_ready','payment_receipt','telemedicine_link','account','general')),
  recipient_address   text NOT NULL,
  status              text NOT NULL DEFAULT 'queued' CHECK (status IN ('queued','sending','sent','delivered','failed','cancelled')),
  provider            text,
  provider_message_id text,
  attempts            smallint NOT NULL DEFAULT 0,
  last_error_code     text,
  sent_at             timestamptz,
  delivered_at        timestamptz,
  created_at          timestamptz NOT NULL DEFAULT now(),
  updated_at          timestamptz NOT NULL DEFAULT now(),
  updated_by          uuid,
  created_by          uuid,
  row_version         integer NOT NULL DEFAULT 1,
  UNIQUE (org_id, id),
  FOREIGN KEY (org_id, patient_id) REFERENCES patient.patient (org_id, id),
  FOREIGN KEY (org_id, template_id) REFERENCES comms.notification_template (org_id, id)
);
CREATE UNIQUE INDEX ux_message_provider_id ON comms.message (provider, provider_message_id) WHERE provider_message_id IS NOT NULL;
CREATE INDEX ix_message_patient ON comms.message (patient_id, created_at DESC);
SELECT sec.attach_standard_triggers('comms.message');

-- Patient messaging requires channel consent (OTP/account messages excepted).
CREATE OR REPLACE FUNCTION comms.message_consent() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.patient_id IS NOT NULL AND NEW.purpose NOT IN ('otp','account') AND NEW.channel IN ('sms','whatsapp') THEN
    IF NOT patient.has_consent(NEW.patient_id, CASE NEW.channel WHEN 'sms' THEN 'sms_notifications' ELSE 'whatsapp_notifications' END) THEN
      RAISE EXCEPTION 'patient has not consented to % notifications', NEW.channel USING ERRCODE = 'SPC01';
    END IF;
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER trg_message_consent BEFORE INSERT ON comms.message FOR EACH ROW EXECUTE FUNCTION comms.message_consent();

CREATE TABLE comms.device (                 -- push registrations for the patient app
  id                    uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id               uuid NOT NULL REFERENCES iam.user_account(id),
  platform              text NOT NULL CHECK (platform IN ('android','ios','web')),
  push_token_ciphertext bytea NOT NULL,
  push_token_bidx       bytea NOT NULL UNIQUE CHECK (length(push_token_bidx) = 32),
  key_ref               text NOT NULL,
  app_version           text,
  last_seen_at          timestamptz NOT NULL DEFAULT now(),
  revoked_at            timestamptz,
  created_at            timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX ix_device_user ON comms.device (user_id) WHERE revoked_at IS NULL;

-- Integrations -----------------------------------------------------------------
CREATE TABLE integration.provider_config (
  id                     uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id                 uuid NOT NULL REFERENCES org.organization(id),
  provider               text NOT NULL,
  purpose                text NOT NULL CHECK (purpose IN ('sms','whatsapp','mobile_money','card','telemedicine',
                                                          'object_storage','email','fiscal_receipt','insurance_claims')),
  status                 text NOT NULL DEFAULT 'disabled' CHECK (status IN ('disabled','sandbox','live')),
  credentials_secret_ref text NOT NULL CHECK (credentials_secret_ref ~ '^(vault|awssm|gcpsm|azkv)://'),  -- pointer only
  config                 jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(config) = 'object'),
  dpa_signed_at          date,          -- BAA / data-processing agreement evidence
  dpa_document_ref       text,
  last_reviewed_at       date,
  created_at  timestamptz NOT NULL DEFAULT now(),
  created_by  uuid DEFAULT sec.current_user_id() REFERENCES iam.user_account(id),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  updated_by  uuid REFERENCES iam.user_account(id),
  row_version integer NOT NULL DEFAULT 1,
  UNIQUE (org_id, provider, purpose),
  CONSTRAINT ck_live_requires_dpa CHECK (status <> 'live' OR dpa_signed_at IS NOT NULL)
);
SELECT sec.attach_standard_triggers('integration.provider_config', false);
SELECT audit.attach_capture('integration.provider_config');

-- Inbound webhooks (payment callbacks, SMS delivery receipts). Dedupe by
-- (provider, external id) gives idempotent processing of provider retries.
CREATE TABLE integration.inbound_event (
  id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  provider          text NOT NULL,
  external_event_id text NOT NULL,
  event_type        text NOT NULL,
  org_id            uuid REFERENCES org.organization(id),
  signature_valid   boolean NOT NULL,
  payload_sha256    bytea NOT NULL CHECK (length(payload_sha256) = 32),
  payload           jsonb NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
  status            text NOT NULL DEFAULT 'received' CHECK (status IN ('received','processed','rejected','failed')),
  attempts          smallint NOT NULL DEFAULT 0,
  error             text,
  received_at       timestamptz NOT NULL DEFAULT now(),
  processed_at      timestamptz,
  UNIQUE (provider, external_event_id),
  CONSTRAINT ck_inbound_signature CHECK (signature_valid OR status = 'rejected')
);
CREATE INDEX ix_inbound_pending ON integration.inbound_event (received_at) WHERE status IN ('received','failed');

-- Governance -------------------------------------------------------------------
CREATE TABLE gov.data_category (
  code              text PRIMARY KEY,
  description       text NOT NULL,
  classification    text NOT NULL CHECK (classification IN ('phi','phi_restricted','pii','confidential','internal','public')),
  retention_period  interval,              -- NULL = PENDING legal/business decision; nothing is purged
  retention_basis   text,                  -- statute / contract / policy reference once decided
  disposal_method   text NOT NULL DEFAULT 'pending_decision'
                    CHECK (disposal_method IN ('pending_decision','hard_delete','crypto_shred','anonymise','archive_then_delete')),
  owner_role        text NOT NULL
);

CREATE TABLE gov.legal_hold (
  id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id      uuid NOT NULL REFERENCES org.organization(id),
  patient_id  uuid,
  scope       text NOT NULL CHECK (scope IN ('patient','org_wide')),
  reason      text NOT NULL,
  reference   text,
  placed_by   uuid NOT NULL DEFAULT sec.current_user_id() REFERENCES iam.user_account(id),
  placed_at   timestamptz NOT NULL DEFAULT now(),
  released_by uuid REFERENCES iam.user_account(id),
  released_at timestamptz,
  FOREIGN KEY (org_id, patient_id) REFERENCES patient.patient (org_id, id),
  CONSTRAINT ck_hold_scope CHECK ((scope = 'patient') = (patient_id IS NOT NULL)),
  CONSTRAINT ck_hold_release CHECK ((released_at IS NULL) = (released_by IS NULL))
);
SELECT audit.attach_capture('gov.legal_hold');

-- Data-subject requests (Tanzania PDPA 2022; HIPAA right of access / amendment).
CREATE TABLE gov.privacy_request (
  id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id        uuid NOT NULL REFERENCES org.organization(id),
  patient_id    uuid NOT NULL,
  request_type  text NOT NULL CHECK (request_type IN ('access','copy_export','correction','restriction','objection',
                                                     'erasure','accounting_of_disclosures')),
  status        text NOT NULL DEFAULT 'received' CHECK (status IN ('received','identity_verified','in_progress',
                                                                   'fulfilled','partially_fulfilled','denied','withdrawn')),
  received_via  text NOT NULL CHECK (received_via IN ('in_person','portal','email','letter')),
  received_at   timestamptz NOT NULL DEFAULT now(),
  due_at        timestamptz NOT NULL,
  identity_verified_by uuid REFERENCES iam.user_account(id),
  handled_by    uuid REFERENCES iam.user_account(id),
  outcome_note  text,
  denial_reason text,
  export_document_id uuid,
  closed_at     timestamptz,
  created_at  timestamptz NOT NULL DEFAULT now(),
  created_by  uuid DEFAULT sec.current_user_id() REFERENCES iam.user_account(id),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  updated_by  uuid REFERENCES iam.user_account(id),
  row_version integer NOT NULL DEFAULT 1,
  UNIQUE (org_id, id),
  FOREIGN KEY (org_id, patient_id) REFERENCES patient.patient (org_id, id),
  FOREIGN KEY (org_id, export_document_id) REFERENCES document.document (org_id, id),
  CONSTRAINT ck_pr_denied CHECK (status <> 'denied' OR denial_reason IS NOT NULL),
  CONSTRAINT ck_pr_closed CHECK ((status IN ('fulfilled','partially_fulfilled','denied','withdrawn')) = (closed_at IS NOT NULL))
);
CREATE INDEX ix_privacy_due ON gov.privacy_request (org_id, due_at) WHERE closed_at IS NULL;
SELECT sec.attach_standard_triggers('gov.privacy_request');
SELECT audit.attach_capture('gov.privacy_request');
