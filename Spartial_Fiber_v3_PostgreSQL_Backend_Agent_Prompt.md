# Spartial Polyclinic Full Backend Implementation Prompt

Copy the prompt below into the coding agent working inside the backend repository.

---

You are the backend engineer implementing the complete backend for Spartial Specialized Polyclinic. Build the software in this repository, run it, test it and document how the frontend and mobile developers use it. Do not stop at a proposal, database models, empty routes or scaffolding.

## 1 Product and scope

Build a clinic management backend supporting staff administration, patient records, appointments, clinical visits, nursing, consultations, diagnostics, prescriptions, pharmacy stock, billing, payments, reporting, patient access, telemedicine, notifications and migration from POWERHMS.

The staff website and patient mobile application will use the same backend with different authorization policies. This task is backend only. Do not spend time building frontend screens.

This prompt replaces the earlier Go/Gin framework choice with **Go Fiber v3**. Use **PostgreSQL** as the authoritative database.

The first working session is approximately three hours. Prioritize the foundation milestone in that session. The full backend remains the overall objective; do not cut the scope or claim that all modules are complete because the first session ends. Track and report exact implementation status.

Treat existing project documents as context. This prompt explicitly requests basic internal laboratory, diagnostic-report, prescribing and dispensing workflows. Specialized device connections, insurance claims, inpatient care, payroll, AI diagnosis, a full PACS archive, independent-clinic SaaS tenancy and full offline clinical synchronization are outside this implementation unless already explicitly authorized elsewhere in this repository.

## 2 Inspect before changing

1. Read repository instructions, README, existing code, migrations and tests.
2. Inspect the working tree and preserve unrelated work. Extend existing functioning code rather than creating a competing backend.
3. Identify what is implemented, incomplete, broken or absent.
4. Check the official Fiber v3 documentation and the selected release's go.mod before choosing the Go toolchain. Pin compatible stable dependency versions in go.mod and commit go.sum. Use github.com/gofiber/fiber/v3 and v3-compatible middleware. Do not import Fiber v2 or mix Gin into the application.
5. Write a short implementation plan and proceed immediately with the first milestone. Make routine implementation decisions yourself and record assumptions. Ask only when a missing decision blocks safe progress.
6. Use development databases and synthetic patients. Do not alter production data or deploy to a live clinic as part of this task.

## 3 Technical architecture

- Use a modular monolith with one coordinated codebase, a Fiber API process and a background worker process.
- Use PostgreSQL with pgx/v5 and pgxpool; prefer sqlc for typed SQL queries. Keep handwritten domain services separate from generated query code. Use one versioned SQL migration tool consistently.
- Keep Fiber handlers thin: bind input, validate, authorize, call a service and return a documented response.
- Services enforce domain rules and orchestrate transactions. Repositories/query code access the database. Do not put business workflows directly in handlers.
- Use explicit request/response DTOs; never bind public requests directly into database entities.
- Use standard context.Context and bounded deadlines for database/provider operations. Do not pass Fiber contexts or request-backed buffers into workers or keep them after handlers return. Copy needed values or use the documented immutable option deliberately.
- Configure Fiber v3 binding/validation and map invalid input to consistent 400/422 responses rather than accidental 500 errors.
- Use private S3-compatible object storage for files, with a local development service in Compose. PostgreSQL owns file metadata and permissions.
- Use PostgreSQL-backed durable jobs and transactional outbox records initially. Redis must not be required unless an existing justified dependency needs it.
- Use structured logging, environment-based configuration, graceful shutdown and explicit startup validation.
- One organization initially; model facilities/departments and enforce facility assignment. Do not build a multi-tenant platform implicitly.
- Store timestamps in UTC and display/report using the configured clinic timezone, initially Africa/Dar_es_Salaam. Preserve date-only and approximate birth information correctly.
- Store monetary amounts using exact numeric/decimal handling and a documented TZS rounding policy, never binary floating point. Encode amounts consistently in the API, preferably decimal strings.

Suggested layout, adaptable to the repository:

```text
cmd/api
cmd/worker
internal/config
internal/platform
internal/modules/identity
internal/modules/facilities
internal/modules/catalogue
internal/modules/patients
internal/modules/privacy
internal/modules/documents
internal/modules/appointments
internal/modules/encounters
internal/modules/nursing
internal/modules/clinical
internal/modules/orders
internal/modules/laboratory
internal/modules/diagnostics
internal/modules/prescriptions
internal/modules/pharmacy
internal/modules/billing
internal/modules/payments
internal/modules/notifications
internal/modules/reports
internal/modules/patientaccess
internal/modules/telemedicine
internal/modules/integrations
internal/modules/migration
db/migrations
db/queries
api/openapi
tests/integration
deploy
docs
```

A module is a responsibility boundary, not a separate microservice. Cross-module transactions may be coordinated by application services without bypassing module rules.

## 4 Required modules

### M01 Authentication user management and permissions

Implement staff invitation/activation, login, logout, current-user profile, password change/reset, account suspension/reactivation, staff creation/update/list/search and session/device management. Prevent deletion of historical clinical authors when an account is disabled.

Implement roles, granular permissions and facility/department assignments. Seed administrator, reception, nurse, doctor, laboratory, pharmacy, cashier, management, patient and privacy-reviewer roles with explicit permissions. Allow authorized role/permission administration. Protect privileged roles and prevent users from granting permissions they are not authorized to delegate. Prevent disabling/removing the last usable administrative account.

Use Argon2id password hashing through a maintained implementation, configurable safe parameters, cryptographic tokens and rate-limited authentication. Store reset/invitation tokens as hashes, expiring and single-use. Do not expose credentials, tokens or password hashes in API responses or logs.

Implement short-lived access credentials with server-side session checks, rotating hashed refresh tokens, reuse detection and revocation. Validate token algorithm, issuer, audience and expiry. Web refresh credentials belong in secure HttpOnly cookies with documented CSRF/origin protection; mobile refresh credentials use the documented mobile flow. Do not put tokens in URLs or prescribe localStorage for browser refresh tokens.

Support TOTP MFA enrollment/confirmation, login challenge, disablement with reauthentication, hashed recovery codes and audited recovery for privileged users. Revocation, suspension and permission changes must take effect on protected requests without waiting for a long token expiry. Never trust role claims alone indefinitely.

### M02 Facility departments rooms and practitioners

Implement clinic/facility settings, departments, service locations, rooms, practitioner profiles and active assignments. Keep practitioner identity separate from login identity. Support working hours and inactive records without rewriting history. Restrict administrative changes by permission.

### M03 Service clinical and price catalogues

Implement consultation/services, test panels/components, units, medication forms/strengths, procedure types, clinical templates and diagnosis code references. Store source/code system/version where relevant. Separate medicine identity from stock batches. Support active dates and versioned price lists. Snapshot applied price, description, unit and relevant clinical reference context into historical transactions. Do not ship invented diagnosis or medication knowledge as clinical truth.

### M04 Patient registry and identity

Implement patient registration, generated unique MRN, profile updates, paginated search, external identifiers, contact details, address and next of kin. Support unknown/approximate birth dates and optional identifiers. Shared phone numbers are valid. Do not use names or phone numbers as primary keys.

Add duplicate suggestions and a supervised merge workflow: preview conflicts/references, authorized confirmation, preserved identifiers and audit trail. Never automatically merge patients. Document the correction process for an incorrect merge; do not promise automatic unmerge without a tested implementation.

### M05 Privacy consent audit and exceptional access

Implement policy enforcement combining permissions, facility, patient relationship and record state. Record consent purpose/version/status, guardian authority and revocation. Build authorized audit queries for sensitive reads, exports, writes, signing, corrections, role changes and exceptional access.

Audit records identify actor, patient/resource, action, time, request ID and outcome without copying entire medical notes into ordinary logs. Make audit append-only for application roles; document privileged database limits. Implement reasoned, time-limited break-glass access with a review task and narrowly defined eligibility. System administration does not automatically grant unrestricted clinical access. Support retention/hold metadata without inventing legal retention periods.

### M06 Private patient documents

Implement upload initiation/completion, metadata, patient/encounter linkage, file checksums, size/type limits, quarantine/scan state, versions and authorized retrieval. Use a real configurable scanning adapter such as ClamAV for the supported deployment. Fail closed for unavailable scanning: keep the file quarantined. Do not mark an unscanned upload clean.

Issue short-lived download access after authorization and audit. No public patient buckets or filesystem paths exposed to clients. Handle incomplete uploads and orphan cleanup safely. Include attachment restoration in backup documentation.

### M07 Appointments and scheduling

Implement availability, capacity/slots, appointment requests, confirmation, rescheduling, cancellation, no-show and check-in linkage. Check facility/practitioner availability. Protect final available capacity under concurrent booking. Support walk-ins without appointments. Prevent duplicate booking/check-in effects and queue reminders after commit.

### M08 Visits reception and queues

Implement encounter creation, patient linkage, visit type, participants, queue stations, priority, call/start/complete/redirect actions and encounter closure/cancellation. Preserve transition history. One patient has many encounters. Clinical and payment status remain separate. Repeated check-in must not create another encounter. Allow diagnostic routing and return to the clinician in the original encounter.

### M09 Nursing observations and triage

Implement structured observations with units, event/recording timestamps and recorder identity, triage assessments, notes, repeats and handover. Preserve corrections. Validate formats and units. Use configurable clinician-approved warning/escalation rules; do not invent medical thresholds or block care using arbitrary clinical assumptions.

### M10 Consultations and longitudinal clinical records

Implement complaints/history, findings, diagnoses, allergies, problem list, treatment plan and visit history. Distinguish unknown allergies from a recorded absence of known allergies. Support draft/save/sign and attributable amendments or entered-in-error records. Signed content must not be silently overwritten or hard-deleted. Use expected versions to reject stale concurrent edits.

### M11 Orders referrals and care tasks

Implement diagnostic/procedure requests, reasons, priorities, destinations, cancellation rules, fulfillment references, referrals and follow-up tasks. Track owner, due date, reassignment, result review and acknowledgement. Closing an encounter must not remove unresolved care tasks. Results received later must reach a responsible clinical owner.

### M12 Laboratory workflow

Implement test/panel requests, accession/sample identifiers, collection, receipt, rejection/recollection, structured results, verification, release and amendments. Verify patient/order/specimen consistency. Store applicable units/reference intervals from approved configuration. Distinguish preliminary, verified and released reports. Create result-review tasks and support configurable critical-result notification/acknowledgement. Laboratory instrument integration is separate.

### M13 Imaging and procedure reporting

Implement worklists, scheduling where applicable, performed/cancelled states, report drafts, approval/amendments and attachments. Preserve performer, patient, encounter and order context. Support external report provenance. Do not equate PDF storage with a PACS/DICOM diagnostic viewing platform.

### M14 Prescribing and medication history

Implement medication orders with form, strength, dose/unit, route, frequency, duration, quantity, instructions, prescriber and signing/cancellation history. Historical reported medications are distinct from newly issued prescriptions. Restrict prescribing to authorized practitioners. Show recorded allergies. Changes must be visible to pharmacy and must preserve quantities already supplied. Do not fabricate automated interaction checking without an approved knowledge source.

### M15 Dispensing pharmacy and stock

Implement medicine batches/lots, locations, expiry, receiving, stock movements, counts, controlled adjustments, blocked batches, dispensing, partial dispensing and returns. Include minimal supplier/receipt references for stock receiving; full procurement/accounting is outside scope.

Use approved FEFO allocation and explicit unit conversions. Atomically record dispensing, batch deductions and policy-required charge effects. Lock/check both stock availability and prescription remainder to prevent oversupply. Prevent negative stock and dispensing expired/blocked medicine. Returned medicine needs suitability approval; refunds do not automatically add usable stock. Inventory balances must reconcile with movements.

### M16 Charges invoices and adjustments

Implement charge creation from unique source events, invoice drafts/issuance, line snapshots, approved discounts, credit notes, cancellations and balance calculations. Define service-specific charge timing: request, performance or dispensing. Avoid duplicate billing across these stages. Preserve posted records with linked corrections. Support explicit unapplied patient credit/overpayment; insurance claim submission is outside scope.

### M17 Payments receipts refunds and reconciliation

Implement manual cash/bank/mobile-money recording, payment attempts/confirmation, allocations, partial payments, unique receipts, refunds with approval, cashier sessions, counted cash, close and discrepancy review. Verify external payments only from authenticated provider evidence/reconciliation. A timed-out payment remains uncertain until checked.

Prevent duplicate settlement, over-allocation, refunds exceeding eligible settlement and repeated refund application. Serialize or atomically guard competing payment/refund/allocation requests. Payment confirmation, allocations and receipt issuance must be consistent. Keep financial clearance rules separate from clinical access; urgent-care exceptions require governed reasons.

### M18 Notifications and durable delivery

Implement templates, patient communication preferences, reminders, channel adapters, attempts, retries, delivery states and failed-job review. Keep message content minimal. An outbox event and its originating record must commit together. Delivery success does not complete a clinical task. Provide a safe development sink that cannot be mistaken for real delivery. Production providers require explicit configuration.

### M19 Reporting and controlled exports

Implement visit/service counts, queue waiting times, pending-result/follow-up lists, diagnostic turnaround, invoiced revenue, collected money, balances, cashier reconciliation and stock reports. Define date basis and inclusion/exclusion of reversals. Restrict sensitive exports, process large jobs asynchronously and expire downloads. Totals must trace to underlying records; billed revenue and cash collected are different metrics.

### M20 Patient and guardian API

Implement patient account invitation/registration, sign-in/recovery, verified linkage, own profile, correction requests, appointments and explicitly released summaries/results/prescriptions/balances. Never link an existing medical record merely because the account has the same phone number. Record staff identity verification or another approved linkage method. Add narrowly scoped guardian grants with revocation. Enforce ownership on lists, individual records, files and exports. Internal drafts are not automatically patient-visible.

### M21 Telemedicine backend

Implement scheduling/linkage, consent metadata, eligibility/status, session creation, authorized short-lived participant access, waiting/join/end states and clinical note linkage. Define a provider interface and implement the selected provider only when its contract/configuration is known. Never return a fabricated working call URL. Without a provider, return a documented not-configured response and report the external session path as blocked. Keep video recording disabled by default.

### M22 External integration framework

Implement provider configuration references, isolated clients, authenticated webhook handling, external ID mapping, deduplication, reconciliation, timeouts, bounded retries and health/status reporting. Verify signatures/authentication before applying changes. Handle replayed and reordered events using valid lifecycle transitions. Restrict outbound destinations and avoid user-controlled arbitrary fetch URLs. Never silently fall back to fake success. No invented POWERHMS, payment, insurance or messaging APIs.

### M23 POWERHMS migration tooling

Implement staging, batch tracking, source-to-target identifiers, dry-run validation, repeatable import, rejected-row reports and reconciliation output. Provide a documented canonical import format and synthetic example CSV/JSON files. Real POWERHMS transformations depend on authorized sample exports; mark those mappings unverified until samples exist. Preserve provenance and attachment checksums. Re-running a batch must not duplicate records. Include cutover, delta/freeze and rollback/reconciliation procedures; do not overwrite new live records with an old backup.

### M24 Operations quality and release

Implement liveness/readiness, structured redacted logs, metrics, request IDs, startup checks, graceful shutdown, connection limits, safe error handling and dependency scanning. Protect metrics/debug endpoints. Add slim multi-stage non-root Docker builds, pinned dependencies/images, Compose development setup, CI, backup/restore procedures and deployment documentation. Keep secrets out of source, examples, images and logs. Do not automatically deploy production.

## 5 Non-negotiable data and workflow rules

1. Use database uniqueness, foreign keys and checks, not application checks alone.
2. Enforce patient/facility/encounter consistency server-side, including cross-module links and attachments.
3. Do not expose generic unrestricted PATCH status endpoints for signing, approval, dispensing or settlement. Use explicit guarded commands.
4. Keep clinical drafts, signed versions, amendments and provenance. Financial and stock history use controlled reversals/adjustments.
5. Implement durable idempotency for important write operations. Scope keys by actor/operation, compare request hashes and replay the committed response. Also enforce business-source uniqueness so changing the key cannot duplicate a charge or fulfillment.
6. Use transactional writes and short-lived row locks or atomic updates for scarce capacity, stock, prescription remainder and money allocation. Use stable lock ordering and safe retry handling.
7. Do not call external providers while holding database locks. Use outbox/workers where appropriate.
8. Workers use leases/locking, bounded retries, backoff and visible failures. Assume at-least-once delivery and make consumers idempotent.
9. Lists use bounded pagination, allowlisted sorts/filters and appropriate indexes. Avoid N+1 queries and unbounded eager loading.
10. Restrict CORS to configured clients; configure trusted proxies carefully. Rate-limit sensitive actions, bound body sizes, reject mass assignment and redact sensitive errors.
11. Separate deactivation/archiving from destructive deletion. Do not apply a blanket soft-delete pattern that breaks uniqueness, clinical history or financial reconciliation.
12. Record unresolved clinical/operational policies explicitly. Do not silently invent retention periods, clinical alert thresholds or legal approvals.

## 6 API contract and documentation

- Version routes under /api/v1 and group by domain. Keep patient-facing routes separate from staff routes.
- Publish a complete OpenAPI specification matching implemented behavior, with authentication, permissions, payloads, pagination, state transitions, errors and synthetic examples.
- Use consistent machine-readable errors including code, message, field errors where relevant and request_id. Use 401/403/404 deliberately, 409 for conflicts and appropriate validation responses. Do not leak resource existence through patient recovery or unauthorized endpoints.
- Document request/response behavior for login/refresh/MFA, facility/staff administration, register/search patient, open encounter, save/sign note, order/verify/release result, prescribe/dispense, invoice/pay/refund and patient release.
- Include runnable curl examples and an API collection or equivalent script for the full test-patient journey.
- Document exact decimal/time/date formats and idempotency/version headers or fields.

## 7 Implementation order

### Milestone A — Current session foundation

Inspect and start the project; establish configuration, migrations, PostgreSQL, health checks, error handling and OpenAPI structure. Implement staff login/logout, account management, initial roles/permissions and facility assignment. Build patient registration/search/profile with unique MRNs and audit. Seed synthetic accounts using explicit development-only credentials or generated values. Demonstrate a receptionist accessing permitted patient operations and being denied privileged actions.

Aim for a tested, running vertical slice in the first three hours. If existing code is absent or broken, prioritize correctness of that slice and report unfinished work. Full MFA, merge, guardian management and other deeper foundation features remain tracked requirements even if they need later milestones.

### Milestone B — Complete foundations and clinical visit

Finish remaining M01–M06 capabilities, then M07–M11. Implement registration to nurse to signed consultation. Define catalogue and billable-event contracts before clinical fulfillment expands.

### Milestone C — Diagnostics medicines and revenue

Implement M12–M17 and reconciled reporting foundations. Prove orders/results, partial dispense/stock and charges/payments/refunds under failure and concurrency.

### Milestone D — Patient services and integrations

Implement M18–M22 and complete M19. Internal workflows must work without external vendors; external actions must report not-configured or blocked honestly until integrated and tested.

### Milestone E — Migration hardening and handover

Complete M23–M24, performance/security checks, recovery rehearsal and documentation. Migration discovery and automated testing start earlier and continue throughout.

Continue through milestones while execution is available. Do not stop after scaffolding or one completed module if the assigned session permits more work. At a session boundary, save a precise continuation record with next tasks and commands. Never silently reduce the full scope to CRUD endpoints.

## 8 Required verification

Use domain unit tests, API authorization tests and integration tests against real PostgreSQL. SQLite and mocks do not prove PostgreSQL locking/constraints. Isolate test data and databases. Run gofmt, go vet, go test ./..., applicable race tests and build checks; add dependency vulnerability checks when available. State exact commands and results.

Cover at least:

- Account suspension/logout, refresh reuse detection, role changes, MFA recovery and privilege escalation.
- Cross-patient/cross-facility access through ordinary endpoints, files, exports and guardian APIs.
- Shared phone numbers, unique MRNs and supervised duplicate handling.
- Concurrent final-slot booking and repeated check-in.
- Stale edits and attempted mutation of signed clinical records.
- Incorrect specimen linkage, rejected samples and amended result review.
- Concurrent/partial dispensing, prescription remainder, expired batches and transaction rollback.
- Price snapshots, duplicate charge events, simultaneous allocations, overpayments, refunds and exact balances.
- Duplicate/reordered webhooks and uncertain payment outcomes.
- Outbox crash/retry behavior and provider outages without lost committed care.
- Quarantine and unauthorized document retrieval.
- Repeated imports, rejected records and financial/attachment reconciliation.
- Database plus file restoration and representative performance checks.

Use a complete synthetic journey: register a patient, book/check in, triage, sign consultation, order and approve a result, prescribe and partially dispense, issue invoice, record partial/full payments, reconcile reports and read only released records through patient access.

## 9 Required deliverables and truthful status

Deliver working source, migrations, compatible dependency locks, development Compose configuration, Dockerfiles, environment example, CI, development seed command, OpenAPI, API examples and meaningful tests.

Create and maintain:

- README.md with exact local setup, migrate, seed, run and test commands.
- docs/ARCHITECTURE.md with module ownership, transaction boundaries and chosen versions.
- docs/MODULE_STATUS.md listing M01–M24 with implemented endpoints, migrations, tests, gaps and evidence.
- docs/PERMISSIONS.md with roles/actions/scope and privileged exceptions.
- docs/WORKFLOWS.md with lifecycle rules and corrections.
- docs/INTEGRATIONS.md with configured providers, contracts and external blockers.
- docs/OPERATIONS.md with deploy, secrets, backup/restore, downtime and rollback procedures.
- docs/CONTINUE_HERE.md with the exact next work if the session ends.

Allowed status labels: not started, in progress, internally tested, externally blocked and accepted. Internally tested is not clinic acceptance. A module with empty handlers, placeholder responses, mock success or missing critical rules is incomplete.

At the end report: what works, which commands actually passed, how to run the backend, how frontend/mobile developers connect, remaining gaps and the next concrete task. Do not claim production readiness without the relevant verification and clinic acceptance. Do not claim provider delivery, video calls or real POWERHMS migration were tested without the required external evidence.

Start now by inspecting the repository and implementing Milestone A. Carry the full module register forward until the backend is complete.

---

Framework references for the implementing agent to verify against its pinned release:

- https://docs.gofiber.io/
- https://docs.gofiber.io/guide/validation/
- https://docs.gofiber.io/api/bind/
- https://github.com/gofiber/fiber

Prepared for Dadi — 17 November 2026.
