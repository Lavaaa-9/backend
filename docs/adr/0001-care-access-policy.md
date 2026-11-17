---
status: proposed
---
# Care relationships and exceptional access

The security engineering spec requires a care relationship in addition to permission and facility assignment. Implement explicit, attributable care assignments now, with later appointment/encounter/order modules responsible for creating and ending their own evidence; reception retains demographic registry access while clinical roles need active evidence. Development defaults permit doctors/nurses to request exceptional access within their assigned facility for at most four hours, reviewed by a different privacy reviewer; this supports testing without treating an implementation default as signed clinic policy.

The user authorized continuing implementation and asked to defer hosting/providers to documentation. Production clinical/break-glass activation requires an explicit clinic approval reference; the clinic lead's named eligibility/reviewer policy and sign-off remain outstanding. An approved reference must identify the actual signed record, not a software-generated approval. Reception can coordinate care assignments but cannot use exceptional access; system administrators have no patient permissions. Exceptional reasons stay in the restricted grant/audit record and ordinary logs receive only a machine reason.

A narrow patient-to-facility scope record supports checking a grant request without exposing demographics before access is authorized. Patient row-level policies independently check live role permissions, facility membership, and care evidence; transaction-local actor settings are always reset on transaction completion. This protects against omitted query filters, not against an attacker who already controls the database connection and can forge context settings.
