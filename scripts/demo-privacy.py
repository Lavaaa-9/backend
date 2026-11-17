#!/usr/bin/env python3
"""Exercise synthetic care access, emergency review and encrypted identifiers on a live dev API."""
import json,os,secrets,sys,urllib.request,urllib.error,uuid,datetime
if os.environ.get('APP_ENV')!='development':sys.exit('Synthetic development environment required.')
base=os.environ.get('API_BASE_URL','http://127.0.0.1:8080')
def call(method,path,body=None,token=None,expected=200,key=None):
 headers={'Content-Type':'application/json'}
 if token:headers['Authorization']='Bearer '+token
 if key:headers['Idempotency-Key']=key
 try:r=urllib.request.urlopen(urllib.request.Request(base+path,data=None if body is None else json.dumps(body).encode(),headers=headers,method=method),timeout=15)
 except urllib.error.HTTPError as e:r=e
 if r.status!=expected:sys.exit(f'{method} {path}: expected {expected}, received {r.status}; sensitive response omitted.')
 return json.loads(r.read()) if r.status!=204 else None
def login(email,password):
 r=call('POST','/api/v1/auth/login',{'email':email,'password':password,'client_type':'mobile'})
 if r.get('mfa_required'):sys.exit('Use isolated integration tests if the demo administrator has enrolled MFA.')
 return r
admin=login('admin@spartial.test',os.environ['SEED_ADMIN_PASSWORD']);a=admin['access_token'];facility=admin['user']['facility_ids'][0]
r=login('reception@spartial.test',os.environ['SEED_RECEPTION_PASSWORD'])['access_token']
created=[];tokens={}
for role in ['doctor','privacy-reviewer']:
 email='privacy-smoke-'+uuid.uuid4().hex+'@spartial.test';password=secrets.token_urlsafe(32)
 user=call('POST','/api/v1/staff/users',{'email':email,'display_name':'Synthetic Privacy Probe','password':password,'role':role,'facility_ids':[facility]},a,201);created.append(user)
 tokens[role]=login(email,password)['access_token']
p=call('POST','/api/v1/staff/patients',{'facility_id':facility,'given_name':'SyntheticPrivacy','family_name':'Patient','birth_precision':'unknown'},r,201,str(uuid.uuid4()));pid=p['id'];doctor=tokens['doctor'];reviewer=tokens['privacy-reviewer'];doctor_id=created[0]['id']
call('GET','/api/v1/staff/patients/'+pid,token=doctor,expected=404)
assignment=call('POST','/api/v1/staff/patients/'+pid+'/care-assignments',{'staff_id':doctor_id,'reason':'Responsible clinician for a synthetic care workflow','valid_until':(datetime.datetime.now(datetime.timezone.utc)+datetime.timedelta(hours=1)).isoformat()},r,201,str(uuid.uuid4()))
call('GET','/api/v1/staff/patients/'+pid,token=doctor)
call('POST','/api/v1/staff/care-assignments/'+assignment['id']+'/revoke',token=r)
call('GET','/api/v1/staff/patients/'+pid,token=doctor,expected=404)
grant=call('POST','/api/v1/staff/patients/'+pid+'/break-glass',{'reason':'Synthetic emergency while routine care assignment is unavailable','duration_minutes':5},doctor,201,str(uuid.uuid4()))
call('GET','/api/v1/staff/patients/'+pid,token=doctor)
call('POST','/api/v1/staff/privacy/break-glass/'+grant['id']+'/review',{'outcome':'inappropriate','note':'Synthetic review deliberately exercises immediate grant invalidation'},reviewer)
call('GET','/api/v1/staff/patients/'+pid,token=doctor,expected=404)
identifier={'kind':'nida','issuer':'Synthetic authority','value':'SYNTHETIC-'+uuid.uuid4().hex}
call('POST','/api/v1/staff/patients/'+pid+'/identifiers',identifier,r,201,str(uuid.uuid4()))
found=call('POST','/api/v1/staff/patient-identifiers/search',identifier,r)
if found['patient_ids']!=[pid]:sys.exit('Scoped encrypted identifier search failed.')
call('GET','/api/v1/staff/privacy/patients/'+pid+'/access',token=reviewer)
for user in created:call('PUT','/api/v1/staff/users/'+user['id'],{'display_name':user['display_name'],'role':user['role'],'active':False,'facility_ids':user['facility_ids'],'version':user['version']},a)
call('POST','/api/v1/auth/logout',token=r,expected=204);call('POST','/api/v1/auth/logout',token=a,expected=204)
print('PASS: care assignment/revocation, audited denials, emergency grant/review, encrypted identifier lookup and privacy reporting.')
print('Synthetic staff accounts deactivated; no credentials or patient bodies printed.')
