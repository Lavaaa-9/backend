# Database Migration & Key Management Guide

## Applying Schema Migrations
Migrations are checksum-verified and executed transactionally as `spartial_owner` via `db/tools/migrate.sh`:

```bash
# Development (starts isolated native PostgreSQL 18 on 127.0.0.1:55432, migrates, provisions spartial_worker, seeds roles)
./scripts/dev-db.sh

# Or run migrations directly (requires DATABASE_ADMIN_URL)
go run ./cmd/manage migrate
```

- Each migration runs inside `BEGIN; SET LOCAL ROLE spartial_owner; SET LOCAL lock_timeout = '5s'; ... COMMIT;`.
- If an applied migration file is altered on disk, `migrate.sh` exits immediately with `CHECKSUM DRIFT`.
- API and worker startup (`platform.CheckSchema`) verify `public.schema_migration` contains `platform.LatestMigration` (`0018_compliance_document_read`) before serving traffic.

## Running Legacy-to-New Data Migration
```bash
LEGACY_DATABASE_URL="postgres://..." \
DATABASE_ADMIN_URL="postgres://spartial_owner:...@.../spartial_hms?sslmode=verify-full&sslrootcert=..." \
PHI_ENCRYPTION_KEYS="k1:..." PHI_ACTIVE_KEY_ID="k1" IDENTIFIER_INDEX_KEY="..." \
go run ./cmd/manage migrate-legacy
```
- Emits a non-PHI JSON reconciliation report (`tables`, `exceptions`, `audit_integrity_ok`, `reconciled`).
- Exits non-zero if any record failed or counts do not reconcile.
- Safe to re-run (idempotent).

## Field-Level Key Rotation
To rotate `PHI_ENCRYPTION_KEYS`:
1. Generate a new 32-byte key: `go run ./cmd/manage new-encryption-key`.
2. Append `k2:<new_key>` to `PHI_ENCRYPTION_KEYS` (keeping `k1`) and set `PHI_ACTIVE_KEY_ID=k2`.
3. Restart API instances so new writes use `k2` while reads decrypt both `k1` and `k2`.
4. Run `go run ./cmd/manage rotate-keys` to re-encrypt `iam.mfa_factor`, `patient.identifier`, `billing.coverage`, and `comms.device` rows to `k2`.
5. Once zero rows reference `k1`, remove `k1` from `PHI_ENCRYPTION_KEYS`.
