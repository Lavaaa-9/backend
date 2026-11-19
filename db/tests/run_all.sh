#!/usr/bin/env bash
# Rebuilds an isolated test database from migrations and runs every suite.
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"; DB="${1:-spartial_test}"
psql -X -q -d postgres -c "DROP DATABASE IF EXISTS \"$DB\" WITH (FORCE)"
"$HERE/../tools/migrate.sh" "$DB" >/dev/null
for f in 000_harness 010_fixtures 100_workflow 200_security 400_scope_gaps; do
  psql -X -q -v ON_ERROR_STOP=1 -d "$DB" -f "$HERE/$f.sql" >/dev/null
done
"$HERE/300_concurrency.sh" "$DB"
psql -X -d "$DB" -c "SELECT suite, count(*) FILTER (WHERE passed) AS passed, count(*) FILTER (WHERE NOT passed) AS failed FROM test.result GROUP BY suite ORDER BY suite"
psql -X -d "$DB" -c "SELECT id, suite, name, detail FROM test.result WHERE NOT passed ORDER BY id"
test "$(psql -X -tA -d "$DB" -c "SELECT count(*) FROM test.result WHERE NOT passed")" = "0"
