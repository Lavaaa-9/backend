#!/usr/bin/env python3
"""Install checksum-pinned upstream tools under ignored .local/tools (Linux amd64)."""
import hashlib,io,json,os,pathlib,platform,sys,tarfile,urllib.request
root=pathlib.Path(__file__).resolve().parent.parent
if platform.system()!='Linux' or platform.machine() not in ('x86_64','amd64'):raise SystemExit('Pinned binaries currently support Linux amd64 only.')
target=root/'.local/tools';target.mkdir(parents=True,exist_ok=True)
for name,spec in json.loads((root/(sys.argv[1] if len(sys.argv)>1 else 'deploy/security-tools.json')).read_text()).items():
 request=urllib.request.Request(spec['url'],headers={'User-Agent':'Spartial-development-security-setup'})
 data=urllib.request.urlopen(request,timeout=90).read()
 if hashlib.sha256(data).hexdigest()!=spec['sha256']:raise SystemExit('Security tool checksum mismatch: '+name)
 with tarfile.open(fileobj=io.BytesIO(data),mode='r:gz') as archive:
  member=archive.getmember(spec['member'])
  if not member.isfile():raise SystemExit('Unexpected security tool archive entry')
  content=archive.extractfile(member).read()
 fd=os.open(target/name,os.O_WRONLY|os.O_CREAT|os.O_TRUNC,0o700)
 with os.fdopen(fd,'wb') as out:out.write(content)
 print('Installed checksum-verified',name,spec['version'])
