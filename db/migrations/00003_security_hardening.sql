-- +goose Up
-- Phase 1: second-factor authentication, privileged-session assurance and
-- tamper-evident audit sealing. This does not establish regulatory compliance.

-- Session assurance: true only when the session was created after a verified second factor.
ALTER TABLE sessions ADD COLUMN mfa_verified boolean NOT NULL DEFAULT false;

-- TOTP secrets are encrypted by the application (AES-256-GCM, keyring in configuration).
-- The ciphertext is bound to the staff id as associated data, so rows cannot be swapped.
CREATE TABLE staff_mfa (
 staff_id uuid PRIMARY KEY REFERENCES staff(id),
 secret_ciphertext bytea NOT NULL CHECK(octet_length(secret_ciphertext) BETWEEN 40 AND 256),
 confirmed_at timestamptz,
 -- Highest accepted 30-second step; a code is never accepted twice (replay protection).
 last_used_step bigint NOT NULL DEFAULT 0 CHECK(last_used_step >= 0),
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now()
);

-- Single-use recovery codes. Only SHA-256 digests of 80-bit random codes are stored.
CREATE TABLE mfa_recovery_codes (
 code_hash text PRIMARY KEY CHECK(length(code_hash) = 64),
 staff_id uuid NOT NULL REFERENCES staff(id),
 used_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX mfa_recovery_staff ON mfa_recovery_codes(staff_id);

-- Password step completed, second factor pending. No session exists until this is consumed.
CREATE TABLE login_challenges (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 token_hash text NOT NULL UNIQUE CHECK(length(token_hash) = 64),
 staff_id uuid NOT NULL REFERENCES staff(id),
 -- A password/role/suspension change bumps staff.version and invalidates pending challenges.
 staff_version integer NOT NULL,
 client_type text NOT NULL CHECK(client_type IN ('web','mobile')),
 attempts integer NOT NULL DEFAULT 0 CHECK(attempts BETWEEN 0 AND 5),
 expires_at timestamptz NOT NULL,
 consumed_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT now(),
 CHECK(expires_at <= created_at + interval '10 minutes')
);
CREATE INDEX login_challenges_staff ON login_challenges(staff_id);

-- Audit context for future break-glass and facility-scoped review.
ALTER TABLE audit_events ADD COLUMN reason text CHECK(reason IS NULL OR length(reason) BETWEEN 1 AND 500);
ALTER TABLE audit_events ADD COLUMN facility_id uuid REFERENCES facilities(id);

-- Tamper evidence: events are chained (h_n = sha256(h_{n-1} || sha256(event_n))) and each
-- seal records the running chain head. Seal heads must also be shipped off-host (log/SIEM),
-- because a database owner could otherwise rewrite events and seals together.
CREATE TABLE audit_seals (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 first_event_id bigint NOT NULL,
 last_event_id bigint NOT NULL UNIQUE,
 event_count integer NOT NULL CHECK(event_count > 0),
 previous_hash bytea NOT NULL CHECK(octet_length(previous_hash) = 32),
 chain_hash bytea NOT NULL CHECK(octet_length(chain_hash) = 32),
 sealed_at timestamptz NOT NULL DEFAULT now(),
 CHECK(first_event_id <= last_event_id)
);
CREATE TRIGGER audit_seals_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON audit_seals FOR EACH STATEMENT EXECUTE FUNCTION reject_audit_mutation();

-- +goose StatementBegin
CREATE FUNCTION audit_event_digest(e audit_events) RETURNS bytea LANGUAGE sql IMMUTABLE AS $$
 SELECT sha256(convert_to(jsonb_build_array(
  e.id::text, e.actor_id::text, e.action, e.resource_id::text,
  e.patient_id::text, e.request_id::text, e.outcome,
  to_char(e.created_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
  e.reason, e.facility_id::text)::text, 'UTF8'))
$$;
-- +goose StatementEnd

-- Seal a contiguous visible prefix. SHARE blocks inserts and waits for current writers;
-- an age filter alone cannot make identity allocation match commit order.
-- +goose StatementBegin
CREATE FUNCTION seal_audit_events(settle interval DEFAULT interval '5 minutes', max_events integer DEFAULT 50000)
RETURNS TABLE(sealed_count integer, last_event_id bigint, chain_hash text) LANGUAGE plpgsql AS $$
DECLARE
 prev bytea;
 after_id bigint;
 h bytea;
 first_id bigint;
 last_id bigint;
 n integer := 0;
 ev audit_events;
BEGIN
 IF settle IS NULL OR settle < interval '0' OR max_events IS NULL OR max_events < 1 OR max_events > 50000 THEN
  RAISE EXCEPTION 'invalid audit sealing limits' USING ERRCODE='22023';
 END IF;
 IF current_setting('transaction_isolation') <> 'read committed' THEN
  RAISE EXCEPTION 'audit sealing requires read committed isolation';
 END IF;
 PERFORM pg_advisory_xact_lock(8201772);
 PERFORM set_config('lock_timeout','2s',true);
 LOCK TABLE audit_events IN SHARE MODE;
 SELECT s.chain_hash, s.last_event_id INTO prev, after_id FROM audit_seals s ORDER BY s.id DESC LIMIT 1;
 IF prev IS NULL THEN prev := '\x0000000000000000000000000000000000000000000000000000000000000000'::bytea; after_id := 0; END IF;
 h := prev;
 FOR ev IN SELECT * FROM audit_events a WHERE a.id > after_id
  AND a.id < COALESCE((SELECT min(waiting.id) FROM audit_events waiting
    WHERE waiting.id > after_id AND waiting.created_at >= now() - settle),9223372036854775807)
  ORDER BY a.id LIMIT max_events LOOP
  IF first_id IS NULL THEN first_id := ev.id; END IF;
  h := sha256(h || audit_event_digest(ev));
  last_id := ev.id;
  n := n + 1;
 END LOOP;
 IF n > 0 THEN
  INSERT INTO audit_seals(first_event_id,last_event_id,event_count,previous_hash,chain_hash) VALUES(first_id,last_id,n,prev,h);
 END IF;
 RETURN QUERY SELECT n, COALESCE(last_id, after_id), encode(h,'hex');
END $$;
-- +goose StatementEnd

-- Recomputes the whole chain. Returns the first seal that does not match, or NULL when intact.
-- +goose StatementBegin
CREATE FUNCTION verify_audit_chain() RETURNS TABLE(seals_checked integer, events_checked bigint, first_broken_seal bigint, unsealed_events bigint) LANGUAGE plpgsql STABLE AS $$
DECLARE
 s audit_seals;
 h bytea := '\x0000000000000000000000000000000000000000000000000000000000000000'::bytea;
 after_id bigint := 0;
 ev audit_events;
 checked integer := 0;
 events bigint := 0;
 n integer;
 first_id bigint;
 tail bigint;
BEGIN
 SELECT count(*) INTO tail FROM audit_events WHERE id>COALESCE((SELECT max(last_event_id) FROM audit_seals),0);
 FOR s IN SELECT * FROM audit_seals ORDER BY id LOOP
  IF s.previous_hash <> h THEN RETURN QUERY SELECT checked, events, s.id, tail; RETURN; END IF;
  n := 0;
  first_id := NULL;
  FOR ev IN SELECT * FROM audit_events a WHERE a.id > after_id AND a.id <= s.last_event_id ORDER BY a.id LOOP
   IF first_id IS NULL THEN first_id := ev.id; END IF;
   h := sha256(h || audit_event_digest(ev));
   n := n + 1;
  END LOOP;
  events := events + n;
  IF h <> s.chain_hash OR n <> s.event_count OR first_id IS DISTINCT FROM s.first_event_id OR s.last_event_id <= after_id THEN RETURN QUERY SELECT checked, events, s.id, tail; RETURN; END IF;
  after_id := s.last_event_id;
  checked := checked + 1;
 END LOOP;
 RETURN QUERY SELECT checked, events, NULL::bigint, tail;
END $$;
-- +goose StatementEnd

REVOKE ALL ON FUNCTION seal_audit_events(interval,integer), verify_audit_chain(), audit_event_digest(audit_events) FROM PUBLIC;

-- +goose Down
DROP FUNCTION verify_audit_chain();
DROP FUNCTION seal_audit_events(interval,integer);
DROP FUNCTION audit_event_digest(audit_events);
DROP TABLE audit_seals;
ALTER TABLE audit_events DROP COLUMN facility_id;
ALTER TABLE audit_events DROP COLUMN reason;
DROP TABLE login_challenges, mfa_recovery_codes, staff_mfa;
ALTER TABLE sessions DROP COLUMN mfa_verified;
