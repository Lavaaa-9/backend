#!/usr/bin/env bash
# Applies migrations in order, each in its own transaction, as spartial_owner.
# Usage: PGHOST=... PGUSER=<admin> tools/migrate.sh <database>
set -euo pipefail
DB="${1:?database name}"
DIR="$(cd "$(dirname "$0")/.." && pwd)/migrations"
psql -v ON_ERROR_STOP=1 -q -d postgres -f "$DIR/0000_cluster_roles.sql"
psql -v ON_ERROR_STOP=1 -q -d postgres -c "CREATE DATABASE \"$DB\" OWNER spartial_owner" 2>/dev/null || true
psql -v ON_ERROR_STOP=1 -q -d "$DB" -c "CREATE TABLE IF NOT EXISTS public.schema_migration (version text PRIMARY KEY, checksum text NOT NULL, applied_at timestamptz NOT NULL DEFAULT now()); ALTER TABLE public.schema_migration OWNER TO spartial_owner;"
for f in "$DIR"/0[0-9][0-9][0-9]_*.sql; do
  v="$(basename "$f" .sql)"; [[ "$v" == 0000_* ]] && continue
  sum="$(sha256sum "$f" | cut -d' ' -f1)"
  applied="$(psql -tA -d "$DB" -c "SELECT checksum FROM public.schema_migration WHERE version = '$v'")"
  if [[ -n "$applied" ]]; then
    [[ "$applied" == "$sum" ]] || { echo "CHECKSUM DRIFT in applied migration $v" >&2; exit 2; }
    continue
  fi
  echo "applying $v"
  { echo "BEGIN; SET LOCAL ROLE spartial_owner; SET LOCAL lock_timeout = '5s';"; cat "$f";
    echo "INSERT INTO public.schema_migration (version, checksum) VALUES ('$v', '$sum'); COMMIT;"; } \
    | psql -v ON_ERROR_STOP=1 -q -X -d "$DB" >/dev/null
done
echo "migrations up to date"
