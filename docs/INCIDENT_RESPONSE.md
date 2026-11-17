# Security incident runbook — clinic completion required

Named incident commander: unassigned. Named DPO/legal notification owner: unassigned. Escalation contacts, regulator/patient notification decisions and applicable deadlines must be supplied and approved by the clinic before go-live; code does not infer them.

Signals include a broken audit seal, unexpected sealed-range changes, repeated MFA/permission denials, privacy volume alerts, scanner detections and loss of backup/recovery capability. A failed verification command can also mean database unavailability or configuration failure; inspect the typed failure before declaring tampering.

1. Record detection time in UTC, request/job IDs and affected systems using the restricted incident record. Preserve original audit rows, seals, logs and independent anchors; do not rewrite history to silence an alert.
2. The incident commander and DPO assess access scope. Suspend/revoke affected accounts/sessions through authorized commands, rotate compromised secrets under maintenance control, isolate affected infrastructure and use the clinic-approved downtime procedure. Avoid copying medical bodies into ordinary tickets/logs.
3. Verify seals and compare independently stored heads. Restore into an isolated environment and reconcile source data, permissions, keys, objects and post-backup activity before any governed cutover. Do not overwrite a live clinic with an old dump.
4. The DPO/legal owner determines required notifications and transfer/provider implications using current official requirements and the clinic's contracts. Maintain the decision, recipient, authorized sender and delivery evidence; no provider notification is sent automatically by this repository.
5. Record recovery verification, remaining care follow-ups, root cause and corrective actions. Rehearse a staged tamper/alert and recovery scenario before accepting deployment.

The supplied systemd units only record failures to authpriv/journald. External forwarding, immutable anchor storage, paging, clinical downtime arrangements and named escalation ownership remain deferred deployment/clinic tasks in DEPLOYMENT_DECISIONS.md.
