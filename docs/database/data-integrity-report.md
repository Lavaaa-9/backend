# Data Integrity & Reconciliation Report

## Database-Enforced Invariants
1. **Tenant & Patient Cross-Entity Safety:** Composite foreign keys `(org_id, encounter_id, patient_id)` across triage, nursing, consultations, diagnoses, procedures, diagnostic requests/items/specimens/results, prescriptions, dispenses, invoices, and telehealth prevent attaching a clinical or financial record to another patient or organization.
2. **Financial Integrity:** `billing.invoice` (`subtotal`, `discount_total`, `total`, `amount_paid`, `balance`) is maintained solely by `SECURITY DEFINER` triggers (`billing.recalc_invoice`). `reporting.v_billing_reconciliation_exceptions` verifies line sums, payment allocations, and 1:1 receipt issuance; any returned row flags a defect.
3. **Stock Ledger Integrity:** `pharmacy.stock_batch.quantity_on_hand` is derived strictly from append-only `pharmacy.stock_movement` rows with row-level locking preventing negative stock under concurrent dispensing.
4. **Gapless Business Numbering:** `org.next_value()` allocates gapless transactional numbers for MRNs, encounters, invoices, receipts, lab requests, prescriptions, and daily queue tickets.
5. **Scheduling & Pricing Non-Overlap:** PostgreSQL GiST exclusion constraints (`ex_appt_practitioner`, `ex_appt_patient`, `ex_price_overlap`, `ex_range_overlap`) prevent double-booking and overlapping effective ranges at the database level (mapped to HTTP `409 Conflict`).
6. **Tamper-Evident Audit Chain:** Every row in `audit.event` stores a SHA-256 `row_hash`; `audit.seal_pending()` chains finalized transaction batches into `audit.seal`; `audit.verify_integrity()` recomputes every row hash and seal chain head.

## Executed Verification Summary
- **`db/tests/run_all.sh` (disposable DB, migrations 0001–0018):** 178 passed, 0 failed (`workflow`: 90, `security`: 55, `scope_gaps`: 31, `concurrency`: 2 — including concurrent dispense race test and gapless/duplicate-free MRN concurrency test).
- **`internal/migration/legacy_test.go` (disposable legacy DB -> disposable `spartial_hms` DB):** `reconciled=true`, `audit_integrity_ok=true`, re-encrypted MFA and NIDA ciphertexts verified under new AADs, zero PHI leaked in JSON report, second pass 100% idempotent (0 new rows).
