# Backup, Point-in-Time Recovery & Audit Seal Archival

## Backup Architecture
- **Continuous WAL Archiving & Base Backups:** Configure `pgBackRest` (see `deploy/pgbackrest.conf.example`) with encrypted off-host repository storage (AES-256) and daily differential + weekly full base backups for Point-in-Time Recovery (PITR).
- **Separate Key Custody:** Never store `PHI_ENCRYPTION_KEYS` or `IDENTIFIER_INDEX_KEY` inside the database or inside the same backup bucket as PostgreSQL dumps. Store them in an independent KMS/secrets manager with versioned backup access.
- **Off-Host Audit Seal Export:** Schedule `cmd/worker` (or `deploy/systemd/spartial-audit-seal.timer`) to run `audit.seal_pending()` and ship resulting `audit.seal` records (`seal_no`, `xid_lower`, `xid_upper`, `row_count`, `seal_hash`) to WORM object storage / external SIEM so even a compromised DB superuser cannot rewrite history undetected.

## Restore Drill Procedure
1. Restore latest base backup + WAL to an isolated recovery host/database.
2. Verify schema migration table: `SELECT version FROM public.schema_migration ORDER BY version DESC LIMIT 1;` -> `0018_compliance_document_read`.
3. Verify tamper-evident audit chain as `spartial_auditor`: `SELECT * FROM audit.verify_integrity();` (must return 0 rows).
4. Verify billing reconciliation view: `SELECT count(*) FROM reporting.v_billing_reconciliation_exceptions;` (must return `0`).
5. Verify API startup against restored DB using `spartial_app` (`GET /health/ready` -> `200`).
6. Document restore duration (RTO) and latest recovered timestamp (RPO), then destroy the temporary drill instance.
