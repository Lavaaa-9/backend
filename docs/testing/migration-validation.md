# Migration Validation Report

## Disposable-Database Verification (`internal/migration/legacy_test.go`)
`TestLegacyToNewMigrationAndReconciliation` validates both schema engineering (Phase 3) and legacy data migration (Phase 4) automatically on every `go test ./...` run:

1. **Isolated Provisioning:** Creates two fresh disposable databases (`spartial_legacy_test_<rand>` and `spartial_target_test_<rand>`) and drops both on cleanup.
2. **Legacy Baseline:** Applies all 6 legacy goose up-migrations (`db/migrations/00001`–`00006`) and inserts representative data across `facilities`, `staff` (active + inactive across mapped roles), `staff_facilities`, `staff_mfa`, `mfa_recovery_codes`, `patients` (exact/year DOB, valid & invalid phone), `patient_identifiers` (legacy AAD ciphertext), `break_glass_access`, `approval_requests` (approved), `privacy_alerts`, and `audit_events`.
3. **Target Schema Build:** Runs `db/tools/migrate.sh` applying all 18 migrations (`0001`–`0018`) under `spartial_owner` with SHA-256 checksum tracking.
4. **Migration Pass 1 Assertions:**
   - `Report.Reconciled == true` (`source == migrated + duplicates + rejected` and `failed == 0` for every table).
   - `Report.AuditIntegrity == true` (`audit.verify_integrity()` returns 0 defects).
   - Inactive staff membership transitioned cleanly `active -> suspended`; approved legacy approval transitioned `pending -> approved` honoring `ref.state_transition`.
   - Legacy MFA TOTP secret decrypted from `staff_mfa:<id>` and re-sealed under `iam_mfa_factor:<id>`; decrypted bytes match original secret.
   - Legacy NIDA identifier decrypted from `patient_identifier:<pid>:nida:NIDA` and re-sealed under `patient_identifier:<org>:<pid>:nida_nin` with recomputed HMAC-SHA256 blind index; decrypted string matches original plaintext.
   - Invalid legacy phone recorded as non-PHI `transformed` exception without dropping patient; serialized JSON report contains zero PHI (verified against names, NIDA, phone, and TOTP secret).
5. **Idempotency Pass 2 Assertions:**
   - Re-running `migration.MigrateLegacy` against the same pair completes with `reconciled == true` and `0` newly inserted rows across all tables.
