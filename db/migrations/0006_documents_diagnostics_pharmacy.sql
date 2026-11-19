-- =============================================================================
-- 0006_documents_diagnostics_pharmacy.sql                               [PHI]
-- =============================================================================

-- Documents: metadata only. Bytes live in a private, encrypted object-storage
-- bucket; downloads are served via short-lived pre-signed URLs issued by the
-- API after an authorisation check (and logged as document.file.download).
CREATE TABLE document.document (
  id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id             uuid NOT NULL,
  patient_id         uuid NOT NULL,
  encounter_id       uuid,
  category           text NOT NULL CHECK (category IN ('identity','referral_letter','lab_report','imaging','consent_form',
                                                       'insurance','clinical_photo','discharge_summary','prescription','other')),
  title              text NOT NULL CHECK (length(btrim(title)) BETWEEN 1 AND 200),
  storage_bucket     text NOT NULL,
  storage_key        text NOT NULL,          -- opaque key, never derived from PHI (e.g. org/uuid)
  content_type       text NOT NULL CHECK (content_type IN ('application/pdf','image/jpeg','image/png','image/webp',
                                                           'application/dicom','text/plain')),
  size_bytes         bigint NOT NULL CHECK (size_bytes BETWEEN 1 AND 52428800),   -- 50 MiB
  sha256             bytea NOT NULL CHECK (length(sha256) = 32),
  encryption_key_ref text NOT NULL,
  status             text NOT NULL DEFAULT 'pending_upload'
                     CHECK (status IN ('pending_upload','scanning','available','quarantined','deleted')),
  legal_hold         boolean NOT NULL DEFAULT false,
  deleted_at         timestamptz,
  deleted_by         uuid REFERENCES iam.user_account(id),
  delete_reason      text,
  created_at  timestamptz NOT NULL DEFAULT now(),
  created_by  uuid DEFAULT sec.current_user_id() REFERENCES iam.user_account(id),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  updated_by  uuid REFERENCES iam.user_account(id),
  row_version integer NOT NULL DEFAULT 1,
  UNIQUE (org_id, id),
  UNIQUE (storage_bucket, storage_key),
  FOREIGN KEY (org_id, patient_id) REFERENCES patient.patient (org_id, id),
  FOREIGN KEY (org_id, encounter_id, patient_id) REFERENCES clinical.encounter (org_id, id, patient_id),
  CONSTRAINT ck_doc_deleted CHECK ((status = 'deleted') = (deleted_at IS NOT NULL)),
  CONSTRAINT ck_doc_delete_reason CHECK (status <> 'deleted' OR delete_reason IS NOT NULL),
  CONSTRAINT ck_doc_hold CHECK (NOT (legal_hold AND status = 'deleted'))
);
CREATE INDEX ix_document_patient ON document.document (patient_id, created_at DESC) WHERE status <> 'deleted';
SELECT sec.attach_standard_triggers('document.document');
SELECT audit.attach_capture('document.document', ARRAY['encryption_key_ref']);

ALTER TABLE patient.consent ADD CONSTRAINT fk_consent_document
  FOREIGN KEY (org_id, document_id) REFERENCES document.document (org_id, id);

-- Diagnostics catalogue ------------------------------------------------------
CREATE TABLE diag.test_catalog (
  id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id           uuid NOT NULL REFERENCES org.organization(id),
  code             text NOT NULL CHECK (code ~ '^[A-Z0-9_-]{2,30}$'),
  name             text NOT NULL,
  category         text NOT NULL CHECK (category IN ('laboratory','imaging','other_diagnostic')),
  specimen_type    text CHECK (specimen_type IN ('blood','serum','plasma','urine','stool','sputum','swab','csf','tissue','other')),
  turnaround_hours smallint CHECK (turnaround_hours BETWEEN 0 AND 720),
  status           text NOT NULL DEFAULT 'active' CHECK (status IN ('active','retired')),
  created_at  timestamptz NOT NULL DEFAULT now(),
  created_by  uuid DEFAULT sec.current_user_id() REFERENCES iam.user_account(id),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  updated_by  uuid REFERENCES iam.user_account(id),
  row_version integer NOT NULL DEFAULT 1,
  UNIQUE (org_id, id),
  UNIQUE (org_id, code),
  CONSTRAINT ck_lab_specimen CHECK (category <> 'laboratory' OR specimen_type IS NOT NULL)
);
SELECT sec.attach_standard_triggers('diag.test_catalog', false);

CREATE TABLE diag.test_component (         -- analytes; a panel (e.g. FBC) has many
  id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id         uuid NOT NULL,
  test_id        uuid NOT NULL,
  code           text NOT NULL,
  name           text NOT NULL,
  value_type     text NOT NULL CHECK (value_type IN ('numeric','text','coded','attachment')),
  unit           text,
  decimal_places smallint CHECK (decimal_places BETWEEN 0 AND 6),
  allowed_values text[],                    -- for coded results e.g. {Positive,Negative}
  display_order  smallint NOT NULL DEFAULT 1,
  status         text NOT NULL DEFAULT 'active' CHECK (status IN ('active','retired')),
  UNIQUE (org_id, id),
  UNIQUE (test_id, code),
  FOREIGN KEY (org_id, test_id) REFERENCES diag.test_catalog (org_id, id),
  CONSTRAINT ck_component_coded CHECK (value_type <> 'coded' OR allowed_values IS NOT NULL)
);

CREATE TABLE diag.reference_range (
  id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id        uuid NOT NULL,
  component_id  uuid NOT NULL,
  sex           text CHECK (sex IN ('female','male')),      -- NULL = any
  age_min_days  integer NOT NULL DEFAULT 0 CHECK (age_min_days >= 0),
  age_max_days  integer NOT NULL DEFAULT 54750 CHECK (age_max_days >= 0),
  low           numeric(18,6),
  high          numeric(18,6),
  critical_low  numeric(18,6),
  critical_high numeric(18,6),
  FOREIGN KEY (org_id, component_id) REFERENCES diag.test_component (org_id, id),
  CONSTRAINT ck_range_age CHECK (age_max_days >= age_min_days),
  CONSTRAINT ck_range_values CHECK (low IS NULL OR high IS NULL OR low <= high),
  CONSTRAINT ex_range_overlap EXCLUDE USING gist (component_id WITH =, (coalesce(sex,'any')) WITH =,
                                                  int4range(age_min_days, age_max_days, '[]') WITH &&)
);

-- Requests -------------------------------------------------------------------
CREATE TABLE diag.request (
  id                     uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id                 uuid NOT NULL,
  facility_id            uuid NOT NULL,
  encounter_id           uuid NOT NULL,
  patient_id             uuid NOT NULL,
  request_no             text NOT NULL,
  ordered_by_membership_id uuid NOT NULL,
  priority               text NOT NULL DEFAULT 'routine' CHECK (priority IN ('routine','urgent','stat')),
  clinical_indication    text NOT NULL CHECK (length(btrim(clinical_indication)) >= 3),
  status                 text NOT NULL DEFAULT 'requested'
                         CHECK (status IN ('requested','in_progress','partially_resulted','completed','cancelled')),
  cancel_reason          text,
  created_at  timestamptz NOT NULL DEFAULT now(),
  created_by  uuid DEFAULT sec.current_user_id() REFERENCES iam.user_account(id),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  updated_by  uuid REFERENCES iam.user_account(id),
  row_version integer NOT NULL DEFAULT 1,
  UNIQUE (org_id, id),
  UNIQUE (org_id, id, patient_id),
  UNIQUE (org_id, request_no),
  FOREIGN KEY (org_id, facility_id) REFERENCES org.facility (org_id, id),
  FOREIGN KEY (org_id, encounter_id, patient_id) REFERENCES clinical.encounter (org_id, id, patient_id),
  FOREIGN KEY (org_id, ordered_by_membership_id) REFERENCES iam.membership (org_id, id),
  CONSTRAINT ck_request_cancel CHECK (status <> 'cancelled' OR cancel_reason IS NOT NULL)
);
CREATE INDEX ix_request_worklist ON diag.request (org_id, facility_id, status, created_at) WHERE status IN ('requested','in_progress','partially_resulted');
CREATE INDEX ix_request_patient ON diag.request (patient_id, created_at DESC);
CREATE INDEX ix_request_encounter ON diag.request (encounter_id);
SELECT sec.attach_standard_triggers('diag.request');
SELECT audit.attach_capture('diag.request');

CREATE TABLE diag.request_item (
  id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id        uuid NOT NULL,
  request_id    uuid NOT NULL,
  patient_id    uuid NOT NULL,
  test_id       uuid NOT NULL,
  status        text NOT NULL DEFAULT 'requested'
                CHECK (status IN ('requested','specimen_collected','in_progress','resulted','verified','cancelled','rejected')),
  cancel_reason text,
  created_at  timestamptz NOT NULL DEFAULT now(),
  created_by  uuid DEFAULT sec.current_user_id() REFERENCES iam.user_account(id),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  updated_by  uuid REFERENCES iam.user_account(id),
  row_version integer NOT NULL DEFAULT 1,
  UNIQUE (org_id, id),
  UNIQUE (org_id, id, patient_id),
  UNIQUE (request_id, test_id),
  FOREIGN KEY (org_id, request_id, patient_id) REFERENCES diag.request (org_id, id, patient_id),
  FOREIGN KEY (org_id, test_id) REFERENCES diag.test_catalog (org_id, id),
  CONSTRAINT ck_item_cancel CHECK (status NOT IN ('cancelled','rejected') OR cancel_reason IS NOT NULL)
);
SELECT sec.attach_standard_triggers('diag.request_item');
SELECT audit.attach_capture('diag.request_item');

CREATE TABLE diag.specimen (
  id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id            uuid NOT NULL,
  request_item_id   uuid NOT NULL,
  patient_id        uuid NOT NULL,
  barcode           text NOT NULL,
  specimen_type     text NOT NULL,
  status            text NOT NULL DEFAULT 'collected' CHECK (status IN ('collected','received','rejected','consumed')),
  collected_at      timestamptz NOT NULL DEFAULT now(),
  collected_by      uuid DEFAULT sec.current_user_id() REFERENCES iam.user_account(id),
  received_at       timestamptz,
  received_by       uuid REFERENCES iam.user_account(id),
  rejection_reason  text CHECK (rejection_reason IN ('haemolysed','clotted','insufficient_volume','unlabelled',
                                                    'mislabelled','wrong_container','delayed_transport','other')),
  created_at  timestamptz NOT NULL DEFAULT now(),
  created_by  uuid DEFAULT sec.current_user_id() REFERENCES iam.user_account(id),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  updated_by  uuid REFERENCES iam.user_account(id),
  row_version integer NOT NULL DEFAULT 1,
  UNIQUE (org_id, barcode),
  FOREIGN KEY (org_id, request_item_id, patient_id) REFERENCES diag.request_item (org_id, id, patient_id),
  CONSTRAINT ck_specimen_rejected CHECK ((status = 'rejected') = (rejection_reason IS NOT NULL)),
  CONSTRAINT ck_specimen_received CHECK (status NOT IN ('received','consumed') OR received_at IS NOT NULL)
);
SELECT sec.attach_standard_triggers('diag.specimen');

CREATE TABLE diag.result (
  id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id           uuid NOT NULL,
  request_item_id  uuid NOT NULL,
  patient_id       uuid NOT NULL,
  component_id     uuid NOT NULL,
  value_numeric    numeric(18,6),
  value_text       text CHECK (length(value_text) <= 4000),
  value_coded      text,
  attachment_document_id uuid,
  unit             text,
  ref_low          numeric(18,6),       -- snapshot at resulting time
  ref_high         numeric(18,6),
  abnormal_flag    text CHECK (abnormal_flag IN ('N','L','H','LL','HH','A')),
  is_critical      boolean NOT NULL DEFAULT false,
  status           text NOT NULL DEFAULT 'preliminary' CHECK (status IN ('preliminary','final','corrected','entered_in_error')),
  entered_by       uuid NOT NULL DEFAULT sec.current_user_id() REFERENCES iam.user_account(id),
  entered_at       timestamptz NOT NULL DEFAULT now(),
  verified_by      uuid REFERENCES iam.user_account(id),
  verified_at      timestamptz,
  supersedes_id    uuid UNIQUE REFERENCES diag.result(id),
  correction_reason text,
  error_reason     text,
  created_at  timestamptz NOT NULL DEFAULT now(),
  created_by  uuid DEFAULT sec.current_user_id() REFERENCES iam.user_account(id),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  updated_by  uuid REFERENCES iam.user_account(id),
  row_version integer NOT NULL DEFAULT 1,
  UNIQUE (org_id, id),
  FOREIGN KEY (org_id, request_item_id, patient_id) REFERENCES diag.request_item (org_id, id, patient_id),
  FOREIGN KEY (org_id, component_id) REFERENCES diag.test_component (org_id, id),
  FOREIGN KEY (org_id, attachment_document_id) REFERENCES document.document (org_id, id),
  CONSTRAINT ck_result_value CHECK (num_nonnulls(value_numeric, value_text, value_coded, attachment_document_id) >= 1),
  CONSTRAINT ck_result_verified CHECK (status <> 'final' OR (verified_by IS NOT NULL AND verified_at IS NOT NULL)),
  CONSTRAINT ck_result_correction CHECK (supersedes_id IS NULL OR correction_reason IS NOT NULL),
  CONSTRAINT ck_result_error CHECK (status <> 'entered_in_error' OR error_reason IS NOT NULL)
);
CREATE UNIQUE INDEX ux_result_current ON diag.result (request_item_id, component_id) WHERE status IN ('preliminary','final');
CREATE INDEX ix_result_patient ON diag.result (patient_id, entered_at DESC);
SELECT sec.attach_standard_triggers('diag.result');
CREATE TRIGGER trg_result_protect BEFORE UPDATE ON diag.result
  FOR EACH ROW EXECUTE FUNCTION sec.protect_finalised('final,corrected,entered_in_error');
SELECT audit.attach_capture('diag.result');

CREATE OR REPLACE FUNCTION diag.result_rules() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE v_self_ok boolean;
BEGIN
  IF NEW.status = 'final' AND (TG_OP = 'INSERT' OR OLD.status <> 'final') THEN
    NEW.verified_by := coalesce(NEW.verified_by, sec.current_user_id());
    NEW.verified_at := now();
    SELECT lab_self_verification_allowed INTO v_self_ok FROM org.organization WHERE id = NEW.org_id;
    IF NEW.verified_by = NEW.entered_by AND NOT coalesce(v_self_ok, false) THEN
      RAISE EXCEPTION 'result must be verified by a different user than the one who entered it' USING ERRCODE = 'SPV01';
    END IF;
  END IF;
  -- derive abnormal flag from the snapshot range when numeric
  IF NEW.value_numeric IS NOT NULL AND NEW.abnormal_flag IS NULL AND (NEW.ref_low IS NOT NULL OR NEW.ref_high IS NOT NULL) THEN
    NEW.abnormal_flag := CASE WHEN NEW.value_numeric < NEW.ref_low THEN 'L'
                              WHEN NEW.value_numeric > NEW.ref_high THEN 'H' ELSE 'N' END;
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER trg_result_rules BEFORE INSERT OR UPDATE ON diag.result
  FOR EACH ROW EXECUTE FUNCTION diag.result_rules();

-- Roll item/request status forward when results are verified.
CREATE OR REPLACE FUNCTION diag.rollup_status() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog AS $$
DECLARE v_req uuid; v_total int; v_done int; v_any int;
BEGIN
  IF NEW.status = 'final' THEN
    UPDATE diag.request_item ri SET status = 'verified'
     WHERE ri.id = NEW.request_item_id AND ri.status IN ('in_progress','resulted','specimen_collected')
       AND NOT EXISTS (SELECT 1 FROM diag.test_component c
                       WHERE c.test_id = ri.test_id AND c.status = 'active'
                         AND NOT EXISTS (SELECT 1 FROM diag.result r WHERE r.request_item_id = ri.id
                                         AND r.component_id = c.id AND r.status = 'final'));
  ELSIF NEW.status = 'preliminary' THEN
    UPDATE diag.request_item SET status = 'resulted'
     WHERE id = NEW.request_item_id AND status IN ('in_progress','specimen_collected');
  END IF;
  SELECT request_id INTO v_req FROM diag.request_item WHERE id = NEW.request_item_id;
  SELECT count(*) FILTER (WHERE status NOT IN ('cancelled','rejected')),
         count(*) FILTER (WHERE status = 'verified'),
         count(*) FILTER (WHERE status IN ('resulted','verified'))
    INTO v_total, v_done, v_any FROM diag.request_item WHERE request_id = v_req;
  UPDATE diag.request SET status = CASE WHEN v_done = v_total THEN 'completed' ELSE 'partially_resulted' END
   WHERE id = v_req AND status IN ('requested','in_progress','partially_resulted') AND v_any > 0
     AND status <> CASE WHEN v_done = v_total THEN 'completed' ELSE 'partially_resulted' END;
  RETURN NULL;
END $$;
CREATE TRIGGER trg_result_rollup AFTER INSERT OR UPDATE OF status ON diag.result
  FOR EACH ROW EXECUTE FUNCTION diag.rollup_status();

-- Pharmacy -------------------------------------------------------------------
CREATE TABLE pharmacy.drug (
  id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id         uuid NOT NULL REFERENCES org.organization(id),
  code           text NOT NULL CHECK (code ~ '^[A-Z0-9_-]{2,30}$'),
  generic_name   text NOT NULL,
  brand_name     text,
  dosage_form    text NOT NULL CHECK (dosage_form IN ('tablet','capsule','syrup','suspension','injection','infusion',
                                                      'cream','ointment','drops','inhaler','suppository','powder','other')),
  strength       text NOT NULL,              -- e.g. '500 mg', '125 mg/5 mL'
  unit_of_issue  text NOT NULL,              -- e.g. 'tablet', 'bottle'
  atc_code       text CHECK (atc_code ~ '^[A-Z][0-9]{2}[A-Z]{2}[0-9]{2}$'),
  is_controlled  boolean NOT NULL DEFAULT false,
  status         text NOT NULL DEFAULT 'active' CHECK (status IN ('active','retired')),
  created_at  timestamptz NOT NULL DEFAULT now(),
  created_by  uuid DEFAULT sec.current_user_id() REFERENCES iam.user_account(id),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  updated_by  uuid REFERENCES iam.user_account(id),
  row_version integer NOT NULL DEFAULT 1,
  UNIQUE (org_id, id),
  UNIQUE (org_id, code)
);
CREATE INDEX ix_drug_name_trgm ON pharmacy.drug USING gin (lower(generic_name || ' ' || coalesce(brand_name,'')) gin_trgm_ops);
SELECT sec.attach_standard_triggers('pharmacy.drug', false);

CREATE TABLE pharmacy.stock_batch (
  id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id           uuid NOT NULL,
  facility_id      uuid NOT NULL,
  drug_id          uuid NOT NULL,
  batch_no         text NOT NULL,
  expiry_date      date NOT NULL,
  quantity_on_hand numeric(12,2) NOT NULL DEFAULT 0 CHECK (quantity_on_hand >= 0),  -- derived from movements
  unit_cost        numeric(14,2) CHECK (unit_cost >= 0),
  status           text NOT NULL DEFAULT 'active' CHECK (status IN ('active','quarantined','expired','depleted')),
  received_at      timestamptz NOT NULL DEFAULT now(),
  created_at  timestamptz NOT NULL DEFAULT now(),
  created_by  uuid DEFAULT sec.current_user_id() REFERENCES iam.user_account(id),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  updated_by  uuid REFERENCES iam.user_account(id),
  row_version integer NOT NULL DEFAULT 1,
  UNIQUE (org_id, id),
  UNIQUE (facility_id, drug_id, batch_no),
  FOREIGN KEY (org_id, facility_id) REFERENCES org.facility (org_id, id),
  FOREIGN KEY (org_id, drug_id) REFERENCES pharmacy.drug (org_id, id)
);
CREATE INDEX ix_batch_fefo ON pharmacy.stock_batch (facility_id, drug_id, expiry_date) WHERE status = 'active' AND quantity_on_hand > 0;

CREATE OR REPLACE FUNCTION pharmacy.guard_batch_qty() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'UPDATE' AND NEW.quantity_on_hand IS DISTINCT FROM OLD.quantity_on_hand AND NOT sec.is_internal() THEN
    RAISE EXCEPTION 'quantity_on_hand is derived; record a stock movement instead' USING ERRCODE = 'SPI01';
  END IF;
  IF TG_OP = 'INSERT' AND NEW.quantity_on_hand <> 0 AND NOT sec.is_internal() THEN
    RAISE EXCEPTION 'new batches start at zero; record a receipt movement' USING ERRCODE = 'SPI01';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER trg_batch_guard BEFORE INSERT OR UPDATE ON pharmacy.stock_batch FOR EACH ROW EXECUTE FUNCTION pharmacy.guard_batch_qty();
SELECT sec.attach_standard_triggers('pharmacy.stock_batch');

CREATE TABLE pharmacy.prescription (
  id                       uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id                   uuid NOT NULL,
  encounter_id             uuid NOT NULL,
  patient_id               uuid NOT NULL,
  prescription_no          text NOT NULL,
  prescriber_membership_id uuid NOT NULL,
  status                   text NOT NULL DEFAULT 'draft'
                           CHECK (status IN ('draft','active','partially_dispensed','dispensed','cancelled','expired')),
  signed_at                timestamptz,
  valid_until              date,
  notes                    text,
  cancel_reason            text,
  created_at  timestamptz NOT NULL DEFAULT now(),
  created_by  uuid DEFAULT sec.current_user_id() REFERENCES iam.user_account(id),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  updated_by  uuid REFERENCES iam.user_account(id),
  row_version integer NOT NULL DEFAULT 1,
  UNIQUE (org_id, id),
  UNIQUE (org_id, id, patient_id),
  UNIQUE (org_id, prescription_no),
  FOREIGN KEY (org_id, encounter_id, patient_id) REFERENCES clinical.encounter (org_id, id, patient_id),
  FOREIGN KEY (org_id, prescriber_membership_id) REFERENCES iam.membership (org_id, id),
  CONSTRAINT ck_rx_signed CHECK (status IN ('draft','cancelled') OR signed_at IS NOT NULL),
  CONSTRAINT ck_rx_cancel CHECK (status <> 'cancelled' OR cancel_reason IS NOT NULL)
);
CREATE INDEX ix_rx_pharmacy_queue ON pharmacy.prescription (org_id, status, signed_at) WHERE status IN ('active','partially_dispensed');
CREATE INDEX ix_rx_patient ON pharmacy.prescription (patient_id, created_at DESC);
SELECT sec.attach_standard_triggers('pharmacy.prescription');
SELECT audit.attach_capture('pharmacy.prescription');

CREATE OR REPLACE FUNCTION pharmacy.prescription_rules() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.status = 'active' AND (TG_OP = 'INSERT' OR OLD.status = 'draft') THEN
    IF sec.current_membership_id() IS DISTINCT FROM NEW.prescriber_membership_id AND NOT sec.is_internal() THEN
      RAISE EXCEPTION 'only the prescriber can sign the prescription' USING ERRCODE = 'SPV01';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pharmacy.prescription_item WHERE prescription_id = NEW.id AND status <> 'cancelled') THEN
      RAISE EXCEPTION 'prescription has no items' USING ERRCODE = 'SPV01';
    END IF;
    NEW.signed_at := now();
    NEW.valid_until := coalesce(NEW.valid_until, org.local_date(NEW.org_id) + 30);
  END IF;
  IF TG_OP = 'UPDATE' AND OLD.status <> 'draft' AND NOT sec.is_internal()
     AND (NEW.encounter_id, NEW.patient_id, NEW.prescriber_membership_id, NEW.notes)
         IS DISTINCT FROM (OLD.encounter_id, OLD.patient_id, OLD.prescriber_membership_id, OLD.notes) THEN
    RAISE EXCEPTION 'signed prescriptions are immutable' USING ERRCODE = 'SPI01';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER trg_rx_rules BEFORE INSERT OR UPDATE ON pharmacy.prescription FOR EACH ROW EXECUTE FUNCTION pharmacy.prescription_rules();

CREATE TABLE pharmacy.prescription_item (
  id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id              uuid NOT NULL,
  prescription_id     uuid NOT NULL,
  patient_id          uuid NOT NULL,
  drug_id             uuid,
  drug_name_text      text,                -- external / non-stocked item
  dose_amount         numeric(10,3) NOT NULL CHECK (dose_amount > 0),
  dose_unit           text NOT NULL,
  route               text NOT NULL CHECK (route IN ('oral','iv','im','sc','topical','inhaled','rectal','vaginal',
                                                     'ophthalmic','otic','nasal','sublingual','other')),
  frequency_code      text NOT NULL CHECK (frequency_code IN ('STAT','OD','BD','TDS','QID','Q4H','Q6H','Q8H','NOCTE','PRN','WEEKLY')),
  duration_days       smallint CHECK (duration_days BETWEEN 1 AND 365),
  quantity_prescribed numeric(12,2) NOT NULL CHECK (quantity_prescribed > 0),
  quantity_dispensed  numeric(12,2) NOT NULL DEFAULT 0,     -- derived from dispense_item
  instructions        text,
  status              text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','partially_dispensed','dispensed','cancelled')),
  created_at  timestamptz NOT NULL DEFAULT now(),
  created_by  uuid DEFAULT sec.current_user_id() REFERENCES iam.user_account(id),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  updated_by  uuid REFERENCES iam.user_account(id),
  row_version integer NOT NULL DEFAULT 1,
  UNIQUE (org_id, id),
  FOREIGN KEY (org_id, prescription_id, patient_id) REFERENCES pharmacy.prescription (org_id, id, patient_id),
  FOREIGN KEY (org_id, drug_id) REFERENCES pharmacy.drug (org_id, id),
  CONSTRAINT ck_rxi_drug CHECK (num_nonnulls(drug_id, drug_name_text) = 1),
  CONSTRAINT ck_rxi_qty CHECK (quantity_dispensed >= 0 AND quantity_dispensed <= quantity_prescribed),
  CONSTRAINT ck_rxi_prn_duration CHECK (frequency_code IN ('STAT','PRN') OR duration_days IS NOT NULL)
);
CREATE INDEX ix_rxi_prescription ON pharmacy.prescription_item (prescription_id);
SELECT sec.attach_standard_triggers('pharmacy.prescription_item');
SELECT audit.attach_capture('pharmacy.prescription_item');

CREATE OR REPLACE FUNCTION pharmacy.guard_rx_item() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE v_rx_status text;
BEGIN
  IF sec.is_internal() THEN RETURN NEW; END IF;
  SELECT status INTO v_rx_status FROM pharmacy.prescription WHERE id = NEW.prescription_id;
  IF v_rx_status <> 'draft' AND (TG_OP = 'INSERT' OR
       (to_jsonb(NEW) - ARRAY['status','updated_at','updated_by','row_version'])
       IS DISTINCT FROM (to_jsonb(OLD) - ARRAY['status','updated_at','updated_by','row_version'])) THEN
    RAISE EXCEPTION 'items of a signed prescription cannot be changed' USING ERRCODE = 'SPI01';
  END IF;
  IF TG_OP = 'UPDATE' AND NEW.quantity_dispensed IS DISTINCT FROM OLD.quantity_dispensed THEN
    RAISE EXCEPTION 'quantity_dispensed is derived from dispensing' USING ERRCODE = 'SPI01';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER trg_rxi_guard BEFORE INSERT OR UPDATE ON pharmacy.prescription_item FOR EACH ROW EXECUTE FUNCTION pharmacy.guard_rx_item();

CREATE TABLE pharmacy.dispense (
  id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id          uuid NOT NULL,
  facility_id     uuid NOT NULL,
  prescription_id uuid NOT NULL,
  patient_id      uuid NOT NULL,
  dispensed_by_membership_id uuid NOT NULL,
  dispensed_at    timestamptz NOT NULL DEFAULT now(),
  counselling_given boolean NOT NULL DEFAULT false,
  status          text NOT NULL DEFAULT 'completed' CHECK (status IN ('completed','reversed')),
  reversal_reason text,
  idempotency_key uuid NOT NULL,
  created_at  timestamptz NOT NULL DEFAULT now(),
  created_by  uuid DEFAULT sec.current_user_id() REFERENCES iam.user_account(id),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  updated_by  uuid REFERENCES iam.user_account(id),
  row_version integer NOT NULL DEFAULT 1,
  UNIQUE (org_id, id),
  UNIQUE (org_id, idempotency_key),
  FOREIGN KEY (org_id, facility_id) REFERENCES org.facility (org_id, id),
  FOREIGN KEY (org_id, prescription_id, patient_id) REFERENCES pharmacy.prescription (org_id, id, patient_id),
  FOREIGN KEY (org_id, dispensed_by_membership_id) REFERENCES iam.membership (org_id, id),
  CONSTRAINT ck_dispense_reversal CHECK ((status = 'reversed') = (reversal_reason IS NOT NULL))
);
SELECT sec.attach_standard_triggers('pharmacy.dispense');
SELECT audit.attach_capture('pharmacy.dispense');

CREATE TABLE pharmacy.dispense_item (
  id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id               uuid NOT NULL,
  dispense_id          uuid NOT NULL,
  prescription_item_id uuid NOT NULL,
  batch_id             uuid NOT NULL,
  quantity             numeric(12,2) NOT NULL CHECK (quantity > 0),
  created_at           timestamptz NOT NULL DEFAULT now(),
  created_by           uuid DEFAULT sec.current_user_id() REFERENCES iam.user_account(id),
  UNIQUE (org_id, id),
  FOREIGN KEY (org_id, dispense_id) REFERENCES pharmacy.dispense (org_id, id),
  FOREIGN KEY (org_id, prescription_item_id) REFERENCES pharmacy.prescription_item (org_id, id),
  FOREIGN KEY (org_id, batch_id) REFERENCES pharmacy.stock_batch (org_id, id)
);
CREATE INDEX ix_dispense_item_rxi ON pharmacy.dispense_item (prescription_item_id);
CREATE TRIGGER trg_dispense_item_append_only BEFORE UPDATE OR DELETE ON pharmacy.dispense_item FOR EACH ROW EXECUTE FUNCTION sec.forbid_mutation();

CREATE TABLE pharmacy.stock_movement (
  id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id           uuid NOT NULL,
  batch_id         uuid NOT NULL,
  movement_type    text NOT NULL CHECK (movement_type IN ('receipt','dispense','dispense_reversal','adjustment_up',
                                     'adjustment_down','expiry_writeoff','return_to_supplier','transfer_in','transfer_out')),
  quantity         numeric(12,2) NOT NULL CHECK (quantity <> 0),
  reason           text,
  dispense_item_id uuid,
  supplier_ref     text,
  performed_by     uuid NOT NULL DEFAULT sec.current_user_id() REFERENCES iam.user_account(id),
  performed_at     timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY (org_id, batch_id) REFERENCES pharmacy.stock_batch (org_id, id),
  FOREIGN KEY (org_id, dispense_item_id) REFERENCES pharmacy.dispense_item (org_id, id),
  CONSTRAINT ck_move_sign CHECK (
    (movement_type IN ('receipt','dispense_reversal','adjustment_up','transfer_in') AND quantity > 0) OR
    (movement_type IN ('dispense','adjustment_down','expiry_writeoff','return_to_supplier','transfer_out') AND quantity < 0)),
  CONSTRAINT ck_move_dispense_link CHECK ((movement_type IN ('dispense','dispense_reversal')) = (dispense_item_id IS NOT NULL)),
  CONSTRAINT ck_move_adjust_reason CHECK (movement_type NOT IN ('adjustment_up','adjustment_down','expiry_writeoff') OR reason IS NOT NULL)
);
CREATE INDEX ix_movement_batch ON pharmacy.stock_movement (batch_id, performed_at);
CREATE TRIGGER trg_movement_append_only BEFORE UPDATE OR DELETE ON pharmacy.stock_movement FOR EACH ROW EXECUTE FUNCTION sec.forbid_mutation();

-- Ledger → balance. UPDATE row-locks the batch, so concurrent dispenses serialise
-- on the batch and can never drive stock negative.
CREATE OR REPLACE FUNCTION pharmacy.apply_movement() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog AS $$
BEGIN
  UPDATE pharmacy.stock_batch
     SET quantity_on_hand = quantity_on_hand + NEW.quantity,
         status = CASE WHEN quantity_on_hand + NEW.quantity = 0 THEN 'depleted'
                       WHEN status = 'depleted' THEN 'active' ELSE status END
   WHERE id = NEW.batch_id AND quantity_on_hand + NEW.quantity >= 0;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'insufficient stock in batch %', NEW.batch_id USING ERRCODE = 'SPS01';
  END IF;
  RETURN NULL;
END $$;
CREATE TRIGGER trg_movement_apply AFTER INSERT ON pharmacy.stock_movement FOR EACH ROW EXECUTE FUNCTION pharmacy.apply_movement();

-- Dispense line → validation, stock movement, prescription progress.
CREATE OR REPLACE FUNCTION pharmacy.apply_dispense_item() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog AS $$
DECLARE r_item pharmacy.prescription_item; r_batch pharmacy.stock_batch; r_disp pharmacy.dispense; v_rx_status text;
BEGIN
  SELECT * INTO r_disp FROM pharmacy.dispense WHERE id = NEW.dispense_id;
  SELECT * INTO r_item FROM pharmacy.prescription_item WHERE id = NEW.prescription_item_id FOR UPDATE;
  SELECT status INTO v_rx_status FROM pharmacy.prescription WHERE id = r_item.prescription_id;
  SELECT * INTO r_batch FROM pharmacy.stock_batch WHERE id = NEW.batch_id;

  IF r_item.prescription_id <> r_disp.prescription_id THEN
    RAISE EXCEPTION 'item does not belong to this prescription' USING ERRCODE = 'SPX01'; END IF;
  IF v_rx_status NOT IN ('active','partially_dispensed') THEN
    RAISE EXCEPTION 'prescription is % and cannot be dispensed', v_rx_status USING ERRCODE = 'SPV01'; END IF;
  IF r_item.drug_id IS DISTINCT FROM r_batch.drug_id THEN
    RAISE EXCEPTION 'batch drug does not match prescribed drug' USING ERRCODE = 'SPX01'; END IF;
  IF r_batch.facility_id <> r_disp.facility_id THEN
    RAISE EXCEPTION 'batch is held at another facility' USING ERRCODE = 'SPX01'; END IF;
  IF r_batch.status <> 'active' OR r_batch.expiry_date < org.local_date(NEW.org_id) THEN
    RAISE EXCEPTION 'batch is % / expired and cannot be dispensed', r_batch.status USING ERRCODE = 'SPV01'; END IF;
  IF r_item.status = 'cancelled' THEN
    RAISE EXCEPTION 'prescription item cancelled' USING ERRCODE = 'SPV01'; END IF;
  IF r_item.quantity_dispensed + NEW.quantity > r_item.quantity_prescribed THEN
    RAISE EXCEPTION 'dispensing % exceeds remaining quantity %', NEW.quantity,
      r_item.quantity_prescribed - r_item.quantity_dispensed USING ERRCODE = 'SPV01'; END IF;

  INSERT INTO pharmacy.stock_movement (org_id, batch_id, movement_type, quantity, dispense_item_id, performed_by)
  VALUES (NEW.org_id, NEW.batch_id, 'dispense', -NEW.quantity, NEW.id, coalesce(NEW.created_by, sec.current_user_id()));

  UPDATE pharmacy.prescription_item
     SET quantity_dispensed = quantity_dispensed + NEW.quantity,
         status = CASE WHEN quantity_dispensed + NEW.quantity = quantity_prescribed THEN 'dispensed' ELSE 'partially_dispensed' END
   WHERE id = r_item.id;

  UPDATE pharmacy.prescription p
     SET status = CASE WHEN NOT EXISTS (SELECT 1 FROM pharmacy.prescription_item i
                                        WHERE i.prescription_id = p.id AND i.status IN ('pending','partially_dispensed')
                                          AND i.drug_id IS NOT NULL)
                       THEN 'dispensed' ELSE 'partially_dispensed' END
   WHERE p.id = r_item.prescription_id;
  RETURN NULL;
END $$;
CREATE TRIGGER trg_dispense_item_apply AFTER INSERT ON pharmacy.dispense_item FOR EACH ROW EXECUTE FUNCTION pharmacy.apply_dispense_item();

-- Reversal (e.g. wrong patient / returned unopened): restores stock and
-- prescription progress in one transaction. Billing credit is handled by the
-- billing workflow (invoice line is voided before the invoice is issued, or a
-- refund is raised after payment).
CREATE OR REPLACE FUNCTION pharmacy.reverse_dispense(p_dispense uuid, p_reason text)
RETURNS void LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog AS $$
DECLARE r record; v_rx uuid;
BEGIN
  IF NOT sec.has_perm('pharmacy.dispense') THEN
    RAISE EXCEPTION 'permission pharmacy.dispense required' USING ERRCODE = '42501'; END IF;
  IF p_reason IS NULL OR length(btrim(p_reason)) < 5 THEN
    RAISE EXCEPTION 'reversal reason required' USING ERRCODE = 'SPV01'; END IF;
  UPDATE pharmacy.dispense SET status = 'reversed', reversal_reason = p_reason
   WHERE id = p_dispense AND status = 'completed' AND org_id = sec.current_org_id()
  RETURNING prescription_id INTO v_rx;
  IF NOT FOUND THEN RAISE EXCEPTION 'dispense not found or already reversed' USING ERRCODE = 'SPV01'; END IF;
  FOR r IN SELECT * FROM pharmacy.dispense_item WHERE dispense_id = p_dispense LOOP
    INSERT INTO pharmacy.stock_movement (org_id, batch_id, movement_type, quantity, dispense_item_id, reason)
    VALUES (r.org_id, r.batch_id, 'dispense_reversal', r.quantity, r.id, p_reason);
    UPDATE pharmacy.prescription_item
       SET quantity_dispensed = quantity_dispensed - r.quantity,
           status = CASE WHEN quantity_dispensed - r.quantity = 0 THEN 'pending' ELSE 'partially_dispensed' END
     WHERE id = r.prescription_item_id;
  END LOOP;
  UPDATE pharmacy.prescription p
     SET status = CASE WHEN EXISTS (SELECT 1 FROM pharmacy.prescription_item i
                                    WHERE i.prescription_id = p.id AND i.quantity_dispensed > 0)
                       THEN 'partially_dispensed' ELSE 'active' END
   WHERE p.id = v_rx;
END $$;
