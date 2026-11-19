# Tenant Isolation & Row-Level Security Validation

## Defence-in-Depth Architecture
1. **Restricted Runtime Roles:** Startup `platform.CheckRuntimeRole` aborts if `DATABASE_URL` or `DATABASE_WORKER_URL` connects as a superuser, `BYPASSRLS`, `CREATEROLE`, `CREATEDB`, `REPLICATION`, or a member of `spartial_owner`.
2. **Database-Derived Session Context:** Every request transaction begins with `SELECT ... FROM sec.begin_request($1::uuid, $2::uuid)`. Session GUCs (`app.org_id`, `app.user_id`, `app.membership_id`, `app.perms`, `app.break_glass`) use `SET LOCAL` (transaction-scoped, safe with PgBouncer transaction pooling) and fail closed (`NULL`) if unset.
3. **RESTRICTIVE Tenant Isolation Policies:** Every tenant table has `ENABLE ROW LEVEL SECURITY`, `FORCE ROW LEVEL SECURITY` (on PHI/financial/business tables), and an `AS RESTRICTIVE` `tenant_isolation` policy requiring `org_id = sec.current_org_id()`.
4. **PERMISSIVE Permission & Patient-Self Policies:** Table-level `SELECT`, `INSERT`, and `UPDATE` policies require `sec.has_any_perm(...)`. Patient portal access is constrained both by `patient_self` RLS policies and `sec.patient_column_guard()` column triggers.
5. **Reporting View Safety:** Every view in `reporting.*` uses `WITH (security_invoker = true)` so RLS applies under the caller's permissions (e.g. a manager without `clinical.read` cannot read `reporting.v_diagnosis_counts`). Materialized views are avoided for tenant data.

## Executed Validation Evidence
- `db/tests/200_security.sql` executed on migrations `0001`–`0018`: **55/55 security tests passed, 0 failed** (cross-tenant isolation, role boundary denials, append-only audit tamper detection, patient portal column guards, break-glass single-patient scoping).
- `tests/integration/backend_complete_test.go`: verified permission denials (`403`), four-eyes enforcement (`422`), legal-hold deletion blocks (`409`), and RLS visibility boundaries under `spartial_app` and `spartial_worker`.
