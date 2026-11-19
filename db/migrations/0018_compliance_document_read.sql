-- 0018: grant compliance_officer document.read so legal-hold and document deletion
-- workflows (which SELECT under RLS before UPDATE) succeed for compliance staff.
INSERT INTO iam.role_permission (role_id, permission_code)
SELECT r.id, 'document.read'
FROM iam.role r
WHERE r.org_id IS NULL AND r.code = 'compliance_officer'
ON CONFLICT DO NOTHING;
