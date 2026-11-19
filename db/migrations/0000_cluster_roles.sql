-- =============================================================================
-- 0000_cluster_roles.sql
-- Spartial Specialized Polyclinic HMS — PostgreSQL 16
-- Cluster-level roles. Run ONCE per cluster by a superuser / cloud admin role.
-- Idempotent. LOGIN users are created per environment by the platform team and
-- granted one of these group roles; their passwords come from the secrets
-- manager and are NEVER committed to this repository.
-- =============================================================================

DO $$
BEGIN
  -- Owns every schema object; used only by the migration pipeline.
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'spartial_owner') THEN
    CREATE ROLE spartial_owner NOLOGIN NOSUPERUSER NOCREATEROLE NOBYPASSRLS;
  END IF;
  -- Runtime role of the Go/Gin API. Subject to RLS. No DDL, no TRUNCATE.
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'spartial_app') THEN
    CREATE ROLE spartial_app NOLOGIN NOSUPERUSER NOCREATEROLE NOBYPASSRLS;
  END IF;
  -- Background workers: outbox dispatch, SMS/WhatsApp, payment callbacks, sealing.
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'spartial_worker') THEN
    CREATE ROLE spartial_worker NOLOGIN NOSUPERUSER NOCREATEROLE NOBYPASSRLS;
  END IF;
  -- Compliance / internal-audit read access to audit trail across tenants.
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'spartial_auditor') THEN
    CREATE ROLE spartial_auditor NOLOGIN NOSUPERUSER NOCREATEROLE NOBYPASSRLS;
  END IF;
END
$$;

-- Example (per environment, NOT executed here):
--   CREATE ROLE spartial_api_prod LOGIN PASSWORD :'from_secrets_manager' IN ROLE spartial_app;
--   ALTER ROLE spartial_api_prod SET statement_timeout = '15s';
--   ALTER ROLE spartial_api_prod SET idle_in_transaction_session_timeout = '30s';
