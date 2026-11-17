# Implemented lifecycle rules

## Staff and credentials

A development administrator provisions an active staff account with role and active facility assignments. This direct provisioning path is implemented; invitation/activation and email recovery delivery are pending. Passwords need 12–256 characters. Users authenticate as web or mobile; this audience cannot be switched during refresh.

Access expires after 15 minutes, session after seven days. Refresh atomically consumes the old refresh token and creates a new one; the previous access token stops working. Reusing an already consumed refresh token revokes its entire session. Clients must serialize refresh calls and sign in again after an ambiguous rotation/reuse failure. Refresh tokens never appear in URLs. Browser refresh tokens are never returned in JSON.

Logout revokes the current session; session management can revoke another owned session. Password change requires the current password and revokes all sessions. Staff update/suspension/reactivation also revokes all target sessions. Disabled users cannot log in or refresh. History is preserved. Optimistic account versions reject stale changes; serialized administration protects the final active administrator.

## Patient registry

Register demographics at an assigned active facility with an `Idempotency-Key` of 8–128 characters. Keys are scoped to actor and operation, persist in PostgreSQL, and are compared against a normalized request hash. A repeat returns the original 201 response. Reusing a key for another payload returns 409. The current foundation retains these records without automatic cleanup; retention requires a later governed policy.

The database generates a unique MRN. Names/phones are never identity keys; shared phones are accepted. `birth_precision=unknown` omits date/year; `year` supplies only `birth_year`; `exact` supplies only a valid past/current `birth_date`. Approximate birth is never converted into a fabricated January 1 date.

Search uses bounded UUID pagination and literal substring matching. Edits fully replace the demographics while preserving MRN/facility/creation time. Supply the last returned version; concurrent stale edits get 409. No patient deletion or automatic merge exists.

Incorrect registration corrections currently use an audited demographic edit with expected version. Duplicate suggestions, merge preview, supervised merge and the correction process for an incorrect merge are **not implemented**. Do not merge directly in SQL or promise automatic unmerge.

## Audit and events

Patient mutation and its audit entry share a transaction. Search/read audit failures prevent returning protected data. Audit rows contain identifiers/action/outcome/request ID rather than patient notes. Runtime SQL grants and triggers block update/delete/truncate. A sufficiently privileged database administrator can disable triggers or modify backups; this is not a cryptographic tamper-proof ledger.

Registration also creates a durable `patient.registered` outbox event. The development worker acknowledges it into `development_event_sink` with lease fencing and idempotency. This means **development observation**, never successful patient messaging. Unsupported events retry and become failed; expired leases can be reclaimed. Failed jobs currently require authorized operational SQL review; a staff review API is pending.

## Workflows awaiting implementation

Booking/check-in, encounter queues, triage, draft/sign/amend notes, orders, sample collection/verification/release, diagnostics, prescribing/partial dispensing, charges/invoicing/payments/refunds, patient publication and telemedicine do not have routes yet. Their required lifecycle rules remain in the original specification. Do not use demographic CRUD or outbox completion as evidence those workflows work.

No clinical warning thresholds, consent policy, retention duration, financial-clearance restriction, monetary rounding mode or external-provider contract has been invented. These need explicit approved configuration as their modules are built.
