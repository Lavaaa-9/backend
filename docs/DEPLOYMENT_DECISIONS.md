# Deferred hosting and clinic decisions

The user asked to continue implementation and explicitly defer production hosting, offsite storage and mail-provider decisions to documentation. No production resources, accounts, DNS, data transfers or provider messages have been created.

| Decision | Required evidence before go-live | Current state |
| --- | --- | --- |
| Care-access policy | Clinic lead's signed eligibility/reviewer/time-limit decision and approval reference | Proposed development policy: doctor/nurse, same active facility, max four hours, independent privacy reviewer. Production enablement requires CARE_POLICY_APPROVAL_REF; a configured string must reference the actual approved record. |
| Hosting and database | Named region/operator, verified PostgreSQL TLS CA/hostname, restricted network and maintenance roles | Undecided. Production configuration rejects unverifiable/plaintext database connections. |
| Backups/PITR | Approved independent destination, encryption/key custody, WAL archive/retention, RPO/RTO, full restore drill | Deferred. pgBackRest AES-256 template is inactive. Local synthetic dump restoration is tested; offsite delivery and PITR are not. |
| Cloudflare | Clinic/DPO choice of DNS-only versus proxy, actual data flows and required approvals/contracts | Undecided; no mode or transfer permission is inferred. |
| Mail/SMS/video/telemetry | Selected provider contracts, regions, credentials, permitted content and failure/reconciliation tests | Deferred. No real delivery/calls claimed. Optional telemetry uses static C1 request metadata only and has no default collector. |
| Audit anchors and alerts | Independent immutable head destination, schedule owner, alert route and forced-tamper drill | Timer/failure-journal templates only; not installed/enabled. No real off-host anchor or page delivered. |
| Clinic accountability | Named DPO/incident commander, legal counsel review, registration/contracts, training and workstation policy | Clinic-owned work, not completed by software. |
| Consent/guardians/retention | Approved wording/version, guardian verification and revocation rules, legal holds, record-type retention periods | Pending; no statutory periods, automatic erasure or untested unmerge behavior invented. |

Use the official [PDPC information](https://pdpc.go.tz/about-us/) and [government Act publication](https://oagmis.oag.go.tz/portal/acts/237) for the clinic's legal review. This repository records implementation evidence and outstanding decisions; it does not certify legal compliance.

Before production, bootstrap two independently identified administrators through the authorized maintenance process, enroll both in MFA and test independent approvals. Ordinary API routes cannot bootstrap a second administrator by bypassing dual control. Maintenance/backup/key-rotation credentials stay off API hosts; RLS-bypass maintenance authority is explicit, never granted to the runtime role.
