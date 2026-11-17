# External integrations

| Capability | Configuration / evidence | State and next requirement |
| --- | --- | --- |
| PostgreSQL | Local PostgreSQL 18.3; pgxpool, Goose, restricted runtime role; real integration tests | Foundation working; no production database connected |
| Object storage | Optional private local MinIO service in Compose | Infrastructure only; file metadata/API/quarantine/scanning not started |
| Document scanning | None | Implement real ClamAV adapter and fail-closed quarantine before downloads |
| Notifications | Explicit PostgreSQL development sink only | No real SMS/email/push delivery; adapter, preferences, templates and delivery review pending |
| Staff invitation/reset | No email provider contract | Internal token lifecycle and delivery flow not implemented; no fake recovery links |
| Payments | None | No payment endpoints or provider evidence; manual financial domain also pending |
| Telemedicine | None | Module not started; provider session path externally blocked until a selected contract/configuration is supplied; no fabricated call URLs |
| POWERHMS | No authorized sample exports or API contract | Module not started; real source mappings unverified; canonical staging format and synthetic import examples must still be built |
| Clinical knowledge | None | Diagnosis/medication catalogues not populated; no invented clinical truth, interaction checking or critical thresholds |

Provider webhooks, signatures, replay protection, reconciliation and restricted outbound destinations remain M22 requirements. Never convert a timeout or missing configuration into success. The local sink is explicitly labeled and unavailable outside development. MinIO is optional and unused by the current API. No Redis dependency is required.

## Deferred engineering-provider decisions

The user explicitly deferred production hosting/offsite storage/mail choices. No Cloudflare mode, transfer permission, S3 destination, real notification delivery, external telemetry delivery, immutable audit anchor or pager has been fabricated. Inactive backup/systemd templates and exact prerequisites are in DEPLOYMENT_DECISIONS.md and SECURITY_ENGINEERING.md. Optional telemetry exports only static HTTP metadata when explicitly configured; default configuration has no collector. Production outbox consumers without a provider fail visibly and retain durable events; the development sink remains explicitly labeled.
