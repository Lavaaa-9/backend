#!/usr/bin/env python3
"""Scan publishable source, including uncommitted work, without copying ignored development secrets."""
import pathlib,subprocess,tempfile,shutil,sys,os
root=pathlib.Path(__file__).resolve().parent.parent
files=subprocess.check_output(['git','ls-files','--cached','--others','--exclude-standard','-z'],cwd=root).decode().split('\0')
with tempfile.TemporaryDirectory(prefix='spartial-source-scan-') as folder:
 target=pathlib.Path(folder)
 for name in filter(None,files):
  src=root/name
  if not src.is_file() or src.is_symlink():continue
  dst=target/name;dst.parent.mkdir(parents=True,exist_ok=True);shutil.copy2(src,dst)
 report=root/'.local/source-secrets.json'
 result=subprocess.run([str(root/'.local/tools/gitleaks'),'dir','--config',str(root/'.gitleaks.toml'),'--redact','--no-banner','--report-format','json','--report-path',str(report),str(target)])
 sys.exit(result.returncode)
