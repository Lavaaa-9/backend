-- =============================================================================
-- 0013_scope_gaps_verification_nursing_procedures_staff_activity.sql
-- Closes the gaps found against "What We Are Building" (19 Nov 2026):
--   1. patient.verification         — "verification history" in the patient record (§3)
--   2. clinical.nursing_observation — nursing observations beyond triage (§4 Nursing)
--   3. clinical.procedure           — structured treatment/procedures performed, billable once (§2 stage 4)
--   4. reporting.staff_activity()   — management view of staff activity and access (§2 stage 8)
-- =============================================================================

-- Reference data ---------------------------------------------------------------
INSERT INTO ref.permission (code, resource, action, description, is_phi, is_privileged) VALUES
 ('nursing.write',          'nursing', 'write',          'Record nursing observations and procedures', true,  false),
 ('procedure.record',       'procedure', 'record',       'Record procedures performed in a visit',     true,  false),
 ('report.staff_activity',  'report',  'staff_activity', 'View staff activity and access summaries',   false, true)
ON CONFLICT (code) DO NOTHING;

INSERT INTO iam.role_permission (role_id, permission_code)
SELECT r.id, v.perm FROM (VALUES
  ('nurse','nursing.write'), ('nurse','procedure.record'),
  ('doctor','procedure.record'),
  ('manager','report.staff_activity'), ('compliance_officer','report.staff_activity'),
  ('administrator','report.staff_activity')
) AS v(role_code, perm) JOIN iam.role r ON r.org_id IS NULL AND r.code = v.role_code
ON CONFLICT DO NOTHING;

INSERT INTO ref.state_transition (entity, from_status, to_status, required_perm) VALUES
  ('clinical.nursing_observation', '_init', 'final', NULL),
  ('clinical.nursing_observation', 'final', 'entered_in_error', NULL),
  ('clinical.procedure', '_init', 'planned', NULL),
  ('clinical.procedure', '_init', 'performed', NULL),
  ('clinical.procedure', 'planned', 'performed', NULL),
  ('clinical.procedure', 'planned', 'cancelled', NULL),
  ('clinical.procedure', 'performed', 'entered_in_error', NULL)
ON CONFLICT DO NOTHING;

-- 1. Patient verification history ---------------------------------------------
-- Each time staff confirm who the patient is or that their details are current.
CREATE TABLE patient.verification (
  id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id        uuid NOT NULL,
  patient_id    uuid NOT NULL,
  encounter_id  uuid,                                   -- the visit at which it happened, if any
  identifier_id uuid,                                   -- the ID document checked, if any
  scope         text[] NOT NULL CHECK (cardinality(scope) >= 1 AND
                 scope <@ ARRAY['identity','contacts','next_of_kin','address','insurance','portal_link']::text[]),
  method        text NOT NULL CHECK (method IN ('id_document_seen','nida_lookup','insurance_card_seen',
                                                'otp_registered_phone','patient_stated','next_of_kin_confirmed')),
  outcome       text NOT NULL CHECK (outcome IN ('confirmed','updated','discrepancy')),
  note          text CHECK (length(note) <= 1000),
  verified_by   uuid NOT NULL DEFAULT sec.current_user_id() REFERENCES iam.user_account(id),
  verified_at   timestamptz NOT NULL DEFAULT now(),
  UNIQUE (org_id, id),
  FOREIGN KEY (org_id, patient_id) REFERENCES patient.patient (org_id, id),
  FOREIGN KEY (org_id, encounter_id, patient_id) REFERENCES clinical.encounter (org_id, id, patient_id),
  FOREIGN KEY (org_id, identifier_id) REFERENCES patient.identifier (org_id, id),
  CONSTRAINT ck_verif_discrepancy_note CHECK (outcome <> 'discrepancy' OR note IS NOT NULL),
  CONSTRAINT ck_verif_document CHECK (method NOT IN ('id_document_seen','nida_lookup') OR identifier_id IS NOT NULL)
);
COMMENT ON TABLE patient.verification IS 'Append-only history of identity/details confirmations (who, when, how, what).';
CREATE INDEX ix_verification_patient ON patient.verification (patient_id, verified_at DESC);
CREATE TRIGGER trg_verification_append_only BEFORE UPDATE OR DELETE ON patient.verification
  FOR EACH ROW EXECUTE FUNCTION sec.forbid_mutation();
SELECT audit.attach_capture('patient.verification');

CREATE OR REPLACE FUNCTION patient.verification_rules() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog AS $$
BEGIN
  IF NEW.identifier_id IS NOT NULL AND NOT EXISTS
     (SELECT 1 FROM patient.identifier WHERE id = NEW.identifier_id AND patient_id = NEW.patient_id) THEN
    RAISE EXCEPTION 'identifier belongs to a different patient' USING ERRCODE = 'SPX01';
  END IF;
  NEW.verified_at := now();                        -- server time, not client-supplied
  NEW.verified_by := coalesce(sec.current_user_id(), NEW.verified_by);
  RETURN NEW;
END $$;
CREATE TRIGGER trg_verification_rules BEFORE INSERT ON patient.verification
  FOR EACH ROW EXECUTE FUNCTION patient.verification_rules();

-- Latest verification per patient (for "last verified" badges on the profile).
CREATE VIEW patient.v_last_verification WITH (security_invoker = true) AS
SELECT DISTINCT ON (patient_id) org_id, patient_id, verified_at, verified_by, method, outcome, scope
FROM patient.verification ORDER BY patient_id, verified_at DESC;

-- 2. Nursing observations ----------------------------------------------------
CREATE TABLE clinical.nursing_observation (
  id                        uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id                    uuid NOT NULL,
  encounter_id              uuid NOT NULL,
  patient_id                uuid NOT NULL,
  recorded_by_membership_id uuid NOT NULL,
  observed_at               timestamptz NOT NULL DEFAULT now(),
  observation_type          text NOT NULL CHECK (observation_type IN ('general','pain_reassessment','wound',
                              'medication_administered','intake_output','fall_risk','patient_education','handover','other')),
  triage_assessment_id      uuid,                   -- repeat vitals recorded as a triage row, linked here
  note                      text NOT NULL CHECK (length(btrim(note)) BETWEEN 2 AND 4000),
  status                    text NOT NULL DEFAULT 'final' CHECK (status IN ('final','entered_in_error')),
  error_reason              text,
  created_at  timestamptz NOT NULL DEFAULT now(),
  created_by  uuid DEFAULT sec.current_user_id() REFERENCES iam.user_account(id),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  updated_by  uuid REFERENCES iam.user_account(id),
  row_version integer NOT NULL DEFAULT 1,
  UNIQUE (org_id, id),
  FOREIGN KEY (org_id, encounter_id, patient_id) REFERENCES clinical.encounter (org_id, id, patient_id),
  FOREIGN KEY (org_id, recorded_by_membership_id) REFERENCES iam.membership (org_id, id),
  FOREIGN KEY (org_id, triage_assessment_id) REFERENCES clinical.triage_assessment (org_id, id),
  CONSTRAINT ck_nobs_error CHECK (status <> 'entered_in_error' OR error_reason IS NOT NULL),
  CONSTRAINT ck_nobs_time CHECK (observed_at <= created_at + interval '5 minutes')   -- no future-dated notes
);
CREATE INDEX ix_nobs_encounter ON clinical.nursing_observation (encounter_id, observed_at);
CREATE INDEX ix_nobs_patient ON clinical.nursing_observation (patient_id, observed_at DESC);
SELECT sec.attach_standard_triggers('clinical.nursing_observation');
CREATE TRIGGER trg_nobs_protect BEFORE UPDATE ON clinical.nursing_observation
  FOR EACH ROW EXECUTE FUNCTION sec.protect_finalised('final,entered_in_error');
SELECT audit.attach_capture('clinical.nursing_observation');

CREATE OR REPLACE FUNCTION clinical.nursing_observation_rules() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.recorded_by_membership_id IS DISTINCT FROM sec.current_membership_id() AND NOT sec.is_internal()
     AND sec.actor_type() IS NOT NULL THEN
    RAISE EXCEPTION 'observations are recorded under the signed-in staff member' USING ERRCODE = 'SPV01';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER trg_nobs_rules BEFORE INSERT ON clinical.nursing_observation
  FOR EACH ROW EXECUTE FUNCTION clinical.nursing_observation_rules();

-- 3. Procedures / treatment performed -----------------------------------------
CREATE TABLE clinical.procedure (
  id                         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id                     uuid NOT NULL,
  encounter_id               uuid NOT NULL,
  patient_id                 uuid NOT NULL,
  consultation_id            uuid,                  -- the note that ordered it, if any
  service_id                 uuid,                  -- billable service (category 'procedure')
  procedure_code             text,                  -- local or ICD-10-PCS/CPT-style code, when adopted
  description                text NOT NULL CHECK (length(btrim(description)) >= 2),
  quantity                   numeric(8,2) NOT NULL DEFAULT 1 CHECK (quantity > 0),
  status                     text NOT NULL CHECK (status IN ('planned','performed','cancelled','entered_in_error')),
  ordered_by_membership_id   uuid,
  performed_by_membership_id uuid,
  performed_at               timestamptz,
  outcome_note               text CHECK (length(outcome_note) <= 4000),
  cancel_reason              text,
  error_reason               text,
  created_at  timestamptz NOT NULL DEFAULT now(),
  created_by  uuid DEFAULT sec.current_user_id() REFERENCES iam.user_account(id),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  updated_by  uuid REFERENCES iam.user_account(id),
  row_version integer NOT NULL DEFAULT 1,
  UNIQUE (org_id, id),
  UNIQUE (org_id, id, patient_id),
  FOREIGN KEY (org_id, encounter_id, patient_id) REFERENCES clinical.encounter (org_id, id, patient_id),
  FOREIGN KEY (org_id, consultation_id, patient_id) REFERENCES clinical.consultation (org_id, id, patient_id),
  FOREIGN KEY (org_id, service_id) REFERENCES billing.service_catalog (org_id, id),
  FOREIGN KEY (org_id, ordered_by_membership_id) REFERENCES iam.membership (org_id, id),
  FOREIGN KEY (org_id, performed_by_membership_id) REFERENCES iam.membership (org_id, id),
  CONSTRAINT ck_proc_performed CHECK (status NOT IN ('performed','entered_in_error')
                                      OR (performed_at IS NOT NULL AND performed_by_membership_id IS NOT NULL)),
  CONSTRAINT ck_proc_cancel CHECK (status <> 'cancelled' OR cancel_reason IS NOT NULL),
  CONSTRAINT ck_proc_error CHECK (status <> 'entered_in_error' OR error_reason IS NOT NULL)
);
CREATE INDEX ix_proc_encounter ON clinical.procedure (encounter_id);
CREATE INDEX ix_proc_patient ON clinical.procedure (patient_id, performed_at DESC);
CREATE INDEX ix_proc_unbilled ON clinical.procedure (org_id, performed_at) WHERE status = 'performed' AND service_id IS NOT NULL;
SELECT sec.attach_standard_triggers('clinical.procedure');
CREATE TRIGGER trg_proc_protect BEFORE UPDATE ON clinical.procedure
  FOR EACH ROW EXECUTE FUNCTION sec.protect_finalised('performed,cancelled,entered_in_error');
SELECT audit.attach_capture('clinical.procedure');

CREATE OR REPLACE FUNCTION clinical.procedure_rules() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog AS $$
DECLARE v_cat text;
BEGIN
  IF NEW.service_id IS NOT NULL THEN
    SELECT category INTO v_cat FROM billing.service_catalog WHERE id = NEW.service_id;
    IF v_cat IS DISTINCT FROM 'procedure' THEN
      RAISE EXCEPTION 'service must be a procedure-category service' USING ERRCODE = 'SPX01'; END IF;
  END IF;
  IF NEW.status = 'performed' AND (TG_OP = 'INSERT' OR OLD.status = 'planned') THEN
    NEW.performed_at := coalesce(NEW.performed_at, now());
    NEW.performed_by_membership_id := coalesce(NEW.performed_by_membership_id, sec.current_membership_id());
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER trg_proc_rules BEFORE INSERT OR UPDATE ON clinical.procedure
  FOR EACH ROW EXECUTE FUNCTION clinical.procedure_rules();

-- Bill procedures exactly once, like lab items and dispensing.
ALTER TABLE billing.invoice_line ADD COLUMN procedure_id uuid;
ALTER TABLE billing.invoice_line ADD CONSTRAINT fk_line_procedure
  FOREIGN KEY (org_id, procedure_id) REFERENCES clinical.procedure (org_id, id);
ALTER TABLE billing.invoice_line DROP CONSTRAINT ck_line_source;
ALTER TABLE billing.invoice_line ADD CONSTRAINT ck_line_source CHECK (
  (source_type = 'diagnostic'   AND diag_request_item_id IS NOT NULL AND num_nonnulls(consultation_id, dispense_item_id, procedure_id) = 0) OR
  (source_type = 'pharmacy'     AND dispense_item_id IS NOT NULL AND num_nonnulls(consultation_id, diag_request_item_id, procedure_id) = 0) OR
  (source_type = 'procedure'    AND procedure_id IS NOT NULL AND num_nonnulls(consultation_id, diag_request_item_id, dispense_item_id) = 0) OR
  (source_type = 'consultation' AND num_nonnulls(diag_request_item_id, dispense_item_id, procedure_id) = 0) OR
  (source_type IN ('registration','telemedicine','manual') AND num_nonnulls(consultation_id, diag_request_item_id, dispense_item_id, procedure_id) = 0));
CREATE UNIQUE INDEX ux_line_procedure ON billing.invoice_line (procedure_id) WHERE procedure_id IS NOT NULL AND NOT voided;

DROP FUNCTION billing.source_patient(uuid, uuid, uuid);
CREATE FUNCTION billing.source_patient(p_consultation uuid, p_diag_item uuid, p_dispense_item uuid, p_procedure uuid)
RETURNS uuid LANGUAGE sql STABLE SECURITY DEFINER SET search_path = pg_catalog AS $$
  SELECT CASE
    WHEN p_diag_item IS NOT NULL THEN (SELECT patient_id FROM diag.request_item WHERE id = p_diag_item)
    WHEN p_dispense_item IS NOT NULL THEN (SELECT d.patient_id FROM pharmacy.dispense_item di
                                           JOIN pharmacy.dispense d ON d.id = di.dispense_id WHERE di.id = p_dispense_item)
    WHEN p_procedure IS NOT NULL THEN (SELECT patient_id FROM clinical.procedure WHERE id = p_procedure AND status = 'performed')
    WHEN p_consultation IS NOT NULL THEN (SELECT patient_id FROM clinical.consultation WHERE id = p_consultation)
  END;
$$;

CREATE OR REPLACE FUNCTION billing.line_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE v_inv billing.invoice; v_src_patient uuid; v_row billing.invoice_line;
BEGIN
  v_row := coalesce(NEW, OLD);
  SELECT * INTO v_inv FROM billing.invoice WHERE id = v_row.invoice_id;
  IF sec.is_internal() THEN RETURN coalesce(NEW, OLD); END IF;
  IF v_inv.status <> 'draft' THEN
    RAISE EXCEPTION 'invoice % is %; lines can only change while draft', v_inv.id, v_inv.status USING ERRCODE = 'SPB01'; END IF;
  IF TG_OP <> 'DELETE' THEN
    v_src_patient := billing.source_patient(NEW.consultation_id, NEW.diag_request_item_id, NEW.dispense_item_id, NEW.procedure_id);
    IF NEW.procedure_id IS NOT NULL AND v_src_patient IS NULL THEN
      RAISE EXCEPTION 'only performed procedures can be billed' USING ERRCODE = 'SPB01'; END IF;
    IF v_src_patient IS NOT NULL AND v_src_patient <> v_inv.patient_id THEN
      RAISE EXCEPTION 'billed item belongs to a different patient' USING ERRCODE = 'SPX01'; END IF;
  END IF;
  RETURN coalesce(NEW, OLD);
END $$;

-- 4. Staff activity and access for management -----------------------------------
-- Aggregates only: managers see who did how much of what, never record content
-- or before/after values (those stay behind audit.read).
CREATE OR REPLACE FUNCTION reporting.staff_activity(p_from date, p_to date)
RETURNS TABLE (activity_date date, user_id uuid, staff_name text, category text, action text,
               events bigint, denied bigint, patients_touched bigint)
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path = pg_catalog AS $$
DECLARE v_org uuid := sec.current_org_id(); v_tz text;
BEGIN
  IF NOT sec.has_perm('report.staff_activity') THEN
    RAISE EXCEPTION 'permission report.staff_activity required' USING ERRCODE = '42501'; END IF;
  IF p_to < p_from OR p_to - p_from > 366 THEN
    RAISE EXCEPTION 'date range must be 0-366 days' USING ERRCODE = 'SPV01'; END IF;
  SELECT timezone INTO v_tz FROM org.organization WHERE id = v_org;
  PERFORM audit.log('report.staff_activity.view', 'administration', 'success',
                    p_changes => jsonb_build_object('from', p_from, 'to', p_to));
  RETURN QUERY
  SELECT (e.occurred_at AT TIME ZONE v_tz)::date, e.actor_user_id, u.display_name, e.category, e.action,
         count(*), count(*) FILTER (WHERE e.outcome IN ('denied','failure')), count(DISTINCT e.patient_id)
  FROM audit.event e LEFT JOIN iam.user_account u ON u.id = e.actor_user_id
  WHERE e.org_id = v_org
    AND e.occurred_at >= (p_from::timestamp AT TIME ZONE v_tz)
    AND e.occurred_at <  ((p_to + 1)::timestamp AT TIME ZONE v_tz)
  GROUP BY 1, 2, 3, 4, 5
  ORDER BY 1, 3, 4, 5;
END $$;

CREATE OR REPLACE FUNCTION reporting.staff_access()
RETURNS TABLE (membership_id uuid, user_id uuid, staff_name text, job_title text, membership_status text,
               roles text[], privileged_permissions text[], mfa_enabled boolean,
               last_login_at timestamptz, open_break_glass bigint)
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path = pg_catalog AS $$
DECLARE v_org uuid := sec.current_org_id();
BEGIN
  IF NOT sec.has_perm('report.staff_activity') THEN
    RAISE EXCEPTION 'permission report.staff_activity required' USING ERRCODE = '42501'; END IF;
  RETURN QUERY
  SELECT m.id, u.id, u.display_name, m.job_title, m.status,
         (SELECT array_agg(DISTINCT r.code ORDER BY r.code) FROM iam.membership_role mr JOIN iam.role r ON r.id = mr.role_id
           WHERE mr.membership_id = m.id AND mr.revoked_at IS NULL AND (mr.expires_at IS NULL OR mr.expires_at > now())),
         (SELECT array_agg(p ORDER BY p) FROM unnest(iam.effective_permissions(m.id)) p
           JOIN ref.permission rp ON rp.code = p WHERE rp.is_privileged),
         EXISTS (SELECT 1 FROM iam.mfa_factor f WHERE f.user_id = u.id AND f.status = 'active'),
         u.last_login_at,
         (SELECT count(*) FROM iam.break_glass_access b WHERE b.membership_id = m.id AND b.reviewed_at IS NULL)
  FROM iam.membership m JOIN iam.user_account u ON u.id = m.user_id
  WHERE m.org_id = v_org
  ORDER BY u.display_name;
END $$;

-- Security: RLS, grants ------------------------------------------------------
SELECT sec.apply_rls('patient.verification',          '{patient.read}',                          '{patient.create,patient.update}',            false, true, 'patient_id');
SELECT sec.apply_rls('clinical.nursing_observation',  '{clinical.read}',                         '{nursing.write}',                            false, true, 'patient_id');
SELECT sec.apply_rls('clinical.procedure',            '{clinical.read,billing.invoice.manage}',  '{procedure.record,consultation.write}',      false, true, 'patient_id');

GRANT SELECT, INSERT ON patient.verification TO spartial_app;
GRANT SELECT ON patient.v_last_verification TO spartial_app;
GRANT SELECT, INSERT, UPDATE ON clinical.nursing_observation, clinical.procedure TO spartial_app;
GRANT EXECUTE ON FUNCTION billing.source_patient(uuid, uuid, uuid, uuid),
                          reporting.staff_activity(date, date), reporting.staff_access() TO spartial_app;
