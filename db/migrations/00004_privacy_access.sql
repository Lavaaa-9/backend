-- +goose Up
INSERT INTO permissions(name) VALUES('care.manage'),('break_glass.request'),('break_glass.review'),('privacy.report'),('metrics.read');
INSERT INTO role_permissions VALUES
 ('reception','care.manage'),
 ('doctor','patients.read'),('doctor','break_glass.request'),
 ('nurse','patients.read'),('nurse','break_glass.request'),
 ('laboratory','patients.read'),('pharmacy','patients.read'),
 ('privacy-reviewer','break_glass.review'),('privacy-reviewer','privacy.report'),
 ('administrator','metrics.read');
ALTER TABLE patients ADD CONSTRAINT patients_id_facility UNIQUE(id,facility_id);
CREATE TABLE patient_scopes (
 patient_id uuid PRIMARY KEY,
 facility_id uuid NOT NULL REFERENCES facilities(id),
 FOREIGN KEY(patient_id,facility_id) REFERENCES patients(id,facility_id)
);
INSERT INTO patient_scopes SELECT id,facility_id FROM patients;
-- +goose StatementBegin
CREATE FUNCTION sync_patient_scope() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,public,pg_temp AS $$
BEGIN INSERT INTO public.patient_scopes(patient_id,facility_id) VALUES(NEW.id,NEW.facility_id); RETURN NEW; END $$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION sync_patient_scope() FROM PUBLIC;
CREATE TRIGGER patient_scope_insert AFTER INSERT ON patients FOR EACH ROW EXECUTE FUNCTION sync_patient_scope();
CREATE INDEX patient_scopes_facility ON patient_scopes(facility_id,patient_id);
CREATE TABLE care_assignments (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 patient_id uuid NOT NULL,
 facility_id uuid NOT NULL,
 staff_id uuid NOT NULL REFERENCES staff(id),
 assigned_by uuid NOT NULL REFERENCES staff(id),
 reason text NOT NULL CHECK(length(reason) BETWEEN 20 AND 1000),
 valid_from timestamptz NOT NULL DEFAULT now(),
 valid_until timestamptz NOT NULL,
 revoked_at timestamptz,
 revoked_by uuid REFERENCES staff(id),
 created_at timestamptz NOT NULL DEFAULT now(),
 CHECK(valid_until>valid_from),
 CHECK((revoked_at IS NULL)=(revoked_by IS NULL)),
 FOREIGN KEY(patient_id,facility_id) REFERENCES patients(id,facility_id)
);
CREATE INDEX care_assignments_access ON care_assignments(staff_id,patient_id,valid_until) WHERE revoked_at IS NULL;
CREATE TABLE break_glass_access (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 patient_id uuid NOT NULL,
 facility_id uuid NOT NULL,
 staff_id uuid NOT NULL REFERENCES staff(id),
 reason text NOT NULL CHECK(length(reason) BETWEEN 20 AND 1000),
 granted_at timestamptz NOT NULL DEFAULT now(),
 expires_at timestamptz NOT NULL,
 reviewed_by uuid REFERENCES staff(id),
 reviewed_at timestamptz,
 review_outcome text CHECK(review_outcome IN ('appropriate','inappropriate')),
 review_note text CHECK(review_note IS NULL OR length(review_note) BETWEEN 20 AND 1000),
 CHECK(expires_at>granted_at AND expires_at<=granted_at+interval '4 hours'),
 CHECK(reviewed_by IS NULL OR reviewed_by<>staff_id),
 CHECK((reviewed_by IS NULL AND reviewed_at IS NULL AND review_outcome IS NULL AND review_note IS NULL)
    OR (reviewed_by IS NOT NULL AND reviewed_at IS NOT NULL AND review_outcome IS NOT NULL AND review_note IS NOT NULL)),
 FOREIGN KEY(patient_id,facility_id) REFERENCES patients(id,facility_id)
);
CREATE INDEX break_glass_access_lookup ON break_glass_access(staff_id,patient_id,expires_at);
CREATE INDEX break_glass_unreviewed ON break_glass_access(granted_at,id) WHERE reviewed_at IS NULL;
-- +goose StatementBegin
CREATE FUNCTION has_care_access(actor uuid, patient uuid) RETURNS boolean LANGUAGE sql STABLE SET search_path=pg_catalog,public,pg_temp AS $$
 SELECT EXISTS(SELECT 1 FROM public.staff u WHERE u.id=actor AND u.active)
 AND (EXISTS(SELECT 1 FROM public.care_assignments c JOIN public.staff_facilities sf ON sf.staff_id=actor AND sf.facility_id=c.facility_id
   JOIN public.facilities f ON f.id=c.facility_id AND f.active
   WHERE c.staff_id=actor AND c.patient_id=patient AND c.revoked_at IS NULL AND c.valid_from<=statement_timestamp() AND c.valid_until>statement_timestamp())
 OR EXISTS(SELECT 1 FROM public.break_glass_access b JOIN public.staff_facilities sf ON sf.staff_id=actor AND sf.facility_id=b.facility_id
   JOIN public.facilities f ON f.id=b.facility_id AND f.active
   WHERE b.staff_id=actor AND b.patient_id=patient AND b.expires_at>statement_timestamp()
    AND (b.review_outcome IS NULL OR b.review_outcome<>'inappropriate')))
$$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION can_access_patient(actor uuid,patient uuid,facility uuid,write_access boolean DEFAULT false) RETURNS boolean LANGUAGE sql STABLE SET search_path=pg_catalog,public,pg_temp AS $$
 SELECT EXISTS(SELECT 1 FROM public.staff u
 JOIN public.staff_facilities sf ON sf.staff_id=u.id AND sf.facility_id=facility
 JOIN public.facilities f ON f.id=sf.facility_id AND f.active
 JOIN public.role_permissions rp ON rp.role_name=u.role_name AND rp.permission_name=CASE WHEN write_access THEN 'patients.write' ELSE 'patients.read' END
 WHERE u.id=actor AND u.active AND
 (u.role_name='reception' OR
  (COALESCE(current_setting('app.care_policy_enabled',true),'false')='true' AND public.has_care_access(actor,patient))))
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION has_care_access(uuid,uuid),can_access_patient(uuid,uuid,uuid,boolean) FROM PUBLIC;
ALTER TABLE patients ENABLE ROW LEVEL SECURITY;
ALTER TABLE patients FORCE ROW LEVEL SECURITY;
CREATE POLICY patient_read_scope ON patients FOR SELECT USING (
 can_access_patient(NULLIF(current_setting('app.actor_id',true),'')::uuid,id,facility_id,false));
CREATE POLICY patient_insert_scope ON patients FOR INSERT WITH CHECK (
 can_access_patient(NULLIF(current_setting('app.actor_id',true),'')::uuid,id,facility_id,true));
CREATE POLICY patient_update_scope ON patients FOR UPDATE USING (
 can_access_patient(NULLIF(current_setting('app.actor_id',true),'')::uuid,id,facility_id,true)) WITH CHECK (
 can_access_patient(NULLIF(current_setting('app.actor_id',true),'')::uuid,id,facility_id,true));
-- +goose Down
DROP POLICY patient_update_scope ON patients;
DROP POLICY patient_insert_scope ON patients;
DROP POLICY patient_read_scope ON patients;
ALTER TABLE patients NO FORCE ROW LEVEL SECURITY;
ALTER TABLE patients DISABLE ROW LEVEL SECURITY;
DROP FUNCTION can_access_patient(uuid,uuid,uuid,boolean),has_care_access(uuid,uuid);
DROP TABLE break_glass_access,care_assignments;
DROP TRIGGER patient_scope_insert ON patients;
DROP FUNCTION sync_patient_scope();
DROP TABLE patient_scopes;
ALTER TABLE patients DROP CONSTRAINT patients_id_facility;
DELETE FROM role_permissions WHERE permission_name IN ('care.manage','break_glass.request','break_glass.review','privacy.report','metrics.read') OR (permission_name='patients.read' AND role_name IN ('doctor','nurse','laboratory','pharmacy'));
DELETE FROM permissions WHERE name IN ('care.manage','break_glass.request','break_glass.review','privacy.report','metrics.read');
