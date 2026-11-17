# Continue the Spartial backend

## Saved state — 17 November 2026

The repository began with only the implementation prompt. Milestone A now has a running, tested vertical slice: Fiber v3.7.0, PostgreSQL, staff authentication/administration, fixed roles/permissions and facility assignments, patient registry with unique MRNs/search/versioned edits, audit, durable idempotency/outbox and a separate explicit development sink worker. The API contract contains 40 implemented operations.

**The full backend is unfinished.** Read MODULE_STATUS.md before choosing the next task. M01/M02/M04/M05/M18/M24 are in progress; the other modules are not started. No module is clinic accepted. No production service/data has been touched or deployed. No external provider has been tested.

Generated local credentials are in ignored `.env`; native PostgreSQL data/logs are in ignored `.local/`. Do not copy their values into code, chat, tests or logs. Synthetic staff accounts are admin@spartial.test and reception@spartial.test. Native PostgreSQL listens on 55432, API on 8080. The API and development worker were launched in this session; inspect readiness/processes before starting a second copy. Compiled local binaries are ignored under `bin/`.

## Resume commands

```bash
cd /home/dadi/Projects/backend
./scripts/dev-db.sh
set -a
source .env
set +a
curl --fail http://127.0.0.1:8080/health/ready
# If API is not running:
go run ./cmd/api
# In another terminal with the same exported environment:
python3 scripts/demo.py
go run ./cmd/worker
```

The seed preserves existing passwords. `scripts/dev-db.sh stop` shuts down only this local cluster. Do not point management commands or tests at production. Compose is an independent development database on host port 55433; do not run a second API on 8080.

## Verification already performed

- Native PostgreSQL 18.3 initialized; both Goose migrations applied; restricted runtime role and synthetic accounts provisioned.
- `go test -count=1 ./...` with TEST_DATABASE_URL passed real PostgreSQL integration tests.
- `make check` passed formatting, vet, race-enabled unit/integration tests, builds and module checksum verification.
- `go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...`: no known vulnerabilities found.
- `go run github.com/getkin/kin-openapi/cmd/validate@v0.133.0 api/openapi/openapi.json` passed.
- `python3 scripts/generate-openapi.py` generated the implemented contract.
- `podman build -t localhost/spartial-backend:development .` passed; container UID/GID 10001 verified, readiness and the live receptionist demo passed in a read-only, capability-dropped smoke container. That disposable container was stopped afterwards.
- Live native `python3 scripts/demo.py` passed registration/search/read, privileged-action denial, refresh rotation, and logout revocation.
- `python3 scripts/restore-check.py` passed synthetic PostgreSQL backup/restoration into an isolated temporary database with matching table counts. An initial run concurrent with live demo writes correctly reported a count mismatch; the stable-source run passed. No file restoration was claimed.
- Compose v2.40.3 was downloaded only into ignored `.local/tools/`, checksum verified, and `.local/tools/docker-compose config --quiet` passed. A system Docker/Compose installation was absent; the entire Compose stack has **not** been run. Native PostgreSQL and the application image were exercised separately.
- Bash syntax and Python compilation checks passed for helper scripts. Generated local password values were checked to be absent from source files.
- A later repeat of the go-run vulnerability command hit a Go proxy timeout; the same pinned v1.9.0 scanner was then built from its cached module and `.local/tools/govulncheck ./...` passed again with no vulnerabilities.

Tests create unique databases and restricted roles per fixture using TEST_DATABASE_URL. No SQLite mocks prove locking. Coverage includes concurrent same-key registration, audit-failure rollback, shared phones/MRNs, exact/approximate/unknown births, stale/cross-facility edits, audit grants/triggers, refresh rotation and concurrent reuse, suspension/reactivation/role changes, last-administrator concurrency, cookie/Origin behavior, password/logout/session ownership, durable rate limits, and outbox lease fencing/exhaustion. Later module tests remain absent because the modules are absent.

## Security phase 1 patch integration — 17 November 2026

The supplied spartial-security-phase1.patch is integrated, with additional hardening and recovery-code regeneration. Current schema is migration 00003 / SchemaVersion 3 and the OpenAPI contract has 25 implemented operations (version 0.2.0).

Implemented and verified: challenge-based TOTP login with no pre-factor session, AES-256-GCM row-bound secret encryption/key IDs, replay protection, hashed one-use recovery codes and regeneration, privileged MFA assurance, live account/session revalidation in MFA mutation transactions, bounded/audited management attempts, ten-minute pending enrollment expiry, challenge invalidation on factor lifecycle changes, and audited disablement with session revocation. MFA SQL lives in mfa_repository.go; services retain policy and transaction coordination.

Audit sealing now waits for existing writers using a short SHARE barrier and selects a contiguous settle-eligible prefix; the original patch's age filter alone could skip lower IDs that commit later. Structured event digests avoid delimiter ambiguity. Runtime roles cannot insert seals or execute sealing. Verification checks range metadata/hash/counts and reports the unsealed tail separately. Off-host head anchoring and deployment scheduling remain unconfigured.

Local native setup safely generated a missing development keyring in ignored mode-600 .env, preserved existing credentials/keys, applied migration 00003, and reran runtime grants. A private pre-migration synthetic backup is retained under ignored .local/backups/. API/worker were rebuilt and restarted on the existing ports. No production data or service was changed.

Current verification evidence:

- `make check` passed with TEST_DATABASE_URL: formatting, vet, race-enabled unit/real-PostgreSQL integration tests, command builds and module checksum verification.
- `go test -count=1 -v ./tests/integration` passed the original foundation/MFA tests plus security_regressions_test.go. New scenarios cover regeneration/concurrent recovery consumption, management throttling/audit/expiry, stale revoked sessions, challenge expiry, browser/mobile isolation and refresh assurance, pending audit writers, settle-prefix ordering and runtime seal denial.
- `.local/tools/govulncheck ./...` passed with no known vulnerabilities; same pinned v1.9.0 scanner.
- OpenAPI generator (25 operations) and kin-openapi v0.133.0 validation passed; Compose `config --quiet` passed.
- `podman build -t localhost/spartial-backend:development .` passed. Full Compose stack execution remains unverified.
- Live `python3 scripts/demo.py` and `python3 scripts/demo-security.py` passed. The latter deactivated its disposable synthetic staff account after exercising MFA without printing codes/passwords.
- `go run ./cmd/manage audit-seal` sealed the eligible development prefix; `audit-verify` reported it intact with an explicit unsealed tail.
- `python3 scripts/restore-check.py` passed with the schema-3 synthetic database and audit seals. This is a row-count restore smoke check, not file restoration, key recovery, full privilege restoration or off-host-anchor verification.
- Bash/Python syntax, whitespace checks and a scan ensuring local passwords/encryption key material are absent from publishable files passed.

See SECURITY_PHASE1.md for frontend enrollment/challenge/recovery instructions and operational limits. Existing unrelated .vscode settings were preserved.

## Latest engineering-spec implementation — schema 6

The supplied Phase 1 Security & Compliance Engineering Spec is saved in PHASE1_ENGINEERING_SPEC.md. The user authorized continuing implementation and explicitly deferred hosting/offsite/mail decisions to documentation. Exact T01–T29 status is SECURITY_TASK_STATUS.md; production/clinic/vendor decisions are DEPLOYMENT_DECISIONS.md. The care policy ADR is proposed, not a fabricated clinic signature.

Implemented since b99765d: care assignments/revocation, audited denied clinical access, four-hour exceptional grants and independent review, patient/identifier FORCE RLS with transaction-local actor, administrator-role/MFA-reset independent approvals, idle expiry/device labels, production DB/API TLS and proxy/network validation, remote clinical MFA, offline common-password screening, privacy reports/volume alerts, encrypted NIDA/insurance identifiers with keyed search, owner key rotation, protected metrics/allowlisted OTel, schema-column classification, security scanners/SBOM/Dependabot, stronger restores and synthetic k6 scripts. API contract is 0.3.0 with 40 implemented operations.

Schema 00004 introduces privacy access/RLS, 00005 introduces approvals/idle/monitoring, 00006 introduces encrypted identifiers. Existing synthetic native data was privately backed up before migration; grants were rerun. Initial two-administrator bootstrap is a trusted maintenance/clinic process, not a single-admin API bypass. Maintain all original M01–M24 gaps in MODULE_STATUS.md; this extension does not implement clinical care, billing, full private documents or full patient exports.

Local verification: make check and targeted PostgreSQL regressions passed, including race detection, migration rollback/reapply, live privacy/MFA demos, audit verification and restored runtime access. Source scanners reported no high gosec findings/secrets/reachable dependency vulnerabilities; Trivy reported zero high/critical image vulnerabilities; Syft generated SPDX. k6 baseline had zero failed requests, and a separate report-privacy check proves setup credentials/options are excluded. API/worker deployment state and hosted CI evidence are updated at final handoff.

## Next concrete task

Finish private document/quarantine/scanning workflows (M06), clinic-approved consent/guardian/retention policy, and representative security/performance review before real-patient use. Hosting, offsite PITR/anchors/paging, mail/invitations, new-device delivery and clinic paperwork were deferred explicitly; do not invent provider success or enable the inactive templates. Staff invitation/activation/password-reset lifecycle still needs implementation with a configured/explicit development delivery contract; no such email is currently sent. Review DEPLOYMENT_DECISIONS.md before production work. Full data-subject exports require the real record/file modules and expiration/ownership checks.

After M01: finish M02 facility/department/room/practitioner boundaries; M04 identifiers/contacts/next-of-kin/duplicate preview/supervised merge; M05 consent/relationship policy/break-glass; M06 private uploads/scan quarantine. Define catalogue/billable-event contracts before appointment/encounter/clinical fulfillment. Follow milestone ordering from the original prompt through M24; do not reduce the scope to CRUD.

## Constraints and unresolved policy

- System administration has no patient or clinical permission by default. Reception currently accesses demographics only within assigned active facilities; clinical relationship/state rules must precede clinical endpoints.
- No generic status PATCH commands, fabricated clinical reference knowledge, thresholds, video URLs or provider delivery success.
- No monetary code is implemented. Agree exact numeric/decimal-string handling and clinic-approved TZS rounding before billing.
- No real POWERHMS export exists. Canonical staging/import tooling and synthetic examples still need implementation; real source mappings remain unverified.
- Date-only/approximate birth information is distinct; use UTC instants and configured clinic timezone for future reports.
- The PostgreSQL development sink means observation, never external patient messaging. Unsupported jobs fail visibly; production consumers are not configured.
- Handwritten repository SQL is currently used; consider sqlc for further typed query generation without moving rules into generated code.
- Add protected metrics, load/performance measurements, image OS scans, attachment recovery, production secret/TLS/proxy design and full release/clinic acceptance evidence before readiness claims.

## Re-check after changes

```bash
set -a; source .env; set +a
make check
go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...
python3 scripts/generate-openapi.py
go run github.com/getkin/kin-openapi/cmd/validate@v0.133.0 api/openapi/openapi.json
python3 scripts/demo.py
python3 scripts/demo-security.py
python3 scripts/demo-privacy.py
go run ./cmd/manage audit-seal
go run ./cmd/manage audit-verify
# Quiesce synthetic writes before comparing a backup to its source:
python3 scripts/restore-check.py
```

Update README, MODULE_STATUS, permissions, workflows, integrations, operations and this record with actual implementation and command evidence. Keep accepted distinct from internally tested.

The schema-3 image also passed the live MFA security demo in a read-only, capability-dropped container running as UID/GID 10001; that disposable container was stopped afterwards.

Benchmark reproduction/limits are in PERFORMANCE.md; classification in DATA_CLASSIFICATION.md; emergency procedures in INCIDENT_RESPONSE.md. Preserve unrelated .vscode settings and all ignored credentials/database state.

## Final verification and push record

Implementation ae658b9c589e7f8029a5c08bdf3a64a56cc99e5c was pushed to origin/main. Hosted backend run 44129631700 and security run 44129631581 both passed, including PostgreSQL 18.7/race checks, OpenAPI/report-privacy, all scanner gates and SPDX artifact publication. Evidence links are in SECURITY_TASK_STATUS.md. Published main history scanned clean; private local checkpoint refs are not part of the published source scan. Unrelated .vscode settings remain untracked and untouched.

Native API/worker were rebuilt/restarted on 8080 against schema 6; readiness and the live contract (0.3.0, 40 operations) passed. The final digest-pinned non-root read-only container passed the live privacy/identifier/review demo as UID/GID 10001 and was stopped. No production resources, external delivery or legal acceptance are claimed. Docs-only follow-up records this observed evidence; all code is in the implementation commit.
