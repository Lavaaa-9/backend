# Implemented foundation API examples

The easiest complete, runnable current journey is `python3 scripts/demo.py` after exporting the generated `.env`. It performs registration/search/read, checks authorization denial, rotates refresh credentials, and verifies logout. The full clinical synthetic journey remains pending later modules.

These curl commands also run against the live development API. They keep credentials in temporary mode-600 files rather than print tokens:

```bash
set -a; source .env; set +a
umask 077
mkdir -p .local
python3 - <<'PY'
import json,os
with open('.local/login.json','w') as f:
 json.dump({'email':'reception@spartial.test','password':os.environ['SEED_RECEPTION_PASSWORD'],'client_type':'mobile'},f)
PY
curl --fail-with-body --silent http://localhost:8080/api/v1/auth/login \
  -H 'Content-Type: application/json' --data-binary @.local/login.json \
  --output .local/session.json
ACCESS_TOKEN=$(python3 -c "import json; print(json.load(open('.local/session.json'))['access_token'])")
FACILITY_ID=$(python3 -c "import json; print(json.load(open('.local/session.json'))['user']['facility_ids'][0])")
export FACILITY_ID
python3 - <<'PY'
import json,os
with open('.local/patient.json','w') as f:
 json.dump({'facility_id':os.environ['FACILITY_ID'],'given_name':'Curl','family_name':'Synthetic','birth_precision':'year','birth_year':1990,'phone':'+255000000000'},f)
PY
curl --fail-with-body http://localhost:8080/api/v1/staff/patients \
  -H "Authorization: Bearer $ACCESS_TOKEN" -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: curl-foundation-patient-v1' --data-binary @.local/patient.json
curl --fail-with-body 'http://localhost:8080/api/v1/staff/patients?q=Curl&limit=25' \
  -H "Authorization: Bearer $ACCESS_TOKEN"
# Expected 403 for receptionist:
curl --silent --output /dev/null --write-out '%{http_code}\n' \
  http://localhost:8080/api/v1/staff/users -H "Authorization: Bearer $ACCESS_TOKEN"
curl --fail-with-body -X POST http://localhost:8080/api/v1/auth/logout \
  -H "Authorization: Bearer $ACCESS_TOKEN"
rm -f .local/login.json .local/session.json .local/patient.json
unset ACCESS_TOKEN FACILITY_ID
```

Use a different idempotency key for a deliberately different patient; keep the same key when safely retrying the same registration. Different bodies with an existing key return 409.

Browser login request: `POST /api/v1/auth/login` with `{"email":"reception@spartial.test","password":"<generated development password>","client_type":"web"}`. Send the configured Origin automatically from your browser and use `credentials: "include"`. Response includes access token and profile, with the refresh token only in HttpOnly cookie. Refresh body is `{"client_type":"web"}`; no refresh token goes into JavaScript storage. Logout sends access Authorization plus Origin. Mobile refresh sends `{"client_type":"mobile","refresh_token":"<current secure-storage token>"}` without Origin.

`PUT /staff/patients/{id}` replaces `given_name`, `family_name`, `birth_precision`, `birth_date`/`birth_year`, `phone`, and `address`, plus current `version`. Omitted phone/address become empty; omit both birth values for unknown precision. MRN and facility cannot be patched. Updated response increments version; stale requests get 409.

Administrator operations: GET/POST `/staff/users`, PUT `/staff/users/{id}`, GET/POST `/staff/facilities`, GET `/staff/audit`. Payloads, bounds and permissions are specified in OpenAPI. No clinical, billing, guardian or patient-account API is available yet.

Error shape:

```json
{"error":{"code":"conflict","message":"Record changed or conflicts with existing data","fields":{}},"request_id":"00000000-0000-4000-8000-000000000001"}
```

Use request_id for support without logging credentials or medical bodies. Access credentials are short-lived opaque tokens. Server session state and live permissions are checked on every protected request.

## MFA flow

An enrolled staff login responds with `mfa_required`, `challenge_token` and `expires_in`, without access/refresh credentials. Complete `POST /api/v1/auth/mfa/verify` using matching client_type plus exactly one `code` or `recovery_code`. Successful browser verification sets the normal HttpOnly refresh cookie and requires allowed Origin. Mobile verification returns refresh JSON without Origin.

Enrollment (`/auth/mfa/enroll`) needs `current_password`; confirmation (`/auth/mfa/confirm`) needs a six-digit code and returns ten recovery codes once. Regeneration (`/auth/mfa/recovery-codes/regenerate`) and disablement (`/auth/mfa/disable`) require password plus one unused factor. See SECURITY_PHASE1.md for replay, revocation, expiry and rate limits. `python3 scripts/demo-security.py` provides a runnable synthetic journey without printing credentials.
