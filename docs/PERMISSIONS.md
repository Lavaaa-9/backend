# Foundation authorization

Permissions come from live database rows on each protected request. Seeded roles are fixed configuration in migration 00001; role/permission administration endpoints are pending.

| Role | Implemented permissions | Scope |
| --- | --- | --- |
| administrator | staff.manage, staff.read, facilities.manage, facilities.read, audit.read | Organization staff/facility administration and audit metadata; no patient demographic or clinical access |
| reception | patients.read, patients.write, facilities.read | Demographics at assigned active facilities |
| nurse, doctor, laboratory, pharmacy, cashier, management | facilities.read | Assigned active facilities only; future clinical permissions are not implicitly granted |
| privacy-reviewer | audit.read | Organization audit metadata review only |
| patient | None | Reserved; cannot be created as a staff role; account APIs pending |

`staff.manage` commands additionally require the live administrator role. Only administrators can provision another administrator or privacy reviewer. There is no ability for ordinary staff to grant themselves permissions. Database runtime privileges prohibit modifying roles/permission definitions.

Facility identifiers on patient creation must be in the actor's active assignment set. Patient lists, individual retrieval and changes all constrain facility server-side. Inaccessible IDs return 404 for reads/edits. There is currently one registration facility per patient; cross-facility care sharing and patient/clinical relationship checks require M05 before clinical routes are exposed.

Account administration requires a current version, preserves author identity, revokes every target session, and serializes changes protecting the last active administrator. Staff emails are immutable in the current update route. Display-name and assignment changes also revoke sessions deliberately. There are no delete-account, delete-patient or delete-audit endpoints.

Session/device listing is limited to the caller's latest 100 sessions. Session revocation cannot target another user. Session metadata currently identifies client type, not device fingerprint/name. Logout, password change, suspension and administrative updates revoke tokens server-side. Changes to role-permission rows are reflected on the next request; administrative mutation APIs for those rows are not yet available.

Sensitive successful patient reads/writes and searches, account writes, login failures/successes, refresh/reuse, password change, logout, permission-middleware denials and audit queries are recorded. Audit visibility is privileged across the initial organization. Cross-facility not-found attempts and all staff-list reads still need richer audit coverage. Break-glass, consent, guardian grants and retention policy are pending. Administration never implies unrestricted clinical access.

## Second factor and session assurance (migration 00003)

Roles with `roles.privileged = true` (administrator, privacy-reviewer) need a session created after a verified second factor before any `require(permission)` route succeeds when `MFA_ENFORCE_PRIVILEGED=true`. This is forced on in production and opt-in for development/test. A denied attempt is audited as `access.denied` with reason `mfa_required`. Authentication routes (`/auth/me`, `/auth/mfa/*`, logout, password) stay available so the account can enroll.

Staff with a confirmed factor never receive a session from `/auth/login`: they receive a single-use, five-minute challenge with at most five attempts, bound to client type and to the staff version (password, role, suspension or display-name changes invalidate it). TOTP steps are accepted once only (replay protection) with one step of clock drift. Recovery codes are single use and stored as SHA-256 digests. Enrollment and disablement require password re-authentication; confirming enrollment revokes all other sessions; disabling revokes all sessions. The later engineering extension adds audited independent approval for MFA reset; the clinic must still approve its identity-check procedure.

Recovery-code regeneration requires password and an unused factor, replaces all prior codes, revokes other sessions and invalidates pending login challenges. Enrollment, confirmation, regeneration and disablement share a ten-attempt per-account 15-minute limit, with failed reauthentication/factor attempts audited. Live account/session state is rechecked in each mutation transaction.

The runtime database role cannot insert audit seals or execute sealing. Privileged management credentials create seals; audit review only verifies/reads them. Off-host anchoring is still required for detection of owner-level rewriting.

## Engineering extension (migrations 00004–00006)

Reception also has care.manage and can coordinate assignments at its assigned active facilities. Doctor/nurse/laboratory/pharmacy now have patients.read, constrained by active care evidence and patient RLS. Doctor/nurse may request exceptional access under the approved policy; privacy-reviewer independently reviews grants and reads privacy reports/alerts. An inappropriate review removes the grant's access effect; changing role never permits self-review.

Administrator gets metrics.read and approvals.request/review. Direct administrator creation/promotion is denied by normal staff commands. Both approval requester and independent reviewer require live MFA-verified administrator sessions, including in development; requester eligibility, subject version, expiry and single execution are checked. Trusted initial administrator bootstrap is maintenance-only and must follow the clinic's identity/approval process.

Production privileged and remote doctor/laboratory/pharmacy staff routes require MFA. TRUSTED_PROXIES and CLINIC_NETWORKS list exact IPs/CIDRs; untrusted forwarding cannot redefine the client or scheme. Web inactivity expires access/refresh after fifteen minutes of server-observed request inactivity. The frontend must additionally implement its human-idle lock; background polling is not proof of human presence.

Patients and patient_identifiers use FORCE RLS with transaction-local actor settings and live permission/facility/care evidence. Runtime cannot bypass RLS, own security tables, create public-schema objects, delete patients, write seals or perform key rotation. A caller who already controls the database connection can forge context settings; RLS is defense against omitted filters, not a substitute for securing credentials and parameterized SQL.
