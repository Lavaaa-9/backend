# Deployment Guide (`spartial_hms`)

## Prerequisites
- PostgreSQL 16+ (verified on PostgreSQL 18 native Linux) with extensions `pgcrypto`, `citext`, `pg_trgm`, `btree_gist`.
- Go 1.27+ to build `cmd/api`, `cmd/worker`, and `cmd/manage`.

## Database Roles & Least Privilege
Create separate login roles inheriting the cluster roles from `0000_cluster_roles.sql`:
- **Migration runner:** member of `spartial_owner` (used ONLY during migrations/maintenance, never by API or worker).
- **API runtime (`DATABASE_URL`):** `LOGIN NOSUPERUSER NOCREATEROLE NOBYPASSRLS IN ROLE spartial_app`.
- **Worker runtime (`DATABASE_WORKER_URL`):** `LOGIN NOSUPERUSER NOCREATEROLE NOBYPASSRLS IN ROLE spartial_worker`.
- **Compliance auditor:** `LOGIN NOSUPERUSER NOCREATEROLE NOBYPASSRLS IN ROLE spartial_auditor`.

## Required Production Environment Variables
- `APP_ENV=production`
- `HTTP_ADDR` (e.g. `127.0.0.1:8080` behind TLS-terminating reverse proxy)
- `DATABASE_URL` (must include `sslmode=verify-full&sslrootcert=/path/to/ca.crt`)
- `DATABASE_WORKER_URL` (same TLS requirement for `cmd/worker`)
- `ALLOWED_ORIGINS` (exact `https://` origin(s), no wildcards)
- `PHI_ENCRYPTION_KEYS` (`id:base64(32 bytes),...`) and `PHI_ACTIVE_KEY_ID`
- `IDENTIFIER_INDEX_KEY` (`base64(32 bytes)`, independent of `PHI_ENCRYPTION_KEYS`)
- `MFA_ENFORCE_PRIVILEGED=true`, `MFA_ENFORCE_REMOTE_CLINICAL=true` (enforced automatically in prod)
- `TRUSTED_PROXIES` and `CLINIC_NETWORKS` (explicit IPs/CIDRs; `/0` rejected)
- `CARE_POLICY_ENABLED=true` and `CARE_POLICY_APPROVAL_REF=<signed-ref>`
- `PRIVACY_READ_THRESHOLD` and `PRIVACY_MFA_THRESHOLD` (positive integers for alert detection)

## Startup & Health Verification
1. Apply migrations: `PGHOST=... PGUSER=... db/tools/migrate.sh spartial_hms`
2. Start API (`./bin/api`) and worker (`./bin/worker`).
3. Verify liveness: `GET /health/live` -> `200 {"status":"ok"}`.
4. Verify readiness (DB ping + schema version `0018_compliance_document_read` check): `GET /health/ready` -> `200 {"status":"ready"}`.
5. Verify OpenAPI spec: `GET /api/openapi.json` (178 paths).
