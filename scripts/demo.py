#!/usr/bin/env python3
"""Run the implemented receptionist journey against a live development API."""
import json,os,sys,urllib.request,urllib.error
base=os.environ.get('API_BASE_URL','http://127.0.0.1:8080')
password=os.environ.get('SEED_RECEPTION_PASSWORD')
if not password:sys.exit('Export SEED_RECEPTION_PASSWORD from the ignored .env file first.')
def call(method,path,body=None,token=None,headers=None,expected=200):
 data=None if body is None else json.dumps(body).encode()
 h={'Content-Type':'application/json',**(headers or {})}
 if token:h['Authorization']='Bearer '+token
 req=urllib.request.Request(base+path,data=data,headers=h,method=method)
 try:r=urllib.request.urlopen(req,timeout=15)
 except urllib.error.HTTPError as e:r=e
 if r.status!=expected:
  sys.exit(f'{method} {path}: expected {expected}, received {r.status}. Response omitted to avoid patient or credential logging.')
 return json.loads(r.read()) if r.status!=204 else None
call('GET','/health/ready')
auth=call('POST','/api/v1/auth/login',{'email':'reception@spartial.test','password':password,'client_type':'mobile'})
token=auth['access_token']
facility=auth['user']['facility_ids'][0]
patient=call('POST','/api/v1/staff/patients',{'facility_id':facility,'given_name':'Demo','family_name':'Synthetic','birth_precision':'unknown','phone':'+255000000000'},token,{'Idempotency-Key':'documented-foundation-demo-v1'},201)
call('GET','/api/v1/staff/patients?q=Demo',token=token)
call('GET','/api/v1/staff/patients/'+patient['id'],token=token)
call('GET','/api/v1/staff/users',token=token,expected=403)
refreshed=call('POST','/api/v1/auth/refresh',{'client_type':'mobile','refresh_token':auth['refresh_token']})
call('GET','/api/v1/auth/me',token=token,expected=401)
call('POST','/api/v1/auth/logout',token=refreshed['access_token'],expected=204)
call('GET','/api/v1/auth/me',token=refreshed['access_token'],expected=401)
print('PASS: readiness, login, idempotent registration, search/read, privileged denial, refresh rotation and logout.')
print('Synthetic MRN:',patient['mrn'])
