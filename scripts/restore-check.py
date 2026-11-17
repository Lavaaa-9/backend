#!/usr/bin/env python3
"""Rehearse a synthetic development database restore in an isolated temporary DB."""
import json,os,subprocess,sys,tempfile,uuid
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
  print('PASS: synthetic PostgreSQL custom-format backup restored into an isolated database; all public table counts match.')
  print('Attachment restoration is pending M06; no files were included or claimed verified.')
finally:
 p=subprocess.run(psql,input='DROP DATABASE '+name+' WITH (FORCE);',env=admin_env,capture_output=True,text=True)
 if p.returncode:print('Temporary database cleanup failed; inspect local spartial_restore_* databases.',file=sys.stderr)
