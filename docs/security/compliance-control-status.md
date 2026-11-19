# Compliance Control Status (HIPAA Security Rule & SOC 2)

> **Important:** Technical controls implemented in software do **not** by themselves constitute HIPAA compliance or SOC 2 certification. Organizational policies, signed BAAs/DPAs, hosting/infrastructure controls, independent audits, and operational procedures are also required.

| Control Area | Reference | Technical Implementation Status | Operational / External Dependency |
|---|---|---|---|
| Unique User ID & Role-Based Access | HIPAA §164.312(a)(2)(i), SOC 2 CC6.1 | **Implemented:** Global `iam.user_account`, tenant `iam.membership`, RBAC + facility scope, DB-enforced RLS | Periodic HR offboarding process |
| Emergency (Break-Glass) Access | HIPAA §164.312(a)(2)(ii) | **Implemented:** `iam.open_break_glass()` (time-boxed 5–240 min, audited, single-patient RLS expansion) + four-eyes compliance review | Clinical break-glass review SLA |
| Automatic Logoff | HIPAA §164.312(a)(2)(iii) | **Implemented:** Sliding `idle_expires_at` (5–60 min) + `absolute_expires_at` (max 24h) enforced in `sec.begin_request()` | Configure per-clinic org timeout |
| Authentication & MFA | HIPAA §164.312(d), SOC 2 CC6.1 | **Implemented:** Argon2id, lockout, TOTP MFA with replay protection, recovery codes, forced MFA for privileged & remote clinical roles in prod | Provision authenticator devices for staff |
| Encryption in Transit & Field-Level | HIPAA §164.312(a)(2)(iv), (e)(1) | **Implemented:** Production enforces `sslmode=verify-full` + `sslrootcert`, HTTPS + HSTS; AES-256-GCM keyring for MFA, identifiers, coverages, devices, local blobs | Managed TLS certs, KMS/HSM key custody, storage-level encryption at rest |
| Audit Controls & Tamper Evidence | HIPAA §164.312(b), SOC 2 CC7.2 | **Implemented:** Partitioned append-only `audit.event` with SHA-256 `row_hash`, `audit.seal_pending()`, `audit.verify_integrity()`, per-patient PHI access reports | Off-host WORM export of `audit.seal` hashes & SIEM alerting |
| Periodic Access Reviews | SOC 2 CC6.2 / CC6.3 | **Implemented:** `iam.access_review` + `iam.access_review_item` workflow | Quarterly execution by compliance officer |
| Data-Subject Rights & Legal Hold | Tanzania PDPA 2022, HIPAA §164.524 | **Implemented:** `gov.privacy_request`, `gov.legal_hold`, `document.document.legal_hold` delete guard | Finalize statutory retention periods (`gov.data_category.retention_period`) |
| Third-Party Processors | HIPAA BAA / PDPA DPA | **Implemented:** `integration.provider_config` blocks `status='live'` unless `dpa_signed_at` is set; secret pointers only | Execute BAAs/DPAs with SMS/video/payment/storage vendors |
