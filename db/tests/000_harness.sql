-- Test harness (test databases only; never deployed).
CREATE SCHEMA IF NOT EXISTS test;
CREATE TABLE IF NOT EXISTS test.result (id serial PRIMARY KEY, suite text, name text, passed boolean, detail text);
GRANT USAGE ON SCHEMA test TO spartial_app, spartial_worker;

CREATE OR REPLACE FUNCTION test.record(p_name text, p_passed boolean, p_detail text DEFAULT NULL)
RETURNS void LANGUAGE sql SECURITY DEFINER AS
$$ INSERT INTO test.result (suite, name, passed, detail) VALUES (current_setting('test.suite', true), p_name, p_passed, p_detail) $$;

CREATE OR REPLACE FUNCTION test.ok(p_cond boolean, p_name text) RETURNS void LANGUAGE plpgsql AS $$
BEGIN PERFORM test.record(p_name, coalesce(p_cond, false), CASE WHEN p_cond IS NULL THEN 'condition was NULL' END); END $$;

CREATE OR REPLACE FUNCTION test.eq(p_actual anyelement, p_expected anyelement, p_name text) RETURNS void LANGUAGE plpgsql AS $$
BEGIN PERFORM test.record(p_name, p_actual IS NOT DISTINCT FROM p_expected,
                          format('expected %s, got %s', p_expected, p_actual)); END $$;

-- Executes p_sql in a subtransaction and expects SQLSTATE p_state.
CREATE OR REPLACE FUNCTION test.throws(p_sql text, p_state text, p_name text) RETURNS void LANGUAGE plpgsql AS $$
BEGIN
  BEGIN
    EXECUTE p_sql;
    PERFORM test.record(p_name, false, 'no error raised');
  EXCEPTION WHEN OTHERS THEN
    PERFORM test.record(p_name, SQLSTATE = p_state, format('expected %s, got %s: %s', p_state, SQLSTATE, SQLERRM));
  END;
END $$;

-- Executes p_sql and expects success.
CREATE OR REPLACE FUNCTION test.lives(p_sql text, p_name text) RETURNS void LANGUAGE plpgsql AS $$
BEGIN
  BEGIN EXECUTE p_sql; PERFORM test.record(p_name, true);
  EXCEPTION WHEN OTHERS THEN PERFORM test.record(p_name, false, SQLSTATE || ': ' || SQLERRM); END;
END $$;

CREATE OR REPLACE FUNCTION test.scalar(p_sql text) RETURNS text LANGUAGE plpgsql AS $$
DECLARE v text; BEGIN EXECUTE p_sql INTO v; RETURN v; END $$;

GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA test TO spartial_app, spartial_worker;
