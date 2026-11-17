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
