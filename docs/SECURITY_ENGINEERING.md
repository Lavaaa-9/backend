# Operating the security engineering extension

Current schema: 6. API: 0.3.0, 40 implemented operations. Read SECURITY_TASK_STATUS.md for exact implementation and external blockers; the original requested spec remains PHASE1_ENGINEERING_SPEC.md.

## Local upgrade and verification

```bash
./scripts/dev-db.sh
set -a; source .env; set +a
make check
python3 scripts/demo-privacy.py
python3 scripts/demo-security.py
python3 scripts/restore-check.py
```

The setup preserves existing keys/passwords, generates only a completely missing development keyring/index key, applies migrations and refreshes restricted grants. Existing schema-3 native data was privately backed up before migrating 4–6 in this session. Stop writes/workers for a stable source when comparing restore counts. Neither real patients nor production resources are used.

Ordinary development admin/reception accounts remain unchanged. Care defaults are enabled for synthetic development. Production requires CARE_POLICY_ENABLED plus a real CARE_POLICY_APPROVAL_REF before clinical care grants, otherwise access fails closed. The clinic's signed policy/named reviewers are still required; the user authorized continuing development, not fabricated clinic paperwork.

## Care and privacy APIs

- Reception POST `/staff/patients/{id}/care-assignments`: staff_id, reason (20–1000 characters), valid_until (RFC3339 future instant), Idempotency-Key. Target must be active clinical staff at the same active facility.
- POST `/staff/care-assignments/{id}/revoke` preserves history and removes access.
- Doctor/nurse POST `/staff/patients/{id}/break-glass`: reason, duration_minutes (1–240), Idempotency-Key. Active concurrent grants cannot multiply by changing the key.
- Privacy reviewer GET `/staff/privacy/break-glass` lists outstanding reviews even after expiry; POST `/{id}/review` with outcome appropriate/inappropriate and note. A requester can never review their own grant. Inappropriate review removes its care-access effect.
- GET `/staff/privacy/patients/{id}/access` and `/staff/privacy/alerts` are restricted, audited review metadata, not clinical record dumps.

Doctor/nurse/laboratory/pharmacy demographic access needs an active assignment or permitted exceptional grant, in addition to live permission and facility scope. Reception remains demographic registry/coordinator access; administration does not get patient access. PostgreSQL independently repeats these rules, including no-actor zero rows, and transaction-local settings never persist into a pooled connection. Every returned patient search hit is audited in one batched write, avoiding N+1 audit round trips.

Emergency justification is kept in the restricted grant record referenced by audit, rather than ordinary logs. Consent/guardians, legal holds, richer clinical relationship sources, document quarantine and data-subject export remain later work.

## Independent administrator changes

POST `/staff/approvals` with kind admin_role_grant or mfa_reset, subject_id, reason and Idempotency-Key. A live MFA-verified administrator is required even in development. The request captures the subject version and expires after one hour. Another live MFA-verified administrator POSTs `/{id}/approve` or `/reject` with a review note. Same terminal decisions replay without repeating effects; stale/expired/contradictory decisions cannot execute.

Direct administrator creation/promotion is rejected by ordinary staff endpoints. Trusted initial two-admin bootstrap must be carried out under the clinic's authorized maintenance/identity process, outside that API. Approval is atomic with role/reset effects, target version increment, session revocation and audit. MFA reset leaves the password unchanged and forces re-enrollment before privileged use; clinic identity-check/recovery procedures remain an organizational requirement.

## Credentials, transport and activity

New passwords are screened offline against the pinned public common-password list plus explicit supplemental weak sequences. This is a limited screen, not proof against all breaches. Password change attempts are rate limited. Web sessions idle for fifteen minutes of backend-observed request inactivity cannot access or refresh; native mobile keeps its absolute seven-day session lifetime. Client-provided device_label is descriptive metadata, not a trusted device identity or proof that a new-device email was delivered.

Production PostgreSQL URLs must use sslmode=verify-full with explicit sslrootcert, and production API requests require HTTPS. Health probes contain no PHI and remain usable on the private HTTP listener. Trusted reverse proxies must be configured explicitly before forwarded scheme/client identity is accepted; broad all-address proxy ranges are rejected. Clinic networks are explicit IP/CIDR sets, never inferred from a caller header. Remote doctor/laboratory/pharmacy staff routes require MFA; the exact configured clinic network defines the local exception.

Do not copy a development environment into production: set production mode, HTTPS origins, verified database CA/hostname, explicit proxies/networks, approved care policy reference and secrets custody. Certificate chain, physical disks, infrastructure, workstation lock screens and provider contracts require deployment verification beyond these local config tests.

## Encrypted identifiers and key rotation

IDENTIFIER_INDEX_KEY is a separate random 32-byte base64 HMAC key. POST `/staff/patients/{id}/identifiers` with kind nida/insurance, issuer, value and Idempotency-Key. Values are trimmed/uppercased; interior formatting and issuer remain exact. No invented NIDA validation or account linking occurs. Creation returns metadata only; plaintext is absent from the database and replay cache. GET the collection decrypts under normal scoped/audited permission. POST `/staff/patient-identifiers/search` keeps lookup values out of URLs and returns at most 25 scoped patient IDs.

For encryption-key rotation, deploy the new active key with all old decryption keys to every writer, stop/drain old-key writers, and run `go run ./cmd/manage rotate-keys` from the authorized maintenance host. It requires explicit RLS-bypass maintenance credentials (never runtime), processes bounded batches, locks MFA rows in staff-first order, re-encrypts MFA/identifiers, audits each change and checks for remaining old-key envelopes. It is resumable and does not delete keys or change HMAC lookup indexes. Retire old keys only after residual verification and a recovery rehearsal including historical backups; blind-index key rotation requires a separate reviewed index rebuild.

## Monitoring and deployment work

`GET /staff/metrics` needs metrics.read and privileged MFA where enforced. Metrics include static route/method/status counts and cumulative durations. OTel exporter setup is explicit via OTEL_EXPORTER_OTLP_ENDPOINT; production requires HTTPS and rejects URL credentials/query strings. No collector is configured by default. Spans contain only normalized method, static route and status, with no names, IDs, bodies, query strings or raw error text.

PRIVACY_READ_THRESHOLD and PRIVACY_MFA_THRESHOLD default to zero (disabled). Explicit positive values enable deduplicated fixed 15-minute volume alerts, checked every minute by the worker; the last 30 minutes are reconsidered safely after retries. Off-hours rules require an approved schedule and are not silently assumed. Production workers mark unconfigured delivery consumers failed/retryable rather than using the development sink.

The timer/service templates in deploy/systemd are inactive; host installation, independent anchor forwarding and real alert delivery remain deferred. The pgBackRest example is also inactive until hosting, encryption-key custody, destination/region, WAL retention and recovery objectives are approved. The current dump restore verifies existing seals and an audited patient read under restored runtime grants; it does not prove PITR, private object restoration or external anchor integrity.

CI security tools are checksum/version pinned. Gitleaks scans commit history; gosec blocks high findings; govulncheck checks reachable dependency vulnerabilities; Trivy blocks high/critical image findings; Syft emits an SPDX SBOM. Dependabot covers Go, actions and Docker dependencies. Scanner success is a limited verification result, not penetration testing or clinic acceptance.
