#!/usr/bin/env python3
"""Generate the implemented foundation contract; unimplemented modules have no fake routes."""
import json
from pathlib import Path

def ref(name):return {'$ref':'#/components/schemas/'+name}
def obj(properties,required=()):return {'type':'object','additionalProperties':False,'properties':properties,'required':list(required)}
def string(**kw):return {'type':'string',**kw}
def array(item):return {'type':'array','items':item}
def response(schema,description='Success'):return {'description':description,'content':{'application/json':{'schema':schema}}}
uuid=string(format='uuid')
version={'type':'integer','minimum':1}
schemas={}
schemas['Error']=obj({'error':obj({'code':string(),'message':string(),'fields':{'type':'object','additionalProperties':{'type':'string'}}},['code','message','fields']),'request_id':uuid},['error','request_id'])
schemas['User']=obj({'id':uuid,'email':string(format='email'),'display_name':string(),'role':string(),'active':{'type':'boolean'},'version':version,'facility_ids':array(uuid)},['id','email','display_name','role','active','version','facility_ids'])
schemas['Actor']={'allOf':[ref('User'),{'type':'object','properties':{'permissions':array(string())},'required':['permissions']}]}
schemas['Tokens']=obj({'access_token':string(description='Opaque bearer credential, held in browser memory; server checks session on every request.'),'token_type':{'type':'string','enum':['Bearer']},'expires_in':{'type':'integer','example':900},'user':ref('User')},['access_token','token_type','expires_in','user'])
schemas['MobileTokens']={'allOf':[ref('Tokens'),{'type':'object','properties':{'refresh_token':string(description='Store in mobile secure credential storage. Rotation invalidates the preceding token.')},'required':['refresh_token']}]}
roles=['administrator','reception','nurse','doctor','laboratory','pharmacy','cashier','management','privacy-reviewer']
schemas['CreateStaff']=obj({'email':string(format='email',maxLength=254),'display_name':string(minLength=1,maxLength=160),'password':string(minLength=12,maxLength=256,writeOnly=True),'role':string(enum=roles),'facility_ids':{'type':'array','items':uuid,'minItems':1,'maxItems':30,'uniqueItems':True}},['email','display_name','password','role','facility_ids'])
schemas['UpdateStaff']=obj({'display_name':string(minLength=1,maxLength=160),'role':string(enum=roles),'active':{'type':'boolean'},'facility_ids':{'type':'array','items':uuid,'minItems':1,'maxItems':30,'uniqueItems':True},'version':version},['display_name','role','active','facility_ids','version'])
schemas['Facility']=obj({'id':uuid,'name':string(),'timezone':string(),'active':{'type':'boolean'}},['id','name','timezone','active'])
schemas['CreateFacility']=obj({'name':string(minLength=1,maxLength=160),'timezone':string(maxLength=100,example='Africa/Dar_es_Salaam')},['name','timezone'])
demographics={'given_name':string(minLength=1,maxLength=100),'family_name':string(minLength=1,maxLength=100),'birth_precision':string(enum=['exact','year','unknown'],description='exact requires only birth_date; year requires only birth_year; unknown omits both.'),'birth_date':string(format='date',nullable=True),'birth_year':{'type':'integer','minimum':1850,'maximum':9999,'nullable':True},'phone':string(maxLength=40,description='Shared phone numbers are allowed.'),'address':string(maxLength=500)}
required=['given_name','family_name','birth_precision']
schemas['CreatePatient']=obj({'facility_id':uuid,**demographics},['facility_id',*required])
schemas['UpdatePatient']=obj({**demographics,'version':version},[*required,'version'])
schemas['Patient']=obj({'id':uuid,'mrn':string(example='SP-0000000001'),'facility_id':uuid,**demographics,'version':version,'created_at':string(format='date-time'),'updated_at':string(format='date-time')},['id','mrn','facility_id',*required,'birth_date','birth_year','phone','address','version','created_at','updated_at'])
schemas['Login']=obj({'email':string(format='email',maxLength=254),'password':string(maxLength=256,writeOnly=True),'client_type':string(enum=['web','mobile'])},['email','password','client_type'])
schemas['Refresh']=obj({'client_type':string(enum=['web','mobile']),'refresh_token':string(maxLength=128,description='Mobile only; web must omit this and send the HttpOnly refresh cookie.')},['client_type'])
schemas['Session']=obj({'id':uuid,'client_type':string(enum=['web','mobile']),'created_at':string(format='date-time'),'expires_at':string(format='date-time'),'revoked_at':string(format='date-time',nullable=True)},['id','client_type','created_at','expires_at','revoked_at'])
schemas['AuditEvent']=obj({'id':{'type':'integer','format':'int64'},'actor_id':string(format='uuid',nullable=True),'action':string(),'resource_id':string(format='uuid',nullable=True),'patient_id':string(format='uuid',nullable=True),'request_id':uuid,'outcome':string(enum=['success','denied','failure']),'created_at':string(format='date-time')},['id','actor_id','action','resource_id','patient_id','request_id','outcome','created_at'])
schemas['Health']=obj({'status':string(enum=['ok','ready'])},['status'])
schemas['Actor']=obj({**schemas['User']['properties'],'permissions':array(string())},[*schemas['User']['required'],'permissions'])
schemas['MobileTokens']=obj({**schemas['Tokens']['properties'],'refresh_token':string()},[*schemas['Tokens']['required'],'refresh_token'])
paths={}
errors={str(c):response(ref('Error'),desc) for c,desc in [(400,'Malformed JSON, unknown field, or invalid body'),(401,'Missing, expired, revoked, or invalid credentials'),(403,'Permission, facility, or browser-origin denied'),(404,'Record not found in permitted scope'),(409,'Version, uniqueness, or idempotency conflict'),(413,'Body exceeds 64 KiB'),(415,'application/json required'),(422,'Validation or invalid database reference'),(429,'Authentication rate limit; retry after 15 minutes'),(500,'Redacted internal error'),(503,'Database not ready')]}
page=[{'name':'cursor','in':'query','schema':uuid,'description':'Exclusive last returned UUID; stable ascending UUID order.'},{'name':'limit','in':'query','schema':{'type':'integer','minimum':1,'maximum':100,'default':25}}]
def listing(name,cursor=False):return obj({'items':array(ref(name)),**({'next_cursor':string(description='Empty when exhausted; an exact full final page may require one empty follow-up request.')} if cursor else {})},['items',*(['next_cursor'] if cursor else [])])
def op(method,path,summary,schema=None,input=None,permission=None,status=200,public=False,parameters=None,description=''):
 d={'operationId':summary.replace(' ','_').lower(),'summary':summary,'description':description,'tags':[('operations' if path.startswith('/health') else 'authentication' if '/auth/' in path else 'patients' if '/patients' in path else 'administration')],'responses':{str(status):response(schema) if schema else {'description':'Completed; no content'},**errors},'security':[] if public else [{'bearerAuth':[]}]}
 if input:d['requestBody']={'required':True,'content':{'application/json':{'schema':ref(input) if isinstance(input,str) else input}}}
 if permission:d['x-permission']=permission
 params=list(parameters or [])
 if '{id}' in path:params.append({'name':'id','in':'path','required':True,'schema':uuid})
 if params:d['parameters']=params
 paths.setdefault(path,{})[method]=d
 return d
op('get','/health/live','Liveness',ref('Health'),public=True)
op('get','/health/ready','Readiness',ref('Health'),public=True,description='Checks database connectivity and exact migration version. Startup also requires a restricted database role.')
op('get','/api/openapi.json','Download OpenAPI contract',{'type':'object'},public=True)
token_response={'oneOf':[ref('Tokens'),ref('MobileTokens')]}
login=op('post','/api/v1/auth/login','Staff login',token_response,'Login',public=True,description='Web requires an allowed Origin and sets a path-scoped HttpOnly SameSite=Strict cookie (Secure outside development/test). Mobile requests must have no Origin. Credentials are rate limited by direct peer IP and normalized email: 10 attempts per 15 minutes. No forwarded client IP is trusted. Access lifetime 15 minutes; session hard expiry 7 days.')
login['responses']['200']['headers']={'Set-Cookie':{'schema':string(),'description':'Web flow only: spartial_refresh; HttpOnly; SameSite=Strict; Path=/api/v1/auth; Secure in production.'}}
op('post','/api/v1/auth/refresh','Rotate refresh credential',token_response,'Refresh',public=True,description='Web uses cookie and exact allowed Origin; mobile sends refresh_token without Origin. Old access token is invalid immediately. Reusing a spent refresh token revokes its entire session, including the latest credentials. Clients must serialize refresh requests, with no automatic retry after ambiguous responses.')
op('get','/api/v1/auth/me','Current staff profile',ref('Actor'))
op('post','/api/v1/auth/logout','Revoke current session',status=204,description='Web sessions additionally require configured Origin. Clears browser cookie. Subsequent access and refresh are denied.')
op('get','/api/v1/auth/sessions','List own sessions',listing('Session'),description='Newest 100 sessions; includes revoked entries. No tokens or credential hashes are returned.')
op('post','/api/v1/auth/sessions/{id}/revoke','Revoke own session',status=204,description='Can revoke only sessions belonging to the current staff account; inaccessible IDs return 404.')
op('post','/api/v1/auth/password','Change own password',input=obj({'current_password':string(maxLength=256,writeOnly=True),'new_password':string(minLength=12,maxLength=256,writeOnly=True)},['current_password','new_password']),status=204,description='Requires current password. Revokes every session including the caller; sign in again.')
op('get','/api/v1/staff/facilities','List facilities',listing('Facility'),permission='facilities.read',description='Assigned active facilities for ordinary staff; facility administrators can list all. Maximum 100 records in this foundation version.')
op('post','/api/v1/staff/facilities','Create facility',ref('Facility'),'CreateFacility','facilities.manage',201)
op('get','/api/v1/staff/users','List staff',listing('User',True),permission='staff.read',parameters=page)
op('post','/api/v1/staff/users','Create staff account',ref('User'),'CreateStaff','staff.manage',201,description='Administrator-only. Requires active facility references. Direct development account provisioning; invitation/activation remains pending.')
op('put','/api/v1/staff/users/{id}','Update staff account',ref('User'),'UpdateStaff','staff.manage',description='Administrator-only. Replaces account administration fields using expected version. Role, assignment, reactivation, suspension, and display-name changes revoke all target sessions. The last active administrator cannot be disabled or demoted. Email and historical authors are preserved.')
op('post','/api/v1/staff/patients','Register patient',ref('Patient'),'CreatePatient','patients.write',201,parameters=[{'name':'Idempotency-Key','in':'header','required':True,'schema':string(minLength=8,maxLength=128)}],description='Requires an assigned active facility. Durable key scoped by actor and patients.create; canonical normalized request hash; changed request returns 409. Replays the committed 201 payload. MRN, audit, outbox, and idempotency response commit in one transaction. Shared phone numbers accepted. Different keys intentionally create distinct registry entries; duplicate suggestions/merge remain pending.')
op('get','/api/v1/staff/patients','Search patients',listing('Patient',True),permission='patients.read',parameters=page+[{'name':'q','in':'query','schema':string(maxLength=100),'description':'Literal case-insensitive substring of MRN, given/family name, or phone; SQL wildcard characters are escaped.'}],description='Demographics only, limited to assigned active facilities. Reads are audited. No clinical record access is implemented.')
op('get','/api/v1/staff/patients/{id}','Read patient profile',ref('Patient'),permission='patients.read',description='Missing and cross-facility records both return 404. Authorized reads commit an audit record before response.')
op('put','/api/v1/staff/patients/{id}','Update patient profile',ref('Patient'),'UpdatePatient','patients.write',description='Full demographic replacement with expected version. Immutable facility_id and MRN; unknown fields rejected. Stale version returns 409; cross-facility returns 404. Preserves audit history.')
op('get','/api/v1/staff/audit','Read audit events',listing('AuditEvent'),permission='audit.read',parameters=[{'name':'before','in':'query','schema':{'type':'integer','format':'int64','minimum':1},'description':'Exclusive event ID; returns newest 100. Use last item ID for the next page.'}],description='Organization-wide privileged metadata review for administrator/privacy-reviewer. Contains identifiers and outcomes, no clinical notes or request bodies. Audit querying itself is recorded.')
spec={'openapi':'3.0.3','info':{'title':'Spartial Polyclinic Foundation API','version':'0.1.0','description':'Milestone A implementation. All listed routes exist; later clinical/patient-access modules are tracked in docs/MODULE_STATUS.md. Not clinic accepted. Timestamps RFC3339 UTC, date-only YYYY-MM-DD. Future financial amounts must be exact decimal strings; no financial endpoints are implemented.'},'servers':[{'url':'http://localhost:8080','description':'Local development'}],'paths':paths,'components':{'securitySchemes':{'bearerAuth':{'type':'http','scheme':'bearer','bearerFormat':'opaque','description':'Random 256-bit opaque access credential; hashed server-side, session and live account/permissions checked on each protected request. No JWT algorithm or client-supplied claims.'}},'schemas':schemas},'security':[{'bearerAuth':[]}]}
Path('api/openapi/openapi.json').write_text(json.dumps(spec,indent=2)+'\n')
print(f'Generated {sum(len(v) for v in paths.values())} operations.')
