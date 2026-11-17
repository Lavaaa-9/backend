# Security phase 1: MFA, encrypted secrets and audit seals

The supplied `spartial-security-phase1.patch` is integrated with additional fixes for management rate limits, session revalidation, recovery regeneration and audit commit-order safety. Migration 00003 introduced the original controls; the engineering extension now runs through schema 6 with 40 operations. See SECURITY_ENGINEERING.md for later care/approval/transport/identifier controls. This phase is internally tested; it does not establish regulatory compliance or complete M01/M05.

## Local setup and migration

`./scripts/dev-db.sh` preserves existing development credentials, adds a missing development encryption keyring without replacing existing keys, migrates and grants the runtime role. For an existing development deployment using another database, back up synthetic data, run `manage migrate` with the schema owner and rerun `manage grant-runtime`; restart the matching API afterwards. Never silently replace an encryption key to fix a decryption error.

`PHI_ENCRYPTION_KEYS` contains comma-separated `key-id:base64(32 random bytes)` entries; `PHI_ACTIVE_KEY_ID` selects the key for new ciphertext. TOTP secrets use AES-256-GCM and associated data containing the staff ID. Ciphertext retains the key ID so old keys can continue decrypting after rotation. `manage new-encryption-key` generates material for secret provisioning; capture it privately. Generated development keys live only in ignored mode-600 `.env`.

Production configuration requires an encryption keyring and forces `MFA_ENFORCE_PRIVILEGED=true`. Development defaults to false so synthetic receptionist/admin provisioning still works. A configured but missing old key fails TOTP verification with 503; it never bypasses MFA. Recovery codes can still be used as their independent factor. No real patient columns are claimed encrypted by this patch.

Retain old keys until a tested re-encryption job and recovery rehearsal prove they are no longer needed. The owner-authorized `manage rotate-keys` command now supports both TOTP and patient identifiers, with per-row audit and a residual-old-key check. Stop old-key writers and keep all decryption keys until the documented recovery/retirement checks pass. Back up key IDs/material separately under a governed secrets process; a database dump alone cannot recover encrypted MFA secrets if keys are lost.

## Browser and mobile login

1. `POST /api/v1/auth/login` with email, password and client_type. With confirmed MFA, response is `{mfa_required:true,challenge_token,expires_in:300}`; no session, access credential or refresh cookie is created by this password step.
2. `POST /api/v1/auth/mfa/verify` with that challenge, matching client_type, and exactly one of `code` (six decimal digits) or `recovery_code`. Successful verification issues the normal tokens/cookie and sets server-side session assurance to MFA verified.
3. Browser requests require the exact configured Origin and use the HttpOnly refresh cookie; the MFA response never includes a browser refresh token. Mobile staff clients have no Origin and receive refresh in JSON for native secure storage. Challenges cannot be moved between client types.
4. Refresh preserves MFA assurance. Suspension, password/account changes and session revocation continue to apply server-side.

Challenges expire in five minutes, allow five failed attempts, are single-use, and are bound to the staff version. Confirmation, disablement and recovery regeneration increment that version, invalidating pending challenges. Per-IP and per-account verification limits are ten attempts per 15 minutes. Expired, reused, exhausted, stale and incorrect challenges are audited with machine reasons; no supplied codes are recorded.

Administrator and privacy-reviewer staff permission routes require MFA-verified sessions when enforcement is enabled. Authentication/session and MFA setup routes remain available to bootstrap enrollment. System administration still does not grant patient or clinical access.

## Enrollment and recovery codes

- `POST /auth/mfa/enroll`: authenticated session plus `current_password`; returns a new base32 secret and `otpauth://` provisioning URI. Store/display them only in the enrollment flow. The pending enrollment lasts ten minutes; restarting replaces only an unconfirmed secret. Confirmed MFA cannot be silently replaced.
- `POST /auth/mfa/confirm`: six-digit `code`. On success, upgrades the calling session and revokes all other sessions, returning ten recovery codes once. Only SHA-256 digests of the random 80-bit recovery codes are stored.
- `POST /auth/mfa/recovery-codes/regenerate`: `current_password` and exactly one unused `code` or `recovery_code`. Atomically replaces all ten codes, invalidates pending login challenges and revokes other sessions. The current session remains active. Prior codes immediately stop working.
- `POST /auth/mfa/disable`: password and exactly one factor; removes secret/codes, invalidates pending challenges, revokes all sessions and clears the web cookie.

All four management commands share a ten-attempt per-account 15-minute limit; invalid reauthentication/factor attempts are audited. Live account and session state are checked again while transaction locks are held, so a request waiting behind logout/suspension cannot confirm or replace MFA using its stale authorization snapshot.

TOTP uses 30-second counters with one step of drift; an accepted step cannot be reused, including across login and management commands. After a TOTP login, wait for a fresh code or use an unused recovery code for a factor-protected change. Recovery codes are consumed atomically even across concurrent independent challenges. Never automatically retry confirmation/regeneration after an ambiguous response: successful one-time material cannot be replayed. Sign in and deliberately regenerate using a fresh factor if necessary.

There is no administrator-assisted MFA reset or account-recovery delivery implementation yet. Approval/identity-check requirements for a locked-out privileged account need a governed clinic workflow; the backend does not expose an unaudited reset shortcut.

## Audit integrity and its limits

`audit_events` and `audit_seals` reject updates/deletes/truncation. The runtime role may insert audit events but cannot create seals or execute the sealing function. Authorized management/sealing credentials must own that operation. Audit queries now include short machine `reason` and optional `facility_id` metadata.

Run `go run ./cmd/manage audit-seal` periodically through authorized deployment scheduling. It seals up to 50,000 events using a five-minute settle window, returning a chain head and event range. SHA-256 event digests use a structured JSON array, including explicit nulls and UTC timestamps; the running digest links each event to the previous head.

A settle delay alone is insufficient because PostgreSQL identity allocation is not commit order. The sealing function serializes sealers, takes a SHARE lock on audit_events to wait for existing writers/block new inserts briefly, and seals only the contiguous eligible prefix in ID order. It requires READ COMMITTED, caps lock waiting at two seconds and runs under the application's bounded statement timeout. Schedule and size batches so this short write barrier fits the operational workload; retry lock timeouts later rather than skipping events.

`go run ./cmd/manage audit-verify` recomputes sealed event ranges, validates first/last range metadata, counts and linked hashes, and reports `unsealed_events` separately. Success applies to the sealed prefix only. Zero seals does not prove a full audit history was sealed. A changed sealed event is detected; normal new events remain an explicit unsealed tail.

Ship each accepted head off-host to an approved immutable/independent destination. That shipping/schedule is not configured or tested. A sufficiently privileged owner could otherwise rewrite events and seals together or remove the latest seal without an independent anchor. No cryptographic chain stored entirely in the same database removes that privileged-database limitation.

## Verification

With the synthetic development environment exported:

```bash
make check
python3 scripts/demo.py
python3 scripts/demo-security.py
go run ./cmd/manage audit-seal
go run ./cmd/manage audit-verify
```

The security demo provisions a disposable synthetic staff account, exercises enrollment/challenge/recovery regeneration/disablement, then deactivates the account preserving history. It never prints secrets/recovery codes. It requires the development administrator's password-only provisioning flow; an administrator already enrolled in MFA should use the isolated PostgreSQL integration suite instead.

The suite verifies RFC TOTP vectors, encrypted-key rotation/AAD/tamper rejection, no pre-factor session issuance, replay/expiry/attempt limits, browser/mobile isolation, assurance across refresh, recovery consumption/regeneration under concurrency, session revocation, denied-action audits, audit tampering, outstanding-writer barriers and settle-window prefix ordering. Files/providers/clinical records remain outside this phase's verification.

References: [TOTP RFC 6238](https://www.rfc-editor.org/rfc/rfc6238), [PostgreSQL table-lock behavior](https://www.postgresql.org/docs/18/explicit-locking.html), and [Go authenticated-encryption API](https://pkg.go.dev/crypto/cipher#NewGCM).
