# Schema Implementation (`spartial_hms`)

## Authoritative Migration Set (`db/migrations/`)
All 18 migrations execute deterministically via `db/tools/migrate.sh` under `spartial_owner`, recording SHA-256 checksums in `public.schema_migration` to detect drift:

| Migration | Scope |
|---|---|
| `0000_cluster_roles.sql` | Cluster roles: `spartial_owner`, `spartial_app`, `spartial_worker`, `spartial_auditor` (`NOSUPERUSER NOCREATEROLE NOBYPASSRLS`) |
| `0001_foundation.sql` | Extensions (`pgcrypto`, `citext`, `pg_trgm`, `btree_gist`), 16 schemas, `sec.*` GUC accessors, `sec.touch_row`, `sec.forbid_mutation`, `sec.protect_finalised`, `ref.state_transition`, `ref.icd10_code`, `ref.permission` |
| `0002_identity_and_organization.sql` | `iam.*` identities, Argon2id credentials, MFA factors/recovery codes, OTP/reset, sessions, refresh tokens, RBAC roles/memberships, API credentials, access reviews; `org.*` tenant/facility/department/service points/gapless counters |
| `0003_audit.sql` | Monthly-partitioned append-only `audit.event` with `row_hash`, `audit.seal`, `audit.seal_pending()`, `audit.verify_integrity()`, automatic `audit.capture_change()` triggers |
| `0004_patient.sql` | `patient.*` master index, contacts, encrypted identifiers + blind index, next of kin, consents, portal linkage, duplicate detection (`pg_trgm`) & merge ledger; `iam.break_glass_access` |
| `0005_clinical.sql` | Allergies, problems, history entries, appointments (GiST anti-overlap exclusion constraints), encounters, daily queue tickets, triage (auto-BMI), signed consultations, ICD-10 diagnoses, follow-up plans |
| `0006_documents_diagnostics_pharmacy.sql` | `document.document` (legal hold), `diag.*` catalogue/components/ranges/requests/specimens/results (dual verification & critical flags), `pharmacy.*` drugs/FEFO stock batches/append-only ledger/prescriptions/dispensing/reversals |
| `0007_billing.sql` | Payers, encrypted coverages, service catalog, GiST non-overlapping prices, cashier shifts, invoices/lines (derived totals, single-billing uniqueness), payments/allocations, gapless receipts, four-eyes refunds |
| `0008_telehealth_comms_integration_gov.sql` | Telemedicine sessions & events (consent-gated), notification templates, transactional outbox, message log (consent-gated), encrypted push devices, integration provider configs (DPA-gated), inbound webhooks, `gov.*` data categories, legal holds, privacy requests |
| `0009_security_rls_grants.sql` | `sec.begin_request()`, `sec.begin_system_request()`, `iam.open_break_glass()`, `sec.apply_rls()` across all tenant tables, least-privilege grants |
| `0010_reporting.sql` | 8 `security_invoker = true` reporting views (`reporting.v_*`) |
| `0011_reference_seed.sql` | State transition table, 50+ permissions, 11 system roles, ICD-10 starter set, governance data categories |
| `0012_patient_portal_column_guards.sql` | Column-level update guards for patient-portal actors |
| `0013_scope_gaps_...sql` | `patient.verification`, `clinical.nursing_observation`, `clinical.procedure`, `reporting.staff_activity()`, `reporting.staff_access()` |
| `0014_api_support.sql` | `iam.my_profile()`, RLS-protected `sec.idempotency` |
| `0015_schema_version_grant.sql` | `SELECT` on `public.schema_migration` for `spartial_app` |
| `0016_billing_collect_charges.sql` | `billing.collect_charges(invoice_id)` (`SECURITY DEFINER` across clinical/pharmacy/lab boundaries) |
| `0017_mfa_and_backend_completion.sql` | `iam.mfa_challenge`, `gov.approval_request`, `gov.privacy_alert`, `gov.detect_privacy_alerts()`, `iam.break_glass_rules` four-eyes trigger, `patient.merge_patients()`, worker/app grants |
| `0018_compliance_document_read.sql` | Grants `document.read` to `compliance_officer` so compliance legal-hold/delete workflows pass RLS SELECT |
