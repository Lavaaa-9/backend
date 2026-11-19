# Production Cutover Plan

> Do **not** enable uncontrolled dual writes between the legacy database and `spartial_hms`. Use a controlled maintenance-window cutover with full verification and reconciliation before switching traffic.

## Pre-Cutover Checklist (T-24h)
1. Provision target PostgreSQL instance, TLS certificates (`verify-full`), cluster roles (`0000_cluster_roles.sql`), and restricted login roles (`spartial_app`, `spartial_worker`, `spartial_auditor`).
2. Run a full dry-run migration against a restored copy of production legacy data using `cmd/manage migrate-legacy` and review the JSON exception & reconciliation report.
3. Verify `spartial-db` migrations `0001`–`0018` pass cleanly and checksums match release artifacts.
4. Confirm secrets (`PHI_ENCRYPTION_KEYS`, `IDENTIFIER_INDEX_KEY`, DB passwords) match the keys used to encrypt legacy MFA/identifier data so AAD re-encryption succeeds.

## Cutover Sequence (Maintenance Window)
1. **Enable Maintenance Mode:** Stop legacy API/worker traffic and verify zero active write sessions on legacy DB.
2. **Pre-Cutover Backup:** Take a verified base backup / `pg_dump -Fc` snapshot of the legacy database (retain read-only until post-cutover sign-off).
3. **Apply Target Schema:** Run `db/tools/migrate.sh spartial_hms` as `spartial_owner`.
4. **Run Legacy Data Migration:** Execute `cmd/manage migrate-legacy` with `LEGACY_DATABASE_URL` and `DATABASE_ADMIN_URL`; archive the non-PHI JSON reconciliation report.
5. **Gate Check — Reconciliation & Integrity:**
   - Confirm `reconciled: true` and `audit_integrity_ok: true` in the migration report.
   - Confirm `SELECT count(*) FROM audit.verify_integrity();` returns `0`.
   - Confirm `SELECT count(*) FROM reporting.v_billing_reconciliation_exceptions;` returns `0`.
   - Review any `rejected` or `transformed` exceptions before proceeding.
6. **Deploy New Backend & Worker:** Start new API (`spartial_app`) and worker (`spartial_worker`). Verify `/health/ready` returns `200`.
7. **Smoke Verification:** Verify login + MFA challenge/verify, patient lookup, blind-index identifier search, audit log read, and worker `--once` outbox + audit seal pass.
8. **Switch Production Traffic:** Point load balancer / ingress to the new API and monitor error rate, latency, and `request_failed` logs.
9. **Preserve Legacy Database:** Keep the legacy database read-only and recoverable for the agreed retention window before decommission.
