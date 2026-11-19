# Executed Test Results

All results below were executed directly against native PostgreSQL 18 (`127.0.0.1:55432`) with migrations `0001`–`0018` applied.

## 1. Database Architecture & Security Suite (`db/tests/run_all.sh`)
Executed against a clean disposable database (`spartial_db_suite_test`) built from scratch via `tools/migrate.sh`:

| Suite | Passed | Failed | Scope Verified |
|---|---:|---:|---|
| `workflow` | 90 | 0 | End-to-end clinical, lab, pharmacy, billing, telehealth, document, governance workflows & state machines |
| `security` | 55 | 0 | Cross-tenant RLS isolation, role boundaries, patient portal column guards, break-glass scoping, audit immutability |
| `scope_gaps` | 31 | 0 | Patient verification history, nursing observations, billable procedures, staff activity/access reports |
| `concurrency` | 2 | 0 | Concurrent dispense serialization (prevents negative stock) & concurrent MRN allocation (`gaps=0, dups=0`) |
| **Total** | **178** | **0** | |

## 2. Go Build, Static Analysis & Unit/Integration Suite (`make check`)
- `gofmt -l main.go cmd internal db api tests`: **clean (0 unformatted files)**
- `go vet ./...`: **clean (0 warnings)**
- `go test -race -count=1 ./...`:
  - `spartial.local/backend/internal/config`: **PASS** (12 configuration & production transport/MFA/key validation subtests)
  - `spartial.local/backend/internal/security`: **PASS** (RFC 6238 TOTP, replay window, recovery codes, AES-256-GCM keyring, password policy, Argon2id hashing, token entropy)
  - `spartial.local/backend/internal/migration`: **PASS** (disposable legacy DB -> disposable `spartial_hms` DB migration, AAD re-encryption, PHI-free report, audit integrity, idempotency pass 2)
  - `spartial.local/backend/tests/integration`: **PASS** (7 end-to-end HTTP API integration suites across all backend modules and `spartial_worker`)
- `go build . ./cmd/...`: **PASS** (`.`, `cmd/api`, `cmd/manage`, `cmd/worker`)
- `go mod verify`: **PASS** (`all modules verified`)

## 3. Worker Execution (`go run ./cmd/worker --once`)
- Connected as restricted role `spartial_worker` (`CheckSchema` + `CheckRuntimeRole` passed).
- Dispatched pending `comms.outbox` event (`status='succeeded'`), ran `gov.detect_privacy_alerts()`, and sealed pending audit events via `audit.seal_pending()`.

## Outstanding Risks / Pending Items
- **Frontend integration (Phase 7):** Existing frontend screens were intentionally not modified per user instruction ("dont do forntend please finnsht alll the backend"). Existing auth/facility/staff/patient endpoints preserve backwards-compatible JSON fields and permission aliases; new clinical/lab/pharmacy/billing/telehealth/privacy screens remain to be built in `../frontend` when requested.
- **External providers:** SMS/WhatsApp/mobile-money/video/object-storage delivery uses local development sinks until vendor BAAs/DPAs and secret manager integrations are provisioned.
- **Retention periods:** `gov.data_category.retention_period` remains `NULL` pending legal/business retention policy approval.
