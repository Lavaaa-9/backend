# Database Transition Architecture & Dependency Map

## Executive Summary
The Spartial HMS backend has transitioned from its legacy 6-migration goose schema (`db/migrations/00001`–`00006`, public schema) to the enterprise **`spartial_hms`** PostgreSQL schema (`db/migrations/0001`–`0018`, 16 domain-separated schemas with database-enforced RLS, state machines, and tamper-evident audit sealing).

## Legacy-to-New Component Dependency Map

| Application Layer | Legacy Dependency (Removed from Runtime) | New `spartial_hms` Authoritative Source |
|---|---|---|
| Session / Request Context | `app.actor_id` GUC + `public.sessions` | `sec.begin_request(session_id, request_id)` deriving `org_id`, `user_id`, `membership_id`, `perms`, `break_glass` |
| Identity & MFA | `public.staff`, `staff_mfa`, `mfa_recovery_codes`, `login_challenges`, `refresh_tokens` | `iam.user_account`, `iam.password_credential`, `iam.password_history`, `iam.membership`, `iam.membership_role`, `iam.mfa_factor`, `iam.mfa_recovery_code`, `iam.mfa_challenge`, `iam.password_reset`, `iam.session`, `iam.refresh_token`, `iam.api_credential` |
| Organization & RBAC | `public.facilities`, `roles`, `permissions`, `role_permissions`, `staff_facilities` | `org.organization`, `org.facility`, `org.department`, `org.service_point`, `org.staff_department`, `org.counter`, `iam.role`, `iam.role_permission`, `ref.permission` |
| Patient Master Index | `public.patients`, `patient_scopes`, `patient_identifiers` | `patient.patient`, `patient.contact_point`, `patient.identifier`, `patient.next_of_kin`, `patient.consent`, `patient.verification`, `patient.portal_account`, `patient.duplicate_review`, `patient.merge_event` |
| Clinical & Scheduling | *(Not present in legacy)* | `clinical.allergy`, `clinical.problem`, `clinical.history_entry`, `clinical.appointment`, `clinical.encounter`, `clinical.queue_ticket`, `clinical.triage_assessment`, `clinical.nursing_observation`, `clinical.consultation`, `clinical.encounter_diagnosis`, `clinical.procedure`, `clinical.follow_up_plan`, `telehealth.session`, `telehealth.session_event`, `document.document` |
| Diagnostics & Pharmacy | *(Not present in legacy)* | `diag.*` (catalogue, components, reference ranges, requests, specimens, results), `pharmacy.*` (drugs, stock batches, append-only movements, prescriptions, dispenses) |
| Billing | *(Not present in legacy)* | `billing.*` (payers, encrypted coverages, service catalog, prices, cashier shifts, invoices, lines, payments, allocations, receipts, four-eyes refunds) |
| Privacy, Governance & Approvals | `public.break_glass_access`, `care_assignments`, `approval_requests`, `privacy_alerts` | `iam.break_glass_access`, `iam.access_review`, `iam.access_review_item`, `gov.approval_request`, `gov.privacy_alert`, `gov.privacy_request`, `gov.legal_hold`, `gov.data_category` |
| Audit & Outbox / Worker | `public.audit_events`, `audit_seals`, `outbox`, `development_event_sink` | `audit.event` (monthly partitioned), `audit.seal`, `comms.notification_template`, `comms.outbox`, `comms.message`, `comms.device`, `integration.provider_config`, `integration.inbound_event` |
| Reporting | *(Ad-hoc queries)* | `reporting.v_*` (`security_invoker = true`) + `reporting.staff_activity()`, `reporting.staff_access()` |

## Legacy Dependency Removal Status
- **Runtime Go code (`main.go`, `cmd/`, `internal/`):** 0 runtime queries against legacy public tables. Legacy migration SQL files under `db/migrations/00001..00006` are retained strictly for historical auditability and disposable-DB legacy migration tests (`internal/migration/legacy_test.go`).
- **Role separation:** The API runs exclusively as `spartial_app` and the background worker exclusively as `spartial_worker`; both are checked at startup by `platform.CheckRuntimeRole` to refuse superuser, `BYPASSRLS`, or `spartial_owner` memberships.
