-- =============================================================================
-- 0014_api_support.sql — support objects needed by the Go API (Fiber backend).
--  * iam.my_profile(): the caller's own role codes and facility scope. Tenant RLS
--    hides iam.membership_role from non-administrators, so the API reads this
--    narrow SECURITY DEFINER view of the *current* membership only.
--  * sec.idempotency: Idempotency-Key replay store for create operations.
-- =============================================================================
CREATE OR REPLACE FUNCTION iam.my_profile()
RETURNS TABLE (email text, display_name text, row_version integer, roles text[], facility_ids uuid[])
LANGUAGE sql STABLE SECURITY DEFINER SET search_path = pg_catalog AS $$
  SELECT u.email::text, u.display_name, u.row_version,
    coalesce((SELECT array_agg(DISTINCT r.code ORDER BY r.code)
                FROM iam.membership_role mr JOIN iam.role r ON r.id = mr.role_id
               WHERE mr.membership_id = sec.current_membership_id() AND mr.revoked_at IS NULL
                 AND (mr.expires_at IS NULL OR mr.expires_at > now())), '{}'::text[]),
    coalesce((SELECT array_agg(DISTINCT f.id ORDER BY f.id)
                FROM iam.membership_role mr
                JOIN org.facility f ON f.org_id = mr.org_id AND f.status = 'active'
                                   AND (mr.facility_id IS NULL OR mr.facility_id = f.id)
               WHERE mr.membership_id = sec.current_membership_id() AND mr.revoked_at IS NULL
                 AND (mr.expires_at IS NULL OR mr.expires_at > now())), '{}'::uuid[])
  FROM iam.user_account u WHERE u.id = sec.current_user_id();
$$;
GRANT EXECUTE ON FUNCTION iam.my_profile() TO spartial_app;

CREATE TABLE sec.idempotency (
  org_id       uuid NOT NULL REFERENCES org.organization(id),
  user_id      uuid NOT NULL REFERENCES iam.user_account(id),
  operation    text NOT NULL,
  key          text NOT NULL CHECK (length(key) BETWEEN 8 AND 128),
  request_hash text NOT NULL,
  response     jsonb NOT NULL,
  created_at   timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (org_id, user_id, operation, key)
);
ALTER TABLE sec.idempotency ENABLE ROW LEVEL SECURITY;
ALTER TABLE sec.idempotency FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON sec.idempotency AS RESTRICTIVE TO PUBLIC
  USING (org_id = sec.current_org_id()) WITH CHECK (org_id = sec.current_org_id());
CREATE POLICY owner_internal ON sec.idempotency TO spartial_owner USING (true) WITH CHECK (true);
CREATE POLICY app_own ON sec.idempotency TO spartial_app
  USING (user_id = sec.current_user_id()) WITH CHECK (user_id = sec.current_user_id());
GRANT SELECT, INSERT ON sec.idempotency TO spartial_app;
