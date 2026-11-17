# Continue the Spartial backend

## Saved state — 17 November 2026

The repository began with only the implementation prompt. Milestone A now has a running, tested vertical slice: Fiber v3.7.0, PostgreSQL, staff authentication/administration, fixed roles/permissions and facility assignments, patient registry with unique MRNs/search/versioned edits, audit, durable idempotency/outbox and a separate explicit development sink worker. The API contract contains 20 implemented operations.

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

## Next concrete task

Finish M01 security before clinical expansion: implement TOTP MFA enrollment/confirmation and challenge-based login, hashed recovery codes with audited recovery and reauthentication for disablement. Keep secrets encrypted using an explicit configured key and test challenge expiry/reuse/revocation and privilege escalation against real PostgreSQL. Do not retrofit token issuance before the second factor is satisfied. Plan an additive migration and update SchemaVersion/OpenAPI/tests together.

Then implement staff invitation/activation and reset-token lifecycle (hashed, expiring, single-use) with an explicit development delivery sink and a real-provider not-configured path. Add granular permission administration with delegation checks, protected roles and last-usable-admin invariants. Add runtime-safe Argon2 parameter policy/upgrades, staff search, session device labels and maintenance/retention for expired auth buckets/tokens/idempotency records.

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
# Quiesce synthetic writes before comparing a backup to its source:
python3 scripts/restore-check.py
```

Update README, MODULE_STATUS, permissions, workflows, integrations, operations and this record with actual implementation and command evidence. Keep accepted distinct from internally tested.
