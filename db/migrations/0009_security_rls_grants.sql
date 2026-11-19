-- =============================================================================
-- 0009_security_rls_grants.sql
-- Request bootstrap, Row-Level Security (tenant + permission + patient-self),
-- and least-privilege grants.
--
-- Layering (defence in depth):
--   1. Go/Gin middleware: authn, permission check per route, input validation.
--   2. sec.begin_request(): DB validates the session and derives org, actor,
--      permissions and break-glass grants itself — the API does not assert them.
--   3. RLS: RESTRICTIVE tenant isolation on every tenant table +
--      PERMISSIVE per-permission policies + patient-self policies.
--   4. Triggers/constraints: business invariants regardless of caller.
-- =============================================================================

-- Counter guard: callers may only advance their own tenant's counters.
CREATE OR REPLACE FUNCTION org.next_value(p_org uuid, p_key text, p_period text DEFAULT '')
RETURNS bigint LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog AS $$
DECLARE v bigint;
BEGIN
  IF p_org IS DISTINCT FROM sec.current_org_id() THEN
    RAISE EXCEPTION 'counter access outside current organization' USING ERRCODE = '42501';
  END IF;
  INSERT INTO org.counter AS c (org_id, counter_key, period_key, next_value)
  VALUES (p_org, p_key, p_period, 2)
  ON CONFLICT (org_id, counter_key, period_key) DO UPDATE SET next_value = c.next_value + 1
  RETURNING c.next_value - 1 INTO v;
  RETURN v;
END $$;

-- Business document numbers for the API (lab requests, prescriptions).
CREATE OR REPLACE FUNCTION org.next_number(p_kind text) RETURNS text
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog AS $$
DECLARE v_org uuid := sec.current_org_id(); v_year text;
BEGIN
  IF p_kind NOT IN ('lab_request','prescription') THEN
    RAISE EXCEPTION 'unknown number kind %', p_kind USING ERRCODE = 'SPV01'; END IF;
  v_year := to_char(org.local_date(v_org), 'YYYY');
  RETURN CASE p_kind WHEN 'lab_request' THEN 'LAB-' ELSE 'RX-' END || v_year || '-' ||
         lpad(org.next_value(v_org, p_kind, v_year)::text, 6, '0');
END $$;

-- Invoker triggers that read across permission boundaries become definers.
ALTER FUNCTION clinical.validate_encounter() SECURITY DEFINER SET search_path = pg_catalog;
ALTER FUNCTION telehealth.session_rules()   SECURITY DEFINER SET search_path = pg_catalog;
ALTER FUNCTION diag.result_rules()          SECURITY DEFINER SET search_path = pg_catalog;
ALTER FUNCTION billing.shift_rules()        SECURITY DEFINER SET search_path = pg_catalog;

-- -----------------------------------------------------------------------------
-- Login helper: organisations a user may sign in to (before tenant context).
-- -----------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION iam.memberships_for_login(p_user uuid)
RETURNS TABLE (membership_id uuid, org_id uuid, org_name text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path = pg_catalog AS $$
  SELECT m.id, o.id, o.display_name
  FROM iam.membership m JOIN org.organization o ON o.id = m.org_id
  WHERE m.user_id = p_user AND m.status = 'active' AND o.status = 'active';
$$;

CREATE OR REPLACE FUNCTION patient.portal_links_for_login(p_user uuid)
RETURNS TABLE (portal_account_id uuid, org_id uuid, patient_id uuid, access_type text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path = pg_catalog AS $$
  SELECT pa.id, pa.org_id, pa.patient_id, pa.access_type
  FROM patient.portal_account pa JOIN org.organization o ON o.id = pa.org_id
  WHERE pa.user_id = p_user AND pa.status = 'active' AND o.status = 'active';
$$;

-- -----------------------------------------------------------------------------
-- sec.begin_request — first statement of every API transaction.
-- -----------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION sec.begin_request(p_session_id uuid, p_request_id uuid DEFAULT NULL)
RETURNS TABLE (user_id uuid, org_id uuid, membership_id uuid, patient_id uuid, actor_type text, permissions text[])
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog AS $$
DECLARE
  s iam.session; u iam.user_account; v_actor text; v_perms text[] := '{}'; v_bg text;
BEGIN
  SELECT * INTO s FROM iam.session WHERE id = p_session_id;
  IF NOT FOUND OR s.revoked_at IS NOT NULL OR now() >= s.idle_expires_at OR now() >= s.absolute_expires_at THEN
    RAISE EXCEPTION 'session invalid or expired' USING ERRCODE = '28000';
  END IF;
  SELECT * INTO u FROM iam.user_account WHERE id = s.user_id;
  IF u.status <> 'active' OR (u.locked_until IS NOT NULL AND u.locked_until > now()) THEN
    RAISE EXCEPTION 'account not active' USING ERRCODE = '28000';
  END IF;
  PERFORM set_config('app.org_id', s.org_id::text, true);   -- needed early for tenant-scoped lookups
  IF NOT EXISTS (SELECT 1 FROM org.organization WHERE id = s.org_id AND status = 'active') THEN
    RAISE EXCEPTION 'organization not active' USING ERRCODE = '28000';
  END IF;

  IF u.account_type IN ('staff','service') THEN
    IF NOT EXISTS (SELECT 1 FROM iam.membership m WHERE m.id = s.membership_id AND m.user_id = u.id
                   AND m.org_id = s.org_id AND m.status = 'active') THEN
      RAISE EXCEPTION 'membership not active' USING ERRCODE = '28000';
    END IF;
    IF u.account_type = 'staff' AND u.mfa_required AND s.mfa_verified_at IS NULL THEN
      RAISE EXCEPTION 'multi-factor authentication required' USING ERRCODE = '28000';
    END IF;
    v_actor := u.account_type;
    v_perms := iam.effective_permissions(s.membership_id);
    SELECT string_agg(b.patient_id::text, ',') INTO v_bg
      FROM iam.break_glass_access b
     WHERE b.membership_id = s.membership_id AND b.ended_at IS NULL AND b.expires_at > now();
  ELSIF u.account_type = 'patient' THEN
    IF NOT EXISTS (SELECT 1 FROM patient.portal_account pa WHERE pa.user_id = u.id AND pa.patient_id = s.patient_id
                   AND pa.org_id = s.org_id AND pa.status = 'active') THEN
      RAISE EXCEPTION 'portal access not active' USING ERRCODE = '28000';
    END IF;
    v_actor := 'patient';
  END IF;

  PERFORM set_config('app.org_id',        s.org_id::text, true);
  PERFORM set_config('app.user_id',       u.id::text, true);
  PERFORM set_config('app.membership_id', coalesce(s.membership_id::text, ''), true);
  PERFORM set_config('app.patient_id',    coalesce(s.patient_id::text, ''), true);
  PERFORM set_config('app.session_id',    s.id::text, true);
  PERFORM set_config('app.request_id',    coalesce(p_request_id, gen_random_uuid())::text, true);
  PERFORM set_config('app.actor_type',    v_actor, true);
  PERFORM set_config('app.perms',         array_to_string(v_perms, ','), true);
  PERFORM set_config('app.break_glass',   coalesce(v_bg, ''), true);

  -- sliding idle timeout (HIPAA automatic logoff); throttled to limit writes
  UPDATE iam.session ss SET last_seen_at = now(),
         idle_expires_at = least(now() + ss.idle_timeout, ss.absolute_expires_at)
   WHERE ss.id = s.id AND ss.last_seen_at < now() - interval '30 seconds';

  RETURN QUERY SELECT u.id, s.org_id, s.membership_id, s.patient_id, v_actor, v_perms;
END $$;

-- Worker context for a tenant job (outbox delivery, payment callback). Callable
-- only by spartial_worker; permissions limited to the worker's allow-list.
CREATE OR REPLACE FUNCTION sec.begin_system_request(p_org uuid, p_request_id uuid, p_perms text[])
RETURNS void LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog AS $$
DECLARE v_allowed text[] := ARRAY['billing.read','billing.payment.record','appointment.read','comms.read',
                                  'patient.read','telehealth.conduct'];
BEGIN
  IF NOT (p_perms <@ v_allowed) THEN
    RAISE EXCEPTION 'worker permission outside allow-list' USING ERRCODE = '42501'; END IF;
  PERFORM set_config('app.org_id', p_org::text, true);
  PERFORM set_config('app.user_id', '', true);
  PERFORM set_config('app.membership_id', '', true);
  PERFORM set_config('app.patient_id', '', true);
  PERFORM set_config('app.request_id', coalesce(p_request_id, gen_random_uuid())::text, true);
  PERFORM set_config('app.actor_type', 'service', true);
  PERFORM set_config('app.perms', array_to_string(p_perms, ','), true);
  PERFORM set_config('app.break_glass', '', true);
END $$;

-- Break-glass: open (audited) and close.
CREATE OR REPLACE FUNCTION iam.open_break_glass(p_patient uuid, p_reason text, p_minutes int DEFAULT 60)
RETURNS uuid LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog AS $$
DECLARE v_id uuid;
BEGIN
  IF NOT sec.has_perm('break_glass.use') THEN
    PERFORM audit.log('iam.break_glass.open', 'authorization', 'denied', 'patient', p_patient, p_patient, p_reason);
    RAISE EXCEPTION 'permission break_glass.use required' USING ERRCODE = '42501'; END IF;
  INSERT INTO iam.break_glass_access (org_id, membership_id, patient_id, reason, expires_at)
  VALUES (sec.current_org_id(), sec.current_membership_id(), p_patient, p_reason,
          now() + make_interval(mins => least(greatest(p_minutes, 5), 240)))
  RETURNING id INTO v_id;
  PERFORM audit.log('iam.break_glass.open', 'phi_access', 'success', 'patient', p_patient, p_patient, p_reason);
  RETURN v_id;
END $$;

-- -----------------------------------------------------------------------------
-- RLS
-- -----------------------------------------------------------------------------
-- Generic policy builder.
--   p_read  : permissions that allow SELECT (NULL = any authenticated staff)
--   p_write : permissions that allow INSERT/UPDATE (NULL = none via RLS)
--   p_portal: patients may SELECT their own rows (patient_id = current patient)
--   p_force : FORCE RLS (owner also subject; PHI/business tables)
CREATE OR REPLACE FUNCTION sec.apply_rls(p_table regclass, p_read text[], p_write text[],
                                         p_portal boolean, p_force boolean DEFAULT true,
                                         p_patient_col text DEFAULT 'patient_id')
RETURNS void LANGUAGE plpgsql AS $$
DECLARE v_read text; v_write text;
BEGIN
  EXECUTE format('ALTER TABLE %s ENABLE ROW LEVEL SECURITY', p_table);
  IF p_force THEN EXECUTE format('ALTER TABLE %s FORCE ROW LEVEL SECURITY', p_table); END IF;

  EXECUTE format('CREATE POLICY tenant_isolation ON %s AS RESTRICTIVE TO PUBLIC
                    USING (org_id = sec.current_org_id()) WITH CHECK (org_id = sec.current_org_id())', p_table);
  EXECUTE format('CREATE POLICY owner_internal ON %s TO spartial_owner USING (true) WITH CHECK (true)', p_table);

  v_read := CASE WHEN p_read IS NULL THEN 'sec.is_staff()'
                 ELSE format('sec.is_staff() AND sec.has_any_perm(%L::text[])', p_read) END;
  -- break-glass widens clinical read access to the specific patient only
  IF p_read IS NOT NULL AND 'clinical.read' = ANY (p_read) AND p_patient_col IS NOT NULL THEN
    v_read := format('(%s) OR (sec.is_staff() AND %I = ANY (sec.break_glass_patient_ids()))', v_read, p_patient_col);
  END IF;
  EXECUTE format('CREATE POLICY staff_select ON %s FOR SELECT TO spartial_app, spartial_worker USING (%s)', p_table, v_read);

  IF p_write IS NOT NULL THEN
    v_write := format('sec.is_staff() AND sec.has_any_perm(%L::text[])', p_write);
    EXECUTE format('CREATE POLICY staff_insert ON %s FOR INSERT TO spartial_app, spartial_worker WITH CHECK (%s)', p_table, v_write);
    EXECUTE format('CREATE POLICY staff_update ON %s FOR UPDATE TO spartial_app, spartial_worker USING (%s) WITH CHECK (%s)', p_table, v_write, v_write);
  END IF;

  IF p_portal THEN
    EXECUTE format('CREATE POLICY patient_self ON %s FOR SELECT TO spartial_app
                      USING (sec.actor_type() = %L AND %I = sec.current_patient_id())', p_table, 'patient', p_patient_col);
  END IF;
END $$;

DO $$
DECLARE r record;
BEGIN
  FOR r IN SELECT * FROM (VALUES
    -- table                              read perms                                                    write perms                                                  portal  force  patient col
    ('org.facility',                  NULL,                                                              '{admin.facility.manage}',                                   false, false, NULL),
    ('org.department',                NULL,                                                              '{admin.facility.manage}',                                   false, false, NULL),
    ('org.service_point',             NULL,                                                              '{admin.facility.manage}',                                   false, false, NULL),
    ('org.staff_department',          NULL,                                                              '{admin.user.manage}',                                       false, false, NULL),
    ('iam.membership',                '{admin.user.manage,admin.access_review,audit.read}',              '{admin.user.manage}',                                       false, false, NULL),
    ('iam.membership_role',           '{admin.user.manage,admin.access_review,audit.read}',              '{admin.user.manage}',                                       false, false, NULL),
    ('iam.api_credential',            '{admin.user.manage}',                                             '{admin.user.manage}',                                       false, false, NULL),
    ('iam.access_review',             '{admin.access_review,audit.read}',                                '{admin.access_review}',                                     false, false, NULL),
    ('iam.access_review_item',        '{admin.access_review,audit.read}',                                '{admin.access_review}',                                     false, false, NULL),
    ('patient.patient',               '{patient.read}',                                                  '{patient.create,patient.update,patient.merge}',             true,  true,  'id'),
    ('patient.contact_point',         '{patient.read}',                                                  '{patient.create,patient.update}',                           true,  true,  'patient_id'),
    ('patient.identifier',            '{patient.read}',                                                  '{patient.create,patient.update}',                           false, true,  'patient_id'),
    ('patient.next_of_kin',           '{patient.read}',                                                  '{patient.create,patient.update}',                           true,  true,  'patient_id'),
    ('patient.consent',               '{patient.read,clinical.read,telehealth.schedule}',                '{patient.create,patient.update,telehealth.schedule}',       true,  true,  'patient_id'),
    ('patient.portal_account',        '{patient.read,admin.user.manage}',                                '{patient.update,admin.user.manage}',                        true,  false, 'patient_id'),
    ('patient.duplicate_review',      '{patient.read}',                                                  '{patient.create,patient.merge}',                            false, true,  NULL),
    ('patient.merge_event',           '{patient.merge,audit.read}',                                      '{patient.merge}',                                           false, true,  NULL),
    ('iam.break_glass_access',        '{audit.read}',                                                    NULL,                                                        false, false, NULL),
    ('clinical.allergy',              '{clinical.read,pharmacy.read}',                                   '{clinical.history.write,triage.write,consultation.write}',  false, true,  'patient_id'),
    ('clinical.problem',              '{clinical.read}',                                                 '{clinical.history.write,consultation.write}',               false, true,  'patient_id'),
    ('clinical.history_entry',        '{clinical.read}',                                                 '{clinical.history.write,consultation.write}',               false, true,  'patient_id'),
    ('clinical.appointment',          '{appointment.read}',                                              '{appointment.manage}',                                      true,  true,  'patient_id'),
    ('clinical.encounter',            '{encounter.read,clinical.read}',                                  '{encounter.manage}',                                        true,  true,  'patient_id'),
    ('clinical.queue_ticket',         '{queue.read}',                                                    '{queue.manage}',                                            false, true,  'patient_id'),
    ('clinical.triage_assessment',    '{clinical.read}',                                                 '{triage.write}',                                            false, true,  'patient_id'),
    ('clinical.consultation',         '{clinical.read}',                                                 '{consultation.write}',                                      false, true,  'patient_id'),
    ('clinical.encounter_diagnosis',  '{clinical.read}',                                                 '{consultation.write}',                                      false, true,  'patient_id'),
    ('clinical.follow_up_plan',       '{clinical.read,appointment.read}',                                '{consultation.write,appointment.manage}',                   false, true,  'patient_id'),
    ('document.document',             '{document.read}',                                                 '{document.upload,document.delete}',                         false, true,  'patient_id'),
    ('diag.test_catalog',             NULL,                                                              '{diag.catalog.manage}',                                     false, true,  NULL),
    ('diag.test_component',           NULL,                                                              '{diag.catalog.manage}',                                     false, true,  NULL),
    ('diag.reference_range',          NULL,                                                              '{diag.catalog.manage}',                                     false, true,  NULL),
    ('diag.request',                  '{diag.request.read,clinical.read}',                               '{diag.request.create,diag.specimen.manage,diag.result.enter}', false, true, 'patient_id'),
    ('diag.request_item',             '{diag.request.read,clinical.read}',                               '{diag.request.create,diag.specimen.manage,diag.result.enter}', false, true, 'patient_id'),
    ('diag.specimen',                 '{diag.request.read,clinical.read}',                               '{diag.specimen.manage}',                                    false, true,  'patient_id'),
    ('diag.result',                   '{diag.result.read,clinical.read}',                                '{diag.result.enter,diag.result.verify}',                    false, true,  'patient_id'),
    ('pharmacy.drug',                 NULL,                                                              '{pharmacy.catalog.manage}',                                 false, true,  NULL),
    ('pharmacy.stock_batch',          '{pharmacy.read,pharmacy.stock.manage}',                           '{pharmacy.stock.manage}',                                   false, true,  NULL),
    ('pharmacy.stock_movement',       '{pharmacy.read,pharmacy.stock.manage}',                           '{pharmacy.stock.manage}',                                   false, true,  NULL),
    ('pharmacy.prescription',         '{pharmacy.read,clinical.read}',                                   '{pharmacy.prescribe}',                                      false, true,  'patient_id'),
    ('pharmacy.prescription_item',    '{pharmacy.read,clinical.read}',                                   '{pharmacy.prescribe}',                                      false, true,  'patient_id'),
    ('pharmacy.dispense',             '{pharmacy.read,clinical.read}',                                   '{pharmacy.dispense}',                                       false, true,  'patient_id'),
    ('pharmacy.dispense_item',        '{pharmacy.read}',                                                 '{pharmacy.dispense}',                                       false, true,  NULL),
    ('billing.payer',                 NULL,                                                              '{billing.price.manage}',                                    false, true,  NULL),
    ('billing.coverage',              '{billing.read,patient.read}',                                     '{billing.invoice.manage,patient.update}',                   false, true,  'patient_id'),
    ('billing.service_catalog',       NULL,                                                              '{billing.price.manage}',                                    false, true,  NULL),
    ('billing.price',                 NULL,                                                              '{billing.price.manage}',                                    false, true,  NULL),
    ('billing.cashier_shift',         '{billing.read}',                                                  '{billing.payment.record,billing.shift.reconcile}',          false, true,  NULL),
    ('billing.invoice',               '{billing.read}',                                                  '{billing.invoice.manage,billing.invoice.void}',             true,  true,  'patient_id'),
    ('billing.invoice_line',          '{billing.read}',                                                  '{billing.invoice.manage}',                                  false, true,  NULL),
    ('billing.payment',               '{billing.read}',                                                  '{billing.payment.record,billing.payment.reverse}',          true,  true,  'patient_id'),
    ('billing.payment_allocation',    '{billing.read}',                                                  '{billing.payment.record}',                                  false, true,  NULL),
    ('billing.receipt',               '{billing.read}',                                                  '{billing.payment.record}',                                  false, true,  NULL),
    ('billing.refund',                '{billing.read}',                                                  '{billing.refund.request,billing.refund.approve}',           false, true,  NULL),
    ('telehealth.session',            '{telehealth.conduct,telehealth.schedule,appointment.read}',       '{telehealth.schedule,telehealth.conduct}',                  true,  true,  'patient_id'),
    ('telehealth.session_event',      '{telehealth.conduct,audit.read}',                                 '{telehealth.conduct}',                                      false, true,  NULL),
    ('comms.notification_template',   NULL,                                                              '{comms.template.manage}',                                   false, false, NULL),
    ('comms.message',                 '{comms.read}',                                                    NULL,                                                        false, true,  NULL),
    ('integration.provider_config',   '{admin.org.manage}',                                              '{admin.org.manage}',                                        false, false, NULL),
    ('gov.legal_hold',                '{privacy.request.manage,audit.read}',                             '{privacy.request.manage}',                                  false, true,  NULL),
    ('gov.privacy_request',           '{privacy.request.manage}',                                        '{privacy.request.manage}',                                  false, true,  NULL)
  ) AS t(tbl, rd, wr, portal, frc, pcol)
  LOOP
    PERFORM sec.apply_rls(r.tbl::regclass, r.rd::text[], r.wr::text[], r.portal, r.frc, r.pcol);
  END LOOP;
END $$;

-- Patients may request appointments for themselves from the app (only as 'requested').
CREATE POLICY patient_request ON clinical.appointment FOR INSERT TO spartial_app
  WITH CHECK (sec.actor_type() = 'patient' AND patient_id = sec.current_patient_id()
              AND status = 'requested' AND source = 'patient_app');
-- Patients may cancel their own requested/booked appointments.
CREATE POLICY patient_cancel ON clinical.appointment FOR UPDATE TO spartial_app
  USING (sec.actor_type() = 'patient' AND patient_id = sec.current_patient_id() AND status IN ('requested','booked','confirmed'))
  WITH CHECK (sec.actor_type() = 'patient' AND patient_id = sec.current_patient_id() AND status IN ('requested','booked','confirmed','cancelled'));
-- Patients join their own telemedicine session (status → waiting_room only).
CREATE POLICY patient_join ON telehealth.session FOR UPDATE TO spartial_app
  USING (sec.actor_type() = 'patient' AND patient_id = sec.current_patient_id() AND status IN ('scheduled','waiting_room'))
  WITH CHECK (sec.actor_type() = 'patient' AND patient_id = sec.current_patient_id() AND status IN ('scheduled','waiting_room'));
-- Patients may withdraw/grant electronic consents for themselves.
CREATE POLICY patient_consent_insert ON patient.consent FOR INSERT TO spartial_app
  WITH CHECK (sec.actor_type() = 'patient' AND patient_id = sec.current_patient_id() AND capture_method = 'electronic_portal');
CREATE POLICY patient_consent_update ON patient.consent FOR UPDATE TO spartial_app
  USING (sec.actor_type() = 'patient' AND patient_id = sec.current_patient_id())
  WITH CHECK (sec.actor_type() = 'patient' AND patient_id = sec.current_patient_id());
-- Staff deleting draft invoice lines.
CREATE POLICY staff_delete ON billing.invoice_line FOR DELETE TO spartial_app
  USING (sec.is_staff() AND sec.has_perm('billing.invoice.manage'));
CREATE POLICY staff_delete ON org.staff_department FOR DELETE TO spartial_app
  USING (sec.is_staff() AND sec.has_perm('admin.user.manage'));
-- Break-glass: clinicians see their own grants.
CREATE POLICY own_grants ON iam.break_glass_access FOR SELECT TO spartial_app
  USING (membership_id = sec.current_membership_id());
CREATE POLICY review_grants ON iam.break_glass_access FOR UPDATE TO spartial_app
  USING (sec.has_perm('audit.read') OR membership_id = sec.current_membership_id())
  WITH CHECK (sec.has_perm('audit.read') OR membership_id = sec.current_membership_id());
-- Worker: message delivery across the current tenant.
CREATE POLICY worker_all ON comms.message TO spartial_worker USING (true) WITH CHECK (true);

-- Organization row: visible only for the current tenant.
ALTER TABLE org.organization ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_self ON org.organization FOR SELECT TO spartial_app, spartial_worker USING (id = sec.current_org_id());
CREATE POLICY tenant_admin ON org.organization FOR UPDATE TO spartial_app
  USING (id = sec.current_org_id() AND sec.has_perm('admin.org.manage'))
  WITH CHECK (id = sec.current_org_id() AND sec.has_perm('admin.org.manage'));

-- Roles: system templates visible to all tenants, read-only; tenant roles isolated.
ALTER TABLE iam.role ENABLE ROW LEVEL SECURITY;
CREATE POLICY role_select ON iam.role FOR SELECT TO spartial_app
  USING (sec.is_staff() AND (org_id IS NULL OR org_id = sec.current_org_id()));
CREATE POLICY role_write ON iam.role FOR INSERT TO spartial_app
  WITH CHECK (org_id = sec.current_org_id() AND NOT is_system AND sec.has_perm('admin.role.manage'));
CREATE POLICY role_update ON iam.role FOR UPDATE TO spartial_app
  USING (org_id = sec.current_org_id() AND sec.has_perm('admin.role.manage'))
  WITH CHECK (org_id = sec.current_org_id() AND NOT is_system);
ALTER TABLE iam.role_permission ENABLE ROW LEVEL SECURITY;
CREATE POLICY rp_select ON iam.role_permission FOR SELECT TO spartial_app
  USING (EXISTS (SELECT 1 FROM iam.role r WHERE r.id = role_id));
CREATE POLICY rp_write ON iam.role_permission FOR ALL TO spartial_app
  USING (sec.has_perm('admin.role.manage') AND EXISTS (SELECT 1 FROM iam.role r WHERE r.id = role_id AND r.org_id = sec.current_org_id()))
  WITH CHECK (sec.has_perm('admin.role.manage') AND EXISTS (SELECT 1 FROM iam.role r WHERE r.id = role_id AND r.org_id = sec.current_org_id()));

-- Audit trail: tenant auditors read their org; the compliance role reads all.
ALTER TABLE audit.event ENABLE ROW LEVEL SECURITY;
CREATE POLICY audit_tenant_read ON audit.event FOR SELECT TO spartial_app
  USING (org_id = sec.current_org_id() AND sec.has_perm('audit.read'));
CREATE POLICY audit_compliance_read ON audit.event FOR SELECT TO spartial_auditor USING (true);

-- Outbox: app enqueues for its tenant; worker processes everything.
ALTER TABLE comms.outbox ENABLE ROW LEVEL SECURITY;
CREATE POLICY outbox_enqueue ON comms.outbox FOR INSERT TO spartial_app
  WITH CHECK (org_id = sec.current_org_id() AND status = 'pending' AND attempts = 0);
CREATE POLICY outbox_worker ON comms.outbox TO spartial_worker USING (true) WITH CHECK (true);

-- Devices: a user manages only their own push registrations.
ALTER TABLE comms.device ENABLE ROW LEVEL SECURITY;
CREATE POLICY device_owner ON comms.device TO spartial_app
  USING (user_id = sec.current_user_id()) WITH CHECK (user_id = sec.current_user_id());
CREATE POLICY device_worker ON comms.device FOR SELECT TO spartial_worker USING (true);

ALTER TABLE integration.inbound_event ENABLE ROW LEVEL SECURITY;
CREATE POLICY inbound_worker ON integration.inbound_event TO spartial_worker USING (true) WITH CHECK (true);

-- -----------------------------------------------------------------------------
-- GRANTS (least privilege). No role but the owner has TRUNCATE/DDL.
-- -----------------------------------------------------------------------------
REVOKE ALL ON ALL TABLES IN SCHEMA sec, ref, iam, org, audit, patient, clinical, diag, pharmacy, billing,
                                    telehealth, document, comms, integration, gov, reporting FROM PUBLIC;

GRANT USAGE ON SCHEMA sec, ref, iam, org, audit, patient, clinical, diag, pharmacy, billing,
                      telehealth, document, comms, integration, gov, reporting
  TO spartial_app, spartial_worker;
GRANT USAGE ON SCHEMA audit, sec TO spartial_auditor;

GRANT SELECT ON ALL TABLES IN SCHEMA ref TO spartial_app, spartial_worker;
GRANT SELECT ON gov.data_category TO spartial_app;

-- Global identity/auth tables (no tenant context during login).
GRANT SELECT, INSERT, UPDATE ON iam.user_account, iam.password_credential, iam.mfa_factor, iam.otp_challenge,
                                iam.password_reset, iam.session, iam.refresh_token, iam.mfa_recovery_code TO spartial_app;
GRANT SELECT, INSERT ON iam.password_history TO spartial_app;

-- Tenant tables: read/insert/update; no DELETE except where listed.
GRANT SELECT, INSERT, UPDATE ON
  org.facility, org.department, org.service_point, org.staff_department,
  iam.role, iam.role_permission, iam.membership, iam.membership_role, iam.api_credential,
  iam.access_review, iam.access_review_item, iam.break_glass_access,
  patient.patient, patient.contact_point, patient.identifier, patient.next_of_kin, patient.consent,
  patient.portal_account, patient.duplicate_review,
  clinical.allergy, clinical.problem, clinical.history_entry, clinical.appointment, clinical.encounter,
  clinical.queue_ticket, clinical.triage_assessment, clinical.consultation, clinical.encounter_diagnosis,
  clinical.follow_up_plan,
  document.document,
  diag.test_catalog, diag.test_component, diag.reference_range, diag.request, diag.request_item,
  diag.specimen, diag.result,
  pharmacy.drug, pharmacy.stock_batch, pharmacy.prescription, pharmacy.prescription_item, pharmacy.dispense,
  billing.payer, billing.coverage, billing.service_catalog, billing.cashier_shift, billing.invoice,
  billing.invoice_line, billing.payment, billing.refund,
  telehealth.session, comms.notification_template, comms.device,
  integration.provider_config, gov.legal_hold, gov.privacy_request
TO spartial_app;
GRANT SELECT ON org.organization TO spartial_app, spartial_worker;
GRANT UPDATE (display_name, legal_name, staff_idle_timeout, staff_session_max, lockout_threshold, lockout_duration,
              lab_self_verification_allowed, updated_by) ON org.organization TO spartial_app;
-- Append-only business ledgers: insert + read only.
GRANT SELECT, INSERT ON patient.merge_event, pharmacy.stock_movement, pharmacy.dispense_item,
                        billing.payment_allocation, billing.price, telehealth.session_event TO spartial_app;
GRANT SELECT ON billing.receipt TO spartial_app;
GRANT UPDATE (print_count) ON billing.receipt TO spartial_app;
GRANT DELETE ON billing.invoice_line, org.staff_department, iam.role_permission, comms.device TO spartial_app;
GRANT SELECT ON comms.message, audit.event TO spartial_app;
GRANT INSERT ON comms.outbox TO spartial_app;
GRANT USAGE ON ALL SEQUENCES IN SCHEMA comms, telehealth TO spartial_app, spartial_worker;

-- Worker.
GRANT SELECT, INSERT, UPDATE ON comms.outbox, comms.message, integration.inbound_event TO spartial_worker;
GRANT SELECT ON comms.notification_template, comms.device, integration.provider_config TO spartial_worker;
GRANT SELECT, UPDATE ON billing.payment, telehealth.session TO spartial_worker;
GRANT SELECT ON billing.invoice, clinical.appointment, patient.patient, patient.contact_point, patient.consent TO spartial_worker;
GRANT INSERT ON telehealth.session_event TO spartial_worker;
GRANT SELECT, INSERT ON audit.seal TO spartial_worker;

-- Auditor (compliance, read-only, cross-tenant audit only).
GRANT SELECT ON audit.event, audit.seal TO spartial_auditor;

-- Functions.
GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA sec TO spartial_app, spartial_worker, spartial_auditor;
REVOKE EXECUTE ON FUNCTION sec.begin_system_request(uuid, uuid, text[]) FROM spartial_app, spartial_auditor;
REVOKE EXECUTE ON FUNCTION sec.apply_rls(regclass, text[], text[], boolean, boolean, text),
                           sec.attach_standard_triggers(regclass, boolean) FROM spartial_app, spartial_worker, spartial_auditor;
GRANT EXECUTE ON FUNCTION
  org.next_value(uuid, text, text), org.next_number(text), org.local_date(uuid, timestamptz),
  patient.next_mrn(uuid), clinical.next_encounter_no(uuid), patient.name_key(text, text, text),
  patient.find_duplicates(text, text, date, text, int), patient.has_consent(uuid, text),
  patient.portal_links_for_login(uuid), iam.memberships_for_login(uuid), iam.effective_permissions(uuid),
  iam.open_break_glass(uuid, text, int),
  billing.price_for(uuid, uuid, timestamptz), billing.source_patient(uuid, uuid, uuid),
  pharmacy.reverse_dispense(uuid, text),
  audit.log(text, text, text, text, uuid, uuid, text, inet, text, jsonb, uuid, uuid, text)
TO spartial_app;
GRANT EXECUTE ON FUNCTION org.next_value(uuid, text, text), org.local_date(uuid, timestamptz),
  patient.has_consent(uuid, text), audit.log(text, text, text, text, uuid, uuid, text, inet, text, jsonb, uuid, uuid, text),
  audit.seal_pending() TO spartial_worker;
GRANT EXECUTE ON FUNCTION audit.verify_integrity() TO spartial_auditor;
