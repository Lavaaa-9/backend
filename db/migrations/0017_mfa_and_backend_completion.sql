-- =============================================================================
-- 0017_mfa_and_backend_completion.sql
-- Completes schema support for the remaining backend modules:
--   1. iam.mfa_challenge            — short-lived pre-session MFA challenges
--   2. gov.approval_request         — dual-control (four-eyes) approval workflow
--   3. gov.privacy_alert            — privacy/security anomaly alerts & detector
--   4. iam.break_glass_rules        — four-eyes break-glass review guard
--   5. patient.merge_patients()     — audited patient merge procedure
--   6. Missing role grants & worker schema_migration/outbox permissions
-- =============================================================================

-- 1. MFA login challenges & recovery-code rotation ----------------------------
CREATE TABLE iam.mfa_challenge (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id      uuid NOT NULL REFERENCES iam.user_account(id),
  token_hash   bytea NOT NULL UNIQUE CHECK (length(token_hash) = 32),
  user_version integer NOT NULL,
  client_type  text NOT NULL CHECK (client_type IN ('web','mobile')),
  device_label text CHECK (length(device_label) <= 512),
  attempts     smallint NOT NULL DEFAULT 0 CHECK (attempts BETWEEN 0 AND 10),
  expires_at   timestamptz NOT NULL,
  consumed_at  timestamptz,
  created_at   timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT ck_mfa_challenge_ttl CHECK (expires_at > created_at AND expires_at <= created_at + interval '15 minutes')
);
CREATE INDEX ix_mfa_challenge_user ON iam.mfa_challenge (user_id, created_at DESC) WHERE consumed_at IS NULL;
GRANT SELECT, INSERT, UPDATE ON iam.mfa_challenge TO spartial_app;
GRANT DELETE ON iam.mfa_recovery_code TO spartial_app;

-- 2. Permissions & system role assignments ------------------------------------
INSERT INTO ref.permission (code, resource, action, description, is_phi, is_privileged) VALUES
  ('approval.request', 'approval', 'request', 'Submit dual-control approval requests', false, false),
  ('approval.review',  'approval', 'review',  'Approve or reject dual-control approval requests', false, true)
ON CONFLICT (code) DO NOTHING;

INSERT INTO iam.role_permission (role_id, permission_code)
SELECT r.id, v.perm FROM (VALUES
  ('administrator',      'approval.request'),
  ('administrator',      'approval.review'),
  ('receptionist',       'approval.request'),
  ('receptionist',       'patient.identifier.reveal'),
  ('nurse',              'approval.request'),
  ('doctor',             'approval.request'),
  ('doctor',             'patient.identifier.reveal'),
  ('lab_technician',     'approval.request'),
  ('lab_supervisor',     'approval.request'),
  ('pharmacist',         'approval.request'),
  ('cashier',            'approval.request'),
  ('billing_supervisor', 'approval.request'),
  ('billing_supervisor', 'approval.review'),
  ('billing_supervisor', 'patient.identifier.reveal'),
  ('manager',            'approval.request'),
  ('manager',            'approval.review'),
  ('compliance_officer', 'approval.request'),
  ('compliance_officer', 'approval.review'),
  ('compliance_officer', 'patient.identifier.reveal'),
  ('compliance_officer', 'patient.merge'),
  ('compliance_officer', 'document.delete')
) AS v(role_code, perm) JOIN iam.role r ON r.org_id IS NULL AND r.code = v.role_code
ON CONFLICT DO NOTHING;

INSERT INTO ref.state_transition (entity, from_status, to_status, required_perm) VALUES
  ('gov.approval_request', '_init',        'pending',        NULL),
  ('gov.approval_request', 'pending',      'approved',       'approval.review'),
  ('gov.approval_request', 'pending',      'rejected',       'approval.review'),
  ('gov.privacy_alert',    '_init',        'open',           NULL),
  ('gov.privacy_alert',    'open',         'acknowledged',   NULL),
  ('gov.privacy_alert',    'open',         'resolved',       NULL),
  ('gov.privacy_alert',    'open',         'false_positive', NULL),
  ('gov.privacy_alert',    'acknowledged', 'resolved',       NULL),
  ('gov.privacy_alert',    'acknowledged', 'false_positive', NULL)
ON CONFLICT DO NOTHING;

-- 3. Dual-control approvals ---------------------------------------------------
CREATE TABLE gov.approval_request (
  id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id        uuid NOT NULL REFERENCES org.organization(id),
  facility_id   uuid,
  action_type   text NOT NULL CHECK (action_type IN ('patient_merge','document_delete','billing_adjustment','role_grant','data_export','other')),
  resource_type text NOT NULL CHECK (length(btrim(resource_type)) BETWEEN 2 AND 80),
  resource_id   uuid NOT NULL,
  reason        text NOT NULL CHECK (length(btrim(reason)) >= 10 AND length(reason) <= 1000),
  payload       jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(payload) = 'object' AND pg_column_size(payload) < 8192),
  status        text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','approved','rejected')),
  requested_by  uuid NOT NULL DEFAULT sec.current_user_id() REFERENCES iam.user_account(id),
  decided_by    uuid REFERENCES iam.user_account(id),
  decided_at    timestamptz,
  decision_note text CHECK (length(decision_note) <= 1000),
  created_at    timestamptz NOT NULL DEFAULT now(),
  created_by    uuid DEFAULT sec.current_user_id() REFERENCES iam.user_account(id),
  updated_at    timestamptz NOT NULL DEFAULT now(),
  updated_by    uuid REFERENCES iam.user_account(id),
  row_version   integer NOT NULL DEFAULT 1,
  UNIQUE (org_id, id),
  FOREIGN KEY (org_id, facility_id) REFERENCES org.facility (org_id, id),
  CONSTRAINT ck_approval_four_eyes CHECK (decided_by IS NULL OR decided_by <> requested_by),
  CONSTRAINT ck_approval_decided CHECK ((status = 'pending') = (decided_at IS NULL AND decided_by IS NULL))
);
CREATE INDEX ix_approval_org_status ON gov.approval_request (org_id, status, created_at DESC);
SELECT sec.attach_standard_triggers('gov.approval_request');
SELECT audit.attach_capture('gov.approval_request');

CREATE OR REPLACE FUNCTION gov.approval_rules() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'UPDATE' AND NOT sec.is_internal() THEN
    IF (NEW.action_type, NEW.resource_type, NEW.resource_id, NEW.reason, NEW.requested_by, NEW.facility_id)
       IS DISTINCT FROM (OLD.action_type, OLD.resource_type, OLD.resource_id, OLD.reason, OLD.requested_by, OLD.facility_id) THEN
      RAISE EXCEPTION 'approval request details are immutable' USING ERRCODE = 'SPI01';
    END IF;
    IF NEW.status IN ('approved','rejected') AND OLD.status = 'pending' THEN
      NEW.decided_by := coalesce(sec.current_user_id(), NEW.decided_by);
      NEW.decided_at := now();
      IF NEW.decided_by = OLD.requested_by THEN
        RAISE EXCEPTION 'approver must be different from requester' USING ERRCODE = 'SPV01';
      END IF;
    END IF;
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER trg_approval_rules BEFORE UPDATE ON gov.approval_request
  FOR EACH ROW EXECUTE FUNCTION gov.approval_rules();

SELECT sec.apply_rls('gov.approval_request', '{approval.request,approval.review,audit.read}', '{approval.request,approval.review}', false, true, NULL);
GRANT SELECT, INSERT, UPDATE ON gov.approval_request TO spartial_app;

-- 4. Privacy anomaly alerts ---------------------------------------------------
CREATE TABLE gov.privacy_alert (
  id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id        uuid NOT NULL REFERENCES org.organization(id),
  alert_type    text NOT NULL CHECK (alert_type IN ('high_phi_read_volume','repeated_mfa_failures','unreviewed_break_glass','token_reuse')),
  actor_user_id uuid REFERENCES iam.user_account(id),
  severity      text NOT NULL DEFAULT 'medium' CHECK (severity IN ('low','medium','high')),
  event_count   integer NOT NULL DEFAULT 1 CHECK (event_count >= 1),
  window_start  timestamptz NOT NULL,
  window_end    timestamptz NOT NULL,
  summary       text NOT NULL CHECK (length(btrim(summary)) BETWEEN 3 AND 1000),
  status        text NOT NULL DEFAULT 'open' CHECK (status IN ('open','acknowledged','resolved','false_positive')),
  reviewed_by   uuid REFERENCES iam.user_account(id),
  reviewed_at   timestamptz,
  review_note   text CHECK (length(review_note) <= 1000),
  created_at    timestamptz NOT NULL DEFAULT now(),
  created_by    uuid DEFAULT sec.current_user_id() REFERENCES iam.user_account(id),
  updated_at    timestamptz NOT NULL DEFAULT now(),
  updated_by    uuid REFERENCES iam.user_account(id),
  row_version   integer NOT NULL DEFAULT 1,
  UNIQUE (org_id, id),
  CONSTRAINT ck_alert_window CHECK (window_end >= window_start)
);
CREATE UNIQUE INDEX ux_privacy_alert_dedupe ON gov.privacy_alert
  (org_id, alert_type, coalesce(actor_user_id, '00000000-0000-0000-0000-000000000000'::uuid), window_start);
CREATE INDEX ix_privacy_alert_open ON gov.privacy_alert (org_id, status, created_at DESC);
SELECT sec.attach_standard_triggers('gov.privacy_alert');
SELECT audit.attach_capture('gov.privacy_alert');

SELECT sec.apply_rls('gov.privacy_alert', '{audit.read,privacy.request.manage}', '{audit.read,privacy.request.manage}', false, true, NULL);
CREATE POLICY worker_all ON gov.privacy_alert TO spartial_worker USING (true) WITH CHECK (true);
GRANT SELECT, INSERT, UPDATE ON gov.privacy_alert TO spartial_app, spartial_worker;

CREATE OR REPLACE FUNCTION gov.detect_privacy_alerts(p_read_threshold int DEFAULT 100, p_mfa_threshold int DEFAULT 5)
RETURNS integer LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog AS $$
DECLARE
  v_win_start timestamptz := date_trunc('hour', now());
  v_win_end   timestamptz := v_win_start + interval '1 hour';
  v_total     integer := 0;
  v_c         integer := 0;
BEGIN
  IF NOT (current_user = 'spartial_owner' OR pg_has_role(current_user, 'spartial_worker', 'MEMBER')
          OR sec.has_any_perm(ARRAY['audit.read','privacy.request.manage'])) THEN
    RAISE EXCEPTION 'permission denied for privacy alert detection' USING ERRCODE = '42501';
  END IF;

  IF p_read_threshold > 0 THEN
    INSERT INTO gov.privacy_alert (org_id, alert_type, actor_user_id, severity, event_count, window_start, window_end, summary)
    SELECT e.org_id, 'high_phi_read_volume', e.actor_user_id, 'high', count(*)::integer, v_win_start, v_win_end,
           format('User recorded %s PHI access events in current hour (threshold %s)', count(*), p_read_threshold)
    FROM audit.event e
    WHERE e.org_id IS NOT NULL AND e.actor_user_id IS NOT NULL
      AND e.category = 'phi_access' AND e.occurred_at >= v_win_start AND e.occurred_at < v_win_end
    GROUP BY e.org_id, e.actor_user_id
    HAVING count(*) >= p_read_threshold
    ON CONFLICT DO NOTHING;
    GET DIAGNOSTICS v_c = ROW_COUNT; v_total := v_total + v_c;
  END IF;

  IF p_mfa_threshold > 0 THEN
    INSERT INTO gov.privacy_alert (org_id, alert_type, actor_user_id, severity, event_count, window_start, window_end, summary)
    SELECT e.org_id, 'repeated_mfa_failures', e.actor_user_id, 'high', count(*)::integer, v_win_start, v_win_end,
           format('User had %s denied authentication/MFA attempts in current hour (threshold %s)', count(*), p_mfa_threshold)
    FROM audit.event e
    WHERE e.org_id IS NOT NULL AND e.actor_user_id IS NOT NULL
      AND e.action IN ('auth.mfa_verify','auth.login') AND e.outcome = 'denied'
      AND e.occurred_at >= v_win_start AND e.occurred_at < v_win_end
    GROUP BY e.org_id, e.actor_user_id
    HAVING count(*) >= p_mfa_threshold
    ON CONFLICT DO NOTHING;
    GET DIAGNOSTICS v_c = ROW_COUNT; v_total := v_total + v_c;
  END IF;

  INSERT INTO gov.privacy_alert (org_id, alert_type, actor_user_id, severity, event_count, window_start, window_end, summary)
  SELECT b.org_id, 'unreviewed_break_glass', m.user_id, 'medium', count(*)::integer, v_win_start, v_win_end,
         format('%s unreviewed break-glass emergency access grant(s) pending compliance review', count(*))
  FROM iam.break_glass_access b
  JOIN iam.membership m ON m.id = b.membership_id
  WHERE b.reviewed_at IS NULL
  GROUP BY b.org_id, m.user_id
  ON CONFLICT DO NOTHING;
  GET DIAGNOSTICS v_c = ROW_COUNT; v_total := v_total + v_c;

  RETURN v_total;
END $$;
GRANT EXECUTE ON FUNCTION gov.detect_privacy_alerts(int, int) TO spartial_app, spartial_worker;

-- 5. Break-glass four-eyes review guard ---------------------------------------
CREATE OR REPLACE FUNCTION iam.break_glass_rules() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog AS $$
DECLARE v_owner uuid;
BEGIN
  IF (NEW.org_id, NEW.membership_id, NEW.patient_id, NEW.reason, NEW.started_at, NEW.expires_at)
     IS DISTINCT FROM (OLD.org_id, OLD.membership_id, OLD.patient_id, OLD.reason, OLD.started_at, OLD.expires_at) THEN
    RAISE EXCEPTION 'break-glass grant metadata is immutable' USING ERRCODE = 'SPI01';
  END IF;
  IF (NEW.reviewed_at IS DISTINCT FROM OLD.reviewed_at) OR (NEW.review_outcome IS DISTINCT FROM OLD.review_outcome)
     OR (NEW.review_note IS DISTINCT FROM OLD.review_note) THEN
    IF NOT sec.has_perm('audit.read') AND NOT sec.is_internal() THEN
      RAISE EXCEPTION 'permission audit.read required to review break-glass access' USING ERRCODE = '42501';
    END IF;
    SELECT user_id INTO v_owner FROM iam.membership WHERE id = NEW.membership_id;
    NEW.reviewed_by := coalesce(sec.current_user_id(), NEW.reviewed_by);
    IF NEW.reviewed_by = v_owner THEN
      RAISE EXCEPTION 'users cannot review their own break-glass access' USING ERRCODE = 'SPV01';
    END IF;
    NEW.reviewed_at := coalesce(NEW.reviewed_at, now());
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER trg_break_glass_rules BEFORE UPDATE ON iam.break_glass_access
  FOR EACH ROW EXECUTE FUNCTION iam.break_glass_rules();

-- 6. Patient merge helper (atomic, audited, permission-checked) ---------------
CREATE OR REPLACE FUNCTION patient.merge_patients(
  p_survivor uuid, p_merged uuid, p_reason text, p_review_id uuid DEFAULT NULL)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog AS $$
DECLARE
  v_org uuid := sec.current_org_id();
  v_surv patient.patient;
  v_merg patient.patient;
  v_counts jsonb;
  v_event patient.merge_event;
BEGIN
  IF NOT sec.has_perm('patient.merge') THEN
    RAISE EXCEPTION 'permission patient.merge required' USING ERRCODE = '42501';
  END IF;
  IF p_survivor = p_merged THEN
    RAISE EXCEPTION 'cannot merge a patient record into itself' USING ERRCODE = 'SPV01';
  END IF;
  IF p_reason IS NULL OR length(btrim(p_reason)) < 10 THEN
    RAISE EXCEPTION 'merge reason must be at least 10 characters' USING ERRCODE = 'SPV01';
  END IF;

  -- Lock in deterministic UUID order to prevent deadlocks.
  PERFORM 1 FROM patient.patient WHERE org_id = v_org AND id IN (p_survivor, p_merged) ORDER BY id FOR UPDATE;
  SELECT * INTO v_surv FROM patient.patient WHERE org_id = v_org AND id = p_survivor;
  SELECT * INTO v_merg FROM patient.patient WHERE org_id = v_org AND id = p_merged;
  IF v_surv.id IS NULL OR v_merg.id IS NULL THEN
    RAISE EXCEPTION 'patient not found' USING ERRCODE = 'SPV01';
  END IF;
  IF v_surv.status = 'merged' OR v_merg.status = 'merged' THEN
    RAISE EXCEPTION 'patient record is already merged' USING ERRCODE = 'SPV01';
  END IF;

  SELECT jsonb_build_object(
    'encounters', (SELECT count(*) FROM clinical.encounter WHERE patient_id = p_merged),
    'appointments', (SELECT count(*) FROM clinical.appointment WHERE patient_id = p_merged),
    'documents', (SELECT count(*) FROM document.document WHERE patient_id = p_merged),
    'invoices', (SELECT count(*) FROM billing.invoice WHERE patient_id = p_merged),
    'identifiers', (SELECT count(*) FROM patient.identifier WHERE patient_id = p_merged)
  ) INTO v_counts;

  UPDATE patient.patient
     SET status = 'merged', merged_into_patient_id = p_survivor
   WHERE id = p_merged AND org_id = v_org;

  IF p_review_id IS NOT NULL THEN
    UPDATE patient.duplicate_review
       SET status = 'merged', resolved_by = sec.current_user_id(), resolved_at = now(), resolution_note = btrim(p_reason)
     WHERE id = p_review_id AND org_id = v_org AND status IN ('open','confirmed_duplicate');
  END IF;

  INSERT INTO patient.merge_event (org_id, survivor_patient_id, merged_patient_id, duplicate_review_id, reason, moved_record_counts)
  VALUES (v_org, p_survivor, p_merged, p_review_id, btrim(p_reason), v_counts)
  RETURNING * INTO v_event;

  PERFORM audit.log('patient.record.merge', 'phi_access', 'success', 'patient', p_merged, p_survivor, btrim(p_reason));
  RETURN to_jsonb(v_event) - 'org_id';
END $$;
GRANT EXECUTE ON FUNCTION patient.merge_patients(uuid, uuid, text, uuid) TO spartial_app;

-- 7. Worker & app support grants ----------------------------------------------
GRANT SELECT ON public.schema_migration TO spartial_worker;
GRANT SELECT ON comms.outbox TO spartial_app;
CREATE POLICY outbox_staff_read ON comms.outbox FOR SELECT TO spartial_app
  USING (org_id = sec.current_org_id() AND sec.has_perm('comms.read'));
