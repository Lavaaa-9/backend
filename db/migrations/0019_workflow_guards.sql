-- =============================================================================
-- 0019_workflow_guards.sql — encounter closure guard helper (zero-PHI counts)
-- =============================================================================
-- Returns counts of incomplete records attached to an encounter so any staff
-- member with encounter.manage (e.g. receptionist, nurse, doctor) is prevented
-- from completing a visit with open drafts or unsettled invoices without
-- needing clinical.read or pharmacy.read access to view those records.

CREATE OR REPLACE FUNCTION clinical.encounter_closure_blockers(p_encounter uuid)
RETURNS TABLE (
  draft_consultations integer,
  draft_triages       integer,
  draft_prescriptions integer,
  unpaid_invoices     integer,
  pending_diagnostics integer
) LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path = pg_catalog AS $$
DECLARE
  v_org uuid := sec.current_org_id();
BEGIN
  IF v_org IS NULL OR NOT sec.has_perm('encounter.manage') THEN
    RAISE EXCEPTION 'encounter.manage permission required' USING ERRCODE = 'SPA01';
  END IF;
  RETURN QUERY
  SELECT
    (SELECT count(*)::int FROM clinical.consultation      c WHERE c.org_id = v_org AND c.encounter_id = p_encounter AND c.status = 'draft'),
    (SELECT count(*)::int FROM clinical.triage_assessment t WHERE t.org_id = v_org AND t.encounter_id = p_encounter AND t.status = 'draft'),
    (SELECT count(*)::int FROM pharmacy.prescription      p WHERE p.org_id = v_org AND p.encounter_id = p_encounter AND p.status = 'draft'),
    (SELECT count(*)::int FROM billing.invoice            i WHERE i.org_id = v_org AND i.encounter_id = p_encounter AND i.status IN ('draft','issued','partially_paid') AND i.balance > 0),
    (SELECT count(*)::int FROM diag.request               r WHERE r.org_id = v_org AND r.encounter_id = p_encounter AND r.status IN ('requested','in_progress','partially_resulted'));
END $$;

REVOKE ALL ON FUNCTION clinical.encounter_closure_blockers(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION clinical.encounter_closure_blockers(uuid) TO spartial_app;
