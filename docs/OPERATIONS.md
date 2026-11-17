# Development operations and release requirements

## Startup and isolation

Use the native local cluster or Compose from README. The migration owner owns schema changes; the API uses a separate login granted specific table operations. `manage grant-runtime` provisions a new role if absent and grants existing permissions; it deliberately does **not** silently rotate an existing role password. To change one, use an authorized owner connection and update both database and secret configuration consistently.

Migration schema is embedded. Run `go run ./cmd/manage migrate` with `DATABASE_ADMIN_URL` before starting the matching API. The API validates the exact schema version, database reachability, and absence of excessive runtime role privileges. Readiness repeats schema/connectivity checks. Liveness only checks process reachability. There is no auto-deployment or production cutover command.

Use one API per HTTP port; both development setups default to 8080. Native PostgreSQL uses 55432; Compose exposes its independent database on 55433. PostgreSQL and MinIO host ports bind loopback. Optional object storage has no public bucket policy. Redis is not required.

## Configuration and secrets

`.env` is ignored, mode 600 when generated, and excluded from container context. Never commit credentials or patient exports. Example values are placeholders; CI values are isolated synthetic database credentials. Keep `DATABASE_ADMIN_URL` and seed credentials out of runtime deployments. Compose's API/worker environments contain only the restricted application URL. Management bootstrap receives the owner URL only for local provisioning.

Required: `DATABASE_URL`; defaults/config: `APP_ENV`, `HTTP_ADDR`, `ALLOWED_ORIGINS`, `CLINIC_TIMEZONE`. Management adds `DATABASE_ADMIN_URL`, `APP_DB_ROLE`, `APP_DB_PASSWORD`, and development-only `SEED_ADMIN_PASSWORD`/`SEED_RECEPTION_PASSWORD`. PostgreSQL owner and storage credentials are infrastructure settings.

For production preparation use a secrets manager, PostgreSQL TLS with server identity validation, a TLS reverse proxy, HTTPS exact Origin allowlists, private network rules, and database roles without ownership or administration rights. Proxy trust is currently disabled. A shared proxy would collapse backend rate limits to its IP; configure and test trusted proxies and perimeter rate controls before real deployment. Browser/API must be same-site for the Strict refresh cookie.

No production readiness is claimed: MFA, account recovery, consent/patient-relationship policies, clinical/financial safeguards, storage/scanning, security review, load baselines, metrics, centralized alerting, and clinic acceptance are unfinished.

## Health, logs and workers

`GET /health/live`, `GET /health/ready`. Public health responses contain no configuration. JSON request logs contain generated request IDs, route templates, duration and status, not payloads or search strings. Application failure logs are redacted. No metrics/debug endpoint is exposed yet.

API shutdown handles SIGINT/SIGTERM and drains within ten seconds. Database request contexts and worker work have bounded deadlines; the pool is capped at ten. Size the total pools across replicas against PostgreSQL limits before scaling.

The worker currently accepts **only APP_ENV=development**. It records an event in `development_event_sink`; it does not deliver patient notifications. Leases last 30 seconds with fencing tokens, max five attempts and backoff; exhausted leases are marked failed. Inspect `outbox` for `status='failed'` using authorized operational access. No automatic requeue command is exposed until the failed-job review policy is implemented. Do not change completed events to pending without consumer-specific reconciliation.

## Backups and restoration

Use synthetic data for this rehearsal:

```bash
set -a; source .env; set +a
python3 scripts/restore-check.py
```

It creates a custom-format PostgreSQL backup, restores it into a unique temporary database on the designated test server, compares all public table counts, and removes the temporary database and dump. Requires `pg_dump`, `pg_restore`, `psql`, and creation/drop privileges only on this development server. Stop writes during the comparison for a stable source. Row-count equality is a basic restore smoke check, not exhaustive clinical/financial reconciliation.

For real operational backup design (not configured here): schedule encrypted PostgreSQL backups and WAL/PITR under a restricted backup role, store copies off-host, record schema/application versions and checksum manifests, and test restoration in isolation. Define RPO/RTO, retention and legal hold with the clinic; none have been invented. An actual deployment should not expose the application owner's password to backup jobs or logs.

Attachments are not implemented. Before M06 is accepted, backup private object versions and metadata/checksums from the same recovery point, restore quarantine/scan state, verify object counts/checksums/permissions, and demonstrate an authorized download. **Database-only rehearsal does not prove file restoration.** Include encryption key recovery without storing keys beside backups.

Do not overwrite a live clinic with an old backup. Restore into an isolated environment, reconcile post-backup activity and provider settlements, and plan a governed cutover. POWERHMS freeze/delta/rollback procedures remain M23 work.

## Change deployment and rollback

Development sequence: backup synthetic database, run migrations with the owner, grant runtime additions if required, start matching application, check readiness, run the synthetic journey, review failed jobs and audit. Use reviewed additive migrations and expand/contract changes for eventual production releases. Roll back the application only when the old binary supports the new schema; current startup requires exact version equality, so coordinate rollback explicitly. Goose Down is destructive development tooling, not an automatic clinic recovery plan.

Container builds are multi-stage/non-root and use version-pinned images. Rebuild and scan base images before release; Go vulnerability scanning does not scan OS packages. Compose config/build and native PostgreSQL tests are separate verification activities; see the evidence in MODULE_STATUS.md. CI is configured but its hosted run has not been observed in this repository.
