# Security engineering task register

This maps the supplied Phase 1 engineering spec to the current implementation. Internally tested is not clinic acceptance. Hosting, offsite storage, mail and related external decisions were explicitly deferred by the user. No legal filing, production deployment, real-patient permission or external delivery is inferred from code.

| Task | Status | Evidence and exact limits |
| --- | --- | --- |
| T01 | externally blocked | Proposed care-access ADR and production approval-reference gate. User authorized development continuation; clinic signature/named reviewers remain a go-live requirement. |
| T02 | internally tested | Migration 00004: care assignments, exceptional grants, facility scope metadata and access functions. Up/down/up and restricted runtime checks pass. |
| T03 | internally tested | Shared privacy policy plus RLS; reception registry access, clinical care evidence, audited 404 denials and patient-level search-hit audit. |
| T04 | internally tested | Reasoned 1–240-minute exceptional grants; durable idempotency and changed-key concurrency protection. Production approval gate retained. |
| T05 | internally tested | Independent privacy-review queue/outcomes; expired grants stay reviewable, self-review denied, inappropriate review removes grant access. |
| T06 | internally tested | Real PostgreSQL regression coverage for assignments, expiry/review, concurrent grants, denied access and actor-context isolation; race suite passed. |
| T07 | internally tested | FORCE RLS on patients and encrypted identifiers, transaction-local actor, no-actor zero rows, live permission/facility/care checks. |
| T08 | externally blocked | Hosting/offsite destination deferred by user. Inactive AES-256 pgBackRest template supplied; no offsite backup/PITR delivery claimed. |
| T09 | in progress | Synthetic dump restore checks source seals, applies runtime grants, proves unscoped zero rows and an audited scoped patient read. PITR/file/key restoration depends on T08/M06. |
| T10 | externally blocked | Reviewed systemd five-minute/daily timer and failure-journal templates supplied. Deployment, independent anchor shipping and real alert delivery deferred with hosting. |
| T11 | internally tested | Production rejects plaintext/unverified DB URLs and requires explicit CA path. Exact IP/CIDR proxy trust, spoofing regressions, HSTS and remote-clinical MFA. Real production certificate/provider setup remains deployment work. |
| T12 | externally blocked | Cloudflare DNS-only/proxy choice and data-location review deferred by user; no mode selected or production DNS changed. |
| T13 | internally tested | Pinned gosec/govulncheck/gitleaks/Trivy/Syft workflow, SPDX artifact and Dependabot passed the first hosted security run. Backend workflow also passed PostgreSQL/race/build/OpenAPI/report-privacy checks. Hosted evidence below; no scanner finding was bypassed. |
| T14 | externally blocked | Clinic-owned registration, DPO, contracts, consent wording, retention and legal notification responsibilities documented; not generated or treated as filed by code. |
| T15 | internally tested | Typed approval requests, one-hour expiry, requester/approver separation, live MFA admin checks, target version and atomic audited effects. |
| T16 | internally tested | Ordinary API cannot create/promote administrators directly. Independent approval applies role grant/MFA reset once and revokes target sessions; trusted bootstrap remains outside ordinary API. |
| T17 | internally tested | Existing recovery-code regeneration verified; old codes/challenges and other sessions invalidate atomically. |
| T18 | externally blocked | Mail/provider decisions deferred by user. No invitation delivery or new account activation token workflow is claimed; direct ordinary development provisioning remains explicit. |
| T19 | internally tested | Pinned offline SecLists common-password screen plus supplemental weak phrases at account creation/password change; seed also validates. Limited common-password screen, not exhaustive breach assurance. |
| T20 | in progress | Durable deduplicated configured read/MFA-denial volume alerts and privacy-review APIs/worker pass tests. Off-hours schedule, alert disposition and external paging remain pending. |
| T21 | internally tested | Row-bound AES-256-GCM NIDA/insurance identifiers plus separately keyed HMAC exact lookup; no plaintext identifier in DB/replay cache and scoped audited reads/search. |
| T22 | internally tested | Owner-authorized resumable key rotation for MFA/identifiers with per-row audit and residual-old-key check; tests remove old key and verify TOTP/identifier lookup. Key retirement requires drained old writers and recovery rehearsal. |
| T23 | externally blocked | Session device labels implemented. New-device email/provider delivery and reliable device enrollment detection deferred with mail decisions; labels are not fingerprints. |
| T24 | internally tested | Permission/MFA-protected Prometheus counters/duration and allowlisted OTel request attributes; tests exclude names, patient IDs, tokens and query strings. Real collector delivery unconfigured. |
| T25 | not started | Full released patient/data-subject export requires later record modules, private files, expiration and DPO-approved workflow; no partial registry dump marketed as full export. |
| T26 | not started | Signed clinical content hashes/versioned amendments remain part of unimplemented M10. |
| T27 | externally blocked | pgaudit installation/host logging decisions deferred; restricted direct-session audit guidance supplied, no host extension enabled. |
| T28 | internally tested | Pinned k6 synthetic login/search/registration baseline passed with zero failed HTTP requests; observed p95/limits are in PERFORMANCE.md. Report-privacy regression passed. Representative production-size capacity testing remains pending. |
| T29 | externally blocked | Independent penetration test and clinic acceptance require external engagement after P0 gates; not substituted with static scans. |

Policy: [care-access decision](adr/0001-care-access-policy.md). Source request: PHASE1_ENGINEERING_SPEC.md. All M01–M24 modules remain tracked separately in MODULE_STATUS.md; these security controls do not implement the later clinical/financial workflows.

## Local verification evidence

Schema 6 native migration, `make check` (including real PostgreSQL and race detection), up/down/up and classification completeness, live privacy/MFA demos, audit verification, the stronger dump restore, OpenAPI validation and Compose configuration checks passed. Gosec v2.30.0 reported zero high findings; govulncheck v1.9.0 found no reachable vulnerabilities; the checksum-pinned gitleaks source scan found no secrets; Trivy v0.76.2 found zero high/critical image vulnerabilities on Alpine 3.23.8; Syft v1.57.2 generated an SPDX SBOM. k6 v2.4.0 baseline and report-privacy checks passed. Hosted backend and security workflows passed on ae658b9c589e7f8029a5c08bdf3a64a56cc99e5c. See the links below.

## Hosted verification

Implementation commit: ae658b9c589e7f8029a5c08bdf3a64a56cc99e5c.

- [Backend workflow passed](https://github.com/Lavaaa-9/backend/actions/runs/44129631700): real PostgreSQL 18.7 service, formatting/vet/race tests/build/checksums, benchmark-report privacy, dependency check, generated-contract validation and Docker build.
- [Security workflow passed](https://github.com/Lavaaa-9/backend/actions/runs/44129631581): published-history secret scan, high-severity static analysis, dependency vulnerabilities, image high/critical gate, SPDX generation and uploaded SBOM artifact.

The final non-root read-only container also passed the live privacy workflow as UID/GID 10001; the disposable container was stopped afterwards. The native schema-6 API/worker remain running. External deployment, clinic approval and provider-dependent items above remain open.
