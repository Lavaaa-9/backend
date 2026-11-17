#!/usr/bin/env python3
"""Rehearse a synthetic development database restore in an isolated temporary DB."""
import json,os,subprocess,sys,tempfile,uuid,secrets
from urllib.parse import urlsplit,unquote,parse_qs
if os.environ.get('APP_ENV')!='development':sys.exit('Restore rehearsal requires APP_ENV=development and synthetic data only.')
source=os.environ.get('DATABASE_ADMIN_URL','')
admin=os.environ.get('TEST_DATABASE_URL','')
if not source or not admin:sys.exit('DATABASE_ADMIN_URL and TEST_DATABASE_URL are required.')
def env_for(url):
 u=urlsplit(url)
 env=os.environ.copy()
 env.update(PGHOST=u.hostname or '',PGPORT=str(u.port or 5432),PGUSER=unquote(u.username or ''),PGPASSWORD=unquote(u.password or ''),PGDATABASE=u.path.lstrip('/'),PGSSLMODE=parse_qs(u.query).get('sslmode',['prefer'])[0])
 return env
def run(args,env,sql=None):
 p=subprocess.run(args,input=sql,env=env,capture_output=True,text=True)
 if p.returncode:sys.exit('Restore rehearsal command failed; diagnostics withheld to protect connection details. Temporary database cleanup is attempted.')
 return p.stdout
admin_env=env_for(admin);source_env=env_for(source)
name='spartial_restore_'+uuid.uuid4().hex
role='spartial_restore_app_'+uuid.uuid4().hex
role_password=secrets.token_hex(24)
psql=['psql','-X','-v','ON_ERROR_STOP=1','-Atq']
run(psql,admin_env,'CREATE DATABASE '+name+';')
restore_env=admin_env.copy();restore_env['PGDATABASE']=name
def counts(env):
 tables=run(psql,env,"SELECT tablename FROM pg_tables WHERE schemaname='public' ORDER BY tablename;").splitlines()
 return {table:int(run(psql,env,'SELECT count(*) FROM "'+table.replace('"','""')+'";')) for table in tables}
try:
 with tempfile.TemporaryDirectory(prefix='spartial-backup-') as tmp:
  path=tmp+'/synthetic.dump'
  run(['pg_dump','--format=custom','--no-owner','--no-acl','--file',path],source_env)
  run(['pg_restore','--exit-on-error','--no-owner','--no-acl','--dbname',name,path],restore_env)
  if counts(source_env)!=counts(restore_env):sys.exit('Restored table counts do not match the synthetic source.')
  report=json.loads(run(psql,restore_env,'SELECT row_to_json(r) FROM verify_audit_chain() r;'))
  if report['first_broken_seal'] is not None:sys.exit('Restored audit chain does not match its source seals.')
  sample=run(psql,restore_env,"SELECT id::text FROM patients ORDER BY id LIMIT 1;").strip()
  actor=run(psql,restore_env,"SELECT id::text FROM staff WHERE active AND role_name='reception' ORDER BY id LIMIT 1;").strip()
  if not sample or not actor:sys.exit('Restore opening check requires a synthetic patient and receptionist; run scripts/demo.py before backing up.')
  from urllib.parse import urlunsplit,quote
  u=urlsplit(source);host=u.hostname+(':'+str(u.port) if u.port else '')
  restored_owner=urlunsplit((u.scheme,u.netloc,'/'+name,u.query,''))
  runtime_url=urlunsplit((u.scheme,quote(role)+':'+quote(role_password)+'@'+host,'/'+name,u.query,''))
  provision=os.environ.copy();provision.update(DATABASE_URL=runtime_url,DATABASE_ADMIN_URL=restored_owner,APP_DB_ROLE=role,APP_DB_PASSWORD=role_password)
  run(['go','run','./cmd/manage','grant-runtime'],provision)
  runtime_env=env_for(runtime_url)
  if run(psql,runtime_env,'SELECT count(*) FROM patients;').strip()!='0':sys.exit('Restored runtime role bypassed patient row isolation.')
  sql="BEGIN; SELECT set_config('app.actor_id','"+actor+"',true); SELECT set_config('app.care_policy_enabled','false',true); SELECT count(*) FROM patients WHERE id='"+sample+"'::uuid; INSERT INTO audit_events(actor_id,action,resource_id,patient_id,request_id,outcome) VALUES('"+actor+"'::uuid,'restore.patient.read','"+sample+"'::uuid,'"+sample+"'::uuid,gen_random_uuid(),'success'); COMMIT;"
  opened=run(psql,runtime_env,sql).strip().splitlines()
  if not opened or opened[-1]!='1':sys.exit('Restored synthetic patient could not be opened by the scoped runtime role.')
  print('PASS: isolated synthetic restore, matching table counts, original sealed-prefix verification and an audited scoped patient read.')
  print('Verified seals:',report['seals_checked'],'unsealed source events:',report['unsealed_events'])
  print('Attachment restoration is pending M06; no files were included or claimed verified.')
finally:
 p=subprocess.run(psql,input='DROP DATABASE '+name+' WITH (FORCE); DROP ROLE IF EXISTS '+role+';',env=admin_env,capture_output=True,text=True)
 if p.returncode:print('Temporary database cleanup failed; inspect local spartial_restore_* databases.',file=sys.stderr)
