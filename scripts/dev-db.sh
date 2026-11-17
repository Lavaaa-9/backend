#!/usr/bin/env bash
# Native PostgreSQL alternative to Compose. Creates only this project's .local database.
set -euo pipefail
cd "$(dirname "$0")/.."
mkdir -p .local
chmod 700 .local
if [[ ! -f .env ]]; then
  python3 - <<'PY'
import os,secrets,base64
owner,app,admin,reception=[secrets.token_hex(24) for _ in range(4)]
phi_key='dev1:'+base64.b64encode(secrets.token_bytes(32)).decode()
lines=dict(APP_ENV='development',HTTP_ADDR='127.0.0.1:8080',ALLOWED_ORIGINS='http://localhost:3000',CLINIC_TIMEZONE='Africa/Dar_es_Salaam',PG_OWNER_PASSWORD=owner,APP_DB_ROLE='spartial_app',APP_DB_PASSWORD=app,DATABASE_ADMIN_URL=f'postgres://spartial_owner:{owner}@127.0.0.1:55432/spartial?sslmode=disable',DATABASE_URL=f'postgres://spartial_app:{app}@127.0.0.1:55432/spartial?sslmode=disable',TEST_DATABASE_URL=f'postgres://spartial_owner:{owner}@127.0.0.1:55432/postgres?sslmode=disable',SEED_ADMIN_PASSWORD=admin,SEED_RECEPTION_PASSWORD=reception,MINIO_ROOT_USER='spartial-development',MINIO_ROOT_PASSWORD=secrets.token_hex(24),PHI_ENCRYPTION_KEYS=phi_key,PHI_ACTIVE_KEY_ID='dev1',MFA_ENFORCE_PRIVILEGED='false')
fd=os.open('.env',os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
with os.fdopen(fd,'w') as f:
 for k,v in lines.items(): f.write(f'{k}={v}\n')
PY
fi
# Backfill only a missing development keyring. Never replace an existing encryption key.
python3 - <<'PYKEY'
from pathlib import Path
import base64,secrets,os
p=Path('.env');s=p.read_text();names={line.partition('=')[0] for line in s.splitlines()}
if 'PHI_ENCRYPTION_KEYS' not in names and 'PHI_ACTIVE_KEY_ID' not in names:
 with p.open('a') as f:
  f.write('\nPHI_ENCRYPTION_KEYS=dev1:'+base64.b64encode(secrets.token_bytes(32)).decode()+'\nPHI_ACTIVE_KEY_ID=dev1\n')
 os.chmod(p,0o600)
PYKEY
set -a
source .env
set +a
if [[ "${1:-start}" == stop ]]; then
  pg_ctl -D "$PWD/.local/postgres" stop -m fast
  exit
fi
if [[ ! -f .local/postgres/PG_VERSION ]]; then
  umask 077
  printf '%s\n' "$PG_OWNER_PASSWORD" > .local/init-password
  trap 'rm -f .local/init-password' EXIT
  initdb -D "$PWD/.local/postgres" -U spartial_owner --auth-local=trust --auth-host=scram-sha-256 --pwfile=.local/init-password > .local/initdb.log
  rm -f .local/init-password
fi
if ! pg_ctl -D "$PWD/.local/postgres" status >/dev/null 2>&1; then
  pg_ctl -D "$PWD/.local/postgres" -l "$PWD/.local/postgres.log" -o "-h 127.0.0.1 -p 55432 -k $PWD/.local" start
fi
if [[ "$(psql "$TEST_DATABASE_URL" -Atqc "SELECT 1 FROM pg_database WHERE datname='spartial'")" != 1 ]]; then
  psql "$TEST_DATABASE_URL" -v ON_ERROR_STOP=1 -c 'CREATE DATABASE spartial' >/dev/null
fi
go run ./cmd/manage migrate
go run ./cmd/manage grant-runtime
go run ./cmd/manage seed
printf '%s\n' 'Development database ready. Credentials are in the ignored .env file.'
