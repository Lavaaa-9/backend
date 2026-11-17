-- +goose Up
ALTER TABLE sessions ADD COLUMN last_seen_at timestamptz NOT NULL DEFAULT now();
ALTER TABLE sessions ADD COLUMN device_label text NOT NULL DEFAULT '' CHECK(length(device_label)<=80);
ALTER TABLE login_challenges ADD COLUMN device_label text NOT NULL DEFAULT '' CHECK(length(device_label)<=80);
CREATE TABLE approval_requests (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 kind text NOT NULL CHECK(kind IN ('admin_role_grant','mfa_reset')),
 subject_id uuid NOT NULL REFERENCES staff(id),
 subject_version integer NOT NULL CHECK(subject_version>0),
 requested_by uuid NOT NULL REFERENCES staff(id),
 reason text NOT NULL CHECK(length(reason) BETWEEN 20 AND 1000),
 expires_at timestamptz NOT NULL DEFAULT now()+interval '1 hour',
 created_at timestamptz NOT NULL DEFAULT now(),
 status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','approved','rejected','expired')),
 reviewed_by uuid REFERENCES staff(id),
 reviewed_at timestamptz,
 review_note text CHECK(review_note IS NULL OR length(review_note) BETWEEN 20 AND 1000),
 CHECK(expires_at>created_at AND expires_at<=created_at+interval '1 hour'),
 CHECK(reviewed_by IS NULL OR reviewed_by<>requested_by),
 CHECK((status='pending' AND reviewed_by IS NULL AND reviewed_at IS NULL AND review_note IS NULL)
    OR (status='expired' AND reviewed_by IS NULL AND reviewed_at IS NULL AND review_note IS NULL)
    OR (status IN ('approved','rejected') AND reviewed_by IS NOT NULL AND reviewed_at IS NOT NULL AND review_note IS NOT NULL))
);
CREATE INDEX approval_requests_pending ON approval_requests(created_at,id) WHERE status='pending';
CREATE TABLE privacy_alerts (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 actor_id uuid NOT NULL REFERENCES staff(id),
 rule text NOT NULL CHECK(rule IN ('patient_read_volume','mfa_denial_volume')),
 window_start timestamptz NOT NULL,
 window_end timestamptz NOT NULL,
 event_count integer NOT NULL CHECK(event_count>0),
 created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(actor_id,rule,window_start),
 CHECK(window_end>window_start)
);
INSERT INTO permissions VALUES('approvals.request'),('approvals.review');
INSERT INTO role_permissions VALUES('administrator','approvals.request'),('administrator','approvals.review');
-- Harden fixed-name audit functions against temporary relation shadowing.
ALTER FUNCTION seal_audit_events(interval,integer) SET search_path=pg_catalog,public,pg_temp;
ALTER FUNCTION verify_audit_chain() SET search_path=pg_catalog,public,pg_temp;
ALTER FUNCTION audit_event_digest(audit_events) SET search_path=pg_catalog,public,pg_temp;
-- +goose Down
ALTER FUNCTION audit_event_digest(audit_events) RESET search_path;
ALTER FUNCTION verify_audit_chain() RESET search_path;
ALTER FUNCTION seal_audit_events(interval,integer) RESET search_path;
DELETE FROM role_permissions WHERE permission_name IN ('approvals.request','approvals.review');
DELETE FROM permissions WHERE name IN ('approvals.request','approvals.review');
DROP TABLE privacy_alerts,approval_requests;
ALTER TABLE login_challenges DROP COLUMN device_label;
ALTER TABLE sessions DROP COLUMN device_label;
ALTER TABLE sessions DROP COLUMN last_seen_at;
