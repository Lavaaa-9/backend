#!/usr/bin/env python3
"""Exercise MFA against the live development API using an isolated synthetic staff account."""
import base64,hashlib,hmac,json,os,secrets,struct,sys,time,urllib.request,urllib.error,uuid
if os.environ.get('APP_ENV')!='development':sys.exit('Security demo requires APP_ENV=development and synthetic data.')
base=os.environ.get('API_BASE_URL','http://127.0.0.1:8080')
admin_password=os.environ.get('SEED_ADMIN_PASSWORD')
if not admin_password:sys.exit('Export the ignored development .env file first.')
def call(method,path,body=None,token=None,expected=200):
 data=None if body is None else json.dumps(body).encode()
 headers={'Content-Type':'application/json'}
 if token:headers['Authorization']='Bearer '+token
 try:r=urllib.request.urlopen(urllib.request.Request(base+path,data=data,headers=headers,method=method),timeout=15)
 except urllib.error.HTTPError as e:r=e
 if r.status!=expected:sys.exit(f'{method} {path}: expected {expected}, received {r.status}. Sensitive response omitted.')
 return json.loads(r.read()) if r.status!=204 else None
admin=call('POST','/api/v1/auth/login',{'email':'admin@spartial.test','password':admin_password,'client_type':'mobile'})
if admin.get('mfa_required'):sys.exit('The development administrator has MFA enabled; use the PostgreSQL integration suite for this demo instead.')
a=admin['access_token'];facility=admin['user']['facility_ids'][0]
email='security-smoke-'+uuid.uuid4().hex+'@spartial.test';password=secrets.token_urlsafe(32)
user=call('POST','/api/v1/staff/users',{'email':email,'display_name':'Synthetic MFA Smoke','password':password,'role':'privacy-reviewer','facility_ids':[facility]},a,201)
def login():return call('POST','/api/v1/auth/login',{'email':email,'password':password,'client_type':'mobile'})
def verify(ch,code):return call('POST','/api/v1/auth/mfa/verify',{'challenge_token':ch,'recovery_code':code,'client_type':'mobile'})
session=login()['access_token']
enrollment=call('POST','/api/v1/auth/mfa/enroll',{'current_password':password},session)
secret=base64.b32decode(enrollment['secret'])
counter=int(time.time())//30-1
mac=hmac.new(secret,struct.pack('>Q',counter),hashlib.sha1).digest();offset=mac[-1]&15
code=f'{(struct.unpack(">I",mac[offset:offset+4])[0]&0x7fffffff)%1000000:06d}'
codes=call('POST','/api/v1/auth/mfa/confirm',{'code':code},session)['recovery_codes']
challenge=login()
if not challenge.get('mfa_required') or 'access_token' in challenge:sys.exit('MFA password step unexpectedly issued credentials.')
second=verify(challenge['challenge_token'],codes[0])['access_token']
replacement=call('POST','/api/v1/auth/mfa/recovery-codes/regenerate',{'current_password':password,'recovery_code':codes[1]},second)['recovery_codes']
call('GET','/api/v1/auth/me',token=session,expected=401)
call('POST','/api/v1/auth/mfa/disable',{'current_password':password,'recovery_code':replacement[0]},second,204)
call('GET','/api/v1/auth/me',token=second,expected=401)
# Preserve history and deactivate the disposable synthetic account with its actual version.
match=None;cursor=''
while match is None:
 page=call('GET','/api/v1/staff/users?limit=100'+('&cursor='+cursor if cursor else ''),token=a)
 match=next((u for u in page['items'] if u['id']==user['id']),None)
 cursor=page['next_cursor']
 if match is None and not cursor:sys.exit('Smoke passed but the synthetic account could not be found for deactivation.')
call('PUT','/api/v1/staff/users/'+user['id'],{'display_name':match['display_name'],'role':match['role'],'active':False,'facility_ids':match['facility_ids'],'version':match['version']},a)
call('POST','/api/v1/auth/logout',token=a,expected=204)
print('PASS: MFA enrollment/confirmation, challenge login, recovery regeneration, session revocation and disablement.')
print('Disposable synthetic staff account deactivated; credentials and recovery codes were not printed.')
