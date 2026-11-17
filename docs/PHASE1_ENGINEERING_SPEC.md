# Spartial Polyclinic — Phase 1 Security & Compliance Engineering Spec

Nov 17, 2026 · @Dady

## 1. Purpose, scope and regulatory basis

The Spartial backend (Go 1.27 · Fiber v3 · PostgreSQL) must protect patient health information to the HIPAA Security Rule standard and meet Tanzania's Personal Data Protection Act 2022 (PDPA), which is the law that actually binds a Tanzanian clinic. HIPAA is adopted here as the engineering baseline because it is the most concrete control catalogue for health data; it does not legally apply unless the clinic handles US-regulated data.

Scope: database design and backend Phase 1 (M01–M06 foundation) plus the controls every later module (M07–M24) inherits. Frontend, mobile and hosting are covered only where the backend depends on them.

| Framework | What it requires of this system | How we use it |
| --- | --- | --- |
| Tanzania PDPA 2022 (in force 1 May 2023) | Health data is *sensitive personal data*; prior written consent, with an exception for medical processing under health-professional supervision; controller registration with the PDPC; a Data Protection Officer; prompt breach notification; PDPC permit before cross-border transfer; rights to access, rectification, erasure and restriction. Fines up to TZS 100,000,000 ([DLA Piper](https://www.dlapiperdataprotection.com/index.html?t=law&c=TZ)). | Legal obligations — consent model, hosting location, data-subject request workflows, breach runbook |
| HIPAA Security Rule, 45 CFR 164.308/310/312 | Administrative, physical and technical safeguards: access control, audit controls, integrity, person/entity authentication, transmission security | Control catalogue and gap analysis (section 7) |
| HIPAA Privacy Rule — minimum necessary | Staff see only the PHI their task needs | Role + facility + care-relationship authorization |
| OWASP ASVS 4.0 Level 2 (Level 3 for M01/M05) | Verifiable application security requirements | Definition of done for every endpoint |
| NIST SP 800-63B AAL2 | Two factors for privileged and remote clinical access, replay-resistant OTP | MFA design (implemented in this pass) |

**Open question for the clinic:** is any data hosted or backed up outside Tanzania (cloud region, offsite backups, video or SMS providers)? Each such flow needs a PDPC transfer permit before go-live ([Clyde & Co](https://www.clydeco.com/en/insights/2024/10/cross-border-personal-data-transfers)). This spec is engineering guidance, not legal advice; confirm the obligations with the clinic's DPO or counsel.

## 2. Data classification

Every column gets one of four classes; the class decides encryption, who may read it, whether reads are audited, and how long it is kept.

| Class | Examples in Spartial | Storage rule | Access and audit rule |
| --- | --- | --- | --- |
| C4 Secret | Passwords, TOTP secrets, refresh/access tokens, recovery codes, provider API keys, encryption keys | Never stored in plaintext: Argon2id (passwords), SHA-256 digest (tokens, recovery codes), AES-256-GCM keyring (TOTP, provider secrets); keys only in the secrets manager | Never returned by any API, never logged, never in backups beside keys |
| C3 Restricted PHI | Diagnoses, consultation notes, lab results, prescriptions, imaging reports, documents, HIV/mental-health/reproductive flags, video consent | Disk/backup encryption; field-level encryption for the most sensitive flags and free-text notes (planned, M10+) | Role + facility + care relationship; every read and write audited with patient\_id |
| C2 PHI identifiers | Name, MRN, birth date, phone, address, next of kin, NIDA/insurance numbers | Disk/backup encryption; NIDA/insurance numbers field-encrypted with a blind index for lookup | Role + facility; reads and searches audited (already implemented for patients) |
| C1 Internal | Facility names, catalogue prices, staff display names, schedules, aggregate reports | Normal | Authenticated staff with the module permission |

Rules that follow from the table:

- A report or export that contains any C2/C3 column is itself C3: permissioned, audited, time-limited download links (M19).
- Notification text sent by SMS/WhatsApp is C1 only — "You have an appointment tomorrow at Spartial" — because phones are shared (guide M18).
- Logs are C1: request id, route template, status, duration. Never bodies, search strings, tokens or names (already enforced in `internal/httpapi`).
- Test, demo and CI data is synthetic only; real POWERHMS exports are C3 and live only on the controlled migration host (M23).

## 3. Threat model

The most likely real-world breach for a polyclinic is not an outside hacker but a stolen staff password or a curious insider; controls are ranked accordingly.

| # | Threat | Likely actor | Impact | Primary controls | Status |
| --- | --- | --- | --- | --- | --- |
| T1 | Phished or shared staff password used to read records | External, colleague | Mass PHI disclosure | MFA (mandatory for privileged roles, then clinicians), rate limits, session revocation, login audit | MFA built this pass; clinician enforcement pending |
| T2 | Insider browsing records with no care relationship ("snooping") | Staff | Privacy breach, PDPA penalty | Facility scope + care-relationship check, audited reads, break-glass with reason and review, privacy-reviewer reports | Facility scope and read audit built; relationship + break-glass pending (M05) |
| T3 | Broken access control via direct API calls (IDOR) | Any authenticated user | Cross-patient or cross-facility access | Server-side permission + scope on every query, 404 for out-of-scope ids, RLS as second layer | Built for patients; RLS proposed |
| T4 | Audit trail altered to hide misuse | Insider with DB access | Undetectable misuse | Append-only triggers, restricted runtime role, hash-chained seals shipped off-host | Built this pass (off-host shipping is a deployment task) |
| T5 | Stolen database dump or backup | Hosting staff, attacker | Full disclosure | Disk + backup encryption, field encryption of C3/C4, keys outside DB and backups | Keyring built; backup encryption pending (M24) |
| T6 | Session/token theft (XSS, device loss) | External | Account takeover | Opaque hashed tokens, 15-min access, rotating refresh with reuse detection, HttpOnly SameSite=Strict cookie, per-session revoke | Built |
| T7 | Malicious file upload | Patient, external referral | Malware to staff PCs | Fail-closed AV scan, quarantine, content-type sniffing, private storage, short signed downloads | Pending (M06) |
| T8 | Duplicate/forged payments or webhooks | External, cashier error | Financial loss | Idempotency keys, webhook signatures + replay window, provider reconciliation | Idempotency built; M17/M22 pending |
| T9 | Patient account takeover via phone number | Relative, attacker | Records exposed to wrong person | Staff-verified linking, OTP plus identity check, guardian grants with expiry | Pending (M20) |
| T10 | Ransomware or server loss | External | Clinic cannot treat patients | Offsite encrypted backups, PITR, tested restores, downtime paper procedure | Partially (restore rehearsal script) |

Out of scope for Phase 1: nation-state attackers, hardware tampering at the clinic, and denial-of-service beyond a reverse-proxy rate limit.

## 4. Security architecture controls

Every request passes the same checks in the same order, so a new module inherits them instead of re-inventing them: transport → authentication → session assurance → permission → data scope → validation → transactional write + audit.

### 4.1 Identity and authentication (M01)

- Passwords: Argon2id (64 MiB, t=3, p=2), 12–256 characters, checked against a breached-password list before acceptance (pending). Dummy-hash comparison for unknown emails so timing does not reveal accounts.
- Second factor: TOTP (RFC 6238) with recovery codes. Login with a confirmed factor returns a five-minute, five-attempt challenge and **no session**; tokens are issued only by `/auth/mfa/verify`.
- Enforcement: administrator and privacy-reviewer must hold an MFA-verified session (forced in production). Next step: extend to doctor, laboratory approver and pharmacy for any remote (non-clinic-network) login.
- Rate limits: 10 attempts / 15 minutes per IP and per account for login and MFA, stored durably in PostgreSQL.

### 4.2 Sessions

- Opaque 256-bit tokens, only SHA-256 digests stored. Access 15 minutes, absolute session 7 days, refresh rotation with reuse detection that revokes the whole session.
- Web: HttpOnly, Secure, SameSite=Strict cookie scoped to `/api/v1/auth`, exact-Origin check. Mobile: refresh token in Android Keystore/iOS Keychain only.
- Revocation on password change, role/facility change, suspension, MFA enable/disable. Add an idle timeout of 15 minutes on shared clinic workstations (front-end lock screen + server check, pending).

### 4.3 Authorization (minimum necessary)

- Three layers, all server-side: permission from live role rows → facility assignment → care relationship (patient has an open visit, appointment, order or task with this clinician, or a break-glass grant).
- Separation of duties: administrators have no clinical permissions; whoever raises a refund cannot approve it; a lab result's author cannot verify it.
- Out-of-scope ids return 404, never 403, so existence is not disclosed.

### 4.4 Encryption

- In transit: TLS 1.2+ at the reverse proxy, HSTS, PostgreSQL `sslmode=verify-full` in production.
- At rest: encrypted disks and encrypted backups (AES-256), backups and WAL archives off-host.
- Field level: `security.Keyring` — AES-256-GCM, key id in every ciphertext for rotation, row id as associated data so ciphertext cannot be moved between rows. Keys from `PHI_ENCRYPTION_KEYS` (secrets manager), never in the database or beside backups.

### 4.5 Audit and integrity

- Who, what, which patient, when, outcome, request id — for every PHI read, search, write, denial and authentication event, written in the same transaction as the action (failure to audit = action fails).
- Append-only triggers + runtime role without UPDATE/DELETE + hash-chained seals; seal heads shipped to a log store the DB owner cannot change.
- Clinical records are never overwritten: drafts, signed versions, amendments with author, time and reason (M09–M14).

### 4.6 Input, output and platform

- Strict JSON (unknown fields rejected, 64 KiB cap), validator tags, typed DTOs, parameterised SQL only, idempotency keys on every create that can be retried.
- Error envelope never leaks SQL or stack traces; `Cache-Control: no-store` on every API response.
- Dependencies pinned, `govulncheck` and image scanning in CI, non-root read-only container, secrets only from environment/secrets manager.

## 5. Database security design

The database enforces the rules the application could forget: least-privilege roles, immutable history, and constraints that make invalid clinical or financial states impossible to store.

### 5.1 Roles

| Role | Used by | Rights |
| --- | --- | --- |
| `spartial_owner` | Migrations, `manage` commands, restore rehearsal | Owns schema; never used by the API; credentials kept out of runtime hosts |
| `spartial_app` | API and worker | SELECT on all tables; INSERT/UPDATE/DELETE only on listed operational tables; INSERT-only on `audit_events`, `audit_seals`, `idempotency_records`; API refuses to start if it has superuser, CREATEROLE, CREATEDB or audit UPDATE/DELETE |
| `spartial_report` (planned, M19) | Report/export jobs | SELECT on reporting views only, no base PHI tables |
| `spartial_backup` (planned, M24) | pg\_dump / WAL archiving | Replication/backup rights only, no login from the app network |

### 5.2 Design rules for every Phase 1+ table

1. `uuid` primary keys (no guessable sequences in URLs); human numbers such as MRN are separate unique columns.
2. `facility_id NOT NULL` on every patient-linked row, indexed with the patient id, so scope checks are cheap.
3. `version integer` + `UPDATE … WHERE version = $expected` on editable records (optimistic locking, already used for patients and staff).
4. Clinical and financial facts are never updated in place: status columns move forward through CHECK-constrained values; corrections are new rows with `amends_id`, `reason`, `author_id`.
5. Money as `numeric(14,2)` with CHECK ≥ 0 and price snapshots on the invoice line; never floats.
6. `created_at`/`updated_at timestamptz` in UTC; author columns reference `staff(id)` and staff are deactivated, never deleted.
7. No `ON DELETE CASCADE` from patients or staff into clinical, financial or audit tables.

### 5.3 Row-level security (defence in depth, proposed for M05)

The application already filters by facility. RLS repeats the rule inside PostgreSQL so a forgotten WHERE clause cannot leak another facility's patients. Each transaction sets the actor; policies read it.

```sql
ALTER TABLE patients ENABLE ROW LEVEL SECURITY;
ALTER TABLE patients FORCE ROW LEVEL SECURITY;
CREATE POLICY patients_facility_scope ON patients
  USING (facility_id IN (
    SELECT sf.facility_id FROM staff_facilities sf
    WHERE sf.staff_id = NULLIF(current_setting('app.actor_id', true), '')::uuid));
-- In every repository transaction:  SELECT set_config('app.actor_id', $1, true);
```

Roll out table by table with tests proving that a transaction without `app.actor_id` sees zero rows.

### 5.4 Proposed M05 schema: consent, guardians, break-glass

Not migrated yet — the clinic must first agree the consent wording, guardian rules and who reviews break-glass use.

```sql
CREATE TABLE patient_consents (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  patient_id uuid NOT NULL REFERENCES patients(id),
  purpose text NOT NULL CHECK (purpose IN ('treatment','telemedicine','sms_reminders','research','record_sharing','video_recording')),
  status text NOT NULL CHECK (status IN ('granted','withdrawn')),
  policy_version text NOT NULL,          -- which wording the patient agreed to
  method text NOT NULL CHECK (method IN ('paper_scanned','in_app','verbal_witnessed')),
  recorded_by uuid NOT NULL REFERENCES staff(id),
  document_id uuid,                      -- scanned form (M06)
  effective_at timestamptz NOT NULL DEFAULT now(),
  created_at timestamptz NOT NULL DEFAULT now()
);  -- append-only: withdrawal is a new row

CREATE TABLE guardian_grants (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  patient_id uuid NOT NULL REFERENCES patients(id),
  guardian_account_id uuid NOT NULL,     -- patient-app account (M20)
  relationship text NOT NULL,
  scope text[] NOT NULL,                 -- e.g. {appointments,results,balances}
  verified_by uuid NOT NULL REFERENCES staff(id),
  valid_from timestamptz NOT NULL DEFAULT now(),
  valid_until timestamptz,               -- minor turns 18 => grant ends
  revoked_at timestamptz, revoked_by uuid REFERENCES staff(id)
);

CREATE TABLE break_glass_access (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  staff_id uuid NOT NULL REFERENCES staff(id),
  patient_id uuid NOT NULL REFERENCES patients(id),
  reason text NOT NULL CHECK (length(reason) BETWEEN 20 AND 1000),
  granted_at timestamptz NOT NULL DEFAULT now(),
  expires_at timestamptz NOT NULL CHECK (expires_at <= granted_at + interval '4 hours'),
  reviewed_by uuid REFERENCES staff(id),
  reviewed_at timestamptz,
  review_outcome text CHECK (review_outcome IN ('appropriate','inappropriate')),
  CHECK (reviewed_by IS NULL OR reviewed_by <> staff_id)
);
```

### 5.5 Retention

PDPA requires keeping personal data no longer than necessary, while clinical records must be kept for the period Tanzanian health regulation sets. Implement retention as data (a `retention_policies` table per record type plus `legal_holds`), never as an automatic DELETE, and agree the periods with the clinic before M19.

## 6. Per-module control matrix

Each module ships only when its row's controls are tested against real PostgreSQL; the baseline in section 4 applies to all of them.

| Module | PHI class | Module-specific controls | Must-pass security test |
| --- | --- | --- | --- |
| M01 Login & permissions | C4 | MFA, challenge login, recovery codes, invitation tokens (hashed, single use, 48 h), dual-control admin MFA reset | Receptionist cannot approve results or refunds via direct API call |
| M02 Facilities & staff | C1 | Deactivate, never delete; history keeps author identity; approver flag for clinicians | Removed staff's past notes still show their name |
| M03 Catalogues & prices | C1 | Separate `prices.manage` vs `clinical_catalogue.manage`; versioned rows | Price change does not alter an issued invoice |
| M04 Patient registry | C2 | Duplicate warning without exposing other patients' details; supervised merge with audit and undo | Cross-facility id returns 404 |
| M05 Privacy & audit | C3 | Care-relationship check, break-glass, consent, guardian grants, RLS, audit seals | Snooping attempt is denied and appears in the privacy report |
| M06 Documents | C3 | Private bucket, AV scan fail-closed, quarantine, SHA-256 checksum, 5-minute signed URL bound to the session | Copied link fails after expiry or for another user |
| M07 Appointments | C2 | Slot locking (`SELECT … FOR UPDATE`), booking request ≠ confirmed | Two concurrent bookings of the last slot: exactly one succeeds |
| M08 Visits & queues | C2 | Idempotent check-in; queue screens show initials + ticket number, not full names | Double check-in creates one visit |
| M09 Nursing | C3 | Corrections as new rows with reason; units stored | Corrected observation keeps the original |
| M10 Consultation | C3 | Draft → signed → amended; signature binds content hash; field-encrypt sensitive flags | Signed note cannot be overwritten |
| M11 Orders & tasks | C3 | Task owner required; tasks close only by explicit action | Message sent does not close a task |
| M12 Laboratory | C3 | Author ≠ verifier; result release separate from entry | Unverified result never visible to patient app |
| M13 Imaging | C3 | Report approval, attachment via M06 | Cancelled procedure status visible, report locked |
| M14 Prescribing | C3 | Prescriber permission + allergy display; signed, versioned | Unsigned prescription not visible to pharmacy |
| M15 Pharmacy stock | C3 | Atomic dispense with batch row locks, FEFO, blocked/expired check, idempotency | Retry does not reduce stock twice |
| M16 Billing | C2 | Price snapshots, numeric money, corrections with reason, one charge per billable source (unique key) | One service creates one charge |
| M17 Payments | C2/C4 | Idempotency, refund dual approval, cashier close with variance record, provider timeout ≠ paid | Repeated payment request records money once |
| M18 Messaging | C1 | Templates with no clinical content; opt-out per channel; outbox | Provider outage does not lose the appointment |
| M19 Reports & exports | C3 | Report role, aggregate by default, export permission, watermark + audit, links expire in 24 h | Export without permission is denied and audited |
| M20 Patient app | C2/C3 | Staff-verified account linking, OTP + identity check, guardian scope, released-only data, no PHI cached unencrypted on device | Knowing a phone number does not reveal a record |
| M21 Video | C3 | Provider BAA/DPA, per-session join tokens for exactly two parties, recording off by default | Third party cannot join with a copied link |
| M22 Integrations | C4 | Secrets in keyring, webhook HMAC + timestamp window + replay store, outbound allowlist | Replayed webhook creates nothing |
| M23 POWERHMS import | C3 | Isolated staging schema, source-id mapping, no guessed clinical data, import audit | Re-importing a file creates no duplicates |
| M24 Operations | All | Encrypted offsite backups, PITR, restore drill incl. documents and audit-verify, alerting, incident runbook | Restored copy passes `audit-verify` and opens a patient |

## 7. HIPAA safeguard mapping and gap analysis

The technical safeguards are now largely met in code for the foundation; the biggest gaps are care-relationship access, backups and the organisational (non-code) safeguards the clinic must own.

| HIPAA standard | Requirement | Spartial implementation | Status |
| --- | --- | --- | --- |
| 164.312(a)(1) Access control | Unique user id | One staff account per person, email unique, no shared logins | Met |
| 164.312(a)(2)(ii) | Emergency access procedure | Break-glass schema (5.4) | Gap — M05 |
| 164.312(a)(2)(iii) | Automatic logoff | 15-min access token, 7-day session; idle lock pending | Partial |
| 164.312(a)(2)(iv) | Encryption and decryption | AES-256-GCM keyring for secrets; PHI field encryption and disk/backup encryption pending | Partial |
| 164.312(b) Audit controls | Record and examine activity | Transactional audit of reads, writes, denials, auth; hash-chained seals; audit query API | Met (review reports pending) |
| 164.312(c)(1) Integrity | Protect PHI from improper alteration | Optimistic versions, append-only audit, seals; signed clinical versions pending | Partial |
| 164.312(d) Authentication | Verify person is who they claim | Argon2id + TOTP MFA, enforced for privileged roles | Met for privileged; extend to clinicians |
| 164.312(e)(1) Transmission security | Guard data in transit | TLS at proxy, Secure cookies; DB TLS verify-full pending in deployment | Partial |
| 164.308(a)(1) Risk analysis | Documented risk assessment | Threat model (section 3); formal assessment with clinic | Gap — organisational |
| 164.308(a)(3)/(4) Workforce access | Authorise, supervise, terminate | Role/facility admin, suspension revokes sessions, last-admin guard | Met |
| 164.308(a)(5) Training | Security awareness | Staff training at handover (M24) | Gap — organisational |
| 164.308(a)(6) Incident procedures | Detect, respond, report | Audit + seals; incident runbook and PDPC notification process | Gap |
| 164.308(a)(7) Contingency plan | Backup, disaster recovery, emergency mode | Restore rehearsal script (DB only) | Gap — M24 |
| 164.308(b)(1) Business associates | Contracts with vendors handling PHI | Hosting, SMS, video, payment providers need DPAs; PDPC permit if offshore | Gap — organisational |
| 164.310 Physical safeguards | Facility, workstation, device controls | Clinic responsibility: locked server room, screen locks, encrypted laptops | Out of code scope |

Top five gaps to close before any real patient data is entered:

1. Care-relationship authorization + break-glass (M05) before any clinical endpoint ships.
2. Encrypted, off-host, tested backups with a restore drill that also runs `audit-verify` (M24).
3. Production TLS everywhere, including PostgreSQL `verify-full`, and a trusted-proxy rate-limit design.
4. Signed DPAs with hosting/SMS/video/payment vendors and PDPC registration, DPO appointment and transfer permits.
5. Incident response runbook naming who decides, who notifies the PDPC and patients, and within what time.

## 8. Implemented in this pass and Phase 1 roadmap

Branch `security/phase1-hardening` on `Lavaaa-9/backend` adds migration 00003 and the code below; the whole suite passes with the race detector against a real PostgreSQL 16 database.

| Area | What was built | Files |
| --- | --- | --- |
| Schema | `staff_mfa`, `mfa_recovery_codes`, `login_challenges`, `sessions.mfa_verified`, audit `reason`/`facility_id`, immutable `audit_seals`, `seal_audit_events()`, `verify_audit_chain()`; runtime grants updated; SchemaVersion 3 | `db/migrations/00003_security_hardening.sql`, `internal/platform/grants.go` |
| Encryption | AES-256-GCM keyring with key ids for rotation and row-bound associated data; required in production | `internal/security/keyring.go`, `internal/config/config.go` |
| MFA | RFC 6238 TOTP (verified against the RFC test vectors), replay protection, recovery codes, challenge-based login, enroll/confirm/disable endpoints, per-IP and per-account limits | `internal/security/totp.go`, `internal/modules/identity/mfa.go` |
| Enforcement | Privileged roles blocked with `mfa_required` until the session is MFA-verified; denial audited with reason | `internal/httpapi/app.go` |
| Audit integrity | `manage audit-seal`, `manage audit-verify`, `manage new-encryption-key`; tamper detection proven in tests | `cmd/manage/main.go`, `internal/platform/auditseal.go` |
| Contract and docs | OpenAPI regenerated (24 operations, validated); MODULE\_STATUS, PERMISSIONS, OPERATIONS, CONTINUE\_HERE updated | `api/openapi/openapi.json`, `docs/` |

Tests added: 11 integration scenarios (3 test functions) in `tests/integration/mfa_test.go` (enrollment, wrong codes, replay, challenge reuse/exhaustion/expiry-by-password-change, recovery single use, disable, rate limit, audit chain tamper) plus unit tests for keyring rotation/tampering and TOTP.

### Phase 1 security roadmap (in order)

1. **M01 finish:** recovery-code regeneration, administrator MFA reset with a second administrator's approval, staff invitation/activation tokens, password-breach check, idle timeout.
2. **M05 privacy core:** care-relationship policy, break-glass with review queue, consent and guardian tables (5.4), RLS on `patients`, privacy-reviewer reports (who viewed patient X; unusual access volume).
3. **M06 documents:** private object storage, ClamAV fail-closed, quarantine, checksums, short signed URLs.
4. **M24 foundations:** encrypted offsite backups + PITR, restore drill that runs `audit-verify`, scheduled `audit-seal` with heads shipped to an external log, TLS to PostgreSQL, alerting on `audit_chain_broken` and repeated `mfa_required` denials.
5. **Clinic paperwork (parallel, not code):** PDPC registration, DPO, vendor DPAs, transfer permits, consent wording, retention periods, incident runbook.

Only after steps 1–4 pass should clinical endpoints (M07 onward) be exposed to real patient data.

## 9. Task list

The 14 P0 tasks add up to about 14.5 developer-days plus the clinic's paperwork, and they gate real patient data. Estimates assume one developer; update Status as work moves.

| # | Task | Priority | Owner | Est. (days) | Depends on | Done when | Status |
| --- | --- | --- | --- | --- | --- | --- | --- |
| T01 | Write the care-access decision record: what counts as a care relationship, who reviews break-glass, maximum grant length (proposed 4 h) | P0 before real data | Dadi + clinic | 0.5 | — | Clinic lead signs off in writing | Not started |
| T02 | Migration 00004: `care_assignments`, `break_glass_access`, SQL function `has_care_access(staff, patient)`; update runtime grants | P0 before real data | Dadi | 1 | T01 | Up/down on a clean DB; `grant-runtime` and `CheckRuntimeRole` pass | Not started |
| T03 | Privacy policy service `CanAccessPatient(actor, patient)`; call it from patient read/update; reception keeps demographics only | P0 before real data | Dadi | 2 | T02 | Doctor without a relationship gets 404 and an audited denial | Not started |
| T04 | Break-glass endpoint `POST /staff/patients/{id}/break-glass` with reason (20+ chars) and expiry | P0 before real data | Dadi | 1 | T03 | Access works until expiry, then 404; reason in audit | Not started |
| T05 | Break-glass review queue for privacy-reviewer: list unreviewed, mark appropriate/inappropriate; reviewer cannot be the requester | P0 before real data | Dadi | 1 | T04 | Self-review returns 403; outcome audited | Not started |
| T06 | Integration tests for T02–T05 against real PostgreSQL, including concurrent grant + expiry | P0 before real data | Dadi | 1 | T05 | `make check` green with race detector | Not started |
| T07 | Row-level security on `patients`; set `app.actor_id` with `set_config` in every repository transaction | P0 before real data | Dadi | 2 | T03 | A transaction without an actor sees 0 rows; full suite still passes | Not started |
| T08 | Encrypted offsite backups with point-in-time recovery (pgBackRest or WAL-G) | P0 before real data | Dadi | 2 | Hosting decided | Nightly full + continuous WAL to a bucket off the server, encrypted | Not started |
| T09 | Extend `restore-check.py`: restore to a point in time, run `audit-verify`, open a synthetic patient | P0 before real data | Dadi | 1 | T08 | Drill passes and is written into OPERATIONS.md | Not started |
| T10 | Schedule `manage audit-seal` every 5 min and daily `audit-verify`; ship seal heads to an external log; alert on failure | P0 before real data | Dadi | 0.5 | Hosting decided | A forced tamper in staging raises an alert | Not started |
| T11 | PostgreSQL TLS `verify-full`; `TRUSTED_PROXIES` setting so rate limits see the real client IP | P0 before real data | Dadi | 1 | — | Config tests cover both; API refuses a plaintext DB URL in production | Not started |
| T12 | Decide Cloudflare mode: DNS-only, or proxied with a PDPC transfer permit | P0 before real data | Dadi + clinic | 0.5 | — | Decision recorded in ARCHITECTURE.md | Not started |
| T13 | CI security pipeline: gosec, govulncheck, gitleaks, trivy, SBOM (syft), Dependabot | P0 before real data | Dadi | 1 | — | Pipeline fails on a high finding; first run observed green | Not started |
| T14 | Clinic paperwork: PDPC registration, DPO named, vendor DPAs, consent wording, retention periods, incident runbook | P0 before real data | Clinic | — | — | Documents filed; runbook names who notifies PDPC | Not started |
| T15 | `approval_requests` table and dual-control service (requester ≠ approver, expiry, audit) | P1 enterprise | Dadi | 2 | T06 | Generic approve/reject flow tested | Not started |
| T16 | Use dual control for admin-role grants and administrator MFA reset | P1 enterprise | Dadi | 1 | T15 | Single admin cannot grant admin or reset MFA alone | Not started |
| T17 | Recovery-code regeneration endpoint (password + current factor) | P1 enterprise | Dadi | 0.5 | — | Old codes stop working; audited | Not started |
| T18 | Staff invitation and activation tokens (hashed, single use, 48 h) replacing direct password provisioning | P1 enterprise | Dadi | 2 | Mail provider | Expired or reused token rejected | Not started |
| T19 | Breached-password check against an embedded common-password list | P1 enterprise | Dadi | 0.5 | — | Common passwords rejected at create and change | Not started |
| T20 | Access-anomaly worker: bulk patient reads, off-hours access, repeated `mfa_required` denials into a `privacy_alerts` table | P1 enterprise | Dadi | 2 | T05 | Synthetic snooping run produces an alert | Not started |
| T21 | NIDA and insurance identifiers: field encryption plus HMAC blind index for exact search | P1 enterprise | Dadi | 2 | — | Plaintext absent in DB; exact search works | Not started |
| T22 | `manage rotate-keys`: re-encrypt ciphertext to the active key, then retire old key ids | P1 enterprise | Dadi | 1 | T21 | No row uses the retired key; MFA still verifies | Not started |
| T23 | New-device login email and device labels on the sessions list | P1 enterprise | Dadi | 1.5 | Mail provider | Email sent once per new device; labels shown | Not started |
| T24 | Protected `/metrics` and OpenTelemetry tracing with no PHI in spans | P1 enterprise | Dadi | 1.5 | — | Test asserts no names or ids in exported attributes | Not started |
| T25 | PDPA data-subject export: full patient record export, permissioned, audited, expiring link | P1 enterprise | Dadi | 1.5 | T03 | Export contains every record class; download expires | Not started |
| T26 | Signed clinical versions: content hash stored at signing (with M10) | P2 later | Dadi | 1 | M10 | Altered signed note fails verification | Not started |
| T27 | Enable `pgaudit` for direct database sessions | P2 later | Dadi | 0.5 | Hosting decided | A `psql` read of patients appears in DB logs | Not started |
| T28 | k6 load baseline for login, search and registration | P2 later | Dadi | 1 | — | p95 latencies recorded in OPERATIONS.md | Not started |
| T29 | External penetration test before go-live | P2 later | Clinic | — | All P0 done | High findings fixed and retested | Not started |

Suggested order this week: T01 → T02 → T03 → T13 (CI can run in parallel), then T04–T07.

## Sources

- [Data Protection in Tanzania — DLA Piper](https://www.dlapiperdataprotection.com/index.html?t=law&c=TZ)
- [Cross-border personal data transfers — Clyde & Co](https://www.clydeco.com/en/insights/2024/10/cross-border-personal-data-transfers)
- Spartial Polyclinic Simple Development Guide (Lavaaa Tech Solutions, 9 November 2026)
- [Lavaaa-9/backend repository](https://github.com/Lavaaa-9/backend)
