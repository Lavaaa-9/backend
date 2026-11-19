-- =============================================================================
-- 0005_clinical.sql — appointments, encounters (visits), queue, triage,
-- consultation, diagnoses, history, follow-up.                          [PHI]
--
-- Integrity pattern used throughout: child records carry patient_id and use
-- the composite FK (org_id, encounter_id, patient_id) → encounter, making it
-- impossible to attach a triage/consultation/order/invoice for patient A to
-- a visit of patient B, or to cross tenants.
-- Finalised clinical records are immutable; corrections create a new version
-- (supersedes_id) and the old one moves to 'amended' or 'entered_in_error'.
-- =============================================================================

-- Medical history ----------------------------------------------------------------
CREATE TABLE clinical.allergy (
  id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id        uuid NOT NULL,
  patient_id    uuid NOT NULL,
  substance     text NOT NULL CHECK (length(btrim(substance)) BETWEEN 2 AND 200),
  category      text NOT NULL CHECK (category IN ('drug','food','environment','other')),
  reaction      text,
  severity      text NOT NULL DEFAULT 'unknown' CHECK (severity IN ('mild','moderate','severe','life_threatening','unknown')),
  verification  text NOT NULL DEFAULT 'unconfirmed' CHECK (verification IN ('unconfirmed','confirmed','refuted')),
  status        text NOT NULL DEFAULT 'active' CHECK (status IN ('active','inactive','resolved','entered_in_error')),
  onset_date    date,
  error_reason  text,
  created_at  timestamptz NOT NULL DEFAULT now(),
  created_by  uuid DEFAULT sec.current_user_id() REFERENCES iam.user_account(id),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  updated_by  uuid REFERENCES iam.user_account(id),
  row_version integer NOT NULL DEFAULT 1,
  UNIQUE (org_id, id),
  FOREIGN KEY (org_id, patient_id) REFERENCES patient.patient (org_id, id),
  CONSTRAINT ck_allergy_error CHECK (status <> 'entered_in_error' OR error_reason IS NOT NULL)
);
CREATE INDEX ix_allergy_patient ON clinical.allergy (patient_id) WHERE status = 'active';
CREATE UNIQUE INDEX ux_allergy_active ON clinical.allergy (patient_id, lower(substance)) WHERE status = 'active';
SELECT sec.attach_standard_triggers('clinical.allergy');
SELECT audit.attach_capture('clinical.allergy');

CREATE TABLE clinical.problem (            -- chronic conditions / problem list
  id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id        uuid NOT NULL,
  patient_id    uuid NOT NULL,
  icd10_code    text REFERENCES ref.icd10_code(code),
  description   text NOT NULL,
  status        text NOT NULL DEFAULT 'active' CHECK (status IN ('active','inactive','resolved','entered_in_error')),
  onset_date    date,
  resolved_date date,
  error_reason  text,
  created_at  timestamptz NOT NULL DEFAULT now(),
  created_by  uuid DEFAULT sec.current_user_id() REFERENCES iam.user_account(id),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  updated_by  uuid REFERENCES iam.user_account(id),
  row_version integer NOT NULL DEFAULT 1,
  UNIQUE (org_id, id),
  FOREIGN KEY (org_id, patient_id) REFERENCES patient.patient (org_id, id),
  CONSTRAINT ck_problem_dates CHECK (resolved_date IS NULL OR onset_date IS NULL OR resolved_date >= onset_date),
  CONSTRAINT ck_problem_resolved CHECK (status <> 'resolved' OR resolved_date IS NOT NULL)
);
CREATE INDEX ix_problem_patient ON clinical.problem (patient_id, status);
SELECT sec.attach_standard_triggers('clinical.problem');
SELECT audit.attach_capture('clinical.problem');

CREATE TABLE clinical.history_entry (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id       uuid NOT NULL,
  patient_id   uuid NOT NULL,
  history_type text NOT NULL CHECK (history_type IN ('past_medical','surgical','family','social','obstetric','immunization','other')),
  description  text NOT NULL CHECK (length(btrim(description)) >= 2),
  occurred_on  date,
  status       text NOT NULL DEFAULT 'active' CHECK (status IN ('active','entered_in_error')),
  error_reason text,
  created_at  timestamptz NOT NULL DEFAULT now(),
  created_by  uuid DEFAULT sec.current_user_id() REFERENCES iam.user_account(id),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  updated_by  uuid REFERENCES iam.user_account(id),
  row_version integer NOT NULL DEFAULT 1,
  UNIQUE (org_id, id),
  FOREIGN KEY (org_id, patient_id) REFERENCES patient.patient (org_id, id)
);
CREATE INDEX ix_history_patient ON clinical.history_entry (patient_id, history_type);
SELECT sec.attach_standard_triggers('clinical.history_entry');
SELECT audit.attach_capture('clinical.history_entry');

-- Appointments ---------------------------------------------------------------
CREATE TABLE clinical.appointment (
  id                        uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id                    uuid NOT NULL,
  facility_id               uuid NOT NULL,
  department_id             uuid NOT NULL,
  patient_id                uuid NOT NULL,
  practitioner_membership_id uuid,
  appointment_type          text NOT NULL DEFAULT 'in_person' CHECK (appointment_type IN ('in_person','telemedicine')),
  source                    text NOT NULL CHECK (source IN ('reception','patient_app','phone','follow_up')),
  scheduled_start           timestamptz NOT NULL,
  scheduled_end             timestamptz NOT NULL,
  reason                    text CHECK (length(reason) <= 500),
  status                    text NOT NULL CHECK (status IN ('requested','booked','confirmed','checked_in',
                                                            'completed','cancelled','no_show','rejected')),
  cancel_reason             text,
  cancelled_by              uuid REFERENCES iam.user_account(id),
  cancelled_at              timestamptz,
  checked_in_at             timestamptz,
  idempotency_key           uuid,
  created_at  timestamptz NOT NULL DEFAULT now(),
  created_by  uuid DEFAULT sec.current_user_id() REFERENCES iam.user_account(id),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  updated_by  uuid REFERENCES iam.user_account(id),
  row_version integer NOT NULL DEFAULT 1,
  UNIQUE (org_id, id),
  UNIQUE (org_id, id, patient_id),
  UNIQUE (org_id, idempotency_key),
  FOREIGN KEY (org_id, facility_id)   REFERENCES org.facility (org_id, id),
  FOREIGN KEY (org_id, department_id) REFERENCES org.department (org_id, id),
  FOREIGN KEY (org_id, patient_id)    REFERENCES patient.patient (org_id, id),
  FOREIGN KEY (org_id, practitioner_membership_id) REFERENCES iam.membership (org_id, id),
  CONSTRAINT ck_appt_window CHECK (scheduled_end > scheduled_start AND scheduled_end <= scheduled_start + interval '8 hours'),
  CONSTRAINT ck_appt_cancel CHECK ((status IN ('cancelled','rejected')) = (cancelled_at IS NOT NULL)),
  CONSTRAINT ck_appt_cancel_reason CHECK (status NOT IN ('cancelled','rejected') OR cancel_reason IS NOT NULL),
  CONSTRAINT ck_appt_checkin CHECK (status NOT IN ('checked_in','completed') OR checked_in_at IS NOT NULL),
  -- No double-booking of a practitioner, nor overlapping bookings for a patient.
  CONSTRAINT ex_appt_practitioner EXCLUDE USING gist (
    practitioner_membership_id WITH =, tstzrange(scheduled_start, scheduled_end) WITH &&)
    WHERE (practitioner_membership_id IS NOT NULL AND status IN ('booked','confirmed','checked_in')),
  CONSTRAINT ex_appt_patient EXCLUDE USING gist (
    patient_id WITH =, tstzrange(scheduled_start, scheduled_end) WITH &&)
    WHERE (status IN ('booked','confirmed','checked_in'))
);
CREATE INDEX ix_appt_facility_day ON clinical.appointment (org_id, facility_id, scheduled_start) WHERE status IN ('requested','booked','confirmed','checked_in');
CREATE INDEX ix_appt_patient ON clinical.appointment (patient_id, scheduled_start DESC);
CREATE INDEX ix_appt_practitioner ON clinical.appointment (practitioner_membership_id, scheduled_start) WHERE practitioner_membership_id IS NOT NULL;
SELECT sec.attach_standard_triggers('clinical.appointment');
SELECT audit.attach_capture('clinical.appointment');

-- Encounters (visits) --------------------------------------------------------
CREATE TABLE clinical.encounter (
  id                      uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id                  uuid NOT NULL,
  facility_id             uuid NOT NULL,
  patient_id              uuid NOT NULL,
  appointment_id          uuid UNIQUE,
  encounter_no            text NOT NULL CHECK (encounter_no ~ '^V-[0-9]{4}-[0-9]{6}$'),
  encounter_type          text NOT NULL CHECK (encounter_type IN ('outpatient','emergency','telemedicine','follow_up')),
  status                  text NOT NULL DEFAULT 'registered' CHECK (status IN ('registered','in_progress','completed','cancelled','entered_in_error')),
  arrived_at              timestamptz NOT NULL DEFAULT now(),
  started_at              timestamptz,
  closed_at               timestamptz,
  closed_by               uuid REFERENCES iam.user_account(id),
  attending_membership_id uuid,
  chief_complaint         text CHECK (length(chief_complaint) <= 500),
  payer_id                uuid,                -- default payer for the visit (FK in 0008)
  cancel_reason           text,
  created_at  timestamptz NOT NULL DEFAULT now(),
  created_by  uuid DEFAULT sec.current_user_id() REFERENCES iam.user_account(id),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  updated_by  uuid REFERENCES iam.user_account(id),
  row_version integer NOT NULL DEFAULT 1,
  UNIQUE (org_id, id),
  UNIQUE (org_id, id, patient_id),
  UNIQUE (org_id, encounter_no),
  FOREIGN KEY (org_id, facility_id) REFERENCES org.facility (org_id, id),
  FOREIGN KEY (org_id, patient_id)  REFERENCES patient.patient (org_id, id),
  FOREIGN KEY (org_id, appointment_id, patient_id) REFERENCES clinical.appointment (org_id, id, patient_id),
  FOREIGN KEY (org_id, attending_membership_id) REFERENCES iam.membership (org_id, id),
  CONSTRAINT ck_enc_closed CHECK ((status IN ('completed','cancelled','entered_in_error')) = (closed_at IS NOT NULL)),
  CONSTRAINT ck_enc_cancel_reason CHECK (status NOT IN ('cancelled','entered_in_error') OR cancel_reason IS NOT NULL),
  CONSTRAINT ck_enc_times CHECK (closed_at IS NULL OR closed_at >= arrived_at)
);
-- One open visit per patient per facility (prevents duplicate check-ins).
CREATE UNIQUE INDEX ux_encounter_open ON clinical.encounter (patient_id, facility_id) WHERE status IN ('registered','in_progress');
CREATE INDEX ix_encounter_patient ON clinical.encounter (patient_id, arrived_at DESC);
CREATE INDEX ix_encounter_facility_day ON clinical.encounter (org_id, facility_id, arrived_at DESC);
SELECT sec.attach_standard_triggers('clinical.encounter');
SELECT audit.attach_capture('clinical.encounter');

CREATE OR REPLACE FUNCTION clinical.next_encounter_no(p_org uuid) RETURNS text
LANGUAGE sql SECURITY DEFINER SET search_path = pg_catalog AS $$
  SELECT 'V-' || to_char(org.local_date(p_org), 'YYYY') || '-' ||
         lpad(org.next_value(p_org, 'encounter', to_char(org.local_date(p_org), 'YYYY'))::text, 6, '0');
$$;

CREATE OR REPLACE FUNCTION clinical.validate_encounter() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE v_status text;
BEGIN
  SELECT status INTO v_status FROM patient.patient WHERE id = NEW.patient_id;
  IF TG_OP = 'INSERT' AND v_status IN ('deceased','merged') THEN
    RAISE EXCEPTION 'cannot open a visit for a % patient', v_status USING ERRCODE = 'SPV01';
  END IF;
  IF NEW.status = 'in_progress' AND NEW.started_at IS NULL THEN NEW.started_at := now(); END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER trg_encounter_validate BEFORE INSERT OR UPDATE ON clinical.encounter
  FOR EACH ROW EXECUTE FUNCTION clinical.validate_encounter();

-- Queue ----------------------------------------------------------------------
CREATE TABLE clinical.queue_ticket (
  id                      uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id                  uuid NOT NULL,
  facility_id             uuid NOT NULL,
  department_id           uuid NOT NULL,
  service_point_id        uuid,
  encounter_id            uuid NOT NULL,
  patient_id              uuid NOT NULL,
  queue_date              date NOT NULL,          -- facility-local date
  ticket_no               integer NOT NULL CHECK (ticket_no > 0),
  priority                text NOT NULL DEFAULT 'routine' CHECK (priority IN ('emergency','urgent','routine')),
  priority_rank           smallint GENERATED ALWAYS AS (CASE priority WHEN 'emergency' THEN 1 WHEN 'urgent' THEN 2 ELSE 3 END) STORED,
  status                  text NOT NULL DEFAULT 'waiting' CHECK (status IN ('waiting','called','in_service','completed','skipped','cancelled','transferred')),
  called_at               timestamptz,
  call_count              smallint NOT NULL DEFAULT 0 CHECK (call_count BETWEEN 0 AND 10),
  service_started_at      timestamptz,
  completed_at            timestamptz,
  served_by_membership_id uuid,
  transferred_to_ticket_id uuid REFERENCES clinical.queue_ticket(id),
  created_at  timestamptz NOT NULL DEFAULT now(),
  created_by  uuid DEFAULT sec.current_user_id() REFERENCES iam.user_account(id),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  updated_by  uuid REFERENCES iam.user_account(id),
  row_version integer NOT NULL DEFAULT 1,
  UNIQUE (org_id, id),
  UNIQUE (department_id, queue_date, ticket_no),
  FOREIGN KEY (org_id, facility_id)      REFERENCES org.facility (org_id, id),
  FOREIGN KEY (org_id, department_id)    REFERENCES org.department (org_id, id),
  FOREIGN KEY (org_id, service_point_id) REFERENCES org.service_point (org_id, id),
  FOREIGN KEY (org_id, encounter_id, patient_id) REFERENCES clinical.encounter (org_id, id, patient_id),
  FOREIGN KEY (org_id, served_by_membership_id)  REFERENCES iam.membership (org_id, id),
  CONSTRAINT ck_queue_transfer CHECK ((status = 'transferred') = (transferred_to_ticket_id IS NOT NULL))
);
-- One active ticket per visit per department; queue board query.
CREATE UNIQUE INDEX ux_queue_active ON clinical.queue_ticket (encounter_id, department_id) WHERE status IN ('waiting','called','in_service');
CREATE INDEX ix_queue_board ON clinical.queue_ticket (department_id, queue_date, priority_rank, created_at) WHERE status IN ('waiting','called','in_service');
SELECT sec.attach_standard_triggers('clinical.queue_ticket');

CREATE OR REPLACE FUNCTION clinical.queue_stamp() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'INSERT' THEN
    NEW.queue_date := org.local_date(NEW.org_id);
    NEW.ticket_no  := org.next_value(NEW.org_id, 'queue:' || NEW.department_id, NEW.queue_date::text);
  ELSIF NEW.status IS DISTINCT FROM OLD.status THEN
    IF NEW.status = 'called' THEN NEW.called_at := now(); NEW.call_count := OLD.call_count + 1; END IF;
    IF NEW.status = 'in_service' THEN NEW.service_started_at := now();
       NEW.served_by_membership_id := coalesce(NEW.served_by_membership_id, sec.current_membership_id()); END IF;
    IF NEW.status IN ('completed','skipped','cancelled','transferred') THEN NEW.completed_at := now(); END IF;
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER trg_queue_stamp BEFORE INSERT OR UPDATE ON clinical.queue_ticket
  FOR EACH ROW EXECUTE FUNCTION clinical.queue_stamp();

-- Triage / vital signs ---------------------------------------------------------
CREATE TABLE clinical.triage_assessment (
  id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id             uuid NOT NULL,
  encounter_id       uuid NOT NULL,
  patient_id         uuid NOT NULL,
  recorded_by_membership_id uuid NOT NULL,
  measured_at        timestamptz NOT NULL DEFAULT now(),
  temperature_c      numeric(4,1)  CHECK (temperature_c BETWEEN 25 AND 45),
  pulse_bpm          smallint      CHECK (pulse_bpm BETWEEN 20 AND 300),
  respiratory_rate   smallint      CHECK (respiratory_rate BETWEEN 4 AND 80),
  systolic_mmhg      smallint      CHECK (systolic_mmhg BETWEEN 40 AND 300),
  diastolic_mmhg     smallint      CHECK (diastolic_mmhg BETWEEN 20 AND 200),
  spo2_pct           smallint      CHECK (spo2_pct BETWEEN 50 AND 100),
  weight_kg          numeric(5,2)  CHECK (weight_kg BETWEEN 0.3 AND 400),
  height_cm          numeric(5,1)  CHECK (height_cm BETWEEN 20 AND 260),
  bmi                numeric(5,2) GENERATED ALWAYS AS
                     (CASE WHEN weight_kg IS NOT NULL AND height_cm IS NOT NULL
                           THEN round(weight_kg / ((height_cm / 100.0) ^ 2), 2) END) STORED,
  muac_cm            numeric(4,1)  CHECK (muac_cm BETWEEN 5 AND 60),
  blood_glucose_mmol numeric(4,1)  CHECK (blood_glucose_mmol BETWEEN 0.5 AND 60),
  pain_score         smallint      CHECK (pain_score BETWEEN 0 AND 10),
  triage_category    text CHECK (triage_category IN ('emergency','priority','queue')),   -- WHO ETAT
  notes              text CHECK (length(notes) <= 4000),
  status             text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','final','amended','entered_in_error')),
  supersedes_id      uuid UNIQUE REFERENCES clinical.triage_assessment(id),
  finalised_at       timestamptz,
  error_reason       text,
  created_at  timestamptz NOT NULL DEFAULT now(),
  created_by  uuid DEFAULT sec.current_user_id() REFERENCES iam.user_account(id),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  updated_by  uuid REFERENCES iam.user_account(id),
  row_version integer NOT NULL DEFAULT 1,
  UNIQUE (org_id, id),
  FOREIGN KEY (org_id, encounter_id, patient_id) REFERENCES clinical.encounter (org_id, id, patient_id),
  FOREIGN KEY (org_id, recorded_by_membership_id) REFERENCES iam.membership (org_id, id),
  CONSTRAINT ck_triage_bp CHECK (systolic_mmhg IS NULL OR diastolic_mmhg IS NULL OR systolic_mmhg > diastolic_mmhg),
  CONSTRAINT ck_triage_bp_pair CHECK ((systolic_mmhg IS NULL) = (diastolic_mmhg IS NULL)),
  CONSTRAINT ck_triage_final CHECK (status = 'draft' OR (finalised_at IS NOT NULL AND triage_category IS NOT NULL AND
     num_nonnulls(temperature_c, pulse_bpm, respiratory_rate, systolic_mmhg, spo2_pct, weight_kg) >= 2)),
  CONSTRAINT ck_triage_error CHECK (status <> 'entered_in_error' OR error_reason IS NOT NULL)
);
CREATE INDEX ix_triage_encounter ON clinical.triage_assessment (encounter_id, measured_at DESC);
CREATE INDEX ix_triage_patient ON clinical.triage_assessment (patient_id, measured_at DESC);
SELECT sec.attach_standard_triggers('clinical.triage_assessment');
CREATE TRIGGER trg_triage_protect BEFORE UPDATE ON clinical.triage_assessment
  FOR EACH ROW EXECUTE FUNCTION sec.protect_finalised('final,amended,entered_in_error');
SELECT audit.attach_capture('clinical.triage_assessment');

-- Consultation -----------------------------------------------------------------
CREATE TABLE clinical.consultation (
  id                        uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id                    uuid NOT NULL,
  encounter_id              uuid NOT NULL,
  patient_id                uuid NOT NULL,
  practitioner_membership_id uuid NOT NULL,
  presenting_complaint      text,
  history_present_illness   text,
  review_of_systems         text,
  examination_findings      text,
  assessment                text,
  treatment_plan            text,
  follow_up_instructions    text,
  status                    text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','signed','amended','entered_in_error')),
  signed_at                 timestamptz,
  supersedes_id             uuid UNIQUE REFERENCES clinical.consultation(id),
  amendment_reason          text,
  error_reason              text,
  created_at  timestamptz NOT NULL DEFAULT now(),
  created_by  uuid DEFAULT sec.current_user_id() REFERENCES iam.user_account(id),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  updated_by  uuid REFERENCES iam.user_account(id),
  row_version integer NOT NULL DEFAULT 1,
  UNIQUE (org_id, id),
  UNIQUE (org_id, id, patient_id),
  FOREIGN KEY (org_id, encounter_id, patient_id) REFERENCES clinical.encounter (org_id, id, patient_id),
  FOREIGN KEY (org_id, practitioner_membership_id) REFERENCES iam.membership (org_id, id),
  CONSTRAINT ck_consult_signed CHECK (status = 'draft' OR (signed_at IS NOT NULL
      AND presenting_complaint IS NOT NULL AND examination_findings IS NOT NULL
      AND assessment IS NOT NULL AND treatment_plan IS NOT NULL)),
  CONSTRAINT ck_consult_amend_reason CHECK (supersedes_id IS NULL OR amendment_reason IS NOT NULL),
  CONSTRAINT ck_consult_error CHECK (status <> 'entered_in_error' OR error_reason IS NOT NULL)
);
CREATE INDEX ix_consult_encounter ON clinical.consultation (encounter_id);
CREATE INDEX ix_consult_patient ON clinical.consultation (patient_id, created_at DESC);
-- At most one draft and one signed (current) note per practitioner per visit;
-- an amendment is drafted alongside the signed version, then swapped in.
CREATE UNIQUE INDEX ux_consult_draft  ON clinical.consultation (encounter_id, practitioner_membership_id) WHERE status = 'draft';
CREATE UNIQUE INDEX ux_consult_signed ON clinical.consultation (encounter_id, practitioner_membership_id) WHERE status = 'signed';
SELECT sec.attach_standard_triggers('clinical.consultation');
CREATE TRIGGER trg_consult_protect BEFORE UPDATE ON clinical.consultation
  FOR EACH ROW EXECUTE FUNCTION sec.protect_finalised('signed,amended,entered_in_error');
SELECT audit.attach_capture('clinical.consultation');

CREATE OR REPLACE FUNCTION clinical.consultation_rules() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  -- Only the authoring practitioner may sign their own note.
  IF NEW.status = 'signed' AND (TG_OP = 'INSERT' OR OLD.status = 'draft') THEN
    IF sec.current_membership_id() IS DISTINCT FROM NEW.practitioner_membership_id AND NOT sec.is_internal() THEN
      RAISE EXCEPTION 'only the authoring practitioner can sign this consultation' USING ERRCODE = 'SPV01';
    END IF;
    NEW.signed_at := now();
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER trg_consult_rules BEFORE INSERT OR UPDATE ON clinical.consultation
  FOR EACH ROW EXECUTE FUNCTION clinical.consultation_rules();

-- Diagnoses --------------------------------------------------------------------
CREATE TABLE clinical.encounter_diagnosis (
  id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id          uuid NOT NULL,
  encounter_id    uuid NOT NULL,
  patient_id      uuid NOT NULL,
  consultation_id uuid,
  icd10_code      text REFERENCES ref.icd10_code(code),
  description     text NOT NULL,
  diagnosis_type  text NOT NULL CHECK (diagnosis_type IN ('provisional','final','differential')),
  is_primary      boolean NOT NULL DEFAULT false,
  status          text NOT NULL DEFAULT 'active' CHECK (status IN ('active','entered_in_error')),
  recorded_by_membership_id uuid NOT NULL,
  error_reason    text,
  created_at  timestamptz NOT NULL DEFAULT now(),
  created_by  uuid DEFAULT sec.current_user_id() REFERENCES iam.user_account(id),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  updated_by  uuid REFERENCES iam.user_account(id),
  row_version integer NOT NULL DEFAULT 1,
  UNIQUE (org_id, id),
  FOREIGN KEY (org_id, encounter_id, patient_id) REFERENCES clinical.encounter (org_id, id, patient_id),
  FOREIGN KEY (org_id, consultation_id, patient_id) REFERENCES clinical.consultation (org_id, id, patient_id),
  FOREIGN KEY (org_id, recorded_by_membership_id) REFERENCES iam.membership (org_id, id),
  CONSTRAINT ck_dx_final_coded CHECK (diagnosis_type <> 'final' OR icd10_code IS NOT NULL),
  CONSTRAINT ck_dx_error CHECK (status <> 'entered_in_error' OR error_reason IS NOT NULL)
);
CREATE UNIQUE INDEX ux_dx_primary ON clinical.encounter_diagnosis (encounter_id) WHERE is_primary AND status = 'active';
CREATE UNIQUE INDEX ux_dx_code ON clinical.encounter_diagnosis (encounter_id, icd10_code, diagnosis_type) WHERE status = 'active' AND icd10_code IS NOT NULL;
CREATE INDEX ix_dx_patient ON clinical.encounter_diagnosis (patient_id);
CREATE INDEX ix_dx_code_report ON clinical.encounter_diagnosis (org_id, icd10_code) WHERE status = 'active';
SELECT sec.attach_standard_triggers('clinical.encounter_diagnosis');
SELECT audit.attach_capture('clinical.encounter_diagnosis');

-- Follow-up ------------------------------------------------------------------
CREATE TABLE clinical.follow_up_plan (
  id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id         uuid NOT NULL,
  encounter_id   uuid NOT NULL,
  patient_id     uuid NOT NULL,
  department_id  uuid,
  practitioner_membership_id uuid,
  due_date       date NOT NULL,
  reason         text NOT NULL,
  status         text NOT NULL DEFAULT 'planned' CHECK (status IN ('planned','booked','completed','cancelled','missed')),
  appointment_id uuid,
  created_at  timestamptz NOT NULL DEFAULT now(),
  created_by  uuid DEFAULT sec.current_user_id() REFERENCES iam.user_account(id),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  updated_by  uuid REFERENCES iam.user_account(id),
  row_version integer NOT NULL DEFAULT 1,
  UNIQUE (org_id, id),
  FOREIGN KEY (org_id, encounter_id, patient_id) REFERENCES clinical.encounter (org_id, id, patient_id),
  FOREIGN KEY (org_id, appointment_id, patient_id) REFERENCES clinical.appointment (org_id, id, patient_id),
  FOREIGN KEY (org_id, department_id) REFERENCES org.department (org_id, id),
  FOREIGN KEY (org_id, practitioner_membership_id) REFERENCES iam.membership (org_id, id),
  CONSTRAINT ck_followup_booked CHECK (status NOT IN ('booked','completed') OR appointment_id IS NOT NULL)
);
CREATE INDEX ix_followup_due ON clinical.follow_up_plan (org_id, due_date) WHERE status = 'planned';
SELECT sec.attach_standard_triggers('clinical.follow_up_plan');
SELECT audit.attach_capture('clinical.follow_up_plan');
