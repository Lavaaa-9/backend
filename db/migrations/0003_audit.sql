-- =============================================================================
-- 0003_audit.sql — tamper-evident audit trail (HIPAA §164.312(b), SOC 2 CC7.2)
--
-- Design:
--  * audit.event is append-only (trigger + no UPDATE/DELETE grants), range
--    partitioned by month.
--  * Each row gets row_hash = SHA-256 over its canonical content at insert.
--  * A sealing job (audit.seal_pending) chains batches: seal_hash =
--    SHA-256(prev_seal_hash || row_hash_1 || ... || row_hash_n). A batch is all
--    rows whose inserting transaction id < the oldest in-progress xid, so the
--    batch membership is final (no late commits can join it). No hot lock is
--    taken on the business write path.
--  * Seal hashes are also exported to WORM object storage (operational control),
--    so a DB superuser rewriting history is detectable.
-- =============================================================================

CREATE SEQUENCE audit.event_id_seq AS bigint;

CREATE TABLE audit.event (
  id               bigint      NOT NULL DEFAULT nextval('audit.event_id_seq'),
  occurred_at      timestamptz NOT NULL DEFAULT clock_timestamp(),
  txid             xid8        NOT NULL DEFAULT pg_current_xact_id(),
  org_id           uuid,                         -- NULL for pre-tenant auth events
  actor_type       text NOT NULL CHECK (actor_type IN ('staff','patient','service','system','anonymous')),
  actor_user_id    uuid,
  actor_membership_id uuid,
  session_id       uuid,
  request_id       uuid,                         -- correlation id (X-Request-ID)
  action           text NOT NULL CHECK (action ~ '^[a-z_]+(\.[a-z_]+){1,4}$'),  -- e.g. patient.record.view
  category         text NOT NULL CHECK (category IN ('authentication','authorization','phi_access','data_change',
                                                     'administration','financial','integration','security','system')),
  resource_type    text,
  resource_id      uuid,
  patient_id       uuid,                         -- enables per-patient access/disclosure reports
  outcome          text NOT NULL CHECK (outcome IN ('success','denied','failure','error')),
  reason           text CHECK (length(reason) <= 1000),   -- break-glass justification, denial reason
  source_ip        inet,
  user_agent       text CHECK (length(user_agent) <= 512),
  changes          jsonb CHECK (changes IS NULL OR jsonb_typeof(changes) = 'object'),
  row_hash         bytea NOT NULL,
  PRIMARY KEY (occurred_at, id)
) PARTITION BY RANGE (occurred_at);
COMMENT ON TABLE audit.event IS 'Append-only, hash-sealed audit trail. Classified PHI-RESTRICTED (may contain before/after clinical values).';

CREATE INDEX ix_audit_org_time      ON audit.event (org_id, occurred_at DESC);
CREATE INDEX ix_audit_patient_time  ON audit.event (patient_id, occurred_at DESC) WHERE patient_id IS NOT NULL;
CREATE INDEX ix_audit_actor_time    ON audit.event (actor_user_id, occurred_at DESC);
CREATE INDEX ix_audit_resource      ON audit.event (resource_type, resource_id);
CREATE INDEX ix_audit_request       ON audit.event (request_id) WHERE request_id IS NOT NULL;
CREATE INDEX ix_audit_txid          ON audit.event (txid);

CREATE TABLE audit.event_default PARTITION OF audit.event DEFAULT;

CREATE OR REPLACE FUNCTION audit.ensure_partitions(p_months_ahead int DEFAULT 3)
RETURNS void LANGUAGE plpgsql AS $$
DECLARE v_start date; v_name text;
BEGIN
  FOR i IN 0..p_months_ahead LOOP
    v_start := (date_trunc('month', now() AT TIME ZONE 'UTC') + make_interval(months => i))::date;
    v_name  := format('event_%s', to_char(v_start, 'YYYYMM'));
    IF to_regclass('audit.' || v_name) IS NULL THEN
      EXECUTE format('CREATE TABLE audit.%I PARTITION OF audit.event FOR VALUES FROM (%L) TO (%L)',
                     v_name, v_start::timestamptz, (v_start + interval '1 month')::timestamptz);
    END IF;
  END LOOP;
END $$;
SELECT audit.ensure_partitions(3);

CREATE OR REPLACE FUNCTION audit.compute_row_hash() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  NEW.actor_user_id       := coalesce(NEW.actor_user_id, sec.current_user_id());
  NEW.actor_membership_id := coalesce(NEW.actor_membership_id, sec.current_membership_id());
  NEW.session_id          := coalesce(NEW.session_id, sec.current_session_id());
  NEW.request_id          := coalesce(NEW.request_id, sec.current_request_id());
  NEW.row_hash := sha256(convert_to(concat_ws('|',
      NEW.id, to_char(NEW.occurred_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US'), NEW.org_id,
      NEW.actor_type, NEW.actor_user_id, NEW.actor_membership_id, NEW.session_id, NEW.request_id,
      NEW.action, NEW.category, NEW.resource_type, NEW.resource_id, NEW.patient_id, NEW.outcome,
      NEW.reason, host(NEW.source_ip), NEW.user_agent, NEW.changes::text), 'UTF8'));
  RETURN NEW;
END $$;
CREATE TRIGGER trg_audit_hash BEFORE INSERT ON audit.event FOR EACH ROW EXECUTE FUNCTION audit.compute_row_hash();
CREATE TRIGGER trg_audit_append_only BEFORE UPDATE OR DELETE ON audit.event FOR EACH ROW EXECUTE FUNCTION sec.forbid_mutation();
CREATE TRIGGER trg_audit_no_truncate BEFORE TRUNCATE ON audit.event FOR EACH STATEMENT EXECUTE FUNCTION sec.forbid_mutation();

-- Seals ----------------------------------------------------------------------
CREATE TABLE audit.seal (
  seal_no       bigint PRIMARY KEY,
  xid_lower     xid8 NOT NULL,
  xid_upper     xid8 NOT NULL,
  row_count     bigint NOT NULL CHECK (row_count >= 0),
  prev_hash     bytea NOT NULL,
  seal_hash     bytea NOT NULL,
  sealed_at     timestamptz NOT NULL DEFAULT now(),
  exported_at   timestamptz,          -- set by the WORM export job
  export_uri    text,
  CONSTRAINT ck_seal_range CHECK (xid_upper >= xid_lower)
);
CREATE TRIGGER trg_seal_append_only BEFORE DELETE ON audit.seal FOR EACH ROW EXECUTE FUNCTION sec.forbid_mutation();
CREATE OR REPLACE FUNCTION audit.seal_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF (to_jsonb(NEW) - ARRAY['exported_at','export_uri']) IS DISTINCT FROM (to_jsonb(OLD) - ARRAY['exported_at','export_uri']) THEN
    RAISE EXCEPTION 'audit seals are immutable' USING ERRCODE = 'SPA01';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER trg_seal_guard BEFORE UPDATE ON audit.seal FOR EACH ROW EXECUTE FUNCTION audit.seal_guard();

CREATE OR REPLACE FUNCTION audit.seal_pending() RETURNS audit.seal
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog AS $$
DECLARE
  v_prev audit.seal; v_lower xid8; v_upper xid8; v_hash bytea; v_count bigint; v_new audit.seal;
BEGIN
  LOCK TABLE audit.seal IN EXCLUSIVE MODE;               -- one sealer at a time
  SELECT * INTO v_prev FROM audit.seal ORDER BY seal_no DESC LIMIT 1;
  v_lower := coalesce(v_prev.xid_upper, '0'::xid8);
  v_upper := pg_snapshot_xmin(pg_current_snapshot());    -- every xid below this has finished
  IF v_upper <= v_lower THEN RETURN NULL; END IF;
  SELECT count(*), string_agg(row_hash, ''::bytea ORDER BY id)
    INTO v_count, v_hash
    FROM audit.event WHERE txid >= v_lower AND txid < v_upper;
  INSERT INTO audit.seal (seal_no, xid_lower, xid_upper, row_count, prev_hash, seal_hash)
  VALUES (coalesce(v_prev.seal_no, 0) + 1, v_lower, v_upper, v_count,
          coalesce(v_prev.seal_hash, '\x00'::bytea),
          sha256(coalesce(v_prev.seal_hash, '\x00'::bytea) || coalesce(v_hash, ''::bytea)))
  RETURNING * INTO v_new;
  RETURN v_new;
END $$;

-- Recompute row hashes and seal chain; returns every inconsistency found.
CREATE OR REPLACE FUNCTION audit.verify_integrity()
RETURNS TABLE (problem text, ref text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog AS $$
DECLARE s audit.seal; v_prev bytea := '\x00'::bytea; v_hash bytea; v_count bigint;
BEGIN
  RETURN QUERY
    SELECT 'row_hash_mismatch', e.id::text FROM audit.event e
    WHERE e.row_hash IS DISTINCT FROM sha256(convert_to(concat_ws('|',
      e.id, to_char(e.occurred_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US'), e.org_id,
      e.actor_type, e.actor_user_id, e.actor_membership_id, e.session_id, e.request_id,
      e.action, e.category, e.resource_type, e.resource_id, e.patient_id, e.outcome,
      e.reason, host(e.source_ip), e.user_agent, e.changes::text), 'UTF8'));
  FOR s IN SELECT * FROM audit.seal ORDER BY seal_no LOOP
    SELECT count(*), string_agg(row_hash, ''::bytea ORDER BY id) INTO v_count, v_hash
      FROM audit.event WHERE txid >= s.xid_lower AND txid < s.xid_upper;
    IF s.prev_hash <> v_prev THEN RETURN QUERY SELECT 'seal_chain_broken', s.seal_no::text; END IF;
    IF v_count <> s.row_count OR sha256(s.prev_hash || coalesce(v_hash, ''::bytea)) <> s.seal_hash THEN
      RETURN QUERY SELECT 'seal_content_mismatch', s.seal_no::text;
    END IF;
    v_prev := s.seal_hash;
  END LOOP;
END $$;

-- Generic data-change capture for PHI and financial tables.
-- TG_ARGV = columns to exclude from before/after values (ciphertext etc.).
CREATE OR REPLACE FUNCTION audit.capture_change() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog AS $$
DECLARE
  v_excl text[] := ARRAY['updated_at','updated_by','row_version','created_at','created_by'] || TG_ARGV::text[];
  v_old jsonb; v_new jsonb; v_changes jsonb; v_row jsonb; v_patient uuid; v_table text;
BEGIN
  v_table := TG_TABLE_SCHEMA || '.' || TG_TABLE_NAME;
  IF TG_OP <> 'INSERT' THEN v_old := to_jsonb(OLD) - v_excl; END IF;
  IF TG_OP <> 'DELETE' THEN v_new := to_jsonb(NEW) - v_excl; END IF;
  v_row := coalesce(to_jsonb(NEW), to_jsonb(OLD));

  IF TG_OP = 'UPDATE' THEN
    SELECT jsonb_object_agg(k, jsonb_build_object('old', v_old -> k, 'new', v_new -> k))
      INTO v_changes
      FROM jsonb_object_keys(v_new) k
     WHERE (v_old -> k) IS DISTINCT FROM (v_new -> k);
    IF v_changes IS NULL THEN RETURN NULL; END IF;     -- no material change
  ELSIF TG_OP = 'INSERT' THEN
    v_changes := jsonb_build_object('new', v_new);
  ELSE
    v_changes := jsonb_build_object('old', v_old);
  END IF;

  v_patient := CASE WHEN v_table = 'patient.patient' THEN (v_row ->> 'id')::uuid
                    ELSE (v_row ->> 'patient_id')::uuid END;

  INSERT INTO audit.event (org_id, actor_type, action, category, resource_type, resource_id,
                           patient_id, outcome, changes)
  VALUES ((v_row ->> 'org_id')::uuid, coalesce(sec.actor_type(), 'system'),
          replace(v_table, '.', '_') || '.' || lower(TG_OP), 'data_change',
          v_table, (v_row ->> 'id')::uuid, v_patient, 'success', v_changes);
  RETURN NULL;
END $$;

CREATE OR REPLACE FUNCTION audit.attach_capture(p_table regclass, p_exclude text[] DEFAULT '{}')
RETURNS void LANGUAGE plpgsql AS $$
BEGIN
  EXECUTE format('CREATE TRIGGER trg_audit_capture AFTER INSERT OR UPDATE OR DELETE ON %s
                  FOR EACH ROW EXECUTE FUNCTION audit.capture_change(%s)',
                 p_table, (SELECT coalesce(string_agg(quote_literal(c), ','), '') FROM unnest(p_exclude) c));
END $$;

-- Explicit application events: PHI reads, exports, logins, denials, break-glass.
CREATE OR REPLACE FUNCTION audit.log(
  p_action text, p_category text, p_outcome text DEFAULT 'success',
  p_resource_type text DEFAULT NULL, p_resource_id uuid DEFAULT NULL, p_patient_id uuid DEFAULT NULL,
  p_reason text DEFAULT NULL, p_source_ip inet DEFAULT NULL, p_user_agent text DEFAULT NULL,
  p_changes jsonb DEFAULT NULL, p_org_id uuid DEFAULT NULL, p_actor_user_id uuid DEFAULT NULL,
  p_actor_type text DEFAULT NULL)
RETURNS bigint LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog AS $$
DECLARE v_id bigint;
BEGIN
  INSERT INTO audit.event (org_id, actor_type, actor_user_id, action, category, resource_type, resource_id,
                           patient_id, outcome, reason, source_ip, user_agent, changes)
  -- Established request context always wins over caller-supplied identity
  -- (overrides exist only for pre-authentication events such as failed logins).
  VALUES (coalesce(sec.current_org_id(), p_org_id), coalesce(sec.actor_type(), p_actor_type, 'anonymous'),
          coalesce(sec.current_user_id(), p_actor_user_id), p_action, p_category, p_resource_type, p_resource_id,
          p_patient_id, p_outcome, p_reason, p_source_ip, left(p_user_agent, 512), p_changes)
  RETURNING id INTO v_id;
  RETURN v_id;
END $$;
