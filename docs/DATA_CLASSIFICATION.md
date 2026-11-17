# Data classification

Every current public-schema column is classified in deploy/data-classification.json. A real-PostgreSQL regression fails if a new column lacks C1–C4 classification. New migrations must update that inventory and the relevant access/encryption/audit rules.

| Class | Current treatment | Limits |
| --- | --- | --- |
| C1 Internal | Facility/catalogue/role metadata, staff display/role metadata, bounded operational counters and independent audit heads | API authentication/permissions apply; arbitrary body/query values are excluded from ordinary telemetry/logs. |
| C2 Identifiers | Patient demographics and scope metadata, staff email, pseudonymous rate buckets and encrypted identity/insurance references | Facility/RLS and care policy for patients; reads/search hits audited. Identifier values use row-bound field encryption and separate-key HMAC lookup. Production disk/backup encryption remains a deployment requirement. |
| C3 Restricted | Audit/access evidence, care assignments, exceptional reasons/reviews, approval requests, privacy alerts, transactional replay/event records | Permissioned access and audit, no ordinary-log bodies. Clinical note/diagnosis/lab/document classes remain unimplemented and must inherit this class or stricter controls. |
| C4 Secret | Password hashes, opaque credential digests, TOTP ciphertext and recovery/challenge hashes; external encryption/index keys | No ordinary entity responses or logging; keys are outside DB/backups. Authentication necessarily issues credentials and one-time authenticator provisioning/recovery material over the designated no-store TLS flow; those exceptions are not generic secret-read endpoints. |

Encrypted identifier creation returns metadata only, so the durable idempotency response does not become a plaintext copy. Lookup request hashes use the keyed blind-index value rather than the raw identifier. Authorized identifier reads decrypt in memory and are audited. The HMAC lookup key is independent of rotating encryption keys and must not be retired as part of routine ciphertext rotation.

Free-form exceptional/approval reasons can contain sensitive context: they stay in restricted records referenced by audit and are never copied into ordinary request logs. Consent wording, guardian rules, legal retention/holds, data-subject exports, private file scanning and the clinical signing lifecycle remain tracked work. No legal period or automatic patient deletion is configured.

Development, test, benchmarks and CI use synthetic data. Private dumps, scanner outputs, runtime configuration and binaries stay under ignored local paths; repository examples contain placeholders or public test vectors only. Production hosting, vendors, key custody and transfers remain in DEPLOYMENT_DECISIONS.md for clinic approval.
