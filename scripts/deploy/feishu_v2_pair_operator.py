"""Current explicitly authorized isolated two-source operator; no production mode."""
import argparse
import sys

import feishu_v2_personal_export_probe as probe

AUTHORIZATION = 'user:2026-10-27:accelerated-two-source-validation'

BACKUP = r'''
if project!='knowhub-feishu-v2-d6e99d894d3a':raise ValueError('exact_isolated_project_required')
folder=site/'accelerated-two-source-20261027'
folder.mkdir(mode=0o700,exist_ok=True)
if folder.is_symlink():raise ValueError('unsafe_operator_directory')
backup_receipt=folder/'backup.json'
if ACTION=='rebind':
 run(compose+['stop','--timeout','45','app','worker2'],120)
 if not backup_receipt.exists():
  backup=folder/'before-rebind.dump'
  with backup.open('xb') as output:
   os.chmod(backup,0o600)
   p=subprocess.run(compose+['exec','-T','postgres','pg_dump','-U','postgres','-Fc','--no-owner','--no-acl','knowhub_feishu_v2_test'],stdout=output,stderr=subprocess.PIPE,timeout=180)
  if p.returncode:raise ValueError('backup_failed')
  restored='feishu_accelerated_restore_20261027'
  run(compose+['exec','-T','postgres','createdb','-U','postgres','--template=template0',restored])
  with backup.open('rb') as incoming:
   p=subprocess.run(compose+['exec','-T','postgres','pg_restore','--exit-on-error','--no-owner','--no-acl','-U','postgres','-d',restored],stdin=incoming,stdout=subprocess.DEVNULL,stderr=subprocess.PIPE,timeout=180)
  if p.returncode:raise ValueError('restore_failed')
  checks={}
  for table in ['users','feishu_connections','feishu_personal_imports','data_sources','sync_logs','knowledges','chunks','embeddings']:
   query="SELECT count(*)::text||':'||md5(coalesce(string_agg(md5(row_to_json(t)::text),'' ORDER BY id::text),'')) FROM "+table+" t;"
   original=run(compose+['exec','-T','postgres','psql','-XAt','-v','ON_ERROR_STOP=1','-U','postgres','-d','knowhub_feishu_v2_test'],input=query)
   recovered=run(compose+['exec','-T','postgres','psql','-XAt','-v','ON_ERROR_STOP=1','-U','postgres','-d',restored],input=query)
   if original!=recovered:raise ValueError('restore_rows_mismatch')
   checks[table]=original.strip()
  backup_receipt.write_text(json.dumps({'backup_sha256':hashlib.sha256(backup.read_bytes()).hexdigest(),'restored':restored,'verified_rows':checks}))
  backup_receipt.chmod(0o600)
 proof=json.loads(backup_receipt.read_text())
 if hashlib.sha256((folder/'before-rebind.dump').read_bytes()).hexdigest()!=proof['backup_sha256']:raise ValueError('backup_changed')
 environment['FEISHU_DIAGNOSTIC_VERIFIED_BACKUP_SHA']=proof['backup_sha256']
environment['FEISHU_DIAGNOSTIC_RECOVERY_ACTION']=ACTION
environment['FEISHU_DIAGNOSTIC_RECOVERY_APPROVAL']=AUTHORIZATION
environment['FEISHU_SOURCE_REBIND_OPERATIONS_ENABLED']='1'
'''


def program(action):
    remote = probe.REMOTE.replace("def run(args,timeout=30):", "def run(args,timeout=30,input=None):")
    remote = remote.replace('text=True,timeout=timeout)', 'text=True,timeout=timeout,input=input)')
    remote = remote.replace("compose+['ps','--quiet','app']", "compose+['ps','--all','--quiet','app']")
    marker = "environment['FEISHU_DIAGNOSTIC_ISOLATED']='1'"
    assert remote.count(marker) == 1
    remote = remote.replace(marker, marker + '\n' + BACKUP)
    # Source permission preflight may refresh the real personal token. Failure
    # leaves workers stopped and the safety latch intact for explicit diagnosis.
    remote = remote.replace("result=json.loads(lines[0]);", "result=json.loads(lines[0]);")
    return 'COMPARE_CONTENT_PATHS=False\nACTION=' + repr(action) + '\nAUTHORIZATION=' + repr(AUTHORIZATION) + '\n' + remote


if __name__ == '__main__':
    parser = argparse.ArgumentParser(add_help=False)
    parser.add_argument('--operation', choices=['rebind', 'reconcile'], required=True)
    options, remaining = parser.parse_known_args()
    probe.transport.REMOTE = program(options.operation)
    sys.argv = [sys.argv[0], *remaining, '--mode', 'status']
    raise SystemExit(probe.transport.main())
