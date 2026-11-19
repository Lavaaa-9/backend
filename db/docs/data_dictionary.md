# Spartial HMS — Data Dictionary

Generated from the live PostgreSQL 16 catalog of the migrated test database by `tools/gen_docs.py`. 86 tables.

Legend: **PK** primary key · **FK→** foreign key target · NN = NOT NULL · (R) = restrictive RLS policy.

## Schema `org`

### `org.counter`

RLS: off (no direct app access or global)  
Triggers: —

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `org_id` | uuid | NN |  | **PK** FK→ `org.organization` |
| `counter_key` | text | NN |  | **PK** |
| `period_key` | text | NN | `''::text` | **PK** |
| `next_value` | bigint | NN |  |  |

Constraints:

- `counter_next_value_check` (check): `CHECK ((next_value > 0))`

Foreign keys:

- `counter_org_id_fkey`: `FOREIGN KEY (org_id) REFERENCES org.organization(id)`

Indexes:

- `counter_pkey`: `btree (org_id, counter_key, period_key)` (unique)

### `org.department`

RLS: enabled — policies: owner_internal, staff_insert, staff_select, staff_update, tenant_isolation(R)  
Triggers: trg_touch

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | uuid | NN | `gen_random_uuid()` | **PK** |
| `org_id` | uuid | NN |  | FK→ `org.facility` |
| `facility_id` | uuid | NN |  | FK→ `org.facility` |
| `code` | text | NN |  |  |
| `name` | text | NN |  |  |
| `dept_type` | text | NN |  |  |
| `status` | text | NN | `'active'::text` |  |
| `created_at` | timestamp with time zone | NN | `now()` |  |
| `created_by` | uuid | null | `sec.current_user_id()` | FK→ `iam.user_account` |
| `updated_at` | timestamp with time zone | NN | `now()` |  |
| `updated_by` | uuid | null |  | FK→ `iam.user_account` |
| `row_version` | integer | NN | `1` |  |

Constraints:

- `department_code_check` (check): `CHECK ((code ~ '^[A-Z0-9_-]{2,20}$'::text))`
- `department_dept_type_check` (check): `CHECK ((dept_type = ANY (ARRAY['reception'::text, 'triage'::text, 'consultation'::text, 'laboratory'::text, 'imaging'::text, 'pharmacy'::text, 'billing'::text, 'administration'::text, 'telemedicine'::text, 'other'::text])))`
- `department_facility_id_code_key` (unique): `UNIQUE (facility_id, code)`
- `department_org_id_id_key` (unique): `UNIQUE (org_id, id)`
- `department_status_check` (check): `CHECK ((status = ANY (ARRAY['active'::text, 'inactive'::text])))`

Foreign keys:

- `department_created_by_fkey`: `FOREIGN KEY (created_by) REFERENCES iam.user_account(id)`
- `department_org_id_facility_id_fkey`: `FOREIGN KEY (org_id, facility_id) REFERENCES org.facility(org_id, id)`
- `department_updated_by_fkey`: `FOREIGN KEY (updated_by) REFERENCES iam.user_account(id)`

Indexes:

- `department_facility_id_code_key`: `btree (facility_id, code)` (unique)
- `department_org_id_id_key`: `btree (org_id, id)` (unique)
- `department_pkey`: `btree (id)` (unique)
- `ix_department_facility`: `btree (org_id, facility_id)`

### `org.facility`

RLS: enabled — policies: owner_internal, staff_insert, staff_select, staff_update, tenant_isolation(R)  
Triggers: trg_touch

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | uuid | NN | `gen_random_uuid()` | **PK** |
| `org_id` | uuid | NN |  | FK→ `org.organization` |
| `code` | text | NN |  |  |
| `name` | text | NN |  |  |
| `hfr_code` | text | null |  |  |
| `phone_e164` | text | null |  |  |
| `region` | text | null |  |  |
| `district` | text | null |  |  |
| `street_address` | text | null |  |  |
| `status` | text | NN | `'active'::text` |  |
| `created_at` | timestamp with time zone | NN | `now()` |  |
| `created_by` | uuid | null | `sec.current_user_id()` | FK→ `iam.user_account` |
| `updated_at` | timestamp with time zone | NN | `now()` |  |
| `updated_by` | uuid | null |  | FK→ `iam.user_account` |
| `row_version` | integer | NN | `1` |  |

Constraints:

- `facility_code_check` (check): `CHECK ((code ~ '^[A-Z0-9_-]{2,20}$'::text))`
- `facility_org_id_code_key` (unique): `UNIQUE (org_id, code)`
- `facility_org_id_id_key` (unique): `UNIQUE (org_id, id)`
- `facility_phone_e164_check` (check): `CHECK ((phone_e164 ~ '^\+[1-9][0-9]{7,14}$'::text))`
- `facility_status_check` (check): `CHECK ((status = ANY (ARRAY['active'::text, 'inactive'::text])))`

Foreign keys:

- `facility_created_by_fkey`: `FOREIGN KEY (created_by) REFERENCES iam.user_account(id)`
- `facility_org_id_fkey`: `FOREIGN KEY (org_id) REFERENCES org.organization(id)`
- `facility_updated_by_fkey`: `FOREIGN KEY (updated_by) REFERENCES iam.user_account(id)`

Indexes:

- `facility_org_id_code_key`: `btree (org_id, code)` (unique)
- `facility_org_id_id_key`: `btree (org_id, id)` (unique)
- `facility_pkey`: `btree (id)` (unique)

### `org.organization`

Tenant. Spartial is tenant #1; model supports further clinics (Lavaa resale) without schema change.

RLS: enabled — policies: tenant_admin, tenant_self  
Triggers: trg_touch

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | uuid | NN | `gen_random_uuid()` | **PK** |
| `code` | text | NN |  |  |
| `legal_name` | text | NN |  |  |
| `display_name` | text | NN |  |  |
| `status` | text | NN | `'active'::text` |  |
| `timezone` | text | NN | `'Africa/Dar_es_Salaam'::text` |  |
| `currency_code` | character(3) | NN | `'TZS'::bpchar` |  |
| `mrn_prefix` | text | NN | `'SP'::text` |  |
| `staff_idle_timeout` | interval | NN | `'00:15:00'::interval` |  |
| `staff_session_max` | interval | NN | `'12:00:00'::interval` |  |
| `lockout_threshold` | smallint | NN | `5` |  |
| `lockout_duration` | interval | NN | `'00:15:00'::interval` |  |
| `lab_self_verification_allowed` | boolean | NN | `false` |  |
| `created_at` | timestamp with time zone | NN | `now()` |  |
| `created_by` | uuid | null | `sec.current_user_id()` | FK→ `iam.user_account` |
| `updated_at` | timestamp with time zone | NN | `now()` |  |
| `updated_by` | uuid | null |  | FK→ `iam.user_account` |
| `row_version` | integer | NN | `1` |  |

Constraints:

- `organization_code_check` (check): `CHECK ((code ~ '^[A-Z0-9_]{2,20}$'::text))`
- `organization_code_key` (unique): `UNIQUE (code)`
- `organization_currency_code_check` (check): `CHECK ((currency_code ~ '^[A-Z]{3}$'::text))`
- `organization_legal_name_check` (check): `CHECK (((length(btrim(legal_name)) >= 2) AND (length(btrim(legal_name)) <= 200)))`
- `organization_lockout_threshold_check` (check): `CHECK (((lockout_threshold >= 3) AND (lockout_threshold <= 10)))`
- `organization_mrn_prefix_check` (check): `CHECK ((mrn_prefix ~ '^[A-Z]{1,6}$'::text))`
- `organization_staff_idle_timeout_check` (check): `CHECK (((staff_idle_timeout >= '00:05:00'::interval) AND (staff_idle_timeout <= '01:00:00'::interval)))`
- `organization_staff_session_max_check` (check): `CHECK ((staff_session_max <= '24:00:00'::interval))`
- `organization_status_check` (check): `CHECK ((status = ANY (ARRAY['active'::text, 'suspended'::text, 'closed'::text])))`

Foreign keys:

- `organization_created_by_fkey`: `FOREIGN KEY (created_by) REFERENCES iam.user_account(id)`
- `organization_updated_by_fkey`: `FOREIGN KEY (updated_by) REFERENCES iam.user_account(id)`

Indexes:

- `organization_code_key`: `btree (code)` (unique)
- `organization_pkey`: `btree (id)` (unique)

### `org.service_point`

RLS: enabled — policies: owner_internal, staff_insert, staff_select, staff_update, tenant_isolation(R)  
Triggers: trg_touch

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | uuid | NN | `gen_random_uuid()` | **PK** |
| `org_id` | uuid | NN |  | FK→ `org.department` |
| `department_id` | uuid | NN |  | FK→ `org.department` |
| `code` | text | NN |  |  |
| `name` | text | NN |  |  |
| `status` | text | NN | `'active'::text` |  |
| `created_at` | timestamp with time zone | NN | `now()` |  |
| `created_by` | uuid | null | `sec.current_user_id()` | FK→ `iam.user_account` |
| `updated_at` | timestamp with time zone | NN | `now()` |  |
| `updated_by` | uuid | null |  | FK→ `iam.user_account` |
| `row_version` | integer | NN | `1` |  |

Constraints:

- `service_point_department_id_code_key` (unique): `UNIQUE (department_id, code)`
- `service_point_org_id_id_key` (unique): `UNIQUE (org_id, id)`
- `service_point_status_check` (check): `CHECK ((status = ANY (ARRAY['active'::text, 'inactive'::text])))`

Foreign keys:

- `service_point_created_by_fkey`: `FOREIGN KEY (created_by) REFERENCES iam.user_account(id)`
- `service_point_org_id_department_id_fkey`: `FOREIGN KEY (org_id, department_id) REFERENCES org.department(org_id, id)`
- `service_point_updated_by_fkey`: `FOREIGN KEY (updated_by) REFERENCES iam.user_account(id)`

Indexes:

- `service_point_department_id_code_key`: `btree (department_id, code)` (unique)
- `service_point_org_id_id_key`: `btree (org_id, id)` (unique)
- `service_point_pkey`: `btree (id)` (unique)

### `org.staff_department`

RLS: enabled — policies: owner_internal, staff_delete, staff_insert, staff_select, staff_update, tenant_isolation(R)  
Triggers: —

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `org_id` | uuid | NN |  | FK→ `org.department` |
| `membership_id` | uuid | NN |  | **PK** FK→ `iam.membership` |
| `department_id` | uuid | NN |  | **PK** FK→ `org.department` |
| `is_primary` | boolean | NN | `false` |  |
| `assigned_at` | timestamp with time zone | NN | `now()` |  |

Foreign keys:

- `staff_department_org_id_department_id_fkey`: `FOREIGN KEY (org_id, department_id) REFERENCES org.department(org_id, id)`
- `staff_department_org_id_membership_id_fkey`: `FOREIGN KEY (org_id, membership_id) REFERENCES iam.membership(org_id, id)`

Indexes:

- `staff_department_pkey`: `btree (membership_id, department_id)` (unique)
- `ux_staff_primary_dept`: `btree (membership_id) WHERE is_primary` (unique)

## Schema `iam`

### `iam.access_review`

RLS: enabled — policies: owner_internal, staff_insert, staff_select, staff_update, tenant_isolation(R)  
Triggers: trg_0_state, trg_touch

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | uuid | NN | `gen_random_uuid()` | **PK** |
| `org_id` | uuid | NN |  | FK→ `org.organization` |
| `title` | text | NN |  |  |
| `period_start` | date | NN |  |  |
| `period_end` | date | NN |  |  |
| `status` | text | NN | `'open'::text` |  |
| `started_by` | uuid | null | `sec.current_user_id()` | FK→ `iam.user_account` |
| `completed_at` | timestamp with time zone | null |  |  |
| `created_at` | timestamp with time zone | NN | `now()` |  |
| `created_by` | uuid | null | `sec.current_user_id()` | FK→ `iam.user_account` |
| `updated_at` | timestamp with time zone | NN | `now()` |  |
| `updated_by` | uuid | null |  | FK→ `iam.user_account` |
| `row_version` | integer | NN | `1` |  |

Constraints:

- `access_review_org_id_id_key` (unique): `UNIQUE (org_id, id)`
- `access_review_status_check` (check): `CHECK ((status = ANY (ARRAY['open'::text, 'completed'::text, 'cancelled'::text])))`
- `ck_review_completed` (check): `CHECK (((status = 'completed'::text) = (completed_at IS NOT NULL)))`
- `ck_review_period` (check): `CHECK ((period_end >= period_start))`

Foreign keys:

- `access_review_created_by_fkey`: `FOREIGN KEY (created_by) REFERENCES iam.user_account(id)`
- `access_review_org_id_fkey`: `FOREIGN KEY (org_id) REFERENCES org.organization(id)`
- `access_review_started_by_fkey`: `FOREIGN KEY (started_by) REFERENCES iam.user_account(id)`
- `access_review_updated_by_fkey`: `FOREIGN KEY (updated_by) REFERENCES iam.user_account(id)`

Indexes:

- `access_review_org_id_id_key`: `btree (org_id, id)` (unique)
- `access_review_pkey`: `btree (id)` (unique)

### `iam.access_review_item`

RLS: enabled — policies: owner_internal, staff_insert, staff_select, staff_update, tenant_isolation(R)  
Triggers: —

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | uuid | NN | `gen_random_uuid()` | **PK** |
| `org_id` | uuid | NN |  | FK→ `iam.membership_role` |
| `review_id` | uuid | NN |  | FK→ `iam.access_review` |
| `membership_role_id` | uuid | NN |  | FK→ `iam.membership_role` |
| `decision` | text | NN | `'pending'::text` |  |
| `decided_by` | uuid | null |  | FK→ `iam.user_account` |
| `decided_at` | timestamp with time zone | null |  |  |
| `note` | text | null |  |  |

Constraints:

- `access_review_item_decision_check` (check): `CHECK ((decision = ANY (ARRAY['pending'::text, 'keep'::text, 'revoke'::text])))`
- `access_review_item_review_id_membership_role_id_key` (unique): `UNIQUE (review_id, membership_role_id)`
- `ck_review_decided` (check): `CHECK (((decision = 'pending'::text) = (decided_at IS NULL)))`

Foreign keys:

- `access_review_item_decided_by_fkey`: `FOREIGN KEY (decided_by) REFERENCES iam.user_account(id)`
- `access_review_item_org_id_membership_role_id_fkey`: `FOREIGN KEY (org_id, membership_role_id) REFERENCES iam.membership_role(org_id, id)`
- `access_review_item_org_id_review_id_fkey`: `FOREIGN KEY (org_id, review_id) REFERENCES iam.access_review(org_id, id)`

Indexes:

- `access_review_item_pkey`: `btree (id)` (unique)
- `access_review_item_review_id_membership_role_id_key`: `btree (review_id, membership_role_id)` (unique)

### `iam.api_credential`

RLS: enabled — policies: owner_internal, staff_insert, staff_select, staff_update, tenant_isolation(R)  
Triggers: —

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | uuid | NN | `gen_random_uuid()` | **PK** |
| `org_id` | uuid | NN |  | FK→ `org.organization` |
| `user_id` | uuid | NN |  | FK→ `iam.user_account` |
| `name` | text | NN |  |  |
| `key_prefix` | text | NN |  |  |
| `key_hash` | bytea | NN |  |  |
| `allowed_cidrs` | _cidr | NN | `'{}'::cidr[]` |  |
| `expires_at` | timestamp with time zone | NN |  |  |
| `last_used_at` | timestamp with time zone | null |  |  |
| `revoked_at` | timestamp with time zone | null |  |  |
| `created_at` | timestamp with time zone | NN | `now()` |  |
| `created_by` | uuid | null | `sec.current_user_id()` | FK→ `iam.user_account` |

Constraints:

- `api_credential_key_hash_check` (check): `CHECK ((length(key_hash) = 32))`
- `api_credential_key_prefix_check` (check): `CHECK ((key_prefix ~ '^sp_[a-z]{4}_[A-Za-z0-9]{8}$'::text))`
- `api_credential_key_prefix_key` (unique): `UNIQUE (key_prefix)`
- `api_credential_org_id_id_key` (unique): `UNIQUE (org_id, id)`
- `ck_api_key_max_life` (check): `CHECK ((expires_at <= (created_at + '366 days'::interval)))`

Foreign keys:

- `api_credential_created_by_fkey`: `FOREIGN KEY (created_by) REFERENCES iam.user_account(id)`
- `api_credential_org_id_fkey`: `FOREIGN KEY (org_id) REFERENCES org.organization(id)`
- `api_credential_user_id_fkey`: `FOREIGN KEY (user_id) REFERENCES iam.user_account(id)`

Indexes:

- `api_credential_key_prefix_key`: `btree (key_prefix)` (unique)
- `api_credential_org_id_id_key`: `btree (org_id, id)` (unique)
- `api_credential_pkey`: `btree (id)` (unique)

### `iam.break_glass_access`

RLS: enabled — policies: own_grants, owner_internal, review_grants, staff_select, tenant_isolation(R)  
Triggers: trg_audit_capture

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | uuid | NN | `gen_random_uuid()` | **PK** |
| `org_id` | uuid | NN |  | FK→ `iam.membership` |
| `membership_id` | uuid | NN |  | FK→ `iam.membership` |
| `patient_id` | uuid | NN |  | FK→ `patient.patient` |
| `reason` | text | NN |  |  |
| `started_at` | timestamp with time zone | NN | `now()` |  |
| `expires_at` | timestamp with time zone | NN |  |  |
| `ended_at` | timestamp with time zone | null |  |  |
| `reviewed_by` | uuid | null |  | FK→ `iam.user_account` |
| `reviewed_at` | timestamp with time zone | null |  |  |
| `review_outcome` | text | null |  |  |
| `review_note` | text | null |  |  |

Constraints:

- `break_glass_access_org_id_id_key` (unique): `UNIQUE (org_id, id)`
- `break_glass_access_reason_check` (check): `CHECK ((length(btrim(reason)) >= 20))`
- `break_glass_access_review_outcome_check` (check): `CHECK ((review_outcome = ANY (ARRAY['appropriate'::text, 'inappropriate'::text, 'needs_follow_up'::text])))`
- `ck_bg_review` (check): `CHECK (((reviewed_at IS NULL) = (review_outcome IS NULL)))`
- `ck_bg_window` (check): `CHECK (((expires_at > started_at) AND (expires_at <= (started_at + '04:00:00'::interval))))`

Foreign keys:

- `break_glass_access_org_id_membership_id_fkey`: `FOREIGN KEY (org_id, membership_id) REFERENCES iam.membership(org_id, id)`
- `break_glass_access_org_id_patient_id_fkey`: `FOREIGN KEY (org_id, patient_id) REFERENCES patient.patient(org_id, id)`
- `break_glass_access_reviewed_by_fkey`: `FOREIGN KEY (reviewed_by) REFERENCES iam.user_account(id)`

Indexes:

- `break_glass_access_org_id_id_key`: `btree (org_id, id)` (unique)
- `break_glass_access_pkey`: `btree (id)` (unique)
- `ix_break_glass_active`: `btree (membership_id, expires_at) WHERE (ended_at IS NULL)`
- `ix_break_glass_unreviewed`: `btree (org_id, started_at) WHERE (reviewed_at IS NULL)`

### `iam.membership`

RLS: enabled — policies: owner_internal, staff_insert, staff_select, staff_update, tenant_isolation(R)  
Triggers: trg_0_state, trg_touch

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | uuid | NN | `gen_random_uuid()` | **PK** |
| `org_id` | uuid | NN |  | FK→ `org.organization` |
| `user_id` | uuid | NN |  | FK→ `iam.user_account` |
| `employee_no` | text | null |  |  |
| `job_title` | text | null |  |  |
| `professional_body` | text | null |  |  |
| `professional_reg_no` | text | null |  |  |
| `status` | text | NN | `'invited'::text` |  |
| `start_date` | date | NN | `CURRENT_DATE` |  |
| `end_date` | date | null |  |  |
| `created_at` | timestamp with time zone | NN | `now()` |  |
| `created_by` | uuid | null | `sec.current_user_id()` | FK→ `iam.user_account` |
| `updated_at` | timestamp with time zone | NN | `now()` |  |
| `updated_by` | uuid | null |  | FK→ `iam.user_account` |
| `row_version` | integer | NN | `1` |  |

Constraints:

- `ck_membership_dates` (check): `CHECK (((end_date IS NULL) OR (end_date >= start_date)))`
- `ck_membership_ended` (check): `CHECK (((status <> 'ended'::text) OR (end_date IS NOT NULL)))`
- `ck_prof_reg` (check): `CHECK (((professional_body IS NULL) = (professional_reg_no IS NULL)))`
- `membership_org_id_id_key` (unique): `UNIQUE (org_id, id)`
- `membership_org_id_user_id_key` (unique): `UNIQUE (org_id, user_id)`
- `membership_professional_body_check` (check): `CHECK ((professional_body = ANY (ARRAY['MCT'::text, 'TNMC'::text, 'PC'::text, 'HLPC'::text, 'other'::text])))`
- `membership_status_check` (check): `CHECK ((status = ANY (ARRAY['invited'::text, 'active'::text, 'suspended'::text, 'ended'::text])))`

Foreign keys:

- `membership_created_by_fkey`: `FOREIGN KEY (created_by) REFERENCES iam.user_account(id)`
- `membership_org_id_fkey`: `FOREIGN KEY (org_id) REFERENCES org.organization(id)`
- `membership_updated_by_fkey`: `FOREIGN KEY (updated_by) REFERENCES iam.user_account(id)`
- `membership_user_id_fkey`: `FOREIGN KEY (user_id) REFERENCES iam.user_account(id)`

Indexes:

- `membership_org_id_id_key`: `btree (org_id, id)` (unique)
- `membership_org_id_user_id_key`: `btree (org_id, user_id)` (unique)
- `membership_pkey`: `btree (id)` (unique)
- `ux_membership_employee_no`: `btree (org_id, employee_no) WHERE (employee_no IS NOT NULL)` (unique)

### `iam.membership_role`

RLS: enabled — policies: owner_internal, staff_insert, staff_select, staff_update, tenant_isolation(R)  
Triggers: trg_membership_role_check

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | uuid | NN | `gen_random_uuid()` | **PK** |
| `org_id` | uuid | NN |  | FK→ `org.facility` |
| `membership_id` | uuid | NN |  | FK→ `iam.membership` |
| `role_id` | uuid | NN |  | FK→ `iam.role` |
| `facility_id` | uuid | null |  | FK→ `org.facility` |
| `granted_by` | uuid | null | `sec.current_user_id()` | FK→ `iam.user_account` |
| `granted_at` | timestamp with time zone | NN | `now()` |  |
| `expires_at` | timestamp with time zone | null |  |  |
| `revoked_at` | timestamp with time zone | null |  |  |
| `revoked_by` | uuid | null |  | FK→ `iam.user_account` |
| `revoke_reason` | text | null |  |  |

Constraints:

- `ck_mr_expiry` (check): `CHECK (((expires_at IS NULL) OR (expires_at > granted_at)))`
- `ck_mr_revoke` (check): `CHECK (((revoked_at IS NULL) = (revoked_by IS NULL)))`
- `membership_role_org_id_id_key` (unique): `UNIQUE (org_id, id)`

Foreign keys:

- `membership_role_granted_by_fkey`: `FOREIGN KEY (granted_by) REFERENCES iam.user_account(id)`
- `membership_role_org_id_facility_id_fkey`: `FOREIGN KEY (org_id, facility_id) REFERENCES org.facility(org_id, id)`
- `membership_role_org_id_membership_id_fkey`: `FOREIGN KEY (org_id, membership_id) REFERENCES iam.membership(org_id, id)`
- `membership_role_revoked_by_fkey`: `FOREIGN KEY (revoked_by) REFERENCES iam.user_account(id)`
- `membership_role_role_id_fkey`: `FOREIGN KEY (role_id) REFERENCES iam.role(id)`

Indexes:

- `ix_membership_role_membership`: `btree (membership_id) WHERE (revoked_at IS NULL)`
- `membership_role_org_id_id_key`: `btree (org_id, id)` (unique)
- `membership_role_pkey`: `btree (id)` (unique)
- `ux_membership_role_active`: `btree (membership_id, role_id, COALESCE(facility_id, '00000000-0000-0000-0000-000000000000'::uuid)) WHERE (revoked_at IS NULL)` (unique)

### `iam.mfa_factor`

RLS: off (no direct app access or global)  
Triggers: —

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | uuid | NN | `gen_random_uuid()` | **PK** |
| `user_id` | uuid | NN |  | FK→ `iam.user_account` |
| `factor_type` | text | NN |  |  |
| `label` | text | NN | `'default'::text` |  |
| `secret_ciphertext` | bytea | null |  |  |
| `key_ref` | text | null |  |  |
| `webauthn_credential_id` | bytea | null |  |  |
| `webauthn_public_key` | bytea | null |  |  |
| `status` | text | NN | `'pending'::text` |  |
| `verified_at` | timestamp with time zone | null |  |  |
| `last_used_at` | timestamp with time zone | null |  |  |
| `last_used_step` | bigint | null |  |  |
| `created_at` | timestamp with time zone | NN | `now()` |  |

Constraints:

- `ck_mfa_active_verified` (check): `CHECK (((status <> 'active'::text) OR (verified_at IS NOT NULL)))`
- `ck_mfa_secret` (check): `CHECK ((((factor_type = 'totp'::text) AND (secret_ciphertext IS NOT NULL) AND (key_ref IS NOT NULL)) OR ((factor_type = 'webauthn'::text) AND (webauthn_credential_id IS NOT NULL) AND (webauthn_public_key IS NOT NULL)) OR (factor_type = 'sms_otp'::text)))`
- `mfa_factor_factor_type_check` (check): `CHECK ((factor_type = ANY (ARRAY['totp'::text, 'sms_otp'::text, 'webauthn'::text])))`
- `mfa_factor_status_check` (check): `CHECK ((status = ANY (ARRAY['pending'::text, 'active'::text, 'revoked'::text])))`

Foreign keys:

- `mfa_factor_user_id_fkey`: `FOREIGN KEY (user_id) REFERENCES iam.user_account(id)`

Indexes:

- `mfa_factor_pkey`: `btree (id)` (unique)
- `ux_mfa_user_label`: `btree (user_id, factor_type, label) WHERE (status <> 'revoked'::text)` (unique)

### `iam.mfa_recovery_code`

RLS: off (no direct app access or global)  
Triggers: —

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | uuid | NN | `gen_random_uuid()` | **PK** |
| `user_id` | uuid | NN |  | FK→ `iam.user_account` |
| `code_hash` | bytea | NN |  |  |
| `used_at` | timestamp with time zone | null |  |  |
| `created_at` | timestamp with time zone | NN | `now()` |  |

Constraints:

- `mfa_recovery_code_code_hash_check` (check): `CHECK ((length(code_hash) = 32))`
- `mfa_recovery_code_code_hash_key` (unique): `UNIQUE (code_hash)`

Foreign keys:

- `mfa_recovery_code_user_id_fkey`: `FOREIGN KEY (user_id) REFERENCES iam.user_account(id)`

Indexes:

- `ix_recovery_user`: `btree (user_id) WHERE (used_at IS NULL)`
- `mfa_recovery_code_code_hash_key`: `btree (code_hash)` (unique)
- `mfa_recovery_code_pkey`: `btree (id)` (unique)

### `iam.otp_challenge`

RLS: off (no direct app access or global)  
Triggers: —

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | uuid | NN | `gen_random_uuid()` | **PK** |
| `user_id` | uuid | null |  | FK→ `iam.user_account` |
| `phone_e164` | text | NN |  |  |
| `purpose` | text | NN |  |  |
| `code_hash` | bytea | NN |  |  |
| `attempts` | smallint | NN | `0` |  |
| `expires_at` | timestamp with time zone | NN |  |  |
| `consumed_at` | timestamp with time zone | null |  |  |
| `requested_ip` | inet | null |  |  |
| `created_at` | timestamp with time zone | NN | `now()` |  |

Constraints:

- `ck_otp_ttl` (check): `CHECK ((expires_at <= (created_at + '00:10:00'::interval)))`
- `otp_challenge_attempts_check` (check): `CHECK (((attempts >= 0) AND (attempts <= 5)))`
- `otp_challenge_code_hash_check` (check): `CHECK ((length(code_hash) = 32))`
- `otp_challenge_phone_e164_check` (check): `CHECK ((phone_e164 ~ '^\+[1-9][0-9]{7,14}$'::text))`
- `otp_challenge_purpose_check` (check): `CHECK ((purpose = ANY (ARRAY['portal_sign_in'::text, 'phone_verification'::text, 'mfa'::text, 'password_reset'::text])))`

Foreign keys:

- `otp_challenge_user_id_fkey`: `FOREIGN KEY (user_id) REFERENCES iam.user_account(id)`

Indexes:

- `ix_otp_phone_recent`: `btree (phone_e164, created_at DESC)`
- `otp_challenge_pkey`: `btree (id)` (unique)

### `iam.password_credential`

RLS: off (no direct app access or global)  
Triggers: —

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `user_id` | uuid | NN |  | **PK** FK→ `iam.user_account` |
| `password_hash` | text | NN |  |  |
| `changed_at` | timestamp with time zone | NN | `now()` |  |
| `expires_at` | timestamp with time zone | null |  |  |

Constraints:

- `password_credential_password_hash_check` (check): `CHECK ((password_hash ~~ '$argon2id$%'::text))`

Foreign keys:

- `password_credential_user_id_fkey`: `FOREIGN KEY (user_id) REFERENCES iam.user_account(id) ON DELETE RESTRICT`

Indexes:

- `password_credential_pkey`: `btree (user_id)` (unique)

### `iam.password_history`

RLS: off (no direct app access or global)  
Triggers: —

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | uuid | NN | `gen_random_uuid()` | **PK** |
| `user_id` | uuid | NN |  | FK→ `iam.user_account` |
| `password_hash` | text | NN |  |  |
| `created_at` | timestamp with time zone | NN | `now()` |  |

Constraints:

- `password_history_password_hash_check` (check): `CHECK ((password_hash ~~ '$argon2id$%'::text))`

Foreign keys:

- `password_history_user_id_fkey`: `FOREIGN KEY (user_id) REFERENCES iam.user_account(id)`

Indexes:

- `ix_pwhist_user`: `btree (user_id, created_at DESC)`
- `password_history_pkey`: `btree (id)` (unique)

### `iam.password_reset`

RLS: off (no direct app access or global)  
Triggers: —

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | uuid | NN | `gen_random_uuid()` | **PK** |
| `user_id` | uuid | NN |  | FK→ `iam.user_account` |
| `token_hash` | bytea | NN |  |  |
| `requested_at` | timestamp with time zone | NN | `now()` |  |
| `expires_at` | timestamp with time zone | NN |  |  |
| `consumed_at` | timestamp with time zone | null |  |  |
| `requested_ip` | inet | null |  |  |

Constraints:

- `ck_reset_ttl` (check): `CHECK ((expires_at <= (requested_at + '01:00:00'::interval)))`
- `password_reset_token_hash_check` (check): `CHECK ((length(token_hash) = 32))`
- `password_reset_token_hash_key` (unique): `UNIQUE (token_hash)`

Foreign keys:

- `password_reset_user_id_fkey`: `FOREIGN KEY (user_id) REFERENCES iam.user_account(id)`

Indexes:

- `ix_reset_user`: `btree (user_id, requested_at DESC)`
- `password_reset_pkey`: `btree (id)` (unique)
- `password_reset_token_hash_key`: `btree (token_hash)` (unique)

### `iam.refresh_token`

RLS: off (no direct app access or global)  
Triggers: —

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | uuid | NN | `gen_random_uuid()` | **PK** |
| `session_id` | uuid | NN |  | FK→ `iam.session` |
| `family_id` | uuid | NN |  |  |
| `token_hash` | bytea | NN |  |  |
| `issued_at` | timestamp with time zone | NN | `now()` |  |
| `expires_at` | timestamp with time zone | NN |  |  |
| `used_at` | timestamp with time zone | null |  |  |
| `replaced_by_id` | uuid | null |  | FK→ `iam.refresh_token` |
| `revoked_at` | timestamp with time zone | null |  |  |

Constraints:

- `ck_refresh_ttl` (check): `CHECK (((expires_at > issued_at) AND (expires_at <= (issued_at + '30 days'::interval))))`
- `refresh_token_token_hash_check` (check): `CHECK ((length(token_hash) = 32))`
- `refresh_token_token_hash_key` (unique): `UNIQUE (token_hash)`

Foreign keys:

- `refresh_token_replaced_by_id_fkey`: `FOREIGN KEY (replaced_by_id) REFERENCES iam.refresh_token(id)`
- `refresh_token_session_id_fkey`: `FOREIGN KEY (session_id) REFERENCES iam.session(id)`

Indexes:

- `ix_refresh_family`: `btree (family_id)`
- `refresh_token_pkey`: `btree (id)` (unique)
- `refresh_token_token_hash_key`: `btree (token_hash)` (unique)

### `iam.role`

RLS: enabled — policies: role_select, role_update, role_write  
Triggers: trg_role_hierarchy, trg_touch

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | uuid | NN | `gen_random_uuid()` | **PK** |
| `org_id` | uuid | null |  | FK→ `org.organization` |
| `code` | text | NN |  |  |
| `name` | text | NN |  |  |
| `description` | text | null |  |  |
| `parent_role_id` | uuid | null |  | FK→ `iam.role` |
| `is_system` | boolean | NN | `false` |  |
| `status` | text | NN | `'active'::text` |  |
| `created_at` | timestamp with time zone | NN | `now()` |  |
| `created_by` | uuid | null | `sec.current_user_id()` | FK→ `iam.user_account` |
| `updated_at` | timestamp with time zone | NN | `now()` |  |
| `updated_by` | uuid | null |  | FK→ `iam.user_account` |
| `row_version` | integer | NN | `1` |  |

Constraints:

- `ck_role_not_own_parent` (check): `CHECK ((parent_role_id IS DISTINCT FROM id))`
- `ck_system_role_global` (check): `CHECK (((NOT is_system) OR (org_id IS NULL)))`
- `role_code_check` (check): `CHECK ((code ~ '^[a-z_]{3,40}$'::text))`
- `role_status_check` (check): `CHECK ((status = ANY (ARRAY['active'::text, 'retired'::text])))`

Foreign keys:

- `role_created_by_fkey`: `FOREIGN KEY (created_by) REFERENCES iam.user_account(id)`
- `role_org_id_fkey`: `FOREIGN KEY (org_id) REFERENCES org.organization(id)`
- `role_parent_role_id_fkey`: `FOREIGN KEY (parent_role_id) REFERENCES iam.role(id)`
- `role_updated_by_fkey`: `FOREIGN KEY (updated_by) REFERENCES iam.user_account(id)`

Indexes:

- `role_pkey`: `btree (id)` (unique)
- `ux_role_code`: `btree (org_id, code) NULLS NOT DISTINCT` (unique)

### `iam.role_permission`

RLS: enabled — policies: rp_select, rp_write  
Triggers: —

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `role_id` | uuid | NN |  | **PK** FK→ `iam.role` |
| `permission_code` | text | NN |  | **PK** FK→ `ref.permission` |
| `granted_at` | timestamp with time zone | NN | `now()` |  |
| `granted_by` | uuid | null | `sec.current_user_id()` | FK→ `iam.user_account` |

Foreign keys:

- `role_permission_granted_by_fkey`: `FOREIGN KEY (granted_by) REFERENCES iam.user_account(id)`
- `role_permission_permission_code_fkey`: `FOREIGN KEY (permission_code) REFERENCES ref.permission(code)`
- `role_permission_role_id_fkey`: `FOREIGN KEY (role_id) REFERENCES iam.role(id) ON DELETE CASCADE`

Indexes:

- `role_permission_pkey`: `btree (role_id, permission_code)` (unique)

### `iam.session`

RLS: off (no direct app access or global)  
Triggers: —

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | uuid | NN | `gen_random_uuid()` | **PK** |
| `user_id` | uuid | NN |  | FK→ `iam.user_account` |
| `org_id` | uuid | null |  | FK→ `patient.patient` |
| `membership_id` | uuid | null |  | FK→ `iam.membership` |
| `patient_id` | uuid | null |  | FK→ `patient.patient` |
| `token_hash` | bytea | NN |  |  |
| `client_type` | text | NN |  |  |
| `auth_method` | text | NN |  |  |
| `ip_address` | inet | null |  |  |
| `user_agent` | text | null |  |  |
| `idle_timeout` | interval | NN |  |  |
| `created_at` | timestamp with time zone | NN | `now()` |  |
| `last_seen_at` | timestamp with time zone | NN | `now()` |  |
| `idle_expires_at` | timestamp with time zone | NN |  |  |
| `absolute_expires_at` | timestamp with time zone | NN |  |  |
| `mfa_verified_at` | timestamp with time zone | null |  |  |
| `revoked_at` | timestamp with time zone | null |  |  |
| `revoked_reason` | text | null |  |  |

Constraints:

- `ck_session_expiry` (check): `CHECK (((absolute_expires_at > created_at) AND (idle_expires_at <= absolute_expires_at)))`
- `ck_session_revoked` (check): `CHECK (((revoked_at IS NULL) = (revoked_reason IS NULL)))`
- `session_auth_method_check` (check): `CHECK ((auth_method = ANY (ARRAY['password'::text, 'password_mfa'::text, 'otp'::text, 'api_key'::text, 'sso'::text])))`
- `session_client_type_check` (check): `CHECK ((client_type = ANY (ARRAY['web'::text, 'mobile'::text, 'api'::text])))`
- `session_revoked_reason_check` (check): `CHECK ((revoked_reason = ANY (ARRAY['logout'::text, 'idle_timeout'::text, 'admin_revoked'::text, 'password_changed'::text, 'token_reuse_detected'::text, 'account_disabled'::text, 'superseded'::text])))`
- `session_token_hash_check` (check): `CHECK ((length(token_hash) = 32))`
- `session_token_hash_key` (unique): `UNIQUE (token_hash)`
- `session_user_agent_check` (check): `CHECK ((length(user_agent) <= 512))`

Foreign keys:

- `fk_session_patient`: `FOREIGN KEY (org_id, patient_id) REFERENCES patient.patient(org_id, id)`
- `session_org_id_fkey`: `FOREIGN KEY (org_id) REFERENCES org.organization(id)`
- `session_org_id_membership_id_fkey`: `FOREIGN KEY (org_id, membership_id) REFERENCES iam.membership(org_id, id)`
- `session_user_id_fkey`: `FOREIGN KEY (user_id) REFERENCES iam.user_account(id)`

Indexes:

- `ix_session_expiry`: `btree (absolute_expires_at) WHERE (revoked_at IS NULL)`
- `ix_session_user_active`: `btree (user_id) WHERE (revoked_at IS NULL)`
- `session_pkey`: `btree (id)` (unique)
- `session_token_hash_key`: `btree (token_hash)` (unique)

### `iam.user_account`

Global login identity (staff, patient portal, service). Never hard-deleted: deactivated.

RLS: off (no direct app access or global)  
Triggers: trg_touch

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | uuid | NN | `gen_random_uuid()` | **PK** |
| `account_type` | text | NN |  |  |
| `username` | citext | null |  |  |
| `email` | citext | null |  |  |
| `phone_e164` | text | null |  |  |
| `display_name` | text | NN |  |  |
| `status` | text | NN | `'pending_activation'::text` |  |
| `failed_login_count` | integer | NN | `0` |  |
| `locked_until` | timestamp with time zone | null |  |  |
| `last_login_at` | timestamp with time zone | null |  |  |
| `must_change_password` | boolean | NN | `false` |  |
| `mfa_required` | boolean | NN | `true` |  |
| `is_test_account` | boolean | NN | `false` |  |
| `created_at` | timestamp with time zone | NN | `now()` |  |
| `created_by` | uuid | null | `sec.current_user_id()` | FK→ `iam.user_account` |
| `updated_at` | timestamp with time zone | NN | `now()` |  |
| `updated_by` | uuid | null |  | FK→ `iam.user_account` |
| `row_version` | integer | NN | `1` |  |

Constraints:

- `ck_user_has_login_identifier` (check): `CHECK ((num_nonnulls(username, email, phone_e164) >= 1))`
- `user_account_account_type_check` (check): `CHECK ((account_type = ANY (ARRAY['staff'::text, 'patient'::text, 'service'::text])))`
- `user_account_display_name_check` (check): `CHECK (((length(btrim(display_name)) >= 1) AND (length(btrim(display_name)) <= 150)))`
- `user_account_email_check` (check): `CHECK ((email ~ '^[^@\s]+@[^@\s]+\.[^@\s]+$'::citext))`
- `user_account_failed_login_count_check` (check): `CHECK ((failed_login_count >= 0))`
- `user_account_phone_e164_check` (check): `CHECK ((phone_e164 ~ '^\+[1-9][0-9]{7,14}$'::text))`
- `user_account_status_check` (check): `CHECK ((status = ANY (ARRAY['pending_activation'::text, 'active'::text, 'suspended'::text, 'deactivated'::text])))`
- `user_account_username_check` (check): `CHECK ((username ~ '^[a-z0-9._-]{3,40}$'::citext))`

Foreign keys:

- `fk_user_created_by`: `FOREIGN KEY (created_by) REFERENCES iam.user_account(id)`
- `fk_user_updated_by`: `FOREIGN KEY (updated_by) REFERENCES iam.user_account(id)`

Indexes:

- `user_account_pkey`: `btree (id)` (unique)
- `ux_user_email`: `btree (account_type, email) WHERE (email IS NOT NULL)` (unique)
- `ux_user_phone`: `btree (account_type, phone_e164) WHERE (phone_e164 IS NOT NULL)` (unique)
- `ux_user_username`: `btree (username) WHERE (username IS NOT NULL)` (unique)

## Schema `patient`

### `patient.consent`

RLS: forced — policies: owner_internal, patient_consent_insert, patient_consent_update, patient_self, staff_insert, staff_select, staff_update, tenant_isolation(R)  
Triggers: trg_0_state, trg_1_patient_guard, trg_audit_capture, trg_touch

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | uuid | NN | `gen_random_uuid()` | **PK** |
| `org_id` | uuid | NN |  | FK→ `patient.patient` |
| `patient_id` | uuid | NN |  | FK→ `patient.patient` |
| `consent_type` | text | NN |  |  |
| `status` | text | NN | `'granted'::text` |  |
| `capture_method` | text | NN |  |  |
| `captured_by` | uuid | null | `sec.current_user_id()` | FK→ `iam.user_account` |
| `witness_membership_id` | uuid | null |  | FK→ `iam.membership` |
| `document_id` | uuid | null |  | FK→ `document.document` |
| `policy_version` | text | NN |  |  |
| `granted_at` | timestamp with time zone | NN | `now()` |  |
| `withdrawn_at` | timestamp with time zone | null |  |  |
| `withdrawal_reason` | text | null |  |  |
| `created_at` | timestamp with time zone | NN | `now()` |  |
| `created_by` | uuid | null | `sec.current_user_id()` | FK→ `iam.user_account` |
| `updated_at` | timestamp with time zone | NN | `now()` |  |
| `updated_by` | uuid | null |  | FK→ `iam.user_account` |
| `row_version` | integer | NN | `1` |  |

Constraints:

- `ck_consent_withdrawn` (check): `CHECK (((status = 'withdrawn'::text) = (withdrawn_at IS NOT NULL)))`
- `ck_consent_witness` (check): `CHECK (((capture_method <> 'verbal_witnessed'::text) OR (witness_membership_id IS NOT NULL)))`
- `consent_capture_method_check` (check): `CHECK ((capture_method = ANY (ARRAY['written'::text, 'verbal_witnessed'::text, 'electronic_portal'::text])))`
- `consent_consent_type_check` (check): `CHECK ((consent_type = ANY (ARRAY['treatment_general'::text, 'telemedicine'::text, 'sms_notifications'::text, 'whatsapp_notifications'::text, 'portal_access'::text, 'data_sharing_insurer'::text, 'research'::text])))`
- `consent_org_id_id_key` (unique): `UNIQUE (org_id, id)`
- `consent_status_check` (check): `CHECK ((status = ANY (ARRAY['granted'::text, 'withdrawn'::text])))`

Foreign keys:

- `consent_captured_by_fkey`: `FOREIGN KEY (captured_by) REFERENCES iam.user_account(id)`
- `consent_created_by_fkey`: `FOREIGN KEY (created_by) REFERENCES iam.user_account(id)`
- `consent_org_id_patient_id_fkey`: `FOREIGN KEY (org_id, patient_id) REFERENCES patient.patient(org_id, id)`
- `consent_org_id_witness_membership_id_fkey`: `FOREIGN KEY (org_id, witness_membership_id) REFERENCES iam.membership(org_id, id)`
- `consent_updated_by_fkey`: `FOREIGN KEY (updated_by) REFERENCES iam.user_account(id)`
- `fk_consent_document`: `FOREIGN KEY (org_id, document_id) REFERENCES document.document(org_id, id)`

Indexes:

- `consent_org_id_id_key`: `btree (org_id, id)` (unique)
- `consent_pkey`: `btree (id)` (unique)
- `ux_consent_active`: `btree (patient_id, consent_type) WHERE (status = 'granted'::text)` (unique)

### `patient.contact_point`

RLS: forced — policies: owner_internal, patient_self, staff_insert, staff_select, staff_update, tenant_isolation(R)  
Triggers: trg_0_state, trg_audit_capture, trg_touch

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | uuid | NN | `gen_random_uuid()` | **PK** |
| `org_id` | uuid | NN |  | FK→ `patient.patient` |
| `patient_id` | uuid | NN |  | FK→ `patient.patient` |
| `system` | text | NN |  |  |
| `value` | text | NN |  |  |
| `is_primary` | boolean | NN | `false` |  |
| `verified_at` | timestamp with time zone | null |  |  |
| `status` | text | NN | `'active'::text` |  |
| `created_at` | timestamp with time zone | NN | `now()` |  |
| `created_by` | uuid | null | `sec.current_user_id()` | FK→ `iam.user_account` |
| `updated_at` | timestamp with time zone | NN | `now()` |  |
| `updated_by` | uuid | null |  | FK→ `iam.user_account` |
| `row_version` | integer | NN | `1` |  |

Constraints:

- `ck_contact_format` (check): `CHECK ((((system = ANY (ARRAY['phone'::text, 'whatsapp'::text])) AND (value ~ '^\+[1-9][0-9]{7,14}$'::text)) OR ((system = 'email'::text) AND (value ~ '^[^@\s]+@[^@\s]+\.[^@\s]+$'::text))))`
- `contact_point_org_id_id_key` (unique): `UNIQUE (org_id, id)`
- `contact_point_status_check` (check): `CHECK ((status = ANY (ARRAY['active'::text, 'inactive'::text])))`
- `contact_point_system_check` (check): `CHECK ((system = ANY (ARRAY['phone'::text, 'whatsapp'::text, 'email'::text])))`

Foreign keys:

- `contact_point_created_by_fkey`: `FOREIGN KEY (created_by) REFERENCES iam.user_account(id)`
- `contact_point_org_id_patient_id_fkey`: `FOREIGN KEY (org_id, patient_id) REFERENCES patient.patient(org_id, id)`
- `contact_point_updated_by_fkey`: `FOREIGN KEY (updated_by) REFERENCES iam.user_account(id)`

Indexes:

- `contact_point_org_id_id_key`: `btree (org_id, id)` (unique)
- `contact_point_pkey`: `btree (id)` (unique)
- `ix_contact_value`: `btree (org_id, lower(value)) WHERE (status = 'active'::text)`
- `ux_contact_dupe`: `btree (patient_id, system, lower(value)) WHERE (status = 'active'::text)` (unique)
- `ux_contact_primary`: `btree (patient_id, system) WHERE (is_primary AND (status = 'active'::text))` (unique)

### `patient.duplicate_review`

RLS: forced — policies: owner_internal, staff_insert, staff_select, staff_update, tenant_isolation(R)  
Triggers: trg_0_state, trg_touch

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | uuid | NN | `gen_random_uuid()` | **PK** |
| `org_id` | uuid | NN |  | FK→ `patient.patient` |
| `patient_id_a` | uuid | NN |  | FK→ `patient.patient` |
| `patient_id_b` | uuid | NN |  | FK→ `patient.patient` |
| `match_score` | numeric(5,4) | NN |  |  |
| `match_reasons` | _text | NN | `'{}'::text[]` |  |
| `status` | text | NN | `'open'::text` |  |
| `resolved_by` | uuid | null |  | FK→ `iam.user_account` |
| `resolved_at` | timestamp with time zone | null |  |  |
| `resolution_note` | text | null |  |  |
| `created_at` | timestamp with time zone | NN | `now()` |  |
| `created_by` | uuid | null | `sec.current_user_id()` | FK→ `iam.user_account` |
| `updated_at` | timestamp with time zone | NN | `now()` |  |
| `updated_by` | uuid | null |  | FK→ `iam.user_account` |
| `row_version` | integer | NN | `1` |  |

Constraints:

- `ck_dup_ordered` (check): `CHECK ((patient_id_a < patient_id_b))`
- `ck_dup_resolved` (check): `CHECK (((status = 'open'::text) = (resolved_at IS NULL)))`
- `duplicate_review_match_score_check` (check): `CHECK (((match_score >= (0)::numeric) AND (match_score <= (1)::numeric)))`
- `duplicate_review_org_id_id_key` (unique): `UNIQUE (org_id, id)`
- `duplicate_review_patient_id_a_patient_id_b_key` (unique): `UNIQUE (patient_id_a, patient_id_b)`
- `duplicate_review_status_check` (check): `CHECK ((status = ANY (ARRAY['open'::text, 'confirmed_distinct'::text, 'confirmed_duplicate'::text, 'merged'::text])))`

Foreign keys:

- `duplicate_review_created_by_fkey`: `FOREIGN KEY (created_by) REFERENCES iam.user_account(id)`
- `duplicate_review_org_id_patient_id_a_fkey`: `FOREIGN KEY (org_id, patient_id_a) REFERENCES patient.patient(org_id, id)`
- `duplicate_review_org_id_patient_id_b_fkey`: `FOREIGN KEY (org_id, patient_id_b) REFERENCES patient.patient(org_id, id)`
- `duplicate_review_resolved_by_fkey`: `FOREIGN KEY (resolved_by) REFERENCES iam.user_account(id)`
- `duplicate_review_updated_by_fkey`: `FOREIGN KEY (updated_by) REFERENCES iam.user_account(id)`

Indexes:

- `duplicate_review_org_id_id_key`: `btree (org_id, id)` (unique)
- `duplicate_review_patient_id_a_patient_id_b_key`: `btree (patient_id_a, patient_id_b)` (unique)
- `duplicate_review_pkey`: `btree (id)` (unique)

### `patient.identifier`

RLS: forced — policies: owner_internal, staff_insert, staff_select, staff_update, tenant_isolation(R)  
Triggers: trg_0_state, trg_audit_capture, trg_touch

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | uuid | NN | `gen_random_uuid()` | **PK** |
| `org_id` | uuid | NN |  | FK→ `patient.patient` |
| `patient_id` | uuid | NN |  | FK→ `patient.patient` |
| `id_type` | text | NN |  |  |
| `value_ciphertext` | bytea | NN |  |  |
| `value_bidx` | bytea | NN |  |  |
| `value_last4` | text | null |  |  |
| `key_ref` | text | NN |  |  |
| `issuing_country` | character(2) | null | `'TZ'::bpchar` |  |
| `status` | text | NN | `'active'::text` |  |
| `created_at` | timestamp with time zone | NN | `now()` |  |
| `created_by` | uuid | null | `sec.current_user_id()` | FK→ `iam.user_account` |
| `updated_at` | timestamp with time zone | NN | `now()` |  |
| `updated_by` | uuid | null |  | FK→ `iam.user_account` |
| `row_version` | integer | NN | `1` |  |

Constraints:

- `identifier_id_type_check` (check): `CHECK ((id_type = ANY (ARRAY['nida_nin'::text, 'nhif_member'::text, 'passport'::text, 'driving_licence'::text, 'voter_id'::text, 'birth_certificate'::text, 'zanzibar_id'::text, 'legacy_powerhms'::text, 'other'::text])))`
- `identifier_org_id_id_key` (unique): `UNIQUE (org_id, id)`
- `identifier_status_check` (check): `CHECK ((status = ANY (ARRAY['active'::text, 'inactive'::text, 'entered_in_error'::text])))`
- `identifier_value_bidx_check` (check): `CHECK ((length(value_bidx) = 32))`
- `identifier_value_last4_check` (check): `CHECK ((value_last4 ~ '^[A-Za-z0-9]{1,4}$'::text))`

Foreign keys:

- `identifier_created_by_fkey`: `FOREIGN KEY (created_by) REFERENCES iam.user_account(id)`
- `identifier_org_id_patient_id_fkey`: `FOREIGN KEY (org_id, patient_id) REFERENCES patient.patient(org_id, id)`
- `identifier_updated_by_fkey`: `FOREIGN KEY (updated_by) REFERENCES iam.user_account(id)`

Indexes:

- `identifier_org_id_id_key`: `btree (org_id, id)` (unique)
- `identifier_pkey`: `btree (id)` (unique)
- `ix_identifier_patient`: `btree (patient_id)`
- `ux_identifier_value`: `btree (org_id, id_type, value_bidx) WHERE (status = 'active'::text)` (unique)

### `patient.merge_event`

RLS: forced — policies: owner_internal, staff_insert, staff_select, staff_update, tenant_isolation(R)  
Triggers: trg_merge_append_only

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | uuid | NN | `gen_random_uuid()` | **PK** |
| `org_id` | uuid | NN |  | FK→ `patient.duplicate_review` |
| `survivor_patient_id` | uuid | NN |  | FK→ `patient.patient` |
| `merged_patient_id` | uuid | NN |  | FK→ `patient.patient` |
| `duplicate_review_id` | uuid | null |  | FK→ `patient.duplicate_review` |
| `reason` | text | NN |  |  |
| `moved_record_counts` | jsonb | NN | `'{}'::jsonb` |  |
| `performed_by` | uuid | NN | `sec.current_user_id()` | FK→ `iam.user_account` |
| `performed_at` | timestamp with time zone | NN | `now()` |  |

Constraints:

- `ck_merge_distinct` (check): `CHECK ((survivor_patient_id <> merged_patient_id))`
- `merge_event_reason_check` (check): `CHECK ((length(reason) >= 10))`

Foreign keys:

- `merge_event_org_id_duplicate_review_id_fkey`: `FOREIGN KEY (org_id, duplicate_review_id) REFERENCES patient.duplicate_review(org_id, id)`
- `merge_event_org_id_merged_patient_id_fkey`: `FOREIGN KEY (org_id, merged_patient_id) REFERENCES patient.patient(org_id, id)`
- `merge_event_org_id_survivor_patient_id_fkey`: `FOREIGN KEY (org_id, survivor_patient_id) REFERENCES patient.patient(org_id, id)`
- `merge_event_performed_by_fkey`: `FOREIGN KEY (performed_by) REFERENCES iam.user_account(id)`

Indexes:

- `merge_event_pkey`: `btree (id)` (unique)

### `patient.next_of_kin`

RLS: forced — policies: owner_internal, patient_self, staff_insert, staff_select, staff_update, tenant_isolation(R)  
Triggers: trg_0_state, trg_audit_capture, trg_touch

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | uuid | NN | `gen_random_uuid()` | **PK** |
| `org_id` | uuid | NN |  | FK→ `patient.patient` |
| `patient_id` | uuid | NN |  | FK→ `patient.patient` |
| `full_name` | text | NN |  |  |
| `relationship` | text | NN |  |  |
| `phone_e164` | text | NN |  |  |
| `alt_phone_e164` | text | null |  |  |
| `address` | text | null |  |  |
| `is_emergency_contact` | boolean | NN | `true` |  |
| `priority` | smallint | NN | `1` |  |
| `status` | text | NN | `'active'::text` |  |
| `created_at` | timestamp with time zone | NN | `now()` |  |
| `created_by` | uuid | null | `sec.current_user_id()` | FK→ `iam.user_account` |
| `updated_at` | timestamp with time zone | NN | `now()` |  |
| `updated_by` | uuid | null |  | FK→ `iam.user_account` |
| `row_version` | integer | NN | `1` |  |

Constraints:

- `next_of_kin_alt_phone_e164_check` (check): `CHECK ((alt_phone_e164 ~ '^\+[1-9][0-9]{7,14}$'::text))`
- `next_of_kin_full_name_check` (check): `CHECK (((length(btrim(full_name)) >= 2) AND (length(btrim(full_name)) <= 150)))`
- `next_of_kin_org_id_id_key` (unique): `UNIQUE (org_id, id)`
- `next_of_kin_phone_e164_check` (check): `CHECK ((phone_e164 ~ '^\+[1-9][0-9]{7,14}$'::text))`
- `next_of_kin_priority_check` (check): `CHECK (((priority >= 1) AND (priority <= 5)))`
- `next_of_kin_relationship_check` (check): `CHECK ((relationship = ANY (ARRAY['spouse'::text, 'parent'::text, 'child'::text, 'sibling'::text, 'guardian'::text, 'relative'::text, 'friend'::text, 'employer'::text, 'other'::text])))`
- `next_of_kin_status_check` (check): `CHECK ((status = ANY (ARRAY['active'::text, 'inactive'::text])))`

Foreign keys:

- `next_of_kin_created_by_fkey`: `FOREIGN KEY (created_by) REFERENCES iam.user_account(id)`
- `next_of_kin_org_id_patient_id_fkey`: `FOREIGN KEY (org_id, patient_id) REFERENCES patient.patient(org_id, id)`
- `next_of_kin_updated_by_fkey`: `FOREIGN KEY (updated_by) REFERENCES iam.user_account(id)`

Indexes:

- `next_of_kin_org_id_id_key`: `btree (org_id, id)` (unique)
- `next_of_kin_pkey`: `btree (id)` (unique)
- `ux_nok_priority`: `btree (patient_id, priority) WHERE (status = 'active'::text)` (unique)

### `patient.patient`

RLS: forced — policies: owner_internal, patient_self, staff_insert, staff_select, staff_update, tenant_isolation(R)  
Triggers: trg_0_state, trg_audit_capture, trg_patient_validate, trg_touch

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | uuid | NN | `gen_random_uuid()` | **PK** |
| `org_id` | uuid | NN |  | FK→ `org.organization` |
| `mrn` | text | NN |  |  |
| `registered_facility_id` | uuid | NN |  | FK→ `org.facility` |
| `first_name` | text | NN |  |  |
| `middle_name` | text | null |  |  |
| `last_name` | text | NN |  |  |
| `preferred_name` | text | null |  |  |
| `sex` | text | NN |  |  |
| `date_of_birth` | date | NN |  |  |
| `dob_is_estimated` | boolean | NN | `false` |  |
| `marital_status` | text | null |  |  |
| `occupation` | text | null |  |  |
| `nationality` | character(2) | NN | `'TZ'::bpchar` |  |
| `preferred_language` | text | NN | `'sw'::text` |  |
| `region` | text | null |  |  |
| `district` | text | null |  |  |
| `ward` | text | null |  |  |
| `street_address` | text | null |  |  |
| `blood_group` | text | null |  |  |
| `status` | text | NN | `'active'::text` |  |
| `deceased_at` | timestamp with time zone | null |  |  |
| `merged_into_patient_id` | uuid | null |  | FK→ `patient.patient` |
| `is_test_data` | boolean | NN | `false` |  |
| `created_at` | timestamp with time zone | NN | `now()` |  |
| `created_by` | uuid | null | `sec.current_user_id()` | FK→ `iam.user_account` |
| `updated_at` | timestamp with time zone | NN | `now()` |  |
| `updated_by` | uuid | null |  | FK→ `iam.user_account` |
| `row_version` | integer | NN | `1` |  |

Constraints:

- `ck_patient_deceased` (check): `CHECK (((status <> 'deceased'::text) OR (deceased_at IS NOT NULL)))`
- `ck_patient_merged` (check): `CHECK (((status = 'merged'::text) = (merged_into_patient_id IS NOT NULL)))`
- `ck_patient_not_self_merge` (check): `CHECK ((merged_into_patient_id IS DISTINCT FROM id))`
- `patient_blood_group_check` (check): `CHECK ((blood_group = ANY (ARRAY['A+'::text, 'A-'::text, 'B+'::text, 'B-'::text, 'AB+'::text, 'AB-'::text, 'O+'::text, 'O-'::text, 'unknown'::text])))`
- `patient_date_of_birth_check` (check): `CHECK ((date_of_birth >= '1900-01-01'::date))`
- `patient_first_name_check` (check): `CHECK (((length(btrim(first_name)) >= 1) AND (length(btrim(first_name)) <= 100)))`
- `patient_last_name_check` (check): `CHECK (((length(btrim(last_name)) >= 1) AND (length(btrim(last_name)) <= 100)))`
- `patient_marital_status_check` (check): `CHECK ((marital_status = ANY (ARRAY['single'::text, 'married'::text, 'divorced'::text, 'widowed'::text, 'separated'::text, 'unknown'::text])))`
- `patient_middle_name_check` (check): `CHECK (((length(btrim(middle_name)) >= 1) AND (length(btrim(middle_name)) <= 100)))`
- `patient_mrn_check` (check): `CHECK ((mrn ~ '^[A-Z]{1,6}-[0-9]{4}-[0-9]{6}$'::text))`
- `patient_nationality_check` (check): `CHECK ((nationality ~ '^[A-Z]{2}$'::text))`
- `patient_org_id_id_key` (unique): `UNIQUE (org_id, id)`
- `patient_org_id_mrn_key` (unique): `UNIQUE (org_id, mrn)`
- `patient_preferred_language_check` (check): `CHECK ((preferred_language = ANY (ARRAY['sw'::text, 'en'::text])))`
- `patient_sex_check` (check): `CHECK ((sex = ANY (ARRAY['female'::text, 'male'::text, 'intersex'::text, 'unknown'::text])))`
- `patient_status_check` (check): `CHECK ((status = ANY (ARRAY['active'::text, 'inactive'::text, 'deceased'::text, 'merged'::text])))`

Foreign keys:

- `patient_created_by_fkey`: `FOREIGN KEY (created_by) REFERENCES iam.user_account(id)`
- `patient_org_id_fkey`: `FOREIGN KEY (org_id) REFERENCES org.organization(id)`
- `patient_org_id_merged_into_patient_id_fkey`: `FOREIGN KEY (org_id, merged_into_patient_id) REFERENCES patient.patient(org_id, id)`
- `patient_org_id_registered_facility_id_fkey`: `FOREIGN KEY (org_id, registered_facility_id) REFERENCES org.facility(org_id, id)`
- `patient_updated_by_fkey`: `FOREIGN KEY (updated_by) REFERENCES iam.user_account(id)`

Indexes:

- `ix_patient_dob`: `btree (org_id, date_of_birth)`
- `ix_patient_last_name`: `btree (org_id, lower(last_name), lower(first_name))`
- `ix_patient_name_trgm`: `gin (patient.name_key(first_name, middle_name, last_name) gin_trgm_ops)`
- `patient_org_id_id_key`: `btree (org_id, id)` (unique)
- `patient_org_id_mrn_key`: `btree (org_id, mrn)` (unique)
- `patient_pkey`: `btree (id)` (unique)

### `patient.portal_account`

RLS: enabled — policies: owner_internal, patient_self, staff_insert, staff_select, staff_update, tenant_isolation(R)  
Triggers: trg_0_state, trg_audit_capture, trg_touch

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | uuid | NN | `gen_random_uuid()` | **PK** |
| `org_id` | uuid | NN |  | FK→ `patient.patient` |
| `patient_id` | uuid | NN |  | FK→ `patient.patient` |
| `user_id` | uuid | NN |  | FK→ `iam.user_account` |
| `access_type` | text | NN | `'self'::text` |  |
| `status` | text | NN | `'pending_verification'::text` |  |
| `verification_method` | text | null |  |  |
| `verified_by` | uuid | null |  | FK→ `iam.user_account` |
| `verified_at` | timestamp with time zone | null |  |  |
| `revoked_reason` | text | null |  |  |
| `created_at` | timestamp with time zone | NN | `now()` |  |
| `created_by` | uuid | null | `sec.current_user_id()` | FK→ `iam.user_account` |
| `updated_at` | timestamp with time zone | NN | `now()` |  |
| `updated_by` | uuid | null |  | FK→ `iam.user_account` |
| `row_version` | integer | NN | `1` |  |

Constraints:

- `ck_guardian_in_person` (check): `CHECK (((access_type <> 'guardian'::text) OR (status <> 'active'::text) OR (verification_method = ANY (ARRAY['in_person_id_check'::text, 'staff_assisted'::text]))))`
- `ck_portal_verified` (check): `CHECK (((status <> 'active'::text) OR ((verified_at IS NOT NULL) AND (verification_method IS NOT NULL))))`
- `portal_account_access_type_check` (check): `CHECK ((access_type = ANY (ARRAY['self'::text, 'guardian'::text])))`
- `portal_account_org_id_id_key` (unique): `UNIQUE (org_id, id)`
- `portal_account_patient_id_user_id_key` (unique): `UNIQUE (patient_id, user_id)`
- `portal_account_status_check` (check): `CHECK ((status = ANY (ARRAY['pending_verification'::text, 'active'::text, 'suspended'::text, 'revoked'::text])))`
- `portal_account_verification_method_check` (check): `CHECK ((verification_method = ANY (ARRAY['in_person_id_check'::text, 'otp_registered_phone'::text, 'staff_assisted'::text])))`

Foreign keys:

- `portal_account_created_by_fkey`: `FOREIGN KEY (created_by) REFERENCES iam.user_account(id)`
- `portal_account_org_id_patient_id_fkey`: `FOREIGN KEY (org_id, patient_id) REFERENCES patient.patient(org_id, id)`
- `portal_account_updated_by_fkey`: `FOREIGN KEY (updated_by) REFERENCES iam.user_account(id)`
- `portal_account_user_id_fkey`: `FOREIGN KEY (user_id) REFERENCES iam.user_account(id)`
- `portal_account_verified_by_fkey`: `FOREIGN KEY (verified_by) REFERENCES iam.user_account(id)`

Indexes:

- `portal_account_org_id_id_key`: `btree (org_id, id)` (unique)
- `portal_account_patient_id_user_id_key`: `btree (patient_id, user_id)` (unique)
- `portal_account_pkey`: `btree (id)` (unique)
- `ux_portal_self`: `btree (user_id) WHERE ((access_type = 'self'::text) AND (status = ANY (ARRAY['pending_verification'::text, 'active'::text])))` (unique)

### `patient.verification`

Append-only history of identity/details confirmations (who, when, how, what).

RLS: forced — policies: owner_internal, staff_insert, staff_select, staff_update, tenant_isolation(R)  
Triggers: trg_audit_capture, trg_verification_append_only, trg_verification_rules

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | uuid | NN | `gen_random_uuid()` | **PK** |
| `org_id` | uuid | NN |  | FK→ `clinical.encounter` |
| `patient_id` | uuid | NN |  | FK→ `clinical.encounter` |
| `encounter_id` | uuid | null |  | FK→ `clinical.encounter` |
| `identifier_id` | uuid | null |  | FK→ `patient.identifier` |
| `scope` | _text | NN |  |  |
| `method` | text | NN |  |  |
| `outcome` | text | NN |  |  |
| `note` | text | null |  |  |
| `verified_by` | uuid | NN | `sec.current_user_id()` | FK→ `iam.user_account` |
| `verified_at` | timestamp with time zone | NN | `now()` |  |

Constraints:

- `ck_verif_discrepancy_note` (check): `CHECK (((outcome <> 'discrepancy'::text) OR (note IS NOT NULL)))`
- `ck_verif_document` (check): `CHECK (((method <> ALL (ARRAY['id_document_seen'::text, 'nida_lookup'::text])) OR (identifier_id IS NOT NULL)))`
- `verification_method_check` (check): `CHECK ((method = ANY (ARRAY['id_document_seen'::text, 'nida_lookup'::text, 'insurance_card_seen'::text, 'otp_registered_phone'::text, 'patient_stated'::text, 'next_of_kin_confirmed'::text])))`
- `verification_note_check` (check): `CHECK ((length(note) <= 1000))`
- `verification_org_id_id_key` (unique): `UNIQUE (org_id, id)`
- `verification_outcome_check` (check): `CHECK ((outcome = ANY (ARRAY['confirmed'::text, 'updated'::text, 'discrepancy'::text])))`
- `verification_scope_check` (check): `CHECK (((cardinality(scope) >= 1) AND (scope <@ ARRAY['identity'::text, 'contacts'::text, 'next_of_kin'::text, 'address'::text, 'insurance'::text, 'portal_link'::text])))`

Foreign keys:

- `verification_org_id_encounter_id_patient_id_fkey`: `FOREIGN KEY (org_id, encounter_id, patient_id) REFERENCES clinical.encounter(org_id, id, patient_id)`
- `verification_org_id_identifier_id_fkey`: `FOREIGN KEY (org_id, identifier_id) REFERENCES patient.identifier(org_id, id)`
- `verification_org_id_patient_id_fkey`: `FOREIGN KEY (org_id, patient_id) REFERENCES patient.patient(org_id, id)`
- `verification_verified_by_fkey`: `FOREIGN KEY (verified_by) REFERENCES iam.user_account(id)`

Indexes:

- `ix_verification_patient`: `btree (patient_id, verified_at DESC)`
- `verification_org_id_id_key`: `btree (org_id, id)` (unique)
- `verification_pkey`: `btree (id)` (unique)

## Schema `clinical`

### `clinical.allergy`

RLS: forced — policies: owner_internal, staff_insert, staff_select, staff_update, tenant_isolation(R)  
Triggers: trg_0_state, trg_audit_capture, trg_touch

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | uuid | NN | `gen_random_uuid()` | **PK** |
| `org_id` | uuid | NN |  | FK→ `patient.patient` |
| `patient_id` | uuid | NN |  | FK→ `patient.patient` |
| `substance` | text | NN |  |  |
| `category` | text | NN |  |  |
| `reaction` | text | null |  |  |
| `severity` | text | NN | `'unknown'::text` |  |
| `verification` | text | NN | `'unconfirmed'::text` |  |
| `status` | text | NN | `'active'::text` |  |
| `onset_date` | date | null |  |  |
| `error_reason` | text | null |  |  |
| `created_at` | timestamp with time zone | NN | `now()` |  |
| `created_by` | uuid | null | `sec.current_user_id()` | FK→ `iam.user_account` |
| `updated_at` | timestamp with time zone | NN | `now()` |  |
| `updated_by` | uuid | null |  | FK→ `iam.user_account` |
| `row_version` | integer | NN | `1` |  |

Constraints:

- `allergy_category_check` (check): `CHECK ((category = ANY (ARRAY['drug'::text, 'food'::text, 'environment'::text, 'other'::text])))`
- `allergy_org_id_id_key` (unique): `UNIQUE (org_id, id)`
- `allergy_severity_check` (check): `CHECK ((severity = ANY (ARRAY['mild'::text, 'moderate'::text, 'severe'::text, 'life_threatening'::text, 'unknown'::text])))`
- `allergy_status_check` (check): `CHECK ((status = ANY (ARRAY['active'::text, 'inactive'::text, 'resolved'::text, 'entered_in_error'::text])))`
- `allergy_substance_check` (check): `CHECK (((length(btrim(substance)) >= 2) AND (length(btrim(substance)) <= 200)))`
- `allergy_verification_check` (check): `CHECK ((verification = ANY (ARRAY['unconfirmed'::text, 'confirmed'::text, 'refuted'::text])))`
- `ck_allergy_error` (check): `CHECK (((status <> 'entered_in_error'::text) OR (error_reason IS NOT NULL)))`

Foreign keys:

- `allergy_created_by_fkey`: `FOREIGN KEY (created_by) REFERENCES iam.user_account(id)`
- `allergy_org_id_patient_id_fkey`: `FOREIGN KEY (org_id, patient_id) REFERENCES patient.patient(org_id, id)`
- `allergy_updated_by_fkey`: `FOREIGN KEY (updated_by) REFERENCES iam.user_account(id)`

Indexes:

- `allergy_org_id_id_key`: `btree (org_id, id)` (unique)
- `allergy_pkey`: `btree (id)` (unique)
- `ix_allergy_patient`: `btree (patient_id) WHERE (status = 'active'::text)`
- `ux_allergy_active`: `btree (patient_id, lower(substance)) WHERE (status = 'active'::text)` (unique)

### `clinical.appointment`

RLS: forced — policies: owner_internal, patient_cancel, patient_request, patient_self, staff_insert, staff_select, staff_update, tenant_isolation(R)  
Triggers: trg_0_state, trg_1_patient_guard, trg_audit_capture, trg_touch

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | uuid | NN | `gen_random_uuid()` | **PK** |
| `org_id` | uuid | NN |  | FK→ `org.department` |
| `facility_id` | uuid | NN |  | FK→ `org.facility` |
| `department_id` | uuid | NN |  | FK→ `org.department` |
| `patient_id` | uuid | NN |  | FK→ `patient.patient` |
| `practitioner_membership_id` | uuid | null |  | FK→ `iam.membership` |
| `appointment_type` | text | NN | `'in_person'::text` |  |
| `source` | text | NN |  |  |
| `scheduled_start` | timestamp with time zone | NN |  |  |
| `scheduled_end` | timestamp with time zone | NN |  |  |
| `reason` | text | null |  |  |
| `status` | text | NN |  |  |
| `cancel_reason` | text | null |  |  |
| `cancelled_by` | uuid | null |  | FK→ `iam.user_account` |
| `cancelled_at` | timestamp with time zone | null |  |  |
| `checked_in_at` | timestamp with time zone | null |  |  |
| `idempotency_key` | uuid | null |  |  |
| `created_at` | timestamp with time zone | NN | `now()` |  |
| `created_by` | uuid | null | `sec.current_user_id()` | FK→ `iam.user_account` |
| `updated_at` | timestamp with time zone | NN | `now()` |  |
| `updated_by` | uuid | null |  | FK→ `iam.user_account` |
| `row_version` | integer | NN | `1` |  |

Constraints:

- `appointment_appointment_type_check` (check): `CHECK ((appointment_type = ANY (ARRAY['in_person'::text, 'telemedicine'::text])))`
- `appointment_org_id_id_key` (unique): `UNIQUE (org_id, id)`
- `appointment_org_id_id_patient_id_key` (unique): `UNIQUE (org_id, id, patient_id)`
- `appointment_org_id_idempotency_key_key` (unique): `UNIQUE (org_id, idempotency_key)`
- `appointment_reason_check` (check): `CHECK ((length(reason) <= 500))`
- `appointment_source_check` (check): `CHECK ((source = ANY (ARRAY['reception'::text, 'patient_app'::text, 'phone'::text, 'follow_up'::text])))`
- `appointment_status_check` (check): `CHECK ((status = ANY (ARRAY['requested'::text, 'booked'::text, 'confirmed'::text, 'checked_in'::text, 'completed'::text, 'cancelled'::text, 'no_show'::text, 'rejected'::text])))`
- `ck_appt_cancel` (check): `CHECK (((status = ANY (ARRAY['cancelled'::text, 'rejected'::text])) = (cancelled_at IS NOT NULL)))`
- `ck_appt_cancel_reason` (check): `CHECK (((status <> ALL (ARRAY['cancelled'::text, 'rejected'::text])) OR (cancel_reason IS NOT NULL)))`
- `ck_appt_checkin` (check): `CHECK (((status <> ALL (ARRAY['checked_in'::text, 'completed'::text])) OR (checked_in_at IS NOT NULL)))`
- `ck_appt_window` (check): `CHECK (((scheduled_end > scheduled_start) AND (scheduled_end <= (scheduled_start + '08:00:00'::interval))))`
- `ex_appt_patient` (exclusion): `EXCLUDE USING gist (patient_id WITH =, tstzrange(scheduled_start, scheduled_end) WITH &&) WHERE ((status = ANY (ARRAY['booked'::text, 'confirmed'::text, 'checked_in'::text])))`
- `ex_appt_practitioner` (exclusion): `EXCLUDE USING gist (practitioner_membership_id WITH =, tstzrange(scheduled_start, scheduled_end) WITH &&) WHERE (((practitioner_membership_id IS NOT NULL) AND (status = ANY (ARRAY['booked'::text, 'confirmed'::text, 'checked_in'::text]))))`

Foreign keys:

- `appointment_cancelled_by_fkey`: `FOREIGN KEY (cancelled_by) REFERENCES iam.user_account(id)`
- `appointment_created_by_fkey`: `FOREIGN KEY (created_by) REFERENCES iam.user_account(id)`
- `appointment_org_id_department_id_fkey`: `FOREIGN KEY (org_id, department_id) REFERENCES org.department(org_id, id)`
- `appointment_org_id_facility_id_fkey`: `FOREIGN KEY (org_id, facility_id) REFERENCES org.facility(org_id, id)`
- `appointment_org_id_patient_id_fkey`: `FOREIGN KEY (org_id, patient_id) REFERENCES patient.patient(org_id, id)`
- `appointment_org_id_practitioner_membership_id_fkey`: `FOREIGN KEY (org_id, practitioner_membership_id) REFERENCES iam.membership(org_id, id)`
- `appointment_updated_by_fkey`: `FOREIGN KEY (updated_by) REFERENCES iam.user_account(id)`

Indexes:

- `appointment_org_id_id_key`: `btree (org_id, id)` (unique)
- `appointment_org_id_id_patient_id_key`: `btree (org_id, id, patient_id)` (unique)
- `appointment_org_id_idempotency_key_key`: `btree (org_id, idempotency_key)` (unique)
- `appointment_pkey`: `btree (id)` (unique)
- `ex_appt_patient`: `gist (patient_id, tstzrange(scheduled_start, scheduled_end)) WHERE (status = ANY (ARRAY['booked'::text, 'confirmed'::text, 'checked_in'::text]))`
- `ex_appt_practitioner`: `gist (practitioner_membership_id, tstzrange(scheduled_start, scheduled_end)) WHERE ((practitioner_membership_id IS NOT NULL) AND (status = ANY (ARRAY['booked'::text, 'confirmed'::text, 'checked_in'::text])))`
- `ix_appt_facility_day`: `btree (org_id, facility_id, scheduled_start) WHERE (status = ANY (ARRAY['requested'::text, 'booked'::text, 'confirmed'::text, 'checked_in'::text]))`
- `ix_appt_patient`: `btree (patient_id, scheduled_start DESC)`
- `ix_appt_practitioner`: `btree (practitioner_membership_id, scheduled_start) WHERE (practitioner_membership_id IS NOT NULL)`

### `clinical.consultation`

RLS: forced — policies: owner_internal, staff_insert, staff_select, staff_update, tenant_isolation(R)  
Triggers: trg_0_state, trg_audit_capture, trg_consult_protect, trg_consult_rules, trg_touch

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | uuid | NN | `gen_random_uuid()` | **PK** |
| `org_id` | uuid | NN |  | FK→ `clinical.encounter` |
| `encounter_id` | uuid | NN |  | FK→ `clinical.encounter` |
| `patient_id` | uuid | NN |  | FK→ `clinical.encounter` |
| `practitioner_membership_id` | uuid | NN |  | FK→ `iam.membership` |
| `presenting_complaint` | text | null |  |  |
| `history_present_illness` | text | null |  |  |
| `review_of_systems` | text | null |  |  |
| `examination_findings` | text | null |  |  |
| `assessment` | text | null |  |  |
| `treatment_plan` | text | null |  |  |
| `follow_up_instructions` | text | null |  |  |
| `status` | text | NN | `'draft'::text` |  |
| `signed_at` | timestamp with time zone | null |  |  |
| `supersedes_id` | uuid | null |  | FK→ `clinical.consultation` |
| `amendment_reason` | text | null |  |  |
| `error_reason` | text | null |  |  |
| `created_at` | timestamp with time zone | NN | `now()` |  |
| `created_by` | uuid | null | `sec.current_user_id()` | FK→ `iam.user_account` |
| `updated_at` | timestamp with time zone | NN | `now()` |  |
| `updated_by` | uuid | null |  | FK→ `iam.user_account` |
| `row_version` | integer | NN | `1` |  |

Constraints:

- `ck_consult_amend_reason` (check): `CHECK (((supersedes_id IS NULL) OR (amendment_reason IS NOT NULL)))`
- `ck_consult_error` (check): `CHECK (((status <> 'entered_in_error'::text) OR (error_reason IS NOT NULL)))`
- `ck_consult_signed` (check): `CHECK (((status = 'draft'::text) OR ((signed_at IS NOT NULL) AND (presenting_complaint IS NOT NULL) AND (examination_findings IS NOT NULL) AND (assessment IS NOT NULL) AND (treatment_plan IS NOT NULL))))`
- `consultation_org_id_id_key` (unique): `UNIQUE (org_id, id)`
- `consultation_org_id_id_patient_id_key` (unique): `UNIQUE (org_id, id, patient_id)`
- `consultation_status_check` (check): `CHECK ((status = ANY (ARRAY['draft'::text, 'signed'::text, 'amended'::text, 'entered_in_error'::text])))`
- `consultation_supersedes_id_key` (unique): `UNIQUE (supersedes_id)`

Foreign keys:

- `consultation_created_by_fkey`: `FOREIGN KEY (created_by) REFERENCES iam.user_account(id)`
- `consultation_org_id_encounter_id_patient_id_fkey`: `FOREIGN KEY (org_id, encounter_id, patient_id) REFERENCES clinical.encounter(org_id, id, patient_id)`
- `consultation_org_id_practitioner_membership_id_fkey`: `FOREIGN KEY (org_id, practitioner_membership_id) REFERENCES iam.membership(org_id, id)`
- `consultation_supersedes_id_fkey`: `FOREIGN KEY (supersedes_id) REFERENCES clinical.consultation(id)`
- `consultation_updated_by_fkey`: `FOREIGN KEY (updated_by) REFERENCES iam.user_account(id)`

Indexes:

- `consultation_org_id_id_key`: `btree (org_id, id)` (unique)
- `consultation_org_id_id_patient_id_key`: `btree (org_id, id, patient_id)` (unique)
- `consultation_pkey`: `btree (id)` (unique)
- `consultation_supersedes_id_key`: `btree (supersedes_id)` (unique)
- `ix_consult_encounter`: `btree (encounter_id)`
- `ix_consult_patient`: `btree (patient_id, created_at DESC)`
- `ux_consult_draft`: `btree (encounter_id, practitioner_membership_id) WHERE (status = 'draft'::text)` (unique)
- `ux_consult_signed`: `btree (encounter_id, practitioner_membership_id) WHERE (status = 'signed'::text)` (unique)

### `clinical.encounter`

RLS: forced — policies: owner_internal, patient_self, staff_insert, staff_select, staff_update, tenant_isolation(R)  
Triggers: trg_0_state, trg_audit_capture, trg_encounter_validate, trg_touch

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | uuid | NN | `gen_random_uuid()` | **PK** |
| `org_id` | uuid | NN |  | FK→ `clinical.appointment` |
| `facility_id` | uuid | NN |  | FK→ `org.facility` |
| `patient_id` | uuid | NN |  | FK→ `clinical.appointment` |
| `appointment_id` | uuid | null |  | FK→ `clinical.appointment` |
| `encounter_no` | text | NN |  |  |
| `encounter_type` | text | NN |  |  |
| `status` | text | NN | `'registered'::text` |  |
| `arrived_at` | timestamp with time zone | NN | `now()` |  |
| `started_at` | timestamp with time zone | null |  |  |
| `closed_at` | timestamp with time zone | null |  |  |
| `closed_by` | uuid | null |  | FK→ `iam.user_account` |
| `attending_membership_id` | uuid | null |  | FK→ `iam.membership` |
| `chief_complaint` | text | null |  |  |
| `payer_id` | uuid | null |  | FK→ `billing.payer` |
| `cancel_reason` | text | null |  |  |
| `created_at` | timestamp with time zone | NN | `now()` |  |
| `created_by` | uuid | null | `sec.current_user_id()` | FK→ `iam.user_account` |
| `updated_at` | timestamp with time zone | NN | `now()` |  |
| `updated_by` | uuid | null |  | FK→ `iam.user_account` |
| `row_version` | integer | NN | `1` |  |

Constraints:

- `ck_enc_cancel_reason` (check): `CHECK (((status <> ALL (ARRAY['cancelled'::text, 'entered_in_error'::text])) OR (cancel_reason IS NOT NULL)))`
- `ck_enc_closed` (check): `CHECK (((status = ANY (ARRAY['completed'::text, 'cancelled'::text, 'entered_in_error'::text])) = (closed_at IS NOT NULL)))`
- `ck_enc_times` (check): `CHECK (((closed_at IS NULL) OR (closed_at >= arrived_at)))`
- `encounter_appointment_id_key` (unique): `UNIQUE (appointment_id)`
- `encounter_chief_complaint_check` (check): `CHECK ((length(chief_complaint) <= 500))`
- `encounter_encounter_no_check` (check): `CHECK ((encounter_no ~ '^V-[0-9]{4}-[0-9]{6}$'::text))`
- `encounter_encounter_type_check` (check): `CHECK ((encounter_type = ANY (ARRAY['outpatient'::text, 'emergency'::text, 'telemedicine'::text, 'follow_up'::text])))`
- `encounter_org_id_encounter_no_key` (unique): `UNIQUE (org_id, encounter_no)`
- `encounter_org_id_id_key` (unique): `UNIQUE (org_id, id)`
- `encounter_org_id_id_patient_id_key` (unique): `UNIQUE (org_id, id, patient_id)`
- `encounter_status_check` (check): `CHECK ((status = ANY (ARRAY['registered'::text, 'in_progress'::text, 'completed'::text, 'cancelled'::text, 'entered_in_error'::text])))`

Foreign keys:

- `encounter_closed_by_fkey`: `FOREIGN KEY (closed_by) REFERENCES iam.user_account(id)`
- `encounter_created_by_fkey`: `FOREIGN KEY (created_by) REFERENCES iam.user_account(id)`
- `encounter_org_id_appointment_id_patient_id_fkey`: `FOREIGN KEY (org_id, appointment_id, patient_id) REFERENCES clinical.appointment(org_id, id, patient_id)`
- `encounter_org_id_attending_membership_id_fkey`: `FOREIGN KEY (org_id, attending_membership_id) REFERENCES iam.membership(org_id, id)`
- `encounter_org_id_facility_id_fkey`: `FOREIGN KEY (org_id, facility_id) REFERENCES org.facility(org_id, id)`
- `encounter_org_id_patient_id_fkey`: `FOREIGN KEY (org_id, patient_id) REFERENCES patient.patient(org_id, id)`
- `encounter_updated_by_fkey`: `FOREIGN KEY (updated_by) REFERENCES iam.user_account(id)`
- `fk_encounter_payer`: `FOREIGN KEY (org_id, payer_id) REFERENCES billing.payer(org_id, id)`

Indexes:

- `encounter_appointment_id_key`: `btree (appointment_id)` (unique)
- `encounter_org_id_encounter_no_key`: `btree (org_id, encounter_no)` (unique)
- `encounter_org_id_id_key`: `btree (org_id, id)` (unique)
- `encounter_org_id_id_patient_id_key`: `btree (org_id, id, patient_id)` (unique)
- `encounter_pkey`: `btree (id)` (unique)
- `ix_encounter_facility_day`: `btree (org_id, facility_id, arrived_at DESC)`
- `ix_encounter_patient`: `btree (patient_id, arrived_at DESC)`
- `ux_encounter_open`: `btree (patient_id, facility_id) WHERE (status = ANY (ARRAY['registered'::text, 'in_progress'::text]))` (unique)

### `clinical.encounter_diagnosis`

RLS: forced — policies: owner_internal, staff_insert, staff_select, staff_update, tenant_isolation(R)  
Triggers: trg_0_state, trg_audit_capture, trg_touch

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | uuid | NN | `gen_random_uuid()` | **PK** |
| `org_id` | uuid | NN |  | FK→ `clinical.consultation` |
| `encounter_id` | uuid | NN |  | FK→ `clinical.encounter` |
| `patient_id` | uuid | NN |  | FK→ `clinical.consultation` |
| `consultation_id` | uuid | null |  | FK→ `clinical.consultation` |
| `icd10_code` | text | null |  | FK→ `ref.icd10_code` |
| `description` | text | NN |  |  |
| `diagnosis_type` | text | NN |  |  |
| `is_primary` | boolean | NN | `false` |  |
| `status` | text | NN | `'active'::text` |  |
| `recorded_by_membership_id` | uuid | NN |  | FK→ `iam.membership` |
| `error_reason` | text | null |  |  |
| `created_at` | timestamp with time zone | NN | `now()` |  |
| `created_by` | uuid | null | `sec.current_user_id()` | FK→ `iam.user_account` |
| `updated_at` | timestamp with time zone | NN | `now()` |  |
| `updated_by` | uuid | null |  | FK→ `iam.user_account` |
| `row_version` | integer | NN | `1` |  |

Constraints:

- `ck_dx_error` (check): `CHECK (((status <> 'entered_in_error'::text) OR (error_reason IS NOT NULL)))`
- `ck_dx_final_coded` (check): `CHECK (((diagnosis_type <> 'final'::text) OR (icd10_code IS NOT NULL)))`
- `encounter_diagnosis_diagnosis_type_check` (check): `CHECK ((diagnosis_type = ANY (ARRAY['provisional'::text, 'final'::text, 'differential'::text])))`
- `encounter_diagnosis_org_id_id_key` (unique): `UNIQUE (org_id, id)`
- `encounter_diagnosis_status_check` (check): `CHECK ((status = ANY (ARRAY['active'::text, 'entered_in_error'::text])))`

Foreign keys:

- `encounter_diagnosis_created_by_fkey`: `FOREIGN KEY (created_by) REFERENCES iam.user_account(id)`
- `encounter_diagnosis_icd10_code_fkey`: `FOREIGN KEY (icd10_code) REFERENCES ref.icd10_code(code)`
- `encounter_diagnosis_org_id_consultation_id_patient_id_fkey`: `FOREIGN KEY (org_id, consultation_id, patient_id) REFERENCES clinical.consultation(org_id, id, patient_id)`
- `encounter_diagnosis_org_id_encounter_id_patient_id_fkey`: `FOREIGN KEY (org_id, encounter_id, patient_id) REFERENCES clinical.encounter(org_id, id, patient_id)`
- `encounter_diagnosis_org_id_recorded_by_membership_id_fkey`: `FOREIGN KEY (org_id, recorded_by_membership_id) REFERENCES iam.membership(org_id, id)`
- `encounter_diagnosis_updated_by_fkey`: `FOREIGN KEY (updated_by) REFERENCES iam.user_account(id)`

Indexes:

- `encounter_diagnosis_org_id_id_key`: `btree (org_id, id)` (unique)
- `encounter_diagnosis_pkey`: `btree (id)` (unique)
- `ix_dx_code_report`: `btree (org_id, icd10_code) WHERE (status = 'active'::text)`
- `ix_dx_patient`: `btree (patient_id)`
- `ux_dx_code`: `btree (encounter_id, icd10_code, diagnosis_type) WHERE ((status = 'active'::text) AND (icd10_code IS NOT NULL))` (unique)
- `ux_dx_primary`: `btree (encounter_id) WHERE (is_primary AND (status = 'active'::text))` (unique)

### `clinical.follow_up_plan`

RLS: forced — policies: owner_internal, staff_insert, staff_select, staff_update, tenant_isolation(R)  
Triggers: trg_0_state, trg_audit_capture, trg_touch

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | uuid | NN | `gen_random_uuid()` | **PK** |
| `org_id` | uuid | NN |  | FK→ `clinical.appointment` |
| `encounter_id` | uuid | NN |  | FK→ `clinical.encounter` |
| `patient_id` | uuid | NN |  | FK→ `clinical.appointment` |
| `department_id` | uuid | null |  | FK→ `org.department` |
| `practitioner_membership_id` | uuid | null |  | FK→ `iam.membership` |
| `due_date` | date | NN |  |  |
| `reason` | text | NN |  |  |
| `status` | text | NN | `'planned'::text` |  |
| `appointment_id` | uuid | null |  | FK→ `clinical.appointment` |
| `created_at` | timestamp with time zone | NN | `now()` |  |
| `created_by` | uuid | null | `sec.current_user_id()` | FK→ `iam.user_account` |
| `updated_at` | timestamp with time zone | NN | `now()` |  |
| `updated_by` | uuid | null |  | FK→ `iam.user_account` |
| `row_version` | integer | NN | `1` |  |

Constraints:

- `ck_followup_booked` (check): `CHECK (((status <> ALL (ARRAY['booked'::text, 'completed'::text])) OR (appointment_id IS NOT NULL)))`
- `follow_up_plan_org_id_id_key` (unique): `UNIQUE (org_id, id)`
- `follow_up_plan_status_check` (check): `CHECK ((status = ANY (ARRAY['planned'::text, 'booked'::text, 'completed'::text, 'cancelled'::text, 'missed'::text])))`

Foreign keys:

- `follow_up_plan_created_by_fkey`: `FOREIGN KEY (created_by) REFERENCES iam.user_account(id)`
- `follow_up_plan_org_id_appointment_id_patient_id_fkey`: `FOREIGN KEY (org_id, appointment_id, patient_id) REFERENCES clinical.appointment(org_id, id, patient_id)`
- `follow_up_plan_org_id_department_id_fkey`: `FOREIGN KEY (org_id, department_id) REFERENCES org.department(org_id, id)`
- `follow_up_plan_org_id_encounter_id_patient_id_fkey`: `FOREIGN KEY (org_id, encounter_id, patient_id) REFERENCES clinical.encounter(org_id, id, patient_id)`
- `follow_up_plan_org_id_practitioner_membership_id_fkey`: `FOREIGN KEY (org_id, practitioner_membership_id) REFERENCES iam.membership(org_id, id)`
- `follow_up_plan_updated_by_fkey`: `FOREIGN KEY (updated_by) REFERENCES iam.user_account(id)`

Indexes:

- `follow_up_plan_org_id_id_key`: `btree (org_id, id)` (unique)
- `follow_up_plan_pkey`: `btree (id)` (unique)
- `ix_followup_due`: `btree (org_id, due_date) WHERE (status = 'planned'::text)`

### `clinical.history_entry`

RLS: forced — policies: owner_internal, staff_insert, staff_select, staff_update, tenant_isolation(R)  
Triggers: trg_0_state, trg_audit_capture, trg_touch

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | uuid | NN | `gen_random_uuid()` | **PK** |
| `org_id` | uuid | NN |  | FK→ `patient.patient` |
| `patient_id` | uuid | NN |  | FK→ `patient.patient` |
| `history_type` | text | NN |  |  |
| `description` | text | NN |  |  |
| `occurred_on` | date | null |  |  |
| `status` | text | NN | `'active'::text` |  |
| `error_reason` | text | null |  |  |
| `created_at` | timestamp with time zone | NN | `now()` |  |
| `created_by` | uuid | null | `sec.current_user_id()` | FK→ `iam.user_account` |
| `updated_at` | timestamp with time zone | NN | `now()` |  |
| `updated_by` | uuid | null |  | FK→ `iam.user_account` |
| `row_version` | integer | NN | `1` |  |

Constraints:

- `history_entry_description_check` (check): `CHECK ((length(btrim(description)) >= 2))`
- `history_entry_history_type_check` (check): `CHECK ((history_type = ANY (ARRAY['past_medical'::text, 'surgical'::text, 'family'::text, 'social'::text, 'obstetric'::text, 'immunization'::text, 'other'::text])))`
- `history_entry_org_id_id_key` (unique): `UNIQUE (org_id, id)`
- `history_entry_status_check` (check): `CHECK ((status = ANY (ARRAY['active'::text, 'entered_in_error'::text])))`

Foreign keys:

- `history_entry_created_by_fkey`: `FOREIGN KEY (created_by) REFERENCES iam.user_account(id)`
- `history_entry_org_id_patient_id_fkey`: `FOREIGN KEY (org_id, patient_id) REFERENCES patient.patient(org_id, id)`
- `history_entry_updated_by_fkey`: `FOREIGN KEY (updated_by) REFERENCES iam.user_account(id)`

Indexes:

- `history_entry_org_id_id_key`: `btree (org_id, id)` (unique)
- `history_entry_pkey`: `btree (id)` (unique)
- `ix_history_patient`: `btree (patient_id, history_type)`

### `clinical.nursing_observation`

RLS: forced — policies: owner_internal, staff_insert, staff_select, staff_update, tenant_isolation(R)  
Triggers: trg_0_state, trg_audit_capture, trg_nobs_protect, trg_nobs_rules, trg_touch

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | uuid | NN | `gen_random_uuid()` | **PK** |
| `org_id` | uuid | NN |  | FK→ `clinical.encounter` |
| `encounter_id` | uuid | NN |  | FK→ `clinical.encounter` |
| `patient_id` | uuid | NN |  | FK→ `clinical.encounter` |
| `recorded_by_membership_id` | uuid | NN |  | FK→ `iam.membership` |
| `observed_at` | timestamp with time zone | NN | `now()` |  |
| `observation_type` | text | NN |  |  |
| `triage_assessment_id` | uuid | null |  | FK→ `clinical.triage_assessment` |
| `note` | text | NN |  |  |
| `status` | text | NN | `'final'::text` |  |
| `error_reason` | text | null |  |  |
| `created_at` | timestamp with time zone | NN | `now()` |  |
| `created_by` | uuid | null | `sec.current_user_id()` | FK→ `iam.user_account` |
| `updated_at` | timestamp with time zone | NN | `now()` |  |
| `updated_by` | uuid | null |  | FK→ `iam.user_account` |
| `row_version` | integer | NN | `1` |  |

Constraints:

- `ck_nobs_error` (check): `CHECK (((status <> 'entered_in_error'::text) OR (error_reason IS NOT NULL)))`
- `ck_nobs_time` (check): `CHECK ((observed_at <= (created_at + '00:05:00'::interval)))`
- `nursing_observation_note_check` (check): `CHECK (((length(btrim(note)) >= 2) AND (length(btrim(note)) <= 4000)))`
- `nursing_observation_observation_type_check` (check): `CHECK ((observation_type = ANY (ARRAY['general'::text, 'pain_reassessment'::text, 'wound'::text, 'medication_administered'::text, 'intake_output'::text, 'fall_risk'::text, 'patient_education'::text, 'handover'::text, 'other'::text])))`
- `nursing_observation_org_id_id_key` (unique): `UNIQUE (org_id, id)`
- `nursing_observation_status_check` (check): `CHECK ((status = ANY (ARRAY['final'::text, 'entered_in_error'::text])))`

Foreign keys:

- `nursing_observation_created_by_fkey`: `FOREIGN KEY (created_by) REFERENCES iam.user_account(id)`
- `nursing_observation_org_id_encounter_id_patient_id_fkey`: `FOREIGN KEY (org_id, encounter_id, patient_id) REFERENCES clinical.encounter(org_id, id, patient_id)`
- `nursing_observation_org_id_recorded_by_membership_id_fkey`: `FOREIGN KEY (org_id, recorded_by_membership_id) REFERENCES iam.membership(org_id, id)`
- `nursing_observation_org_id_triage_assessment_id_fkey`: `FOREIGN KEY (org_id, triage_assessment_id) REFERENCES clinical.triage_assessment(org_id, id)`
- `nursing_observation_updated_by_fkey`: `FOREIGN KEY (updated_by) REFERENCES iam.user_account(id)`

Indexes:

- `ix_nobs_encounter`: `btree (encounter_id, observed_at)`
- `ix_nobs_patient`: `btree (patient_id, observed_at DESC)`
- `nursing_observation_org_id_id_key`: `btree (org_id, id)` (unique)
- `nursing_observation_pkey`: `btree (id)` (unique)

### `clinical.problem`

RLS: forced — policies: owner_internal, staff_insert, staff_select, staff_update, tenant_isolation(R)  
Triggers: trg_0_state, trg_audit_capture, trg_touch

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | uuid | NN | `gen_random_uuid()` | **PK** |
| `org_id` | uuid | NN |  | FK→ `patient.patient` |
| `patient_id` | uuid | NN |  | FK→ `patient.patient` |
| `icd10_code` | text | null |  | FK→ `ref.icd10_code` |
| `description` | text | NN |  |  |
| `status` | text | NN | `'active'::text` |  |
| `onset_date` | date | null |  |  |
| `resolved_date` | date | null |  |  |
| `error_reason` | text | null |  |  |
| `created_at` | timestamp with time zone | NN | `now()` |  |
| `created_by` | uuid | null | `sec.current_user_id()` | FK→ `iam.user_account` |
| `updated_at` | timestamp with time zone | NN | `now()` |  |
| `updated_by` | uuid | null |  | FK→ `iam.user_account` |
| `row_version` | integer | NN | `1` |  |

Constraints:

- `ck_problem_dates` (check): `CHECK (((resolved_date IS NULL) OR (onset_date IS NULL) OR (resolved_date >= onset_date)))`
- `ck_problem_resolved` (check): `CHECK (((status <> 'resolved'::text) OR (resolved_date IS NOT NULL)))`
- `problem_org_id_id_key` (unique): `UNIQUE (org_id, id)`
- `problem_status_check` (check): `CHECK ((status = ANY (ARRAY['active'::text, 'inactive'::text, 'resolved'::text, 'entered_in_error'::text])))`

Foreign keys:

- `problem_created_by_fkey`: `FOREIGN KEY (created_by) REFERENCES iam.user_account(id)`
- `problem_icd10_code_fkey`: `FOREIGN KEY (icd10_code) REFERENCES ref.icd10_code(code)`
- `problem_org_id_patient_id_fkey`: `FOREIGN KEY (org_id, patient_id) REFERENCES patient.patient(org_id, id)`
- `problem_updated_by_fkey`: `FOREIGN KEY (updated_by) REFERENCES iam.user_account(id)`

Indexes:

- `ix_problem_patient`: `btree (patient_id, status)`
- `problem_org_id_id_key`: `btree (org_id, id)` (unique)
- `problem_pkey`: `btree (id)` (unique)

### `clinical.procedure`

RLS: forced — policies: owner_internal, staff_insert, staff_select, staff_update, tenant_isolation(R)  
Triggers: trg_0_state, trg_audit_capture, trg_proc_protect, trg_proc_rules, trg_touch

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | uuid | NN | `gen_random_uuid()` | **PK** |
| `org_id` | uuid | NN |  | FK→ `clinical.consultation` |
| `encounter_id` | uuid | NN |  | FK→ `clinical.encounter` |
| `patient_id` | uuid | NN |  | FK→ `clinical.consultation` |
| `consultation_id` | uuid | null |  | FK→ `clinical.consultation` |
| `service_id` | uuid | null |  | FK→ `billing.service_catalog` |
| `procedure_code` | text | null |  |  |
| `description` | text | NN |  |  |
| `quantity` | numeric(8,2) | NN | `1` |  |
| `status` | text | NN |  |  |
| `ordered_by_membership_id` | uuid | null |  | FK→ `iam.membership` |
| `performed_by_membership_id` | uuid | null |  | FK→ `iam.membership` |
| `performed_at` | timestamp with time zone | null |  |  |
| `outcome_note` | text | null |  |  |
| `cancel_reason` | text | null |  |  |
| `error_reason` | text | null |  |  |
| `created_at` | timestamp with time zone | NN | `now()` |  |
| `created_by` | uuid | null | `sec.current_user_id()` | FK→ `iam.user_account` |
| `updated_at` | timestamp with time zone | NN | `now()` |  |
| `updated_by` | uuid | null |  | FK→ `iam.user_account` |
| `row_version` | integer | NN | `1` |  |

Constraints:

- `ck_proc_cancel` (check): `CHECK (((status <> 'cancelled'::text) OR (cancel_reason IS NOT NULL)))`
- `ck_proc_error` (check): `CHECK (((status <> 'entered_in_error'::text) OR (error_reason IS NOT NULL)))`
- `ck_proc_performed` (check): `CHECK (((status <> ALL (ARRAY['performed'::text, 'entered_in_error'::text])) OR ((performed_at IS NOT NULL) AND (performed_by_membership_id IS NOT NULL))))`
- `procedure_description_check` (check): `CHECK ((length(btrim(description)) >= 2))`
- `procedure_org_id_id_key` (unique): `UNIQUE (org_id, id)`
- `procedure_org_id_id_patient_id_key` (unique): `UNIQUE (org_id, id, patient_id)`
- `procedure_outcome_note_check` (check): `CHECK ((length(outcome_note) <= 4000))`
- `procedure_quantity_check` (check): `CHECK ((quantity > (0)::numeric))`
- `procedure_status_check` (check): `CHECK ((status = ANY (ARRAY['planned'::text, 'performed'::text, 'cancelled'::text, 'entered_in_error'::text])))`

Foreign keys:

- `procedure_created_by_fkey`: `FOREIGN KEY (created_by) REFERENCES iam.user_account(id)`
- `procedure_org_id_consultation_id_patient_id_fkey`: `FOREIGN KEY (org_id, consultation_id, patient_id) REFERENCES clinical.consultation(org_id, id, patient_id)`
- `procedure_org_id_encounter_id_patient_id_fkey`: `FOREIGN KEY (org_id, encounter_id, patient_id) REFERENCES clinical.encounter(org_id, id, patient_id)`
- `procedure_org_id_ordered_by_membership_id_fkey`: `FOREIGN KEY (org_id, ordered_by_membership_id) REFERENCES iam.membership(org_id, id)`
- `procedure_org_id_performed_by_membership_id_fkey`: `FOREIGN KEY (org_id, performed_by_membership_id) REFERENCES iam.membership(org_id, id)`
- `procedure_org_id_service_id_fkey`: `FOREIGN KEY (org_id, service_id) REFERENCES billing.service_catalog(org_id, id)`
- `procedure_updated_by_fkey`: `FOREIGN KEY (updated_by) REFERENCES iam.user_account(id)`

Indexes:

- `ix_proc_encounter`: `btree (encounter_id)`
- `ix_proc_patient`: `btree (patient_id, performed_at DESC)`
- `ix_proc_unbilled`: `btree (org_id, performed_at) WHERE ((status = 'performed'::text) AND (service_id IS NOT NULL))`
- `procedure_org_id_id_key`: `btree (org_id, id)` (unique)
- `procedure_org_id_id_patient_id_key`: `btree (org_id, id, patient_id)` (unique)
- `procedure_pkey`: `btree (id)` (unique)

### `clinical.queue_ticket`

RLS: forced — policies: owner_internal, staff_insert, staff_select, staff_update, tenant_isolation(R)  
Triggers: trg_0_state, trg_queue_stamp, trg_touch

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | uuid | NN | `gen_random_uuid()` | **PK** |
| `org_id` | uuid | NN |  | FK→ `org.department` |
| `facility_id` | uuid | NN |  | FK→ `org.facility` |
| `department_id` | uuid | NN |  | FK→ `org.department` |
| `service_point_id` | uuid | null |  | FK→ `org.service_point` |
| `encounter_id` | uuid | NN |  | FK→ `clinical.encounter` |
| `patient_id` | uuid | NN |  | FK→ `clinical.encounter` |
| `queue_date` | date | NN |  |  |
| `ticket_no` | integer | NN |  |  |
| `priority` | text | NN | `'routine'::text` |  |
| `priority_rank` | smallint | null | GENERATED: `
CASE priority
    WHEN 'emergency'::text THEN 1
    WHEN 'urgent'::text THEN 2
    ELSE 3
END` |  |
| `status` | text | NN | `'waiting'::text` |  |
| `called_at` | timestamp with time zone | null |  |  |
| `call_count` | smallint | NN | `0` |  |
| `service_started_at` | timestamp with time zone | null |  |  |
| `completed_at` | timestamp with time zone | null |  |  |
| `served_by_membership_id` | uuid | null |  | FK→ `iam.membership` |
| `transferred_to_ticket_id` | uuid | null |  | FK→ `clinical.queue_ticket` |
| `created_at` | timestamp with time zone | NN | `now()` |  |
| `created_by` | uuid | null | `sec.current_user_id()` | FK→ `iam.user_account` |
| `updated_at` | timestamp with time zone | NN | `now()` |  |
| `updated_by` | uuid | null |  | FK→ `iam.user_account` |
| `row_version` | integer | NN | `1` |  |

Constraints:

- `ck_queue_transfer` (check): `CHECK (((status = 'transferred'::text) = (transferred_to_ticket_id IS NOT NULL)))`
- `queue_ticket_call_count_check` (check): `CHECK (((call_count >= 0) AND (call_count <= 10)))`
- `queue_ticket_department_id_queue_date_ticket_no_key` (unique): `UNIQUE (department_id, queue_date, ticket_no)`
- `queue_ticket_org_id_id_key` (unique): `UNIQUE (org_id, id)`
- `queue_ticket_priority_check` (check): `CHECK ((priority = ANY (ARRAY['emergency'::text, 'urgent'::text, 'routine'::text])))`
- `queue_ticket_status_check` (check): `CHECK ((status = ANY (ARRAY['waiting'::text, 'called'::text, 'in_service'::text, 'completed'::text, 'skipped'::text, 'cancelled'::text, 'transferred'::text])))`
- `queue_ticket_ticket_no_check` (check): `CHECK ((ticket_no > 0))`

Foreign keys:

- `queue_ticket_created_by_fkey`: `FOREIGN KEY (created_by) REFERENCES iam.user_account(id)`
- `queue_ticket_org_id_department_id_fkey`: `FOREIGN KEY (org_id, department_id) REFERENCES org.department(org_id, id)`
- `queue_ticket_org_id_encounter_id_patient_id_fkey`: `FOREIGN KEY (org_id, encounter_id, patient_id) REFERENCES clinical.encounter(org_id, id, patient_id)`
- `queue_ticket_org_id_facility_id_fkey`: `FOREIGN KEY (org_id, facility_id) REFERENCES org.facility(org_id, id)`
- `queue_ticket_org_id_served_by_membership_id_fkey`: `FOREIGN KEY (org_id, served_by_membership_id) REFERENCES iam.membership(org_id, id)`
- `queue_ticket_org_id_service_point_id_fkey`: `FOREIGN KEY (org_id, service_point_id) REFERENCES org.service_point(org_id, id)`
- `queue_ticket_transferred_to_ticket_id_fkey`: `FOREIGN KEY (transferred_to_ticket_id) REFERENCES clinical.queue_ticket(id)`
- `queue_ticket_updated_by_fkey`: `FOREIGN KEY (updated_by) REFERENCES iam.user_account(id)`

Indexes:

- `ix_queue_board`: `btree (department_id, queue_date, priority_rank, created_at) WHERE (status = ANY (ARRAY['waiting'::text, 'called'::text, 'in_service'::text]))`
- `queue_ticket_department_id_queue_date_ticket_no_key`: `btree (department_id, queue_date, ticket_no)` (unique)
- `queue_ticket_org_id_id_key`: `btree (org_id, id)` (unique)
- `queue_ticket_pkey`: `btree (id)` (unique)
- `ux_queue_active`: `btree (encounter_id, department_id) WHERE (status = ANY (ARRAY['waiting'::text, 'called'::text, 'in_service'::text]))` (unique)

### `clinical.triage_assessment`

RLS: forced — policies: owner_internal, staff_insert, staff_select, staff_update, tenant_isolation(R)  
Triggers: trg_0_state, trg_audit_capture, trg_touch, trg_triage_protect

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | uuid | NN | `gen_random_uuid()` | **PK** |
| `org_id` | uuid | NN |  | FK→ `clinical.encounter` |
| `encounter_id` | uuid | NN |  | FK→ `clinical.encounter` |
| `patient_id` | uuid | NN |  | FK→ `clinical.encounter` |
| `recorded_by_membership_id` | uuid | NN |  | FK→ `iam.membership` |
| `measured_at` | timestamp with time zone | NN | `now()` |  |
| `temperature_c` | numeric(4,1) | null |  |  |
| `pulse_bpm` | smallint | null |  |  |
| `respiratory_rate` | smallint | null |  |  |
| `systolic_mmhg` | smallint | null |  |  |
| `diastolic_mmhg` | smallint | null |  |  |
| `spo2_pct` | smallint | null |  |  |
| `weight_kg` | numeric(5,2) | null |  |  |
| `height_cm` | numeric(5,1) | null |  |  |
| `bmi` | numeric(5,2) | null | GENERATED: `
CASE
    WHEN ((weight_kg IS NOT NULL) AND (height_cm IS NOT NULL)) THEN round((weight_kg / ((height_cm / 100.0) ^ (2)::numeric)), 2)
    ELSE NULL::numeric
END` |  |
| `muac_cm` | numeric(4,1) | null |  |  |
| `blood_glucose_mmol` | numeric(4,1) | null |  |  |
| `pain_score` | smallint | null |  |  |
| `triage_category` | text | null |  |  |
| `notes` | text | null |  |  |
| `status` | text | NN | `'draft'::text` |  |
| `supersedes_id` | uuid | null |  | FK→ `clinical.triage_assessment` |
| `finalised_at` | timestamp with time zone | null |  |  |
| `error_reason` | text | null |  |  |
| `created_at` | timestamp with time zone | NN | `now()` |  |
| `created_by` | uuid | null | `sec.current_user_id()` | FK→ `iam.user_account` |
| `updated_at` | timestamp with time zone | NN | `now()` |  |
| `updated_by` | uuid | null |  | FK→ `iam.user_account` |
| `row_version` | integer | NN | `1` |  |

Constraints:

- `ck_triage_bp` (check): `CHECK (((systolic_mmhg IS NULL) OR (diastolic_mmhg IS NULL) OR (systolic_mmhg > diastolic_mmhg)))`
- `ck_triage_bp_pair` (check): `CHECK (((systolic_mmhg IS NULL) = (diastolic_mmhg IS NULL)))`
- `ck_triage_error` (check): `CHECK (((status <> 'entered_in_error'::text) OR (error_reason IS NOT NULL)))`
- `ck_triage_final` (check): `CHECK (((status = 'draft'::text) OR ((finalised_at IS NOT NULL) AND (triage_category IS NOT NULL) AND (num_nonnulls(temperature_c, pulse_bpm, respiratory_rate, systolic_mmhg, spo2_pct, weight_kg) >= 2))))`
- `triage_assessment_blood_glucose_mmol_check` (check): `CHECK (((blood_glucose_mmol >= 0.5) AND (blood_glucose_mmol <= (60)::numeric)))`
- `triage_assessment_diastolic_mmhg_check` (check): `CHECK (((diastolic_mmhg >= 20) AND (diastolic_mmhg <= 200)))`
- `triage_assessment_height_cm_check` (check): `CHECK (((height_cm >= (20)::numeric) AND (height_cm <= (260)::numeric)))`
- `triage_assessment_muac_cm_check` (check): `CHECK (((muac_cm >= (5)::numeric) AND (muac_cm <= (60)::numeric)))`
- `triage_assessment_notes_check` (check): `CHECK ((length(notes) <= 4000))`
- `triage_assessment_org_id_id_key` (unique): `UNIQUE (org_id, id)`
- `triage_assessment_pain_score_check` (check): `CHECK (((pain_score >= 0) AND (pain_score <= 10)))`
- `triage_assessment_pulse_bpm_check` (check): `CHECK (((pulse_bpm >= 20) AND (pulse_bpm <= 300)))`
- `triage_assessment_respiratory_rate_check` (check): `CHECK (((respiratory_rate >= 4) AND (respiratory_rate <= 80)))`
- `triage_assessment_spo2_pct_check` (check): `CHECK (((spo2_pct >= 50) AND (spo2_pct <= 100)))`
- `triage_assessment_status_check` (check): `CHECK ((status = ANY (ARRAY['draft'::text, 'final'::text, 'amended'::text, 'entered_in_error'::text])))`
- `triage_assessment_supersedes_id_key` (unique): `UNIQUE (supersedes_id)`
- `triage_assessment_systolic_mmhg_check` (check): `CHECK (((systolic_mmhg >= 40) AND (systolic_mmhg <= 300)))`
- `triage_assessment_temperature_c_check` (check): `CHECK (((temperature_c >= (25)::numeric) AND (temperature_c <= (45)::numeric)))`
- `triage_assessment_triage_category_check` (check): `CHECK ((triage_category = ANY (ARRAY['emergency'::text, 'priority'::text, 'queue'::text])))`
- `triage_assessment_weight_kg_check` (check): `CHECK (((weight_kg >= 0.3) AND (weight_kg <= (400)::numeric)))`

Foreign keys:

- `triage_assessment_created_by_fkey`: `FOREIGN KEY (created_by) REFERENCES iam.user_account(id)`
- `triage_assessment_org_id_encounter_id_patient_id_fkey`: `FOREIGN KEY (org_id, encounter_id, patient_id) REFERENCES clinical.encounter(org_id, id, patient_id)`
- `triage_assessment_org_id_recorded_by_membership_id_fkey`: `FOREIGN KEY (org_id, recorded_by_membership_id) REFERENCES iam.membership(org_id, id)`
- `triage_assessment_supersedes_id_fkey`: `FOREIGN KEY (supersedes_id) REFERENCES clinical.triage_assessment(id)`
- `triage_assessment_updated_by_fkey`: `FOREIGN KEY (updated_by) REFERENCES iam.user_account(id)`

Indexes:

- `ix_triage_encounter`: `btree (encounter_id, measured_at DESC)`
- `ix_triage_patient`: `btree (patient_id, measured_at DESC)`
- `triage_assessment_org_id_id_key`: `btree (org_id, id)` (unique)
- `triage_assessment_pkey`: `btree (id)` (unique)
- `triage_assessment_supersedes_id_key`: `btree (supersedes_id)` (unique)

## Schema `diag`

### `diag.reference_range`

RLS: forced — policies: owner_internal, staff_insert, staff_select, staff_update, tenant_isolation(R)  
Triggers: —

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | uuid | NN | `gen_random_uuid()` | **PK** |
| `org_id` | uuid | NN |  | FK→ `diag.test_component` |
| `component_id` | uuid | NN |  | FK→ `diag.test_component` |
| `sex` | text | null |  |  |
| `age_min_days` | integer | NN | `0` |  |
| `age_max_days` | integer | NN | `54750` |  |
| `low` | numeric(18,6) | null |  |  |
| `high` | numeric(18,6) | null |  |  |
| `critical_low` | numeric(18,6) | null |  |  |
| `critical_high` | numeric(18,6) | null |  |  |

Constraints:

- `ck_range_age` (check): `CHECK ((age_max_days >= age_min_days))`
- `ck_range_values` (check): `CHECK (((low IS NULL) OR (high IS NULL) OR (low <= high)))`
- `ex_range_overlap` (exclusion): `EXCLUDE USING gist (component_id WITH =, COALESCE(sex, 'any'::text) WITH =, int4range(age_min_days, age_max_days, '[]'::text) WITH &&)`
- `reference_range_age_max_days_check` (check): `CHECK ((age_max_days >= 0))`
- `reference_range_age_min_days_check` (check): `CHECK ((age_min_days >= 0))`
- `reference_range_sex_check` (check): `CHECK ((sex = ANY (ARRAY['female'::text, 'male'::text])))`

Foreign keys:

- `reference_range_org_id_component_id_fkey`: `FOREIGN KEY (org_id, component_id) REFERENCES diag.test_component(org_id, id)`

Indexes:

- `ex_range_overlap`: `gist (component_id, COALESCE(sex, 'any'::text), int4range(age_min_days, age_max_days, '[]'::text))`
- `reference_range_pkey`: `btree (id)` (unique)

### `diag.request`

RLS: forced — policies: owner_internal, staff_insert, staff_select, staff_update, tenant_isolation(R)  
Triggers: trg_0_state, trg_audit_capture, trg_touch

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | uuid | NN | `gen_random_uuid()` | **PK** |
| `org_id` | uuid | NN |  | FK→ `clinical.encounter` |
| `facility_id` | uuid | NN |  | FK→ `org.facility` |
| `encounter_id` | uuid | NN |  | FK→ `clinical.encounter` |
| `patient_id` | uuid | NN |  | FK→ `clinical.encounter` |
| `request_no` | text | NN |  |  |
| `ordered_by_membership_id` | uuid | NN |  | FK→ `iam.membership` |
| `priority` | text | NN | `'routine'::text` |  |
| `clinical_indication` | text | NN |  |  |
| `status` | text | NN | `'requested'::text` |  |
| `cancel_reason` | text | null |  |  |
| `created_at` | timestamp with time zone | NN | `now()` |  |
| `created_by` | uuid | null | `sec.current_user_id()` | FK→ `iam.user_account` |
| `updated_at` | timestamp with time zone | NN | `now()` |  |
| `updated_by` | uuid | null |  | FK→ `iam.user_account` |
| `row_version` | integer | NN | `1` |  |

Constraints:

- `ck_request_cancel` (check): `CHECK (((status <> 'cancelled'::text) OR (cancel_reason IS NOT NULL)))`
- `request_clinical_indication_check` (check): `CHECK ((length(btrim(clinical_indication)) >= 3))`
- `request_org_id_id_key` (unique): `UNIQUE (org_id, id)`
- `request_org_id_id_patient_id_key` (unique): `UNIQUE (org_id, id, patient_id)`
- `request_org_id_request_no_key` (unique): `UNIQUE (org_id, request_no)`
- `request_priority_check` (check): `CHECK ((priority = ANY (ARRAY['routine'::text, 'urgent'::text, 'stat'::text])))`
- `request_status_check` (check): `CHECK ((status = ANY (ARRAY['requested'::text, 'in_progress'::text, 'partially_resulted'::text, 'completed'::text, 'cancelled'::text])))`

Foreign keys:

- `request_created_by_fkey`: `FOREIGN KEY (created_by) REFERENCES iam.user_account(id)`
- `request_org_id_encounter_id_patient_id_fkey`: `FOREIGN KEY (org_id, encounter_id, patient_id) REFERENCES clinical.encounter(org_id, id, patient_id)`
- `request_org_id_facility_id_fkey`: `FOREIGN KEY (org_id, facility_id) REFERENCES org.facility(org_id, id)`
- `request_org_id_ordered_by_membership_id_fkey`: `FOREIGN KEY (org_id, ordered_by_membership_id) REFERENCES iam.membership(org_id, id)`
- `request_updated_by_fkey`: `FOREIGN KEY (updated_by) REFERENCES iam.user_account(id)`

Indexes:

- `ix_request_encounter`: `btree (encounter_id)`
- `ix_request_patient`: `btree (patient_id, created_at DESC)`
- `ix_request_worklist`: `btree (org_id, facility_id, status, created_at) WHERE (status = ANY (ARRAY['requested'::text, 'in_progress'::text, 'partially_resulted'::text]))`
- `request_org_id_id_key`: `btree (org_id, id)` (unique)
- `request_org_id_id_patient_id_key`: `btree (org_id, id, patient_id)` (unique)
- `request_org_id_request_no_key`: `btree (org_id, request_no)` (unique)
- `request_pkey`: `btree (id)` (unique)

### `diag.request_item`

RLS: forced — policies: owner_internal, staff_insert, staff_select, staff_update, tenant_isolation(R)  
Triggers: trg_0_state, trg_audit_capture, trg_touch

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | uuid | NN | `gen_random_uuid()` | **PK** |
| `org_id` | uuid | NN |  | FK→ `diag.request` |
| `request_id` | uuid | NN |  | FK→ `diag.request` |
| `patient_id` | uuid | NN |  | FK→ `diag.request` |
| `test_id` | uuid | NN |  | FK→ `diag.test_catalog` |
| `status` | text | NN | `'requested'::text` |  |
| `cancel_reason` | text | null |  |  |
| `created_at` | timestamp with time zone | NN | `now()` |  |
| `created_by` | uuid | null | `sec.current_user_id()` | FK→ `iam.user_account` |
| `updated_at` | timestamp with time zone | NN | `now()` |  |
| `updated_by` | uuid | null |  | FK→ `iam.user_account` |
| `row_version` | integer | NN | `1` |  |

Constraints:

- `ck_item_cancel` (check): `CHECK (((status <> ALL (ARRAY['cancelled'::text, 'rejected'::text])) OR (cancel_reason IS NOT NULL)))`
- `request_item_org_id_id_key` (unique): `UNIQUE (org_id, id)`
- `request_item_org_id_id_patient_id_key` (unique): `UNIQUE (org_id, id, patient_id)`
- `request_item_request_id_test_id_key` (unique): `UNIQUE (request_id, test_id)`
- `request_item_status_check` (check): `CHECK ((status = ANY (ARRAY['requested'::text, 'specimen_collected'::text, 'in_progress'::text, 'resulted'::text, 'verified'::text, 'cancelled'::text, 'rejected'::text])))`

Foreign keys:

- `request_item_created_by_fkey`: `FOREIGN KEY (created_by) REFERENCES iam.user_account(id)`
- `request_item_org_id_request_id_patient_id_fkey`: `FOREIGN KEY (org_id, request_id, patient_id) REFERENCES diag.request(org_id, id, patient_id)`
- `request_item_org_id_test_id_fkey`: `FOREIGN KEY (org_id, test_id) REFERENCES diag.test_catalog(org_id, id)`
- `request_item_updated_by_fkey`: `FOREIGN KEY (updated_by) REFERENCES iam.user_account(id)`

Indexes:

- `request_item_org_id_id_key`: `btree (org_id, id)` (unique)
- `request_item_org_id_id_patient_id_key`: `btree (org_id, id, patient_id)` (unique)
- `request_item_pkey`: `btree (id)` (unique)
- `request_item_request_id_test_id_key`: `btree (request_id, test_id)` (unique)

### `diag.result`

RLS: forced — policies: owner_internal, staff_insert, staff_select, staff_update, tenant_isolation(R)  
Triggers: trg_0_state, trg_audit_capture, trg_result_protect, trg_result_rollup, trg_result_rules, trg_touch

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | uuid | NN | `gen_random_uuid()` | **PK** |
| `org_id` | uuid | NN |  | FK→ `document.document` |
| `request_item_id` | uuid | NN |  | FK→ `diag.request_item` |
| `patient_id` | uuid | NN |  | FK→ `diag.request_item` |
| `component_id` | uuid | NN |  | FK→ `diag.test_component` |
| `value_numeric` | numeric(18,6) | null |  |  |
| `value_text` | text | null |  |  |
| `value_coded` | text | null |  |  |
| `attachment_document_id` | uuid | null |  | FK→ `document.document` |
| `unit` | text | null |  |  |
| `ref_low` | numeric(18,6) | null |  |  |
| `ref_high` | numeric(18,6) | null |  |  |
| `abnormal_flag` | text | null |  |  |
| `is_critical` | boolean | NN | `false` |  |
| `status` | text | NN | `'preliminary'::text` |  |
| `entered_by` | uuid | NN | `sec.current_user_id()` | FK→ `iam.user_account` |
| `entered_at` | timestamp with time zone | NN | `now()` |  |
| `verified_by` | uuid | null |  | FK→ `iam.user_account` |
| `verified_at` | timestamp with time zone | null |  |  |
| `supersedes_id` | uuid | null |  | FK→ `diag.result` |
| `correction_reason` | text | null |  |  |
| `error_reason` | text | null |  |  |
| `created_at` | timestamp with time zone | NN | `now()` |  |
| `created_by` | uuid | null | `sec.current_user_id()` | FK→ `iam.user_account` |
| `updated_at` | timestamp with time zone | NN | `now()` |  |
| `updated_by` | uuid | null |  | FK→ `iam.user_account` |
| `row_version` | integer | NN | `1` |  |

Constraints:

- `ck_result_correction` (check): `CHECK (((supersedes_id IS NULL) OR (correction_reason IS NOT NULL)))`
- `ck_result_error` (check): `CHECK (((status <> 'entered_in_error'::text) OR (error_reason IS NOT NULL)))`
- `ck_result_value` (check): `CHECK ((num_nonnulls(value_numeric, value_text, value_coded, attachment_document_id) >= 1))`
- `ck_result_verified` (check): `CHECK (((status <> 'final'::text) OR ((verified_by IS NOT NULL) AND (verified_at IS NOT NULL))))`
- `result_abnormal_flag_check` (check): `CHECK ((abnormal_flag = ANY (ARRAY['N'::text, 'L'::text, 'H'::text, 'LL'::text, 'HH'::text, 'A'::text])))`
- `result_org_id_id_key` (unique): `UNIQUE (org_id, id)`
- `result_status_check` (check): `CHECK ((status = ANY (ARRAY['preliminary'::text, 'final'::text, 'corrected'::text, 'entered_in_error'::text])))`
- `result_supersedes_id_key` (unique): `UNIQUE (supersedes_id)`
- `result_value_text_check` (check): `CHECK ((length(value_text) <= 4000))`

Foreign keys:

- `result_created_by_fkey`: `FOREIGN KEY (created_by) REFERENCES iam.user_account(id)`
- `result_entered_by_fkey`: `FOREIGN KEY (entered_by) REFERENCES iam.user_account(id)`
- `result_org_id_attachment_document_id_fkey`: `FOREIGN KEY (org_id, attachment_document_id) REFERENCES document.document(org_id, id)`
- `result_org_id_component_id_fkey`: `FOREIGN KEY (org_id, component_id) REFERENCES diag.test_component(org_id, id)`
- `result_org_id_request_item_id_patient_id_fkey`: `FOREIGN KEY (org_id, request_item_id, patient_id) REFERENCES diag.request_item(org_id, id, patient_id)`
- `result_supersedes_id_fkey`: `FOREIGN KEY (supersedes_id) REFERENCES diag.result(id)`
- `result_updated_by_fkey`: `FOREIGN KEY (updated_by) REFERENCES iam.user_account(id)`
- `result_verified_by_fkey`: `FOREIGN KEY (verified_by) REFERENCES iam.user_account(id)`

Indexes:

- `ix_result_patient`: `btree (patient_id, entered_at DESC)`
- `result_org_id_id_key`: `btree (org_id, id)` (unique)
- `result_pkey`: `btree (id)` (unique)
- `result_supersedes_id_key`: `btree (supersedes_id)` (unique)
- `ux_result_current`: `btree (request_item_id, component_id) WHERE (status = ANY (ARRAY['preliminary'::text, 'final'::text]))` (unique)

### `diag.specimen`

RLS: forced — policies: owner_internal, staff_insert, staff_select, staff_update, tenant_isolation(R)  
Triggers: trg_0_state, trg_touch

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | uuid | NN | `gen_random_uuid()` | **PK** |
| `org_id` | uuid | NN |  | FK→ `diag.request_item` |
| `request_item_id` | uuid | NN |  | FK→ `diag.request_item` |
| `patient_id` | uuid | NN |  | FK→ `diag.request_item` |
| `barcode` | text | NN |  |  |
| `specimen_type` | text | NN |  |  |
| `status` | text | NN | `'collected'::text` |  |
| `collected_at` | timestamp with time zone | NN | `now()` |  |
| `collected_by` | uuid | null | `sec.current_user_id()` | FK→ `iam.user_account` |
| `received_at` | timestamp with time zone | null |  |  |
| `received_by` | uuid | null |  | FK→ `iam.user_account` |
| `rejection_reason` | text | null |  |  |
| `created_at` | timestamp with time zone | NN | `now()` |  |
| `created_by` | uuid | null | `sec.current_user_id()` | FK→ `iam.user_account` |
| `updated_at` | timestamp with time zone | NN | `now()` |  |
| `updated_by` | uuid | null |  | FK→ `iam.user_account` |
| `row_version` | integer | NN | `1` |  |

Constraints:

- `ck_specimen_received` (check): `CHECK (((status <> ALL (ARRAY['received'::text, 'consumed'::text])) OR (received_at IS NOT NULL)))`
- `ck_specimen_rejected` (check): `CHECK (((status = 'rejected'::text) = (rejection_reason IS NOT NULL)))`
- `specimen_org_id_barcode_key` (unique): `UNIQUE (org_id, barcode)`
- `specimen_rejection_reason_check` (check): `CHECK ((rejection_reason = ANY (ARRAY['haemolysed'::text, 'clotted'::text, 'insufficient_volume'::text, 'unlabelled'::text, 'mislabelled'::text, 'wrong_container'::text, 'delayed_transport'::text, 'other'::text])))`
- `specimen_status_check` (check): `CHECK ((status = ANY (ARRAY['collected'::text, 'received'::text, 'rejected'::text, 'consumed'::text])))`

Foreign keys:

- `specimen_collected_by_fkey`: `FOREIGN KEY (collected_by) REFERENCES iam.user_account(id)`
- `specimen_created_by_fkey`: `FOREIGN KEY (created_by) REFERENCES iam.user_account(id)`
- `specimen_org_id_request_item_id_patient_id_fkey`: `FOREIGN KEY (org_id, request_item_id, patient_id) REFERENCES diag.request_item(org_id, id, patient_id)`
- `specimen_received_by_fkey`: `FOREIGN KEY (received_by) REFERENCES iam.user_account(id)`
- `specimen_updated_by_fkey`: `FOREIGN KEY (updated_by) REFERENCES iam.user_account(id)`

Indexes:

- `specimen_org_id_barcode_key`: `btree (org_id, barcode)` (unique)
- `specimen_pkey`: `btree (id)` (unique)

### `diag.test_catalog`

RLS: forced — policies: owner_internal, staff_insert, staff_select, staff_update, tenant_isolation(R)  
Triggers: trg_touch

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | uuid | NN | `gen_random_uuid()` | **PK** |
| `org_id` | uuid | NN |  | FK→ `org.organization` |
| `code` | text | NN |  |  |
| `name` | text | NN |  |  |
| `category` | text | NN |  |  |
| `specimen_type` | text | null |  |  |
| `turnaround_hours` | smallint | null |  |  |
| `status` | text | NN | `'active'::text` |  |
| `created_at` | timestamp with time zone | NN | `now()` |  |
| `created_by` | uuid | null | `sec.current_user_id()` | FK→ `iam.user_account` |
| `updated_at` | timestamp with time zone | NN | `now()` |  |
| `updated_by` | uuid | null |  | FK→ `iam.user_account` |
| `row_version` | integer | NN | `1` |  |

Constraints:

- `ck_lab_specimen` (check): `CHECK (((category <> 'laboratory'::text) OR (specimen_type IS NOT NULL)))`
- `test_catalog_category_check` (check): `CHECK ((category = ANY (ARRAY['laboratory'::text, 'imaging'::text, 'other_diagnostic'::text])))`
- `test_catalog_code_check` (check): `CHECK ((code ~ '^[A-Z0-9_-]{2,30}$'::text))`
- `test_catalog_org_id_code_key` (unique): `UNIQUE (org_id, code)`
- `test_catalog_org_id_id_key` (unique): `UNIQUE (org_id, id)`
- `test_catalog_specimen_type_check` (check): `CHECK ((specimen_type = ANY (ARRAY['blood'::text, 'serum'::text, 'plasma'::text, 'urine'::text, 'stool'::text, 'sputum'::text, 'swab'::text, 'csf'::text, 'tissue'::text, 'other'::text])))`
- `test_catalog_status_check` (check): `CHECK ((status = ANY (ARRAY['active'::text, 'retired'::text])))`
- `test_catalog_turnaround_hours_check` (check): `CHECK (((turnaround_hours >= 0) AND (turnaround_hours <= 720)))`

Foreign keys:

- `test_catalog_created_by_fkey`: `FOREIGN KEY (created_by) REFERENCES iam.user_account(id)`
- `test_catalog_org_id_fkey`: `FOREIGN KEY (org_id) REFERENCES org.organization(id)`
- `test_catalog_updated_by_fkey`: `FOREIGN KEY (updated_by) REFERENCES iam.user_account(id)`

Indexes:

- `test_catalog_org_id_code_key`: `btree (org_id, code)` (unique)
- `test_catalog_org_id_id_key`: `btree (org_id, id)` (unique)
- `test_catalog_pkey`: `btree (id)` (unique)

### `diag.test_component`

RLS: forced — policies: owner_internal, staff_insert, staff_select, staff_update, tenant_isolation(R)  
Triggers: —

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | uuid | NN | `gen_random_uuid()` | **PK** |
| `org_id` | uuid | NN |  | FK→ `diag.test_catalog` |
| `test_id` | uuid | NN |  | FK→ `diag.test_catalog` |
| `code` | text | NN |  |  |
| `name` | text | NN |  |  |
| `value_type` | text | NN |  |  |
| `unit` | text | null |  |  |
| `decimal_places` | smallint | null |  |  |
| `allowed_values` | _text | null |  |  |
| `display_order` | smallint | NN | `1` |  |
| `status` | text | NN | `'active'::text` |  |

Constraints:

- `ck_component_coded` (check): `CHECK (((value_type <> 'coded'::text) OR (allowed_values IS NOT NULL)))`
- `test_component_decimal_places_check` (check): `CHECK (((decimal_places >= 0) AND (decimal_places <= 6)))`
- `test_component_org_id_id_key` (unique): `UNIQUE (org_id, id)`
- `test_component_status_check` (check): `CHECK ((status = ANY (ARRAY['active'::text, 'retired'::text])))`
- `test_component_test_id_code_key` (unique): `UNIQUE (test_id, code)`
- `test_component_value_type_check` (check): `CHECK ((value_type = ANY (ARRAY['numeric'::text, 'text'::text, 'coded'::text, 'attachment'::text])))`

Foreign keys:

- `test_component_org_id_test_id_fkey`: `FOREIGN KEY (org_id, test_id) REFERENCES diag.test_catalog(org_id, id)`

Indexes:

- `test_component_org_id_id_key`: `btree (org_id, id)` (unique)
- `test_component_pkey`: `btree (id)` (unique)
- `test_component_test_id_code_key`: `btree (test_id, code)` (unique)

## Schema `pharmacy`

### `pharmacy.dispense`

RLS: forced — policies: owner_internal, staff_insert, staff_select, staff_update, tenant_isolation(R)  
Triggers: trg_0_state, trg_audit_capture, trg_touch

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | uuid | NN | `gen_random_uuid()` | **PK** |
| `org_id` | uuid | NN |  | FK→ `iam.membership` |
| `facility_id` | uuid | NN |  | FK→ `org.facility` |
| `prescription_id` | uuid | NN |  | FK→ `pharmacy.prescription` |
| `patient_id` | uuid | NN |  | FK→ `pharmacy.prescription` |
| `dispensed_by_membership_id` | uuid | NN |  | FK→ `iam.membership` |
| `dispensed_at` | timestamp with time zone | NN | `now()` |  |
| `counselling_given` | boolean | NN | `false` |  |
| `status` | text | NN | `'completed'::text` |  |
| `reversal_reason` | text | null |  |  |
| `idempotency_key` | uuid | NN |  |  |
| `created_at` | timestamp with time zone | NN | `now()` |  |
| `created_by` | uuid | null | `sec.current_user_id()` | FK→ `iam.user_account` |
| `updated_at` | timestamp with time zone | NN | `now()` |  |
| `updated_by` | uuid | null |  | FK→ `iam.user_account` |
| `row_version` | integer | NN | `1` |  |

Constraints:

- `ck_dispense_reversal` (check): `CHECK (((status = 'reversed'::text) = (reversal_reason IS NOT NULL)))`
- `dispense_org_id_id_key` (unique): `UNIQUE (org_id, id)`
- `dispense_org_id_idempotency_key_key` (unique): `UNIQUE (org_id, idempotency_key)`
- `dispense_status_check` (check): `CHECK ((status = ANY (ARRAY['completed'::text, 'reversed'::text])))`

Foreign keys:

- `dispense_created_by_fkey`: `FOREIGN KEY (created_by) REFERENCES iam.user_account(id)`
- `dispense_org_id_dispensed_by_membership_id_fkey`: `FOREIGN KEY (org_id, dispensed_by_membership_id) REFERENCES iam.membership(org_id, id)`
- `dispense_org_id_facility_id_fkey`: `FOREIGN KEY (org_id, facility_id) REFERENCES org.facility(org_id, id)`
- `dispense_org_id_prescription_id_patient_id_fkey`: `FOREIGN KEY (org_id, prescription_id, patient_id) REFERENCES pharmacy.prescription(org_id, id, patient_id)`
- `dispense_updated_by_fkey`: `FOREIGN KEY (updated_by) REFERENCES iam.user_account(id)`

Indexes:

- `dispense_org_id_id_key`: `btree (org_id, id)` (unique)
- `dispense_org_id_idempotency_key_key`: `btree (org_id, idempotency_key)` (unique)
- `dispense_pkey`: `btree (id)` (unique)

### `pharmacy.dispense_item`

RLS: forced — policies: owner_internal, staff_insert, staff_select, staff_update, tenant_isolation(R)  
Triggers: trg_dispense_item_append_only, trg_dispense_item_apply

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | uuid | NN | `gen_random_uuid()` | **PK** |
| `org_id` | uuid | NN |  | FK→ `pharmacy.stock_batch` |
| `dispense_id` | uuid | NN |  | FK→ `pharmacy.dispense` |
| `prescription_item_id` | uuid | NN |  | FK→ `pharmacy.prescription_item` |
| `batch_id` | uuid | NN |  | FK→ `pharmacy.stock_batch` |
| `quantity` | numeric(12,2) | NN |  |  |
| `created_at` | timestamp with time zone | NN | `now()` |  |
| `created_by` | uuid | null | `sec.current_user_id()` | FK→ `iam.user_account` |

Constraints:

- `dispense_item_org_id_id_key` (unique): `UNIQUE (org_id, id)`
- `dispense_item_quantity_check` (check): `CHECK ((quantity > (0)::numeric))`

Foreign keys:

- `dispense_item_created_by_fkey`: `FOREIGN KEY (created_by) REFERENCES iam.user_account(id)`
- `dispense_item_org_id_batch_id_fkey`: `FOREIGN KEY (org_id, batch_id) REFERENCES pharmacy.stock_batch(org_id, id)`
- `dispense_item_org_id_dispense_id_fkey`: `FOREIGN KEY (org_id, dispense_id) REFERENCES pharmacy.dispense(org_id, id)`
- `dispense_item_org_id_prescription_item_id_fkey`: `FOREIGN KEY (org_id, prescription_item_id) REFERENCES pharmacy.prescription_item(org_id, id)`

Indexes:

- `dispense_item_org_id_id_key`: `btree (org_id, id)` (unique)
- `dispense_item_pkey`: `btree (id)` (unique)
- `ix_dispense_item_rxi`: `btree (prescription_item_id)`

### `pharmacy.drug`

RLS: forced — policies: owner_internal, staff_insert, staff_select, staff_update, tenant_isolation(R)  
Triggers: trg_touch

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | uuid | NN | `gen_random_uuid()` | **PK** |
| `org_id` | uuid | NN |  | FK→ `org.organization` |
| `code` | text | NN |  |  |
| `generic_name` | text | NN |  |  |
| `brand_name` | text | null |  |  |
| `dosage_form` | text | NN |  |  |
| `strength` | text | NN |  |  |
| `unit_of_issue` | text | NN |  |  |
| `atc_code` | text | null |  |  |
| `is_controlled` | boolean | NN | `false` |  |
| `status` | text | NN | `'active'::text` |  |
| `created_at` | timestamp with time zone | NN | `now()` |  |
| `created_by` | uuid | null | `sec.current_user_id()` | FK→ `iam.user_account` |
| `updated_at` | timestamp with time zone | NN | `now()` |  |
| `updated_by` | uuid | null |  | FK→ `iam.user_account` |
| `row_version` | integer | NN | `1` |  |

Constraints:

- `drug_atc_code_check` (check): `CHECK ((atc_code ~ '^[A-Z][0-9]{2}[A-Z]{2}[0-9]{2}$'::text))`
- `drug_code_check` (check): `CHECK ((code ~ '^[A-Z0-9_-]{2,30}$'::text))`
- `drug_dosage_form_check` (check): `CHECK ((dosage_form = ANY (ARRAY['tablet'::text, 'capsule'::text, 'syrup'::text, 'suspension'::text, 'injection'::text, 'infusion'::text, 'cream'::text, 'ointment'::text, 'drops'::text, 'inhaler'::text, 'suppository'::text, 'powder'::text, 'other'::text])))`
- `drug_org_id_code_key` (unique): `UNIQUE (org_id, code)`
- `drug_org_id_id_key` (unique): `UNIQUE (org_id, id)`
- `drug_status_check` (check): `CHECK ((status = ANY (ARRAY['active'::text, 'retired'::text])))`

Foreign keys:

- `drug_created_by_fkey`: `FOREIGN KEY (created_by) REFERENCES iam.user_account(id)`
- `drug_org_id_fkey`: `FOREIGN KEY (org_id) REFERENCES org.organization(id)`
- `drug_updated_by_fkey`: `FOREIGN KEY (updated_by) REFERENCES iam.user_account(id)`

Indexes:

- `drug_org_id_code_key`: `btree (org_id, code)` (unique)
- `drug_org_id_id_key`: `btree (org_id, id)` (unique)
- `drug_pkey`: `btree (id)` (unique)
- `ix_drug_name_trgm`: `gin (lower(((generic_name || ' '::text) || COALESCE(brand_name, ''::text))) gin_trgm_ops)`

### `pharmacy.prescription`

RLS: forced — policies: owner_internal, staff_insert, staff_select, staff_update, tenant_isolation(R)  
Triggers: trg_0_state, trg_audit_capture, trg_rx_rules, trg_touch

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | uuid | NN | `gen_random_uuid()` | **PK** |
| `org_id` | uuid | NN |  | FK→ `clinical.encounter` |
| `encounter_id` | uuid | NN |  | FK→ `clinical.encounter` |
| `patient_id` | uuid | NN |  | FK→ `clinical.encounter` |
| `prescription_no` | text | NN |  |  |
| `prescriber_membership_id` | uuid | NN |  | FK→ `iam.membership` |
| `status` | text | NN | `'draft'::text` |  |
| `signed_at` | timestamp with time zone | null |  |  |
| `valid_until` | date | null |  |  |
| `notes` | text | null |  |  |
| `cancel_reason` | text | null |  |  |
| `created_at` | timestamp with time zone | NN | `now()` |  |
| `created_by` | uuid | null | `sec.current_user_id()` | FK→ `iam.user_account` |
| `updated_at` | timestamp with time zone | NN | `now()` |  |
| `updated_by` | uuid | null |  | FK→ `iam.user_account` |
| `row_version` | integer | NN | `1` |  |

Constraints:

- `ck_rx_cancel` (check): `CHECK (((status <> 'cancelled'::text) OR (cancel_reason IS NOT NULL)))`
- `ck_rx_signed` (check): `CHECK (((status = ANY (ARRAY['draft'::text, 'cancelled'::text])) OR (signed_at IS NOT NULL)))`
- `prescription_org_id_id_key` (unique): `UNIQUE (org_id, id)`
- `prescription_org_id_id_patient_id_key` (unique): `UNIQUE (org_id, id, patient_id)`
- `prescription_org_id_prescription_no_key` (unique): `UNIQUE (org_id, prescription_no)`
- `prescription_status_check` (check): `CHECK ((status = ANY (ARRAY['draft'::text, 'active'::text, 'partially_dispensed'::text, 'dispensed'::text, 'cancelled'::text, 'expired'::text])))`

Foreign keys:

- `prescription_created_by_fkey`: `FOREIGN KEY (created_by) REFERENCES iam.user_account(id)`
- `prescription_org_id_encounter_id_patient_id_fkey`: `FOREIGN KEY (org_id, encounter_id, patient_id) REFERENCES clinical.encounter(org_id, id, patient_id)`
- `prescription_org_id_prescriber_membership_id_fkey`: `FOREIGN KEY (org_id, prescriber_membership_id) REFERENCES iam.membership(org_id, id)`
- `prescription_updated_by_fkey`: `FOREIGN KEY (updated_by) REFERENCES iam.user_account(id)`

Indexes:

- `ix_rx_patient`: `btree (patient_id, created_at DESC)`
- `ix_rx_pharmacy_queue`: `btree (org_id, status, signed_at) WHERE (status = ANY (ARRAY['active'::text, 'partially_dispensed'::text]))`
- `prescription_org_id_id_key`: `btree (org_id, id)` (unique)
- `prescription_org_id_id_patient_id_key`: `btree (org_id, id, patient_id)` (unique)
- `prescription_org_id_prescription_no_key`: `btree (org_id, prescription_no)` (unique)
- `prescription_pkey`: `btree (id)` (unique)

### `pharmacy.prescription_item`

RLS: forced — policies: owner_internal, staff_insert, staff_select, staff_update, tenant_isolation(R)  
Triggers: trg_0_state, trg_audit_capture, trg_rxi_guard, trg_touch

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | uuid | NN | `gen_random_uuid()` | **PK** |
| `org_id` | uuid | NN |  | FK→ `pharmacy.drug` |
| `prescription_id` | uuid | NN |  | FK→ `pharmacy.prescription` |
| `patient_id` | uuid | NN |  | FK→ `pharmacy.prescription` |
| `drug_id` | uuid | null |  | FK→ `pharmacy.drug` |
| `drug_name_text` | text | null |  |  |
| `dose_amount` | numeric(10,3) | NN |  |  |
| `dose_unit` | text | NN |  |  |
| `route` | text | NN |  |  |
| `frequency_code` | text | NN |  |  |
| `duration_days` | smallint | null |  |  |
| `quantity_prescribed` | numeric(12,2) | NN |  |  |
| `quantity_dispensed` | numeric(12,2) | NN | `0` |  |
| `instructions` | text | null |  |  |
| `status` | text | NN | `'pending'::text` |  |
| `created_at` | timestamp with time zone | NN | `now()` |  |
| `created_by` | uuid | null | `sec.current_user_id()` | FK→ `iam.user_account` |
| `updated_at` | timestamp with time zone | NN | `now()` |  |
| `updated_by` | uuid | null |  | FK→ `iam.user_account` |
| `row_version` | integer | NN | `1` |  |

Constraints:

- `ck_rxi_drug` (check): `CHECK ((num_nonnulls(drug_id, drug_name_text) = 1))`
- `ck_rxi_prn_duration` (check): `CHECK (((frequency_code = ANY (ARRAY['STAT'::text, 'PRN'::text])) OR (duration_days IS NOT NULL)))`
- `ck_rxi_qty` (check): `CHECK (((quantity_dispensed >= (0)::numeric) AND (quantity_dispensed <= quantity_prescribed)))`
- `prescription_item_dose_amount_check` (check): `CHECK ((dose_amount > (0)::numeric))`
- `prescription_item_duration_days_check` (check): `CHECK (((duration_days >= 1) AND (duration_days <= 365)))`
- `prescription_item_frequency_code_check` (check): `CHECK ((frequency_code = ANY (ARRAY['STAT'::text, 'OD'::text, 'BD'::text, 'TDS'::text, 'QID'::text, 'Q4H'::text, 'Q6H'::text, 'Q8H'::text, 'NOCTE'::text, 'PRN'::text, 'WEEKLY'::text])))`
- `prescription_item_org_id_id_key` (unique): `UNIQUE (org_id, id)`
- `prescription_item_quantity_prescribed_check` (check): `CHECK ((quantity_prescribed > (0)::numeric))`
- `prescription_item_route_check` (check): `CHECK ((route = ANY (ARRAY['oral'::text, 'iv'::text, 'im'::text, 'sc'::text, 'topical'::text, 'inhaled'::text, 'rectal'::text, 'vaginal'::text, 'ophthalmic'::text, 'otic'::text, 'nasal'::text, 'sublingual'::text, 'other'::text])))`
- `prescription_item_status_check` (check): `CHECK ((status = ANY (ARRAY['pending'::text, 'partially_dispensed'::text, 'dispensed'::text, 'cancelled'::text])))`

Foreign keys:

- `prescription_item_created_by_fkey`: `FOREIGN KEY (created_by) REFERENCES iam.user_account(id)`
- `prescription_item_org_id_drug_id_fkey`: `FOREIGN KEY (org_id, drug_id) REFERENCES pharmacy.drug(org_id, id)`
- `prescription_item_org_id_prescription_id_patient_id_fkey`: `FOREIGN KEY (org_id, prescription_id, patient_id) REFERENCES pharmacy.prescription(org_id, id, patient_id)`
- `prescription_item_updated_by_fkey`: `FOREIGN KEY (updated_by) REFERENCES iam.user_account(id)`

Indexes:

- `ix_rxi_prescription`: `btree (prescription_id)`
- `prescription_item_org_id_id_key`: `btree (org_id, id)` (unique)
- `prescription_item_pkey`: `btree (id)` (unique)

### `pharmacy.stock_batch`

RLS: forced — policies: owner_internal, staff_insert, staff_select, staff_update, tenant_isolation(R)  
Triggers: trg_0_state, trg_batch_guard, trg_touch

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | uuid | NN | `gen_random_uuid()` | **PK** |
| `org_id` | uuid | NN |  | FK→ `pharmacy.drug` |
| `facility_id` | uuid | NN |  | FK→ `org.facility` |
| `drug_id` | uuid | NN |  | FK→ `pharmacy.drug` |
| `batch_no` | text | NN |  |  |
| `expiry_date` | date | NN |  |  |
| `quantity_on_hand` | numeric(12,2) | NN | `0` |  |
| `unit_cost` | numeric(14,2) | null |  |  |
| `status` | text | NN | `'active'::text` |  |
| `received_at` | timestamp with time zone | NN | `now()` |  |
| `created_at` | timestamp with time zone | NN | `now()` |  |
| `created_by` | uuid | null | `sec.current_user_id()` | FK→ `iam.user_account` |
| `updated_at` | timestamp with time zone | NN | `now()` |  |
| `updated_by` | uuid | null |  | FK→ `iam.user_account` |
| `row_version` | integer | NN | `1` |  |

Constraints:

- `stock_batch_facility_id_drug_id_batch_no_key` (unique): `UNIQUE (facility_id, drug_id, batch_no)`
- `stock_batch_org_id_id_key` (unique): `UNIQUE (org_id, id)`
- `stock_batch_quantity_on_hand_check` (check): `CHECK ((quantity_on_hand >= (0)::numeric))`
- `stock_batch_status_check` (check): `CHECK ((status = ANY (ARRAY['active'::text, 'quarantined'::text, 'expired'::text, 'depleted'::text])))`
- `stock_batch_unit_cost_check` (check): `CHECK ((unit_cost >= (0)::numeric))`

Foreign keys:

- `stock_batch_created_by_fkey`: `FOREIGN KEY (created_by) REFERENCES iam.user_account(id)`
- `stock_batch_org_id_drug_id_fkey`: `FOREIGN KEY (org_id, drug_id) REFERENCES pharmacy.drug(org_id, id)`
- `stock_batch_org_id_facility_id_fkey`: `FOREIGN KEY (org_id, facility_id) REFERENCES org.facility(org_id, id)`
- `stock_batch_updated_by_fkey`: `FOREIGN KEY (updated_by) REFERENCES iam.user_account(id)`

Indexes:

- `ix_batch_fefo`: `btree (facility_id, drug_id, expiry_date) WHERE ((status = 'active'::text) AND (quantity_on_hand > (0)::numeric))`
- `stock_batch_facility_id_drug_id_batch_no_key`: `btree (facility_id, drug_id, batch_no)` (unique)
- `stock_batch_org_id_id_key`: `btree (org_id, id)` (unique)
- `stock_batch_pkey`: `btree (id)` (unique)

### `pharmacy.stock_movement`

RLS: forced — policies: owner_internal, staff_insert, staff_select, staff_update, tenant_isolation(R)  
Triggers: trg_movement_append_only, trg_movement_apply

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | uuid | NN | `gen_random_uuid()` | **PK** |
| `org_id` | uuid | NN |  | FK→ `pharmacy.stock_batch` |
| `batch_id` | uuid | NN |  | FK→ `pharmacy.stock_batch` |
| `movement_type` | text | NN |  |  |
| `quantity` | numeric(12,2) | NN |  |  |
| `reason` | text | null |  |  |
| `dispense_item_id` | uuid | null |  | FK→ `pharmacy.dispense_item` |
| `supplier_ref` | text | null |  |  |
| `performed_by` | uuid | NN | `sec.current_user_id()` | FK→ `iam.user_account` |
| `performed_at` | timestamp with time zone | NN | `now()` |  |

Constraints:

- `ck_move_adjust_reason` (check): `CHECK (((movement_type <> ALL (ARRAY['adjustment_up'::text, 'adjustment_down'::text, 'expiry_writeoff'::text])) OR (reason IS NOT NULL)))`
- `ck_move_dispense_link` (check): `CHECK (((movement_type = ANY (ARRAY['dispense'::text, 'dispense_reversal'::text])) = (dispense_item_id IS NOT NULL)))`
- `ck_move_sign` (check): `CHECK ((((movement_type = ANY (ARRAY['receipt'::text, 'dispense_reversal'::text, 'adjustment_up'::text, 'transfer_in'::text])) AND (quantity > (0)::numeric)) OR ((movement_type = ANY (ARRAY['dispense'::text, 'adjustment_down'::text, 'expiry_writeoff'::text, 'return_to_supplier'::text, 'transfer_out'::text])) AND (quantity < (0)::numeric))))`
- `stock_movement_movement_type_check` (check): `CHECK ((movement_type = ANY (ARRAY['receipt'::text, 'dispense'::text, 'dispense_reversal'::text, 'adjustment_up'::text, 'adjustment_down'::text, 'expiry_writeoff'::text, 'return_to_supplier'::text, 'transfer_in'::text, 'transfer_out'::text])))`
- `stock_movement_quantity_check` (check): `CHECK ((quantity <> (0)::numeric))`

Foreign keys:

- `stock_movement_org_id_batch_id_fkey`: `FOREIGN KEY (org_id, batch_id) REFERENCES pharmacy.stock_batch(org_id, id)`
- `stock_movement_org_id_dispense_item_id_fkey`: `FOREIGN KEY (org_id, dispense_item_id) REFERENCES pharmacy.dispense_item(org_id, id)`
- `stock_movement_performed_by_fkey`: `FOREIGN KEY (performed_by) REFERENCES iam.user_account(id)`

Indexes:

- `ix_movement_batch`: `btree (batch_id, performed_at)`
- `stock_movement_pkey`: `btree (id)` (unique)

## Schema `billing`

### `billing.cashier_shift`

RLS: forced — policies: owner_internal, staff_insert, staff_select, staff_update, tenant_isolation(R)  
Triggers: trg_0_state, trg_shift_rules, trg_touch

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | uuid | NN | `gen_random_uuid()` | **PK** |
| `org_id` | uuid | NN |  | FK→ `org.facility` |
| `facility_id` | uuid | NN |  | FK→ `org.facility` |
| `membership_id` | uuid | NN |  | FK→ `iam.membership` |
| `opened_at` | timestamp with time zone | NN | `now()` |  |
| `opening_float` | numeric(14,2) | NN | `0` |  |
| `closed_at` | timestamp with time zone | null |  |  |
| `counted_cash` | numeric(14,2) | null |  |  |
| `expected_cash` | numeric(14,2) | null |  |  |
| `variance` | numeric(14,2) | null | GENERATED: `(counted_cash - expected_cash)` |  |
| `status` | text | NN | `'open'::text` |  |
| `reconciled_by` | uuid | null |  | FK→ `iam.user_account` |
| `reconciled_at` | timestamp with time zone | null |  |  |
| `variance_note` | text | null |  |  |
| `created_at` | timestamp with time zone | NN | `now()` |  |
| `created_by` | uuid | null | `sec.current_user_id()` | FK→ `iam.user_account` |
| `updated_at` | timestamp with time zone | NN | `now()` |  |
| `updated_by` | uuid | null |  | FK→ `iam.user_account` |
| `row_version` | integer | NN | `1` |  |

Constraints:

- `cashier_shift_counted_cash_check` (check): `CHECK ((counted_cash >= (0)::numeric))`
- `cashier_shift_opening_float_check` (check): `CHECK ((opening_float >= (0)::numeric))`
- `cashier_shift_org_id_id_key` (unique): `UNIQUE (org_id, id)`
- `cashier_shift_status_check` (check): `CHECK ((status = ANY (ARRAY['open'::text, 'closed'::text, 'reconciled'::text])))`
- `ck_shift_closed` (check): `CHECK (((status = 'open'::text) OR ((closed_at IS NOT NULL) AND (counted_cash IS NOT NULL) AND (expected_cash IS NOT NULL))))`
- `ck_shift_reconciled` (check): `CHECK (((status = 'reconciled'::text) = (reconciled_at IS NOT NULL)))`

Foreign keys:

- `cashier_shift_created_by_fkey`: `FOREIGN KEY (created_by) REFERENCES iam.user_account(id)`
- `cashier_shift_org_id_facility_id_fkey`: `FOREIGN KEY (org_id, facility_id) REFERENCES org.facility(org_id, id)`
- `cashier_shift_org_id_membership_id_fkey`: `FOREIGN KEY (org_id, membership_id) REFERENCES iam.membership(org_id, id)`
- `cashier_shift_reconciled_by_fkey`: `FOREIGN KEY (reconciled_by) REFERENCES iam.user_account(id)`
- `cashier_shift_updated_by_fkey`: `FOREIGN KEY (updated_by) REFERENCES iam.user_account(id)`

Indexes:

- `cashier_shift_org_id_id_key`: `btree (org_id, id)` (unique)
- `cashier_shift_pkey`: `btree (id)` (unique)
- `ux_shift_open`: `btree (membership_id) WHERE (status = 'open'::text)` (unique)

### `billing.coverage`

RLS: forced — policies: owner_internal, staff_insert, staff_select, staff_update, tenant_isolation(R)  
Triggers: trg_0_state, trg_audit_capture, trg_touch

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | uuid | NN | `gen_random_uuid()` | **PK** |
| `org_id` | uuid | NN |  | FK→ `patient.patient` |
| `patient_id` | uuid | NN |  | FK→ `patient.patient` |
| `payer_id` | uuid | NN |  | FK→ `billing.payer` |
| `member_no_ciphertext` | bytea | NN |  |  |
| `member_no_bidx` | bytea | NN |  |  |
| `member_no_last4` | text | null |  |  |
| `key_ref` | text | NN |  |  |
| `scheme_name` | text | null |  |  |
| `valid_from` | date | NN |  |  |
| `valid_to` | date | null |  |  |
| `status` | text | NN | `'active'::text` |  |
| `last_verified_at` | timestamp with time zone | null |  |  |
| `created_at` | timestamp with time zone | NN | `now()` |  |
| `created_by` | uuid | null | `sec.current_user_id()` | FK→ `iam.user_account` |
| `updated_at` | timestamp with time zone | NN | `now()` |  |
| `updated_by` | uuid | null |  | FK→ `iam.user_account` |
| `row_version` | integer | NN | `1` |  |

Constraints:

- `ck_coverage_dates` (check): `CHECK (((valid_to IS NULL) OR (valid_to >= valid_from)))`
- `coverage_member_no_bidx_check` (check): `CHECK ((length(member_no_bidx) = 32))`
- `coverage_org_id_id_key` (unique): `UNIQUE (org_id, id)`
- `coverage_status_check` (check): `CHECK ((status = ANY (ARRAY['active'::text, 'expired'::text, 'suspended'::text, 'cancelled'::text])))`

Foreign keys:

- `coverage_created_by_fkey`: `FOREIGN KEY (created_by) REFERENCES iam.user_account(id)`
- `coverage_org_id_patient_id_fkey`: `FOREIGN KEY (org_id, patient_id) REFERENCES patient.patient(org_id, id)`
- `coverage_org_id_payer_id_fkey`: `FOREIGN KEY (org_id, payer_id) REFERENCES billing.payer(org_id, id)`
- `coverage_updated_by_fkey`: `FOREIGN KEY (updated_by) REFERENCES iam.user_account(id)`

Indexes:

- `coverage_org_id_id_key`: `btree (org_id, id)` (unique)
- `coverage_pkey`: `btree (id)` (unique)
- `ix_coverage_patient`: `btree (patient_id)`
- `ux_coverage_member`: `btree (payer_id, member_no_bidx) WHERE (status = 'active'::text)` (unique)

### `billing.invoice`

RLS: forced — policies: owner_internal, patient_self, staff_insert, staff_select, staff_update, tenant_isolation(R)  
Triggers: trg_0_state, trg_audit_capture, trg_invoice_rules, trg_invoice_void, trg_touch

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | uuid | NN | `gen_random_uuid()` | **PK** |
| `org_id` | uuid | NN |  | FK→ `clinical.encounter` |
| `facility_id` | uuid | NN |  | FK→ `org.facility` |
| `patient_id` | uuid | NN |  | FK→ `clinical.encounter` |
| `encounter_id` | uuid | null |  | FK→ `clinical.encounter` |
| `payer_id` | uuid | NN |  | FK→ `billing.payer` |
| `invoice_no` | text | null |  |  |
| `status` | text | NN | `'draft'::text` |  |
| `currency_code` | character(3) | NN | `'TZS'::bpchar` |  |
| `subtotal` | numeric(14,2) | NN | `0` |  |
| `discount_total` | numeric(14,2) | NN | `0` |  |
| `total` | numeric(14,2) | NN | `0` |  |
| `amount_paid` | numeric(14,2) | NN | `0` |  |
| `balance` | numeric(14,2) | null | GENERATED: `(total - amount_paid)` |  |
| `issued_at` | timestamp with time zone | null |  |  |
| `issued_by` | uuid | null |  | FK→ `iam.user_account` |
| `due_date` | date | null |  |  |
| `voided_at` | timestamp with time zone | null |  |  |
| `voided_by` | uuid | null |  | FK→ `iam.user_account` |
| `void_reason` | text | null |  |  |
| `created_at` | timestamp with time zone | NN | `now()` |  |
| `created_by` | uuid | null | `sec.current_user_id()` | FK→ `iam.user_account` |
| `updated_at` | timestamp with time zone | NN | `now()` |  |
| `updated_by` | uuid | null |  | FK→ `iam.user_account` |
| `row_version` | integer | NN | `1` |  |

Constraints:

- `ck_inv_issued` (check): `CHECK (((status = 'draft'::text) OR ((invoice_no IS NOT NULL) AND (issued_at IS NOT NULL)) OR ((status = 'void'::text) AND (invoice_no IS NULL))))`
- `ck_inv_paid` (check): `CHECK ((amount_paid <= total))`
- `ck_inv_status_paid` (check): `CHECK (((status <> 'paid'::text) OR (amount_paid = total)))`
- `ck_inv_totals` (check): `CHECK ((total = (subtotal - discount_total)))`
- `ck_inv_void` (check): `CHECK ((((status = 'void'::text) = (voided_at IS NOT NULL)) AND ((status <> 'void'::text) OR ((void_reason IS NOT NULL) AND (amount_paid = (0)::numeric)))))`
- `invoice_amount_paid_check` (check): `CHECK ((amount_paid >= (0)::numeric))`
- `invoice_discount_total_check` (check): `CHECK ((discount_total >= (0)::numeric))`
- `invoice_org_id_id_key` (unique): `UNIQUE (org_id, id)`
- `invoice_org_id_invoice_no_key` (unique): `UNIQUE (org_id, invoice_no)`
- `invoice_status_check` (check): `CHECK ((status = ANY (ARRAY['draft'::text, 'issued'::text, 'partially_paid'::text, 'paid'::text, 'void'::text])))`
- `invoice_subtotal_check` (check): `CHECK ((subtotal >= (0)::numeric))`
- `invoice_total_check` (check): `CHECK ((total >= (0)::numeric))`

Foreign keys:

- `invoice_created_by_fkey`: `FOREIGN KEY (created_by) REFERENCES iam.user_account(id)`
- `invoice_issued_by_fkey`: `FOREIGN KEY (issued_by) REFERENCES iam.user_account(id)`
- `invoice_org_id_encounter_id_patient_id_fkey`: `FOREIGN KEY (org_id, encounter_id, patient_id) REFERENCES clinical.encounter(org_id, id, patient_id)`
- `invoice_org_id_facility_id_fkey`: `FOREIGN KEY (org_id, facility_id) REFERENCES org.facility(org_id, id)`
- `invoice_org_id_patient_id_fkey`: `FOREIGN KEY (org_id, patient_id) REFERENCES patient.patient(org_id, id)`
- `invoice_org_id_payer_id_fkey`: `FOREIGN KEY (org_id, payer_id) REFERENCES billing.payer(org_id, id)`
- `invoice_updated_by_fkey`: `FOREIGN KEY (updated_by) REFERENCES iam.user_account(id)`
- `invoice_voided_by_fkey`: `FOREIGN KEY (voided_by) REFERENCES iam.user_account(id)`

Indexes:

- `invoice_org_id_id_key`: `btree (org_id, id)` (unique)
- `invoice_org_id_invoice_no_key`: `btree (org_id, invoice_no)` (unique)
- `invoice_pkey`: `btree (id)` (unique)
- `ix_invoice_issued_day`: `btree (org_id, issued_at) WHERE (status <> 'draft'::text)`
- `ix_invoice_open`: `btree (org_id, facility_id, issued_at) WHERE (status = ANY (ARRAY['issued'::text, 'partially_paid'::text]))`
- `ix_invoice_patient`: `btree (patient_id, created_at DESC)`
- `ux_invoice_draft`: `btree (encounter_id, payer_id) WHERE ((status = 'draft'::text) AND (encounter_id IS NOT NULL))` (unique)

### `billing.invoice_line`

RLS: forced — policies: owner_internal, staff_delete, staff_insert, staff_select, staff_update, tenant_isolation(R)  
Triggers: trg_audit_capture, trg_line_guard, trg_line_recalc

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | uuid | NN | `gen_random_uuid()` | **PK** |
| `org_id` | uuid | NN |  | FK→ `clinical.procedure` |
| `invoice_id` | uuid | NN |  | FK→ `billing.invoice` |
| `service_id` | uuid | NN |  | FK→ `billing.service_catalog` |
| `source_type` | text | NN |  |  |
| `consultation_id` | uuid | null |  | FK→ `clinical.consultation` |
| `diag_request_item_id` | uuid | null |  | FK→ `diag.request_item` |
| `dispense_item_id` | uuid | null |  | FK→ `pharmacy.dispense_item` |
| `description` | text | NN |  |  |
| `quantity` | numeric(10,2) | NN |  |  |
| `unit_price` | numeric(14,2) | NN |  |  |
| `discount_amount` | numeric(14,2) | NN | `0` |  |
| `discount_reason` | text | null |  |  |
| `line_total` | numeric(14,2) | null | GENERATED: `(round((quantity * unit_price), 2) - discount_amount)` |  |
| `voided` | boolean | NN | `false` |  |
| `created_at` | timestamp with time zone | NN | `now()` |  |
| `created_by` | uuid | null | `sec.current_user_id()` | FK→ `iam.user_account` |
| `procedure_id` | uuid | null |  | FK→ `clinical.procedure` |

Constraints:

- `ck_line_discount` (check): `CHECK ((discount_amount <= round((quantity * unit_price), 2)))`
- `ck_line_discount_reason` (check): `CHECK (((discount_amount = (0)::numeric) OR (discount_reason IS NOT NULL)))`
- `ck_line_source` (check): `CHECK ((((source_type = 'diagnostic'::text) AND (diag_request_item_id IS NOT NULL) AND (num_nonnulls(consultation_id, dispense_item_id, procedure_id) = 0)) OR ((source_type = 'pharmacy'::text) AND (dispense_item_id IS NOT NULL) AND (num_nonnulls(consultation_id, diag_request_item_id, procedure_id) = 0)) OR ((source_type = 'procedure'::text) AND (procedure_id IS NOT NULL) AND (num_nonnulls(consultation_id, diag_request_item_id, dispense_item_id) = 0)) OR ((source_type = 'consultation'::text) AND (num_nonnulls(diag_request_item_id, dispense_item_id, procedure_id) = 0)) OR ((source_type = ANY (ARRAY['registration'::text, 'telemedicine'::text, 'manual'::text])) AND (num_nonnulls(consultation_id, diag_request_item_id, dispense_item_id, procedure_id) = 0))))`
- `invoice_line_discount_amount_check` (check): `CHECK ((discount_amount >= (0)::numeric))`
- `invoice_line_org_id_id_key` (unique): `UNIQUE (org_id, id)`
- `invoice_line_quantity_check` (check): `CHECK ((quantity > (0)::numeric))`
- `invoice_line_source_type_check` (check): `CHECK ((source_type = ANY (ARRAY['registration'::text, 'consultation'::text, 'diagnostic'::text, 'pharmacy'::text, 'procedure'::text, 'telemedicine'::text, 'manual'::text])))`
- `invoice_line_unit_price_check` (check): `CHECK ((unit_price >= (0)::numeric))`

Foreign keys:

- `fk_line_procedure`: `FOREIGN KEY (org_id, procedure_id) REFERENCES clinical.procedure(org_id, id)`
- `invoice_line_created_by_fkey`: `FOREIGN KEY (created_by) REFERENCES iam.user_account(id)`
- `invoice_line_org_id_consultation_id_fkey`: `FOREIGN KEY (org_id, consultation_id) REFERENCES clinical.consultation(org_id, id)`
- `invoice_line_org_id_diag_request_item_id_fkey`: `FOREIGN KEY (org_id, diag_request_item_id) REFERENCES diag.request_item(org_id, id)`
- `invoice_line_org_id_dispense_item_id_fkey`: `FOREIGN KEY (org_id, dispense_item_id) REFERENCES pharmacy.dispense_item(org_id, id)`
- `invoice_line_org_id_invoice_id_fkey`: `FOREIGN KEY (org_id, invoice_id) REFERENCES billing.invoice(org_id, id)`
- `invoice_line_org_id_service_id_fkey`: `FOREIGN KEY (org_id, service_id) REFERENCES billing.service_catalog(org_id, id)`

Indexes:

- `invoice_line_org_id_id_key`: `btree (org_id, id)` (unique)
- `invoice_line_pkey`: `btree (id)` (unique)
- `ix_line_invoice`: `btree (invoice_id)`
- `ux_line_consult`: `btree (consultation_id) WHERE ((consultation_id IS NOT NULL) AND (NOT voided))` (unique)
- `ux_line_diag`: `btree (diag_request_item_id) WHERE ((diag_request_item_id IS NOT NULL) AND (NOT voided))` (unique)
- `ux_line_dispense`: `btree (dispense_item_id) WHERE ((dispense_item_id IS NOT NULL) AND (NOT voided))` (unique)
- `ux_line_procedure`: `btree (procedure_id) WHERE ((procedure_id IS NOT NULL) AND (NOT voided))` (unique)

### `billing.payer`

RLS: forced — policies: owner_internal, staff_insert, staff_select, staff_update, tenant_isolation(R)  
Triggers: trg_touch

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | uuid | NN | `gen_random_uuid()` | **PK** |
| `org_id` | uuid | NN |  | FK→ `org.organization` |
| `code` | text | NN |  |  |
| `name` | text | NN |  |  |
| `payer_type` | text | NN |  |  |
| `status` | text | NN | `'active'::text` |  |
| `created_at` | timestamp with time zone | NN | `now()` |  |
| `created_by` | uuid | null | `sec.current_user_id()` | FK→ `iam.user_account` |
| `updated_at` | timestamp with time zone | NN | `now()` |  |
| `updated_by` | uuid | null |  | FK→ `iam.user_account` |
| `row_version` | integer | NN | `1` |  |

Constraints:

- `payer_code_check` (check): `CHECK ((code ~ '^[A-Z0-9_-]{2,30}$'::text))`
- `payer_org_id_code_key` (unique): `UNIQUE (org_id, code)`
- `payer_org_id_id_key` (unique): `UNIQUE (org_id, id)`
- `payer_payer_type_check` (check): `CHECK ((payer_type = ANY (ARRAY['self_pay'::text, 'insurance'::text, 'corporate'::text, 'government'::text])))`
- `payer_status_check` (check): `CHECK ((status = ANY (ARRAY['active'::text, 'inactive'::text])))`

Foreign keys:

- `payer_created_by_fkey`: `FOREIGN KEY (created_by) REFERENCES iam.user_account(id)`
- `payer_org_id_fkey`: `FOREIGN KEY (org_id) REFERENCES org.organization(id)`
- `payer_updated_by_fkey`: `FOREIGN KEY (updated_by) REFERENCES iam.user_account(id)`

Indexes:

- `payer_org_id_code_key`: `btree (org_id, code)` (unique)
- `payer_org_id_id_key`: `btree (org_id, id)` (unique)
- `payer_pkey`: `btree (id)` (unique)

### `billing.payment`

RLS: forced — policies: owner_internal, patient_self, staff_insert, staff_select, staff_update, tenant_isolation(R)  
Triggers: trg_0_state, trg_audit_capture, trg_payment_rules, trg_payment_status, trg_touch

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | uuid | NN | `gen_random_uuid()` | **PK** |
| `org_id` | uuid | NN |  | FK→ `billing.cashier_shift` |
| `facility_id` | uuid | NN |  | FK→ `org.facility` |
| `patient_id` | uuid | null |  | FK→ `patient.patient` |
| `payer_id` | uuid | null |  | FK→ `billing.payer` |
| `method` | text | NN |  |  |
| `provider` | text | null |  |  |
| `provider_reference` | text | null |  |  |
| `amount` | numeric(14,2) | NN |  |  |
| `currency_code` | character(3) | NN | `'TZS'::bpchar` |  |
| `status` | text | NN |  |  |
| `idempotency_key` | uuid | NN |  |  |
| `cashier_shift_id` | uuid | null |  | FK→ `billing.cashier_shift` |
| `received_by_membership_id` | uuid | null |  | FK→ `iam.membership` |
| `received_at` | timestamp with time zone | null |  |  |
| `failure_reason` | text | null |  |  |
| `reversed_at` | timestamp with time zone | null |  |  |
| `reversal_reason` | text | null |  |  |
| `created_at` | timestamp with time zone | NN | `now()` |  |
| `created_by` | uuid | null | `sec.current_user_id()` | FK→ `iam.user_account` |
| `updated_at` | timestamp with time zone | NN | `now()` |  |
| `updated_by` | uuid | null |  | FK→ `iam.user_account` |
| `row_version` | integer | NN | `1` |  |

Constraints:

- `ck_pay_cash_shift` (check): `CHECK (((method <> 'cash'::text) OR (cashier_shift_id IS NOT NULL)))`
- `ck_pay_completed` (check): `CHECK (((status <> ALL (ARRAY['completed'::text, 'reversed'::text])) OR (received_at IS NOT NULL)))`
- `ck_pay_completed_ref` (check): `CHECK (((status <> 'completed'::text) OR (method = 'cash'::text) OR (provider_reference IS NOT NULL)))`
- `ck_pay_electronic_ref` (check): `CHECK (((method = ANY (ARRAY['cash'::text, 'insurance'::text])) OR (provider IS NOT NULL)))`
- `ck_pay_failed` (check): `CHECK (((status <> 'failed'::text) OR (failure_reason IS NOT NULL)))`
- `ck_pay_party` (check): `CHECK ((num_nonnulls(patient_id, payer_id) >= 1))`
- `ck_pay_reversed` (check): `CHECK ((((status = 'reversed'::text) = (reversed_at IS NOT NULL)) AND ((status <> 'reversed'::text) OR (reversal_reason IS NOT NULL))))`
- `payment_amount_check` (check): `CHECK ((amount > (0)::numeric))`
- `payment_method_check` (check): `CHECK ((method = ANY (ARRAY['cash'::text, 'mobile_money'::text, 'card'::text, 'bank_transfer'::text, 'insurance'::text])))`
- `payment_org_id_id_key` (unique): `UNIQUE (org_id, id)`
- `payment_org_id_idempotency_key_key` (unique): `UNIQUE (org_id, idempotency_key)`
- `payment_provider_check` (check): `CHECK ((provider = ANY (ARRAY['mpesa'::text, 'airtel_money'::text, 'mixx_by_yas'::text, 'halopesa'::text, 'selcom'::text, 'azampay'::text, 'bank'::text, 'nhif'::text, 'other'::text])))`
- `payment_status_check` (check): `CHECK ((status = ANY (ARRAY['pending'::text, 'completed'::text, 'failed'::text, 'cancelled'::text, 'reversed'::text])))`

Foreign keys:

- `payment_created_by_fkey`: `FOREIGN KEY (created_by) REFERENCES iam.user_account(id)`
- `payment_org_id_cashier_shift_id_fkey`: `FOREIGN KEY (org_id, cashier_shift_id) REFERENCES billing.cashier_shift(org_id, id)`
- `payment_org_id_facility_id_fkey`: `FOREIGN KEY (org_id, facility_id) REFERENCES org.facility(org_id, id)`
- `payment_org_id_patient_id_fkey`: `FOREIGN KEY (org_id, patient_id) REFERENCES patient.patient(org_id, id)`
- `payment_org_id_payer_id_fkey`: `FOREIGN KEY (org_id, payer_id) REFERENCES billing.payer(org_id, id)`
- `payment_org_id_received_by_membership_id_fkey`: `FOREIGN KEY (org_id, received_by_membership_id) REFERENCES iam.membership(org_id, id)`
- `payment_updated_by_fkey`: `FOREIGN KEY (updated_by) REFERENCES iam.user_account(id)`

Indexes:

- `ix_payment_day`: `btree (org_id, facility_id, received_at) WHERE (status = 'completed'::text)`
- `ix_payment_pending`: `btree (created_at) WHERE (status = 'pending'::text)`
- `ix_payment_shift`: `btree (cashier_shift_id) WHERE (cashier_shift_id IS NOT NULL)`
- `payment_org_id_id_key`: `btree (org_id, id)` (unique)
- `payment_org_id_idempotency_key_key`: `btree (org_id, idempotency_key)` (unique)
- `payment_pkey`: `btree (id)` (unique)
- `ux_payment_provider_ref`: `btree (org_id, provider, provider_reference) WHERE (provider_reference IS NOT NULL)` (unique)

### `billing.payment_allocation`

RLS: forced — policies: owner_internal, staff_insert, staff_select, staff_update, tenant_isolation(R)  
Triggers: trg_alloc_append_only, trg_alloc_recalc, trg_alloc_rules

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | uuid | NN | `gen_random_uuid()` | **PK** |
| `org_id` | uuid | NN |  | FK→ `billing.invoice` |
| `payment_id` | uuid | NN |  | FK→ `billing.payment` |
| `invoice_id` | uuid | NN |  | FK→ `billing.invoice` |
| `amount` | numeric(14,2) | NN |  |  |
| `allocated_at` | timestamp with time zone | NN | `now()` |  |
| `allocated_by` | uuid | null | `sec.current_user_id()` | FK→ `iam.user_account` |

Constraints:

- `payment_allocation_amount_check` (check): `CHECK ((amount > (0)::numeric))`
- `payment_allocation_payment_id_invoice_id_key` (unique): `UNIQUE (payment_id, invoice_id)`

Foreign keys:

- `payment_allocation_allocated_by_fkey`: `FOREIGN KEY (allocated_by) REFERENCES iam.user_account(id)`
- `payment_allocation_org_id_invoice_id_fkey`: `FOREIGN KEY (org_id, invoice_id) REFERENCES billing.invoice(org_id, id)`
- `payment_allocation_org_id_payment_id_fkey`: `FOREIGN KEY (org_id, payment_id) REFERENCES billing.payment(org_id, id)`

Indexes:

- `ix_alloc_invoice`: `btree (invoice_id)`
- `payment_allocation_payment_id_invoice_id_key`: `btree (payment_id, invoice_id)` (unique)
- `payment_allocation_pkey`: `btree (id)` (unique)

### `billing.price`

RLS: forced — policies: owner_internal, staff_insert, staff_select, staff_update, tenant_isolation(R)  
Triggers: trg_audit_capture

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | uuid | NN | `gen_random_uuid()` | **PK** |
| `org_id` | uuid | NN |  | FK→ `billing.payer` |
| `service_id` | uuid | NN |  | FK→ `billing.service_catalog` |
| `payer_id` | uuid | null |  | FK→ `billing.payer` |
| `unit_price` | numeric(14,2) | NN |  |  |
| `effective` | tstzrange | NN |  |  |
| `created_at` | timestamp with time zone | NN | `now()` |  |
| `created_by` | uuid | null | `sec.current_user_id()` | FK→ `iam.user_account` |

Constraints:

- `ex_price_overlap` (exclusion): `EXCLUDE USING gist (service_id WITH =, COALESCE(payer_id, '00000000-0000-0000-0000-000000000000'::uuid) WITH =, effective WITH &&)`
- `price_effective_check` (check): `CHECK (((NOT isempty(effective)) AND (lower(effective) IS NOT NULL)))`
- `price_unit_price_check` (check): `CHECK ((unit_price >= (0)::numeric))`

Foreign keys:

- `price_created_by_fkey`: `FOREIGN KEY (created_by) REFERENCES iam.user_account(id)`
- `price_org_id_payer_id_fkey`: `FOREIGN KEY (org_id, payer_id) REFERENCES billing.payer(org_id, id)`
- `price_org_id_service_id_fkey`: `FOREIGN KEY (org_id, service_id) REFERENCES billing.service_catalog(org_id, id)`

Indexes:

- `ex_price_overlap`: `gist (service_id, COALESCE(payer_id, '00000000-0000-0000-0000-000000000000'::uuid), effective)`
- `price_pkey`: `btree (id)` (unique)

### `billing.receipt`

RLS: forced — policies: owner_internal, staff_insert, staff_select, staff_update, tenant_isolation(R)  
Triggers: trg_receipt_guard, trg_receipt_no_delete

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | uuid | NN | `gen_random_uuid()` | **PK** |
| `org_id` | uuid | NN |  | FK→ `billing.payment` |
| `payment_id` | uuid | NN |  | FK→ `billing.payment` |
| `receipt_no` | text | NN |  |  |
| `issued_at` | timestamp with time zone | NN | `now()` |  |
| `issued_by` | uuid | null |  | FK→ `iam.user_account` |
| `print_count` | integer | NN | `0` |  |
| `fiscal_receipt_ref` | text | null |  |  |

Constraints:

- `receipt_org_id_receipt_no_key` (unique): `UNIQUE (org_id, receipt_no)`
- `receipt_payment_id_key` (unique): `UNIQUE (payment_id)`
- `receipt_print_count_check` (check): `CHECK ((print_count >= 0))`

Foreign keys:

- `receipt_issued_by_fkey`: `FOREIGN KEY (issued_by) REFERENCES iam.user_account(id)`
- `receipt_org_id_payment_id_fkey`: `FOREIGN KEY (org_id, payment_id) REFERENCES billing.payment(org_id, id)`

Indexes:

- `receipt_org_id_receipt_no_key`: `btree (org_id, receipt_no)` (unique)
- `receipt_payment_id_key`: `btree (payment_id)` (unique)
- `receipt_pkey`: `btree (id)` (unique)

### `billing.refund`

RLS: forced — policies: owner_internal, staff_insert, staff_select, staff_update, tenant_isolation(R)  
Triggers: trg_0_state, trg_audit_capture, trg_refund_rules, trg_touch

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | uuid | NN | `gen_random_uuid()` | **PK** |
| `org_id` | uuid | NN |  | FK→ `billing.payment` |
| `payment_id` | uuid | NN |  | FK→ `billing.payment` |
| `amount` | numeric(14,2) | NN |  |  |
| `reason` | text | NN |  |  |
| `status` | text | NN | `'requested'::text` |  |
| `requested_by` | uuid | NN | `sec.current_user_id()` | FK→ `iam.user_account` |
| `approved_by` | uuid | null |  | FK→ `iam.user_account` |
| `decided_at` | timestamp with time zone | null |  |  |
| `paid_at` | timestamp with time zone | null |  |  |
| `payout_reference` | text | null |  |  |
| `created_at` | timestamp with time zone | NN | `now()` |  |
| `created_by` | uuid | null | `sec.current_user_id()` | FK→ `iam.user_account` |
| `updated_at` | timestamp with time zone | NN | `now()` |  |
| `updated_by` | uuid | null |  | FK→ `iam.user_account` |
| `row_version` | integer | NN | `1` |  |

Constraints:

- `ck_refund_decided` (check): `CHECK (((status = 'requested'::text) OR ((approved_by IS NOT NULL) AND (decided_at IS NOT NULL))))`
- `ck_refund_four_eyes` (check): `CHECK (((approved_by IS NULL) OR (approved_by <> requested_by)))`
- `ck_refund_paid` (check): `CHECK (((status = 'paid'::text) = (paid_at IS NOT NULL)))`
- `refund_amount_check` (check): `CHECK ((amount > (0)::numeric))`
- `refund_reason_check` (check): `CHECK ((length(btrim(reason)) >= 10))`
- `refund_status_check` (check): `CHECK ((status = ANY (ARRAY['requested'::text, 'approved'::text, 'rejected'::text, 'paid'::text])))`

Foreign keys:

- `refund_approved_by_fkey`: `FOREIGN KEY (approved_by) REFERENCES iam.user_account(id)`
- `refund_created_by_fkey`: `FOREIGN KEY (created_by) REFERENCES iam.user_account(id)`
- `refund_org_id_payment_id_fkey`: `FOREIGN KEY (org_id, payment_id) REFERENCES billing.payment(org_id, id)`
- `refund_requested_by_fkey`: `FOREIGN KEY (requested_by) REFERENCES iam.user_account(id)`
- `refund_updated_by_fkey`: `FOREIGN KEY (updated_by) REFERENCES iam.user_account(id)`

Indexes:

- `refund_pkey`: `btree (id)` (unique)

### `billing.service_catalog`

RLS: forced — policies: owner_internal, staff_insert, staff_select, staff_update, tenant_isolation(R)  
Triggers: trg_touch

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | uuid | NN | `gen_random_uuid()` | **PK** |
| `org_id` | uuid | NN |  | FK→ `diag.test_catalog` |
| `code` | text | NN |  |  |
| `name` | text | NN |  |  |
| `category` | text | NN |  |  |
| `diag_test_id` | uuid | null |  | FK→ `diag.test_catalog` |
| `drug_id` | uuid | null |  | FK→ `pharmacy.drug` |
| `status` | text | NN | `'active'::text` |  |
| `created_at` | timestamp with time zone | NN | `now()` |  |
| `created_by` | uuid | null | `sec.current_user_id()` | FK→ `iam.user_account` |
| `updated_at` | timestamp with time zone | NN | `now()` |  |
| `updated_by` | uuid | null |  | FK→ `iam.user_account` |
| `row_version` | integer | NN | `1` |  |

Constraints:

- `ck_service_link` (check): `CHECK ((((category = ANY (ARRAY['laboratory'::text, 'imaging'::text])) AND (diag_test_id IS NOT NULL) AND (drug_id IS NULL)) OR ((category = 'pharmacy'::text) AND (drug_id IS NOT NULL) AND (diag_test_id IS NULL)) OR ((category <> ALL (ARRAY['laboratory'::text, 'imaging'::text, 'pharmacy'::text])) AND (diag_test_id IS NULL) AND (drug_id IS NULL))))`
- `service_catalog_category_check` (check): `CHECK ((category = ANY (ARRAY['registration'::text, 'consultation'::text, 'laboratory'::text, 'imaging'::text, 'pharmacy'::text, 'procedure'::text, 'telemedicine'::text, 'other'::text])))`
- `service_catalog_code_check` (check): `CHECK ((code ~ '^[A-Z0-9_-]{2,30}$'::text))`
- `service_catalog_org_id_code_key` (unique): `UNIQUE (org_id, code)`
- `service_catalog_org_id_id_key` (unique): `UNIQUE (org_id, id)`
- `service_catalog_status_check` (check): `CHECK ((status = ANY (ARRAY['active'::text, 'retired'::text])))`

Foreign keys:

- `service_catalog_created_by_fkey`: `FOREIGN KEY (created_by) REFERENCES iam.user_account(id)`
- `service_catalog_org_id_diag_test_id_fkey`: `FOREIGN KEY (org_id, diag_test_id) REFERENCES diag.test_catalog(org_id, id)`
- `service_catalog_org_id_drug_id_fkey`: `FOREIGN KEY (org_id, drug_id) REFERENCES pharmacy.drug(org_id, id)`
- `service_catalog_org_id_fkey`: `FOREIGN KEY (org_id) REFERENCES org.organization(id)`
- `service_catalog_updated_by_fkey`: `FOREIGN KEY (updated_by) REFERENCES iam.user_account(id)`

Indexes:

- `service_catalog_org_id_code_key`: `btree (org_id, code)` (unique)
- `service_catalog_org_id_id_key`: `btree (org_id, id)` (unique)
- `service_catalog_pkey`: `btree (id)` (unique)
- `ux_service_drug`: `btree (drug_id) WHERE ((drug_id IS NOT NULL) AND (status = 'active'::text))` (unique)
- `ux_service_test`: `btree (diag_test_id) WHERE ((diag_test_id IS NOT NULL) AND (status = 'active'::text))` (unique)

## Schema `telehealth`

### `telehealth.session`

RLS: forced — policies: owner_internal, patient_join, patient_self, staff_insert, staff_select, staff_update, tenant_isolation(R)  
Triggers: trg_0_state, trg_1_patient_guard, trg_audit_capture, trg_tele_rules, trg_touch

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | uuid | NN | `gen_random_uuid()` | **PK** |
| `org_id` | uuid | NN |  | FK→ `clinical.appointment` |
| `appointment_id` | uuid | NN |  | FK→ `clinical.appointment` |
| `encounter_id` | uuid | null |  | FK→ `clinical.encounter` |
| `patient_id` | uuid | NN |  | FK→ `clinical.appointment` |
| `practitioner_membership_id` | uuid | NN |  | FK→ `iam.membership` |
| `consent_id` | uuid | NN |  | FK→ `patient.consent` |
| `provider` | text | NN |  |  |
| `provider_room_ref` | text | null |  |  |
| `scheduled_start` | timestamp with time zone | NN |  |  |
| `status` | text | NN | `'scheduled'::text` |  |
| `started_at` | timestamp with time zone | null |  |  |
| `ended_at` | timestamp with time zone | null |  |  |
| `end_reason` | text | null |  |  |
| `recording_enabled` | boolean | NN | `false` |  |
| `created_at` | timestamp with time zone | NN | `now()` |  |
| `created_by` | uuid | null | `sec.current_user_id()` | FK→ `iam.user_account` |
| `updated_at` | timestamp with time zone | NN | `now()` |  |
| `updated_by` | uuid | null |  | FK→ `iam.user_account` |
| `row_version` | integer | NN | `1` |  |

Constraints:

- `ck_tele_recording` (check): `CHECK ((NOT recording_enabled))`
- `ck_tele_times` (check): `CHECK (((ended_at IS NULL) OR ((started_at IS NOT NULL) AND (ended_at >= started_at))))`
- `session_appointment_id_key` (unique): `UNIQUE (appointment_id)`
- `session_org_id_id_key` (unique): `UNIQUE (org_id, id)`
- `session_provider_check` (check): `CHECK ((provider = ANY (ARRAY['jitsi_self_hosted'::text, 'daily'::text, 'twilio'::text, 'agora'::text, 'other'::text])))`
- `session_status_check` (check): `CHECK ((status = ANY (ARRAY['scheduled'::text, 'waiting_room'::text, 'in_progress'::text, 'completed'::text, 'cancelled'::text, 'failed'::text, 'no_show'::text])))`

Foreign keys:

- `session_created_by_fkey`: `FOREIGN KEY (created_by) REFERENCES iam.user_account(id)`
- `session_org_id_appointment_id_patient_id_fkey`: `FOREIGN KEY (org_id, appointment_id, patient_id) REFERENCES clinical.appointment(org_id, id, patient_id)`
- `session_org_id_consent_id_fkey`: `FOREIGN KEY (org_id, consent_id) REFERENCES patient.consent(org_id, id)`
- `session_org_id_encounter_id_patient_id_fkey`: `FOREIGN KEY (org_id, encounter_id, patient_id) REFERENCES clinical.encounter(org_id, id, patient_id)`
- `session_org_id_practitioner_membership_id_fkey`: `FOREIGN KEY (org_id, practitioner_membership_id) REFERENCES iam.membership(org_id, id)`
- `session_updated_by_fkey`: `FOREIGN KEY (updated_by) REFERENCES iam.user_account(id)`

Indexes:

- `ix_tele_patient`: `btree (patient_id, scheduled_start DESC)`
- `ix_tele_practitioner`: `btree (practitioner_membership_id, scheduled_start)`
- `session_appointment_id_key`: `btree (appointment_id)` (unique)
- `session_org_id_id_key`: `btree (org_id, id)` (unique)
- `session_pkey`: `btree (id)` (unique)

### `telehealth.session_event`

RLS: forced — policies: owner_internal, staff_insert, staff_select, staff_update, tenant_isolation(R)  
Triggers: trg_tele_event_append_only

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | bigint | NN |  | **PK** |
| `org_id` | uuid | NN |  | FK→ `telehealth.session` |
| `session_id` | uuid | NN |  | FK→ `telehealth.session` |
| `event_type` | text | NN |  |  |
| `participant_role` | text | null |  |  |
| `actor_user_id` | uuid | null |  | FK→ `iam.user_account` |
| `occurred_at` | timestamp with time zone | NN | `now()` |  |
| `detail` | jsonb | null |  |  |

Constraints:

- `session_event_detail_check` (check): `CHECK (((detail IS NULL) OR ((jsonb_typeof(detail) = 'object'::text) AND (pg_column_size(detail) < 2048))))`
- `session_event_event_type_check` (check): `CHECK ((event_type = ANY (ARRAY['participant_joined'::text, 'participant_left'::text, 'connection_lost'::text, 'reconnected'::text, 'provider_error'::text, 'ended'::text])))`
- `session_event_participant_role_check` (check): `CHECK ((participant_role = ANY (ARRAY['patient'::text, 'practitioner'::text, 'interpreter'::text, 'guardian'::text])))`

Foreign keys:

- `session_event_actor_user_id_fkey`: `FOREIGN KEY (actor_user_id) REFERENCES iam.user_account(id)`
- `session_event_org_id_session_id_fkey`: `FOREIGN KEY (org_id, session_id) REFERENCES telehealth.session(org_id, id)`

Indexes:

- `ix_tele_event_session`: `btree (session_id, occurred_at)`
- `session_event_pkey`: `btree (id)` (unique)

## Schema `document`

### `document.document`

RLS: forced — policies: owner_internal, staff_insert, staff_select, staff_update, tenant_isolation(R)  
Triggers: trg_0_state, trg_audit_capture, trg_touch

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | uuid | NN | `gen_random_uuid()` | **PK** |
| `org_id` | uuid | NN |  | FK→ `clinical.encounter` |
| `patient_id` | uuid | NN |  | FK→ `clinical.encounter` |
| `encounter_id` | uuid | null |  | FK→ `clinical.encounter` |
| `category` | text | NN |  |  |
| `title` | text | NN |  |  |
| `storage_bucket` | text | NN |  |  |
| `storage_key` | text | NN |  |  |
| `content_type` | text | NN |  |  |
| `size_bytes` | bigint | NN |  |  |
| `sha256` | bytea | NN |  |  |
| `encryption_key_ref` | text | NN |  |  |
| `status` | text | NN | `'pending_upload'::text` |  |
| `legal_hold` | boolean | NN | `false` |  |
| `deleted_at` | timestamp with time zone | null |  |  |
| `deleted_by` | uuid | null |  | FK→ `iam.user_account` |
| `delete_reason` | text | null |  |  |
| `created_at` | timestamp with time zone | NN | `now()` |  |
| `created_by` | uuid | null | `sec.current_user_id()` | FK→ `iam.user_account` |
| `updated_at` | timestamp with time zone | NN | `now()` |  |
| `updated_by` | uuid | null |  | FK→ `iam.user_account` |
| `row_version` | integer | NN | `1` |  |

Constraints:

- `ck_doc_delete_reason` (check): `CHECK (((status <> 'deleted'::text) OR (delete_reason IS NOT NULL)))`
- `ck_doc_deleted` (check): `CHECK (((status = 'deleted'::text) = (deleted_at IS NOT NULL)))`
- `ck_doc_hold` (check): `CHECK ((NOT (legal_hold AND (status = 'deleted'::text))))`
- `document_category_check` (check): `CHECK ((category = ANY (ARRAY['identity'::text, 'referral_letter'::text, 'lab_report'::text, 'imaging'::text, 'consent_form'::text, 'insurance'::text, 'clinical_photo'::text, 'discharge_summary'::text, 'prescription'::text, 'other'::text])))`
- `document_content_type_check` (check): `CHECK ((content_type = ANY (ARRAY['application/pdf'::text, 'image/jpeg'::text, 'image/png'::text, 'image/webp'::text, 'application/dicom'::text, 'text/plain'::text])))`
- `document_org_id_id_key` (unique): `UNIQUE (org_id, id)`
- `document_sha256_check` (check): `CHECK ((length(sha256) = 32))`
- `document_size_bytes_check` (check): `CHECK (((size_bytes >= 1) AND (size_bytes <= 52428800)))`
- `document_status_check` (check): `CHECK ((status = ANY (ARRAY['pending_upload'::text, 'scanning'::text, 'available'::text, 'quarantined'::text, 'deleted'::text])))`
- `document_storage_bucket_storage_key_key` (unique): `UNIQUE (storage_bucket, storage_key)`
- `document_title_check` (check): `CHECK (((length(btrim(title)) >= 1) AND (length(btrim(title)) <= 200)))`

Foreign keys:

- `document_created_by_fkey`: `FOREIGN KEY (created_by) REFERENCES iam.user_account(id)`
- `document_deleted_by_fkey`: `FOREIGN KEY (deleted_by) REFERENCES iam.user_account(id)`
- `document_org_id_encounter_id_patient_id_fkey`: `FOREIGN KEY (org_id, encounter_id, patient_id) REFERENCES clinical.encounter(org_id, id, patient_id)`
- `document_org_id_patient_id_fkey`: `FOREIGN KEY (org_id, patient_id) REFERENCES patient.patient(org_id, id)`
- `document_updated_by_fkey`: `FOREIGN KEY (updated_by) REFERENCES iam.user_account(id)`

Indexes:

- `document_org_id_id_key`: `btree (org_id, id)` (unique)
- `document_pkey`: `btree (id)` (unique)
- `document_storage_bucket_storage_key_key`: `btree (storage_bucket, storage_key)` (unique)
- `ix_document_patient`: `btree (patient_id, created_at DESC) WHERE (status <> 'deleted'::text)`

## Schema `comms`

### `comms.device`

RLS: enabled — policies: device_owner, device_worker  
Triggers: —

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | uuid | NN | `gen_random_uuid()` | **PK** |
| `user_id` | uuid | NN |  | FK→ `iam.user_account` |
| `platform` | text | NN |  |  |
| `push_token_ciphertext` | bytea | NN |  |  |
| `push_token_bidx` | bytea | NN |  |  |
| `key_ref` | text | NN |  |  |
| `app_version` | text | null |  |  |
| `last_seen_at` | timestamp with time zone | NN | `now()` |  |
| `revoked_at` | timestamp with time zone | null |  |  |
| `created_at` | timestamp with time zone | NN | `now()` |  |

Constraints:

- `device_platform_check` (check): `CHECK ((platform = ANY (ARRAY['android'::text, 'ios'::text, 'web'::text])))`
- `device_push_token_bidx_check` (check): `CHECK ((length(push_token_bidx) = 32))`
- `device_push_token_bidx_key` (unique): `UNIQUE (push_token_bidx)`

Foreign keys:

- `device_user_id_fkey`: `FOREIGN KEY (user_id) REFERENCES iam.user_account(id)`

Indexes:

- `device_pkey`: `btree (id)` (unique)
- `device_push_token_bidx_key`: `btree (push_token_bidx)` (unique)
- `ix_device_user`: `btree (user_id) WHERE (revoked_at IS NULL)`

### `comms.message`

RLS: forced — policies: owner_internal, staff_select, tenant_isolation(R), worker_all  
Triggers: trg_0_state, trg_message_consent, trg_touch

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | uuid | NN | `gen_random_uuid()` | **PK** |
| `org_id` | uuid | NN |  | FK→ `patient.patient` |
| `patient_id` | uuid | null |  | FK→ `patient.patient` |
| `user_id` | uuid | null |  | FK→ `iam.user_account` |
| `outbox_id` | bigint | null |  | FK→ `comms.outbox` |
| `template_id` | uuid | null |  | FK→ `comms.notification_template` |
| `channel` | text | NN |  |  |
| `purpose` | text | NN |  |  |
| `recipient_address` | text | NN |  |  |
| `status` | text | NN | `'queued'::text` |  |
| `provider` | text | null |  |  |
| `provider_message_id` | text | null |  |  |
| `attempts` | smallint | NN | `0` |  |
| `last_error_code` | text | null |  |  |
| `sent_at` | timestamp with time zone | null |  |  |
| `delivered_at` | timestamp with time zone | null |  |  |
| `created_at` | timestamp with time zone | NN | `now()` |  |
| `updated_at` | timestamp with time zone | NN | `now()` |  |
| `updated_by` | uuid | null |  |  |
| `created_by` | uuid | null |  |  |
| `row_version` | integer | NN | `1` |  |

Constraints:

- `message_channel_check` (check): `CHECK ((channel = ANY (ARRAY['sms'::text, 'whatsapp'::text, 'email'::text, 'push'::text])))`
- `message_org_id_id_key` (unique): `UNIQUE (org_id, id)`
- `message_purpose_check` (check): `CHECK ((purpose = ANY (ARRAY['otp'::text, 'appointment_confirmation'::text, 'appointment_reminder'::text, 'results_ready'::text, 'payment_receipt'::text, 'telemedicine_link'::text, 'account'::text, 'general'::text])))`
- `message_status_check` (check): `CHECK ((status = ANY (ARRAY['queued'::text, 'sending'::text, 'sent'::text, 'delivered'::text, 'failed'::text, 'cancelled'::text])))`

Foreign keys:

- `message_org_id_patient_id_fkey`: `FOREIGN KEY (org_id, patient_id) REFERENCES patient.patient(org_id, id)`
- `message_org_id_template_id_fkey`: `FOREIGN KEY (org_id, template_id) REFERENCES comms.notification_template(org_id, id)`
- `message_outbox_id_fkey`: `FOREIGN KEY (outbox_id) REFERENCES comms.outbox(id)`
- `message_user_id_fkey`: `FOREIGN KEY (user_id) REFERENCES iam.user_account(id)`

Indexes:

- `ix_message_patient`: `btree (patient_id, created_at DESC)`
- `message_org_id_id_key`: `btree (org_id, id)` (unique)
- `message_pkey`: `btree (id)` (unique)
- `ux_message_provider_id`: `btree (provider, provider_message_id) WHERE (provider_message_id IS NOT NULL)` (unique)

### `comms.notification_template`

RLS: enabled — policies: owner_internal, staff_insert, staff_select, staff_update, tenant_isolation(R)  
Triggers: trg_0_state, trg_touch

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | uuid | NN | `gen_random_uuid()` | **PK** |
| `org_id` | uuid | NN |  | FK→ `org.organization` |
| `code` | text | NN |  |  |
| `channel` | text | NN |  |  |
| `language` | text | NN |  |  |
| `body` | text | NN |  |  |
| `version` | integer | NN | `1` |  |
| `status` | text | NN | `'draft'::text` |  |
| `approved_by` | uuid | null |  | FK→ `iam.user_account` |
| `created_at` | timestamp with time zone | NN | `now()` |  |
| `created_by` | uuid | null | `sec.current_user_id()` | FK→ `iam.user_account` |
| `updated_at` | timestamp with time zone | NN | `now()` |  |
| `updated_by` | uuid | null |  | FK→ `iam.user_account` |
| `row_version` | integer | NN | `1` |  |

Constraints:

- `ck_template_approved` (check): `CHECK (((status <> 'active'::text) OR (approved_by IS NOT NULL)))`
- `notification_template_body_check` (check): `CHECK ((length(body) <= 1000))`
- `notification_template_channel_check` (check): `CHECK ((channel = ANY (ARRAY['sms'::text, 'whatsapp'::text, 'email'::text, 'push'::text])))`
- `notification_template_code_check` (check): `CHECK ((code ~ '^[a-z_]{3,50}$'::text))`
- `notification_template_language_check` (check): `CHECK ((language = ANY (ARRAY['sw'::text, 'en'::text])))`
- `notification_template_org_id_code_channel_language_version_key` (unique): `UNIQUE (org_id, code, channel, language, version)`
- `notification_template_org_id_id_key` (unique): `UNIQUE (org_id, id)`
- `notification_template_status_check` (check): `CHECK ((status = ANY (ARRAY['draft'::text, 'active'::text, 'retired'::text])))`

Foreign keys:

- `notification_template_approved_by_fkey`: `FOREIGN KEY (approved_by) REFERENCES iam.user_account(id)`
- `notification_template_created_by_fkey`: `FOREIGN KEY (created_by) REFERENCES iam.user_account(id)`
- `notification_template_org_id_fkey`: `FOREIGN KEY (org_id) REFERENCES org.organization(id)`
- `notification_template_updated_by_fkey`: `FOREIGN KEY (updated_by) REFERENCES iam.user_account(id)`

Indexes:

- `notification_template_org_id_code_channel_language_version_key`: `btree (org_id, code, channel, language, version)` (unique)
- `notification_template_org_id_id_key`: `btree (org_id, id)` (unique)
- `notification_template_pkey`: `btree (id)` (unique)
- `ux_template_active`: `btree (org_id, code, channel, language) WHERE (status = 'active'::text)` (unique)

### `comms.outbox`

RLS: enabled — policies: outbox_enqueue, outbox_worker  
Triggers: —

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | bigint | NN |  | **PK** |
| `org_id` | uuid | null |  | FK→ `org.organization` |
| `event_type` | text | NN |  |  |
| `aggregate_type` | text | NN |  |  |
| `aggregate_id` | uuid | NN |  |  |
| `dedupe_key` | text | null |  |  |
| `payload` | jsonb | NN | `'{}'::jsonb` |  |
| `status` | text | NN | `'pending'::text` |  |
| `attempts` | smallint | NN | `0` |  |
| `max_attempts` | smallint | NN | `8` |  |
| `available_at` | timestamp with time zone | NN | `now()` |  |
| `locked_by` | text | null |  |  |
| `locked_at` | timestamp with time zone | null |  |  |
| `last_error` | text | null |  |  |
| `created_at` | timestamp with time zone | NN | `now()` |  |
| `processed_at` | timestamp with time zone | null |  |  |
| `request_id` | uuid | null | `sec.current_request_id()` |  |

Constraints:

- `outbox_attempts_check` (check): `CHECK ((attempts >= 0))`
- `outbox_event_type_check` (check): `CHECK ((event_type ~ '^[a-z_]+(\.[a-z_]+){1,3}$'::text))`
- `outbox_last_error_check` (check): `CHECK ((length(last_error) <= 1000))`
- `outbox_payload_check` (check): `CHECK (((jsonb_typeof(payload) = 'object'::text) AND (pg_column_size(payload) < 8192)))`
- `outbox_status_check` (check): `CHECK ((status = ANY (ARRAY['pending'::text, 'processing'::text, 'succeeded'::text, 'failed'::text, 'dead'::text])))`

Foreign keys:

- `outbox_org_id_fkey`: `FOREIGN KEY (org_id) REFERENCES org.organization(id)`

Indexes:

- `ix_outbox_dead`: `btree (created_at) WHERE (status = 'dead'::text)`
- `ix_outbox_ready`: `btree (available_at) WHERE (status = ANY (ARRAY['pending'::text, 'failed'::text]))`
- `outbox_pkey`: `btree (id)` (unique)
- `ux_outbox_dedupe`: `btree (org_id, dedupe_key) WHERE (dedupe_key IS NOT NULL)` (unique)

## Schema `integration`

### `integration.inbound_event`

RLS: enabled — policies: inbound_worker  
Triggers: —

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | uuid | NN | `gen_random_uuid()` | **PK** |
| `provider` | text | NN |  |  |
| `external_event_id` | text | NN |  |  |
| `event_type` | text | NN |  |  |
| `org_id` | uuid | null |  | FK→ `org.organization` |
| `signature_valid` | boolean | NN |  |  |
| `payload_sha256` | bytea | NN |  |  |
| `payload` | jsonb | NN |  |  |
| `status` | text | NN | `'received'::text` |  |
| `attempts` | smallint | NN | `0` |  |
| `error` | text | null |  |  |
| `received_at` | timestamp with time zone | NN | `now()` |  |
| `processed_at` | timestamp with time zone | null |  |  |

Constraints:

- `ck_inbound_signature` (check): `CHECK ((signature_valid OR (status = 'rejected'::text)))`
- `inbound_event_payload_check` (check): `CHECK ((jsonb_typeof(payload) = 'object'::text))`
- `inbound_event_payload_sha256_check` (check): `CHECK ((length(payload_sha256) = 32))`
- `inbound_event_provider_external_event_id_key` (unique): `UNIQUE (provider, external_event_id)`
- `inbound_event_status_check` (check): `CHECK ((status = ANY (ARRAY['received'::text, 'processed'::text, 'rejected'::text, 'failed'::text])))`

Foreign keys:

- `inbound_event_org_id_fkey`: `FOREIGN KEY (org_id) REFERENCES org.organization(id)`

Indexes:

- `inbound_event_pkey`: `btree (id)` (unique)
- `inbound_event_provider_external_event_id_key`: `btree (provider, external_event_id)` (unique)
- `ix_inbound_pending`: `btree (received_at) WHERE (status = ANY (ARRAY['received'::text, 'failed'::text]))`

### `integration.provider_config`

RLS: enabled — policies: owner_internal, staff_insert, staff_select, staff_update, tenant_isolation(R)  
Triggers: trg_audit_capture, trg_touch

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | uuid | NN | `gen_random_uuid()` | **PK** |
| `org_id` | uuid | NN |  | FK→ `org.organization` |
| `provider` | text | NN |  |  |
| `purpose` | text | NN |  |  |
| `status` | text | NN | `'disabled'::text` |  |
| `credentials_secret_ref` | text | NN |  |  |
| `config` | jsonb | NN | `'{}'::jsonb` |  |
| `dpa_signed_at` | date | null |  |  |
| `dpa_document_ref` | text | null |  |  |
| `last_reviewed_at` | date | null |  |  |
| `created_at` | timestamp with time zone | NN | `now()` |  |
| `created_by` | uuid | null | `sec.current_user_id()` | FK→ `iam.user_account` |
| `updated_at` | timestamp with time zone | NN | `now()` |  |
| `updated_by` | uuid | null |  | FK→ `iam.user_account` |
| `row_version` | integer | NN | `1` |  |

Constraints:

- `ck_live_requires_dpa` (check): `CHECK (((status <> 'live'::text) OR (dpa_signed_at IS NOT NULL)))`
- `provider_config_config_check` (check): `CHECK ((jsonb_typeof(config) = 'object'::text))`
- `provider_config_credentials_secret_ref_check` (check): `CHECK ((credentials_secret_ref ~ '^(vault\|awssm\|gcpsm\|azkv)://'::text))`
- `provider_config_org_id_provider_purpose_key` (unique): `UNIQUE (org_id, provider, purpose)`
- `provider_config_purpose_check` (check): `CHECK ((purpose = ANY (ARRAY['sms'::text, 'whatsapp'::text, 'mobile_money'::text, 'card'::text, 'telemedicine'::text, 'object_storage'::text, 'email'::text, 'fiscal_receipt'::text, 'insurance_claims'::text])))`
- `provider_config_status_check` (check): `CHECK ((status = ANY (ARRAY['disabled'::text, 'sandbox'::text, 'live'::text])))`

Foreign keys:

- `provider_config_created_by_fkey`: `FOREIGN KEY (created_by) REFERENCES iam.user_account(id)`
- `provider_config_org_id_fkey`: `FOREIGN KEY (org_id) REFERENCES org.organization(id)`
- `provider_config_updated_by_fkey`: `FOREIGN KEY (updated_by) REFERENCES iam.user_account(id)`

Indexes:

- `provider_config_org_id_provider_purpose_key`: `btree (org_id, provider, purpose)` (unique)
- `provider_config_pkey`: `btree (id)` (unique)

## Schema `gov`

### `gov.data_category`

RLS: off (no direct app access or global)  
Triggers: —

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `code` | text | NN |  | **PK** |
| `description` | text | NN |  |  |
| `classification` | text | NN |  |  |
| `retention_period` | interval | null |  |  |
| `retention_basis` | text | null |  |  |
| `disposal_method` | text | NN | `'pending_decision'::text` |  |
| `owner_role` | text | NN |  |  |

Constraints:

- `data_category_classification_check` (check): `CHECK ((classification = ANY (ARRAY['phi'::text, 'phi_restricted'::text, 'pii'::text, 'confidential'::text, 'internal'::text, 'public'::text])))`
- `data_category_disposal_method_check` (check): `CHECK ((disposal_method = ANY (ARRAY['pending_decision'::text, 'hard_delete'::text, 'crypto_shred'::text, 'anonymise'::text, 'archive_then_delete'::text])))`

Indexes:

- `data_category_pkey`: `btree (code)` (unique)

### `gov.legal_hold`

RLS: forced — policies: owner_internal, staff_insert, staff_select, staff_update, tenant_isolation(R)  
Triggers: trg_audit_capture

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | uuid | NN | `gen_random_uuid()` | **PK** |
| `org_id` | uuid | NN |  | FK→ `org.organization` |
| `patient_id` | uuid | null |  | FK→ `patient.patient` |
| `scope` | text | NN |  |  |
| `reason` | text | NN |  |  |
| `reference` | text | null |  |  |
| `placed_by` | uuid | NN | `sec.current_user_id()` | FK→ `iam.user_account` |
| `placed_at` | timestamp with time zone | NN | `now()` |  |
| `released_by` | uuid | null |  | FK→ `iam.user_account` |
| `released_at` | timestamp with time zone | null |  |  |

Constraints:

- `ck_hold_release` (check): `CHECK (((released_at IS NULL) = (released_by IS NULL)))`
- `ck_hold_scope` (check): `CHECK (((scope = 'patient'::text) = (patient_id IS NOT NULL)))`
- `legal_hold_scope_check` (check): `CHECK ((scope = ANY (ARRAY['patient'::text, 'org_wide'::text])))`

Foreign keys:

- `legal_hold_org_id_fkey`: `FOREIGN KEY (org_id) REFERENCES org.organization(id)`
- `legal_hold_org_id_patient_id_fkey`: `FOREIGN KEY (org_id, patient_id) REFERENCES patient.patient(org_id, id)`
- `legal_hold_placed_by_fkey`: `FOREIGN KEY (placed_by) REFERENCES iam.user_account(id)`
- `legal_hold_released_by_fkey`: `FOREIGN KEY (released_by) REFERENCES iam.user_account(id)`

Indexes:

- `legal_hold_pkey`: `btree (id)` (unique)

### `gov.privacy_request`

RLS: forced — policies: owner_internal, staff_insert, staff_select, staff_update, tenant_isolation(R)  
Triggers: trg_0_state, trg_audit_capture, trg_touch

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | uuid | NN | `gen_random_uuid()` | **PK** |
| `org_id` | uuid | NN |  | FK→ `document.document` |
| `patient_id` | uuid | NN |  | FK→ `patient.patient` |
| `request_type` | text | NN |  |  |
| `status` | text | NN | `'received'::text` |  |
| `received_via` | text | NN |  |  |
| `received_at` | timestamp with time zone | NN | `now()` |  |
| `due_at` | timestamp with time zone | NN |  |  |
| `identity_verified_by` | uuid | null |  | FK→ `iam.user_account` |
| `handled_by` | uuid | null |  | FK→ `iam.user_account` |
| `outcome_note` | text | null |  |  |
| `denial_reason` | text | null |  |  |
| `export_document_id` | uuid | null |  | FK→ `document.document` |
| `closed_at` | timestamp with time zone | null |  |  |
| `created_at` | timestamp with time zone | NN | `now()` |  |
| `created_by` | uuid | null | `sec.current_user_id()` | FK→ `iam.user_account` |
| `updated_at` | timestamp with time zone | NN | `now()` |  |
| `updated_by` | uuid | null |  | FK→ `iam.user_account` |
| `row_version` | integer | NN | `1` |  |

Constraints:

- `ck_pr_closed` (check): `CHECK (((status = ANY (ARRAY['fulfilled'::text, 'partially_fulfilled'::text, 'denied'::text, 'withdrawn'::text])) = (closed_at IS NOT NULL)))`
- `ck_pr_denied` (check): `CHECK (((status <> 'denied'::text) OR (denial_reason IS NOT NULL)))`
- `privacy_request_org_id_id_key` (unique): `UNIQUE (org_id, id)`
- `privacy_request_received_via_check` (check): `CHECK ((received_via = ANY (ARRAY['in_person'::text, 'portal'::text, 'email'::text, 'letter'::text])))`
- `privacy_request_request_type_check` (check): `CHECK ((request_type = ANY (ARRAY['access'::text, 'copy_export'::text, 'correction'::text, 'restriction'::text, 'objection'::text, 'erasure'::text, 'accounting_of_disclosures'::text])))`
- `privacy_request_status_check` (check): `CHECK ((status = ANY (ARRAY['received'::text, 'identity_verified'::text, 'in_progress'::text, 'fulfilled'::text, 'partially_fulfilled'::text, 'denied'::text, 'withdrawn'::text])))`

Foreign keys:

- `privacy_request_created_by_fkey`: `FOREIGN KEY (created_by) REFERENCES iam.user_account(id)`
- `privacy_request_handled_by_fkey`: `FOREIGN KEY (handled_by) REFERENCES iam.user_account(id)`
- `privacy_request_identity_verified_by_fkey`: `FOREIGN KEY (identity_verified_by) REFERENCES iam.user_account(id)`
- `privacy_request_org_id_export_document_id_fkey`: `FOREIGN KEY (org_id, export_document_id) REFERENCES document.document(org_id, id)`
- `privacy_request_org_id_fkey`: `FOREIGN KEY (org_id) REFERENCES org.organization(id)`
- `privacy_request_org_id_patient_id_fkey`: `FOREIGN KEY (org_id, patient_id) REFERENCES patient.patient(org_id, id)`
- `privacy_request_updated_by_fkey`: `FOREIGN KEY (updated_by) REFERENCES iam.user_account(id)`

Indexes:

- `ix_privacy_due`: `btree (org_id, due_at) WHERE (closed_at IS NULL)`
- `privacy_request_org_id_id_key`: `btree (org_id, id)` (unique)
- `privacy_request_pkey`: `btree (id)` (unique)

## Schema `audit`

### `audit.event`

Append-only, hash-sealed audit trail. Classified PHI-RESTRICTED (may contain before/after clinical values).

RLS: enabled — policies: audit_compliance_read, audit_tenant_read  
Triggers: trg_audit_append_only, trg_audit_hash, trg_audit_no_truncate

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `id` | bigint | NN | `nextval('audit.event_id_seq'::regclass)` | **PK** |
| `occurred_at` | timestamp with time zone | NN | `clock_timestamp()` | **PK** |
| `txid` | xid8 | NN | `pg_current_xact_id()` |  |
| `org_id` | uuid | null |  |  |
| `actor_type` | text | NN |  |  |
| `actor_user_id` | uuid | null |  |  |
| `actor_membership_id` | uuid | null |  |  |
| `session_id` | uuid | null |  |  |
| `request_id` | uuid | null |  |  |
| `action` | text | NN |  |  |
| `category` | text | NN |  |  |
| `resource_type` | text | null |  |  |
| `resource_id` | uuid | null |  |  |
| `patient_id` | uuid | null |  |  |
| `outcome` | text | NN |  |  |
| `reason` | text | null |  |  |
| `source_ip` | inet | null |  |  |
| `user_agent` | text | null |  |  |
| `changes` | jsonb | null |  |  |
| `row_hash` | bytea | NN |  |  |

Constraints:

- `event_action_check` (check): `CHECK ((action ~ '^[a-z_]+(\.[a-z_]+){1,4}$'::text))`
- `event_actor_type_check` (check): `CHECK ((actor_type = ANY (ARRAY['staff'::text, 'patient'::text, 'service'::text, 'system'::text, 'anonymous'::text])))`
- `event_category_check` (check): `CHECK ((category = ANY (ARRAY['authentication'::text, 'authorization'::text, 'phi_access'::text, 'data_change'::text, 'administration'::text, 'financial'::text, 'integration'::text, 'security'::text, 'system'::text])))`
- `event_changes_check` (check): `CHECK (((changes IS NULL) OR (jsonb_typeof(changes) = 'object'::text)))`
- `event_outcome_check` (check): `CHECK ((outcome = ANY (ARRAY['success'::text, 'denied'::text, 'failure'::text, 'error'::text])))`
- `event_reason_check` (check): `CHECK ((length(reason) <= 1000))`
- `event_user_agent_check` (check): `CHECK ((length(user_agent) <= 512))`

Indexes:

- `event_pkey`: `btree (occurred_at, id)` (unique)
- `ix_audit_actor_time`: `btree (actor_user_id, occurred_at DESC)`
- `ix_audit_org_time`: `btree (org_id, occurred_at DESC)`
- `ix_audit_patient_time`: `btree (patient_id, occurred_at DESC) WHERE (patient_id IS NOT NULL)`
- `ix_audit_request`: `btree (request_id) WHERE (request_id IS NOT NULL)`
- `ix_audit_resource`: `btree (resource_type, resource_id)`
- `ix_audit_txid`: `btree (txid)`

### `audit.seal`

RLS: off (no direct app access or global)  
Triggers: trg_seal_append_only, trg_seal_guard

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `seal_no` | bigint | NN |  | **PK** |
| `xid_lower` | xid8 | NN |  |  |
| `xid_upper` | xid8 | NN |  |  |
| `row_count` | bigint | NN |  |  |
| `prev_hash` | bytea | NN |  |  |
| `seal_hash` | bytea | NN |  |  |
| `sealed_at` | timestamp with time zone | NN | `now()` |  |
| `exported_at` | timestamp with time zone | null |  |  |
| `export_uri` | text | null |  |  |

Constraints:

- `ck_seal_range` (check): `CHECK ((xid_upper >= xid_lower))`
- `seal_row_count_check` (check): `CHECK ((row_count >= 0))`

Indexes:

- `seal_pkey`: `btree (seal_no)` (unique)

## Schema `ref`

### `ref.icd10_code`

RLS: off (no direct app access or global)  
Triggers: —

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `code` | text | NN |  | **PK** |
| `title` | text | NN |  |  |
| `chapter` | text | null |  |  |
| `is_billable` | boolean | NN | `true` |  |
| `status` | text | NN | `'active'::text` |  |

Constraints:

- `icd10_code_code_check` (check): `CHECK ((code ~ '^[A-Z][0-9]{2}(\.[0-9A-Z]{1,4})?$'::text))`
- `icd10_code_status_check` (check): `CHECK ((status = ANY (ARRAY['active'::text, 'retired'::text])))`

Indexes:

- `icd10_code_pkey`: `btree (code)` (unique)
- `ix_icd10_title_trgm`: `gin (lower(title) gin_trgm_ops)`

### `ref.permission`

RLS: off (no direct app access or global)  
Triggers: —

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `code` | text | NN |  | **PK** |
| `resource` | text | NN |  |  |
| `action` | text | NN |  |  |
| `description` | text | NN |  |  |
| `is_phi` | boolean | NN | `false` |  |
| `is_privileged` | boolean | NN | `false` |  |

Constraints:

- `permission_code_check` (check): `CHECK ((code ~ '^[a-z_]+(\.[a-z_]+){1,3}$'::text))`

Indexes:

- `permission_pkey`: `btree (code)` (unique)

### `ref.state_transition`

Allowed lifecycle transitions per entity; from_status=_init lists valid initial states.

RLS: off (no direct app access or global)  
Triggers: —

| Column | Type | Null | Default / generated | Key |
|---|---|---|---|---|
| `entity` | text | NN |  | **PK** |
| `from_status` | text | NN |  | **PK** |
| `to_status` | text | NN |  | **PK** |
| `required_perm` | text | null |  |  |
| `description` | text | null |  |  |

Indexes:

- `state_transition_pkey`: `btree (entity, from_status, to_status)` (unique)
