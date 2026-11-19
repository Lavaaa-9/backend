# Backend Refactoring Report

## Architecture Summary
The Go Fiber v3 backend (`spartial.local/backend`) has been completely re-engineered against `spartial_hms` (migrations `0001`–`0018`). Every authenticated API request executes inside a single PostgreSQL transaction initiated by `platform.BeginRequest` -> `sec.begin_request(session_id, request_id)` so the database derives `org_id`, `user_id`, `membership_id`, `permissions`, and active `break_glass` grants directly from the session rather than trusting application-asserted headers or claims.

## Implemented Backend Modules

| Package | Responsibilities |
|---|---|
| `internal/platform` | Connection pooling, `CheckSchema`, `CheckRuntimeRole`, `BeginRequest`/`Run`, `Idempotent` (`sec.idempotency`), `Audit`/`AuditPreAuth`, SQLSTATE normalization (`23P01`/`SPT01`/`SPI01`/`SPS01`/`SPB01` -> 409, `SPV01`/`SPC01`/`SPX01` -> 422, `SPA01`/`42501` -> 403, `28000` -> 401), worker outbox/sealing/alert helpers |
| `internal/modules/identity` | Login, refresh-token rotation with reuse detection, session list/revoke, password change, non-enumerating password reset, TOTP MFA (enroll/confirm/verify/recovery-codes/disable), push device registration, staff user management |
| `internal/modules/facilities` | Facility listing & creation |
| `internal/modules/patients` | Patient registration/search/update, verification history, encrypted identifiers + blind-index search + audited reveal, contacts, next of kin, consents, portal account linkage, duplicate check/reviews & atomic patient merge |
| `internal/modules/visits` | Encounters, status transitions, nursing observations, billable procedures |
| `internal/modules/clinical` | Queue tickets & board, triage (draft/finalise/void), consultations (draft/edit/sign/void), ICD-10 diagnoses, follow-up plans, allergies, chronic problems, medical history entries, departments & service points |
| `internal/modules/telehealth` | Appointments (GiST double-booking protection, outbox event enqueue) & consent-gated telemedicine sessions + session events |
| `internal/modules/documents` | Document metadata lifecycle, inline encrypted dev storage, legal hold enforcement, audited downloads |
| `internal/modules/diagnostics` | Test catalogue, components & sex/age reference ranges, requests, specimen collect/receive/reject/recollect, result entry with auto-abnormal/critical flagging, supervisor dual-verification |
| `internal/modules/pharmacy` | Drug catalogue, FEFO stock batches & append-only movements, prescriptions (create/sign/cancel), idempotent dispensing & atomic reversals |
| `internal/modules/billing` | Payers, encrypted insurance coverages + reveal, service catalog & non-overlapping prices, draft invoices, `collect-charges`, issue/void, cashier shifts & dual-control reconciliation, idempotent payments & gapless receipts, reversals, four-eyes refunds |
| `internal/modules/privacy` | Break-glass open/end/list/four-eyes review, per-patient PHI access report, privacy alerts & detection, data-subject privacy requests, legal holds, periodic SOC 2 access reviews |
| `internal/modules/approvals` | Dual-control four-eyes approval requests (`gov.approval_request`) |
| `internal/modules/reports` | `staff-activity`, `staff-access`, and all 8 `reporting.v_*` management views |
| `internal/modules/comms` | Notification templates (versioned draft/active/retired), message delivery log, outbox listing & enqueue |
| `internal/modules/admin` | Organization settings, staff-department assignments, permissions & custom RBAC roles, API credentials, DPA-gated integration providers, governance data categories |
| `internal/modules/audit` | Cursor-paged audit event viewer (metadata only, no raw change payloads) |
| `internal/migration` | Safe, idempotent legacy-to-new data migrator & reconciler |
| `cmd/worker` | Restricted `spartial_worker` process for outbox dispatch, audit sealing (`audit.seal_pending`), and privacy alert detection (`gov.detect_privacy_alerts`) with `--once` mode |
| `cmd/manage` | `migrate`, `seed`, `bootstrap-development`, `new-encryption-key`, `rotate-keys`, `migrate-legacy` |
