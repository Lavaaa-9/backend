# Spartial Polyclinic backend

Working **Milestone A foundation**, built from [the implementation specification](Spartial_Fiber_v3_PostgreSQL_Backend_Agent_Prompt.md). Go Fiber v3, PostgreSQL, a separate development worker, and 40 documented API operations. This is an initial implementation; clinical workflows and the full backend remain unfinished. See [the module register](docs/MODULE_STATUS.md).

## Run locally

Requires Go **1.27.4**, PostgreSQL **18** command-line/server tools, Python 3, and Bash. No production database or external provider is used. Linux native setup:

```bash
./scripts/dev-db.sh
go run .
```

The script creates an isolated cluster under `.local/postgres`, listening on `127.0.0.1:55432`; generates credentials in ignored, mode-600 `.env`; applies migrations; creates a restricted application database role; and seeds synthetic accounts. It preserves existing accounts and never resets their passwords. The migration owner and runtime account are separate. Do not reuse this database for real patients.

Run `go run .` from the project root to start the API. It loads `.env` automatically, preserving any variables already set in your shell. PostgreSQL must be running with migrations and runtime grants applied. Docker and Podman are optional.

The API listens at `http://127.0.0.1:8080`. In another terminal, export `.env` as above and run:

```bash
curl --fail http://127.0.0.1:8080/health/ready
python3 scripts/demo.py
go run ./cmd/worker
```

The demo proves login, idempotent patient registration, search/read, receptionist denial on staff administration, refresh rotation, and logout. The worker records **development sink observations only**; it sends no messages. To stop the native database: `./scripts/dev-db.sh stop`. Use Ctrl-C for API/worker shutdown.

Synthetic logins: `admin@spartial.test` and `reception@spartial.test`. Their generated passwords are `SEED_ADMIN_PASSWORD` and `SEED_RECEPTION_PASSWORD` in `.env`. Administrator has staff/facility/audit access; receptionist has patient demographic access. Administration does not grant clinical access.

To connect an existing **development** PostgreSQL database, copy `.env.example`, generate replacements for every password placeholder, and set both database URLs. Run:

```bash
set -a; source .env; set +a
go run ./cmd/manage migrate
go run ./cmd/manage grant-runtime
go run ./cmd/manage seed
```

`DATABASE_ADMIN_URL` is used only by management commands. The API rejects superuser, database-creation, role-creation, and audit-mutation privileges. `seed` and `bootstrap-development` reject non-development environments. Public deployment and live clinic data are outside this session.

## Containers

The Dockerfile builds API, worker and management binaries into a non-root runtime. With generated `.env` values:

```bash
docker compose up --build -d
curl --fail http://127.0.0.1:8080/health/ready
docker compose logs api worker
docker compose down
```

Compose uses its own volume/database, with host PostgreSQL port **55433**; native development uses **55432**. Avoid running both APIs on port 8080. Bootstrap migrates, grants runtime privileges, and seeds before the API starts. Compose substitutes internal database addresses itself; host-side URLs in `.env` apply to native development. Do not run `docker compose down -v` unless you intend to discard all development data.

Private object-storage infrastructure is optional: `docker compose --profile documents up -d minio`. The document API/scanner is **not implemented** and no bucket is made public or uploads marked clean. MinIO configuration is preparation for M06.

## Security engineering extension

The engineering spec is implemented through schema version **6**, with **40 API operations**. New controls include care assignments, reasoned four-hour exceptional access and independent reviews, PostgreSQL row isolation, two-administrator approvals for admin grants/MFA reset, web idle expiry/device labels, offline common-password screening, encrypted identifiers and exact lookup, resumable key rotation, privacy reports/alerts, protected metrics, and PHI-free OpenTelemetry attributes.

Run `python3 scripts/demo-privacy.py` after exporting the development environment. Production care access requires an approved policy reference; production API traffic requires HTTPS through an explicitly trusted proxy, and PostgreSQL uses `sslmode=verify-full` with an explicit CA. Remote doctors/laboratory/pharmacy require MFA. See [the task register](docs/SECURITY_TASK_STATUS.md) for exact completed controls and remaining gaps, [deferred hosting/clinic decisions](docs/DEPLOYMENT_DECISIONS.md), and [current operator instructions](docs/SECURITY_ENGINEERING.md).

Hosting/offsite storage/mail, clinic paperwork, private document scanning, full data-subject exports and later clinical modules remain pending. No legal certification or production readiness is claimed. The new security workflow blocks high/critical image findings, checks source/dependencies/secrets and publishes an SPDX SBOM.

## Security phase 1

The security patch is integrated: encrypted TOTP secrets, challenge-based MFA login, one-use recovery codes and regeneration, privileged-session enforcement, and tamper-evident audit seals. Run `python3 scripts/demo-security.py` with the development environment exported to exercise a disposable synthetic account. See [the MFA and audit guide](docs/SECURITY_PHASE1.md) for frontend flows, migration/key rotation, assurance rules and remaining recovery gaps.

Existing native development `.env` files get a missing keyring generated by `scripts/dev-db.sh`; existing key material is preserved. An existing database must apply migrations through 00006 and rerun `manage grant-runtime`. Production configuration requires keys and enforces MFA for privileged staff routes. The broader backend remains unfinished.

## Frontend and mobile connection

The machine-readable contract is [api/openapi/openapi.json](api/openapi/openapi.json), served at `/api/openapi.json`. Base URL: `http://localhost:8080/api/v1`. Staff domain routes live under `/staff`; authentication under `/auth`. Patient-facing account routes are pending, and mobile clients must not treat the current staff login as a patient login.

If staff MFA is confirmed, `/auth/login` returns a challenge without credentials; complete `/auth/mfa/verify` before using staff routes. For a browser, use `client_type: "web"`, an exact configured Origin (default `http://localhost:3000`), and `credentials: "include"`. Refresh credentials stay in an HttpOnly cookie; keep access credentials in memory. SameSite=Strict requires same-site frontend/API deployment. Production cookies are Secure. Refresh/logout require the configured Origin. For mobile staff clients, use `client_type: "mobile"`, send no Origin, and store the returned refresh credential in platform secure storage. Serialize refresh calls: reuse of an old token revokes the session.

Send `Authorization: Bearer <access_token>` to protected endpoints. Patient registration requires `Idempotency-Key`; updates require the current integer `version`. Date-only values are `YYYY-MM-DD`; instants are RFC3339 UTC. Exact birth dates, approximate birth years, and unknown birth dates are distinct. Shared phone numbers are valid. See [permissions](docs/PERMISSIONS.md), [workflow rules](docs/WORKFLOWS.md), and [curl examples](docs/API_EXAMPLES.md).

## Verify

Use a dedicated local PostgreSQL administrator connection in `TEST_DATABASE_URL`. Tests create and drop randomly named databases and runtime roles; never point this variable at a production server.

```bash
set -a; source .env; set +a
make check
go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...
go run github.com/getkin/kin-openapi/cmd/validate@v0.133.0 api/openapi/openapi.json
python3 scripts/generate-openapi.py
```

`make check` checks formatting, vet, race-enabled tests, command builds, and dependency checksums. Without `TEST_DATABASE_URL`, PostgreSQL tests explicitly skip; that is **not** database verification. Tests use PostgreSQL, not SQLite or mocked locking. CI supplies a real database and builds the image.

See [architecture](docs/ARCHITECTURE.md), [operations](docs/OPERATIONS.md), [external blockers](docs/INTEGRATIONS.md), and [continuation tasks](docs/CONTINUE_HERE.md). No module is clinic accepted or production ready.
