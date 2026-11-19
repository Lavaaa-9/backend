-- =============================================================================
-- 0012_patient_portal_column_guards.sql
-- RLS limits patient-app UPDATEs to the patient's own rows and allowed statuses,
-- but RLS cannot restrict columns. These guards close that gap: a patient may
-- only cancel an appointment, only withdraw a consent, only move a
-- telemedicine session into the waiting room.
-- =============================================================================
CREATE OR REPLACE FUNCTION sec.patient_column_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE v_allowed text[] := string_to_array(TG_ARGV[0], ',') || ARRAY['updated_at','updated_by','row_version'];
BEGIN
  IF sec.actor_type() = 'patient' AND NOT sec.is_internal() THEN
    IF (to_jsonb(NEW) - v_allowed) IS DISTINCT FROM (to_jsonb(OLD) - v_allowed) THEN
      RAISE EXCEPTION 'patients may only change % on %.%', TG_ARGV[0], TG_TABLE_SCHEMA, TG_TABLE_NAME
        USING ERRCODE = '42501';
    END IF;
    IF TG_TABLE_NAME = 'appointment' AND NEW.status IS DISTINCT FROM OLD.status AND NEW.status <> 'cancelled' THEN
      RAISE EXCEPTION 'patients may only cancel appointments' USING ERRCODE = '42501';
    END IF;
    IF TG_TABLE_NAME = 'consent' AND NEW.status IS DISTINCT FROM OLD.status AND NEW.status <> 'withdrawn' THEN
      RAISE EXCEPTION 'patients may only withdraw consent' USING ERRCODE = '42501';
    END IF;
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER trg_1_patient_guard BEFORE UPDATE ON clinical.appointment
  FOR EACH ROW EXECUTE FUNCTION sec.patient_column_guard('status,cancel_reason,cancelled_at,cancelled_by');
CREATE TRIGGER trg_1_patient_guard BEFORE UPDATE ON patient.consent
  FOR EACH ROW EXECUTE FUNCTION sec.patient_column_guard('status,withdrawn_at,withdrawal_reason');
CREATE TRIGGER trg_1_patient_guard BEFORE UPDATE ON telehealth.session
  FOR EACH ROW EXECUTE FUNCTION sec.patient_column_guard('status');
GRANT EXECUTE ON FUNCTION sec.patient_column_guard() TO spartial_app;
