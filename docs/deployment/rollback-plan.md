# Rollback & Forward-Recovery Plan

## Case 1: Failure Before Traffic Cutover (Steps 1–7)
Because the legacy database is untouched and frozen in read-only mode during cutover:
1. Abort the cutover immediately.
2. Stop the new API and worker instances.
3. Re-enable the legacy API and worker pointed at the preserved legacy database.
4. Investigate the migration exception report / schema error in an isolated staging restore before rescheduling cutover.

## Case 2: Failure After Traffic Cutover (Post-Step 8)
Once new clinical, diagnostic, pharmacy, or billing records are written to `spartial_hms`, **do not** blindly revert to the legacy database (which lacks clinical/lab/pharmacy/billing tables and would lose post-cutover clinical/financial writes).
1. **Prefer Forward Recovery:**
   - Diagnose failing endpoint or database constraint from structured logs (`request_id`, HTTP status, SQLSTATE).
   - Deploy a hotfix binary or forward-only additive migration (`0019_...sql`) via `tools/migrate.sh`.
   - Never modify or delete an already-applied migration file (checksum drift will be rejected).
2. **Emergency Revert (Only Within Minutes of Cutover with Zero/Exported New Writes):**
   - Put API into maintenance mode.
   - Export any post-cutover transactions from `spartial_hms` for clinical/financial reconciliation.
   - Restore legacy API pointing to the preserved legacy database snapshot and manually reconcile exported events.
