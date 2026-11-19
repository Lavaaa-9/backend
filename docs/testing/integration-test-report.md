# Integration Test Report

## Go Integration Suite (`tests/integration/backend_complete_test.go`)
Executed in-process against the real Fiber v3 `httpapi.New` router and live `spartial_hms` PostgreSQL database under `spartial_app` and `spartial_worker` roles with `-race`:

| Test Function | Verified Workflows & Security Controls |
|---|---|
| `TestMFALifecycle` | Staff creation -> password login -> `/auth/mfa/enroll` -> RFC 6238 TOTP `/auth/mfa/confirm` (10 recovery codes) -> `/auth/me` (`mfa_verified`, `mfa_enabled`) -> login challenge -> TOTP replay blocked (`401`) -> recovery code verification (`200`) -> recovery code reuse blocked (`401`) -> recovery code regeneration -> `/auth/mfa/disable` (`204`, revokes sessions) -> password login restored |
| `TestPatientExtendedAndMerge` | Encrypted NIDA identifier (`value_ciphertext`/`value_bidx` never leaked) -> duplicate active identifier blocked (`409`) -> blind-index search -> audited reveal with reason -> contacts, next of kin, consent grant & withdraw -> fuzzy duplicate check, duplicate review -> atomic patient merge as `compliance_officer` |
| `TestClinicalHistory` | Allergies (including cross-role `pharmacy.read`), chronic problems (ICD-10 + mandatory `resolved_date` validation), medical history entries & voiding |
| `TestAppointmentsAndTelehealth` | Appointment booking + GiST overlap conflict (`409`) -> status confirmation -> telehealth session blocked without consent (`422`) -> consent grant -> session lifecycle (`scheduled -> in_progress -> completed`) + append-only `session_event` |
| `TestDocumentsAndCoverages` | Inline encrypted document upload & SHA-256 verification -> audited download -> legal hold blocking deletion (`409`) -> hold release & soft-delete as `compliance_officer` -> insurance payer & encrypted coverage creation, masked list, audited reveal, status update |
| `TestPrivacyGovernanceApprovalsReportsAndWorker` | Break-glass open -> pending review view -> four-eyes compliance review -> early end -> per-patient PHI access log -> privacy request lifecycle -> legal hold place/release -> periodic access review -> dual-control approval & self-approval rejection (`422`) -> all management reporting views -> comms template activation + outbox enqueue -> `spartial_worker` claim/complete/audit-seal/alert-detect |
| `TestAdminPortalResetAndDevices` | Org settings update -> permissions & custom RBAC role -> API credential creation (secret returned once, `key_hash` hidden) & revoke -> DPA-gated integration provider config -> governance data categories -> patient portal account activation & suspend -> non-enumerating password reset request/confirm -> encrypted push device register & revoke |

## End-to-End HTTP Smoke Script (`scripts/smoke_api.py`)
Covers a full patient visit across 11 seeded role accounts (`admin`, `reception`, `nurse`, `doctor`, `laboratory`, `lab-supervisor`, `pharmacy`, `cashier`, `billing-supervisor`, `management`, `privacy-reviewer`) exercising queue, triage, consultation, diagnosis, follow-up, lab specimens/results/verification, pharmacy stock/prescribing/idempotent dispensing, procedures, billing charge collection/invoicing/idempotent payments/receipts/four-eyes refunds, identifiers, contacts, NOK, consents, allergies, problems, history, appointments, telehealth, documents, coverages, break-glass, privacy access, approvals, and reports.
