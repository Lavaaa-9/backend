# Module register

Status recorded 2026-11-17. Overall backend: **in progress**, initial Milestone A vertical slice implemented and internally tested. Module status reflects each module's full required scope, not merely its current routes. None is clinic accepted. There are no placeholder clinical handlers or fake provider successes.

Evidence shorthand: F = `tests/integration/foundation_test.go` (`TestFoundation` with authorization, concurrent registration, profile versions, cross-facility, audit, refresh, account lifecycle, browser, worker, password/session and rate-limit subtests); A = `TestConcurrentAdministratorProtection`; S = `internal/security/security_test.go`.

| Module | Status | Implemented endpoints / schema / evidence | Remaining required work |
| --- | --- | --- | --- |
| M01 Authentication/user permissions | in progress | `/auth/login`, `/refresh`, `/me`, `/logout`, `/sessions`, `/sessions/{id}/revoke`, `/password`; GET/POST `/staff/users`, PUT `/staff/users/{id}`. Migration 00001: staff, fixed roles/permissions, assignments, sessions, hashed rotating refresh tokens, durable rate limits. F, A, S. | Invitation/activation, reset/recovery delivery, TOTP MFA and recovery codes, runtime Argon2 parameter policy/upgrades, granular role/permission administration/delegation, richer device management, staff search. Direct provisioning is not invitation activation. |
| M02 Facility/departments/practitioners | in progress | GET/POST `/staff/facilities`; facility table, timezone and assignment checks in 00001. F. | Updates/deactivation, settings, departments, rooms, service locations, practitioner identity/profiles/assignments/hours, bounded pagination beyond initial 100 facilities. |
| M03 Catalogues/prices | not started | None. | All catalogues, provenance/versioning, approved references, exact money/rounding policy and historical snapshots. |
| M04 Patient registry | in progress | POST/GET `/staff/patients`, GET/PUT `/staff/patients/{id}`; patients, MRN sequence, version, indexed facility scope, idempotency in 00001. F; live demo. | External identifiers, next of kin, richer contacts/address, duplicate suggestions, supervised merge/preview/correction, archive policy. |
| M05 Privacy/consent/audit | in progress | GET `/staff/audit`; append-only audit table/trigger in 00001; permission/facility checks, audited demographic reads/writes and sensitive auth/account actions. F. | Consent/guardian authority, clinical relationship/state policy, break-glass eligibility/review/expiry, retention/holds, richer audit filters and denied/read coverage. Clinical APIs must wait for appropriate policy. |
| M06 Private documents | not started | Optional MinIO Compose infrastructure only; no document endpoints/schema. | Metadata, uploads/checksums/limits, fail-closed ClamAV adapter, quarantine, versions, authorized short download access, cleanup and file restoration. |
| M07 Appointments | not started | None. | Availability, slot/capacity locking, booking/rescheduling/cancellation/no-show, check-in/reminders, concurrency verification. |
| M08 Encounters/queues | not started | None. | Walk-ins, idempotent check-in, participants/stations/transitions/history/closure, diagnostic return path. |
| M09 Nursing/triage | not started | None. | Structured observations/units/corrections/handover, approved configurable escalation. |
| M10 Consultations/longitudinal record | not started | None. | Draft/save/sign/amend/versioning, diagnoses/history/allergies/problem lists and clinical access policy. |
| M11 Orders/referrals/tasks | not started | None. | Guarded order lifecycle, responsible owners, due/reassign/review/acknowledgement and unresolved tasks. |
| M12 Laboratory | not started | None. | Panels/specimens/link consistency, collection/rejection/results/verification/release/amendment, approved units/ranges and result review. |
| M13 Imaging/procedures | not started | None. | Worklists/scheduling/performance, reports/approval/amendments/attachments and provenance. |
| M14 Prescribing | not started | None. | Authorized practitioner medication orders/signing/cancellation/history and allergy visibility. |
| M15 Pharmacy/stock | not started | None. | Batch FEFO/expiry/blocks, receipt/movements/adjustments/returns, atomic partial dispense and oversupply concurrency checks. |
| M16 Billing/invoices | not started | None. | Unique billable-source contracts, price snapshots, exact decimal accounting, issue/discount/credit/correction/balance/overpayment. |
| M17 Payments/refunds/reconciliation | not started | None. | Manual settlement, allocation locks/idempotency, receipts, approved refunds, cashier sessions/count/reconciliation and authentic provider evidence. |
| M18 Notifications/durable delivery | in progress | Outbox in 00001, explicit development sink in 00002, separate worker; claim/lease/fencing/retry/failure logic. F. | Channel adapters, templates/preferences/reminders, delivery attempts/states/review, real provider contracts; development sink is not delivery. |
| M19 Reporting/exports | not started | None. | Operational and financial reports with defined date bases, reconciled totals, export permissions, async expiration/downloads. |
| M20 Patient/guardian API | not started | None; reserved patient role has no staff access. | Accounts/recovery, staff-verified linkage, guardian grants/revocation, ownership checks, correction requests and explicit publication. |
| M21 Telemedicine | not started | None. | Internal scheduling/consent/lifecycle/provider interface. External session creation blocked on selected provider contract; no fake working URLs. |
| M22 External framework | not started | None. | Provider config/client bounds, webhook auth/replay/reordering, ID mapping/reconciliation/status; external contracts absent. |
| M23 POWERHMS migration | not started | None. | Canonical CSV/JSON/staging/dry run/idempotent import/rejections/reconciliation/provenance, synthetic examples, cutover/freeze/delta/rollback. Real mapping unverified pending authorized exports. |
| M24 Operations/release | in progress | Health/readiness/startup role checks, redacted structured logs/request IDs, deadlines/pool/shutdown, embedded migrations, non-root multi-stage image, dev Compose, CI, seed, OpenAPI, synthetic demo and restore rehearsal script. | Protected metrics/tracing, performance baselines, OS image scanning, attachment backup/recovery, production deployment/secrets/PITR/alerts/runbook validation, clinic security/acceptance. CI configured but hosted execution unobserved. |

## Verification evidence

The following commands have been executed locally against synthetic data, with real PostgreSQL 18.3 and the restricted runtime role:

- `scripts/dev-db.sh`: cluster started, migrations 00001/00002 applied, roles granted, generated accounts seeded.
- `go test -count=1 ./...` with TEST_DATABASE_URL: passed, including isolated-database PostgreSQL tests.
- `go test -race -count=1 ./...` with TEST_DATABASE_URL: passed; no race report.
- `go vet ./...`: passed.
- `go build ./cmd/...`: passed; API and worker binaries also built and run.
- `go mod verify`: all modules verified.
- `go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...`: no vulnerabilities found.
- `go run github.com/getkin/kin-openapi/cmd/validate@v0.133.0 api/openapi/openapi.json`: passed.
- `python3 scripts/generate-openapi.py`: generated the 20-operation implemented contract.
- `podman build -t localhost/spartial-backend:development .`: passed; non-root image produced.
- Live `python3 scripts/demo.py`: passed readiness, receptionist journey, privileged denial, refresh and logout.

Additional verification: `make check` passed formatting, vet, race tests, builds and checksum verification. `python3 scripts/restore-check.py` passed an isolated synthetic PostgreSQL restore with matching table counts after quiescing writes. Compose v2.40.3 `config --quiet` passed. A read-only, non-root container passed readiness and the live demo; the disposable smoke container was then stopped. The entire Compose stack was not run; a system Docker/Compose installation was absent. Bash syntax and Python compilation checks passed. Details and exact continuation commands are in CONTINUE_HERE.md. None of these checks demonstrates an unimplemented clinical/financial workflow, patient-file restoration, external delivery, a video call, or real POWERHMS import.
