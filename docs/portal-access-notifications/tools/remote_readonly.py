"""Bounded Portal metadata audit using the repository's existing pinned hosts.

Credentials stay in memory. Remote commands never write application files,
change services, run migrations, or query document bodies/user identities.
Detailed business metadata is saved only to the caller's local evidence path.
"""
import argparse
import base64
import datetime
import hashlib
import importlib.util
import json
import logging
from pathlib import Path
import sys

import paramiko


REMOTE = r'''
import datetime,json,subprocess,sys
mode=sys.argv[1]
out={'mode':mode,'observed_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),
     'read_only':True,'production_changes':False}
def run(args,data=None):
 p=subprocess.run(args,input=data,stdout=subprocess.PIPE,stderr=subprocess.DEVNULL,timeout=25)
 if p.returncode:raise RuntimeError('read_unavailable')
 return p.stdout.decode()
try:
 if mode=='production':
  def sql(database,query):
   statement="BEGIN READ ONLY; SET LOCAL statement_timeout='10s'; "+query+"; ROLLBACK;"
   return run(['docker','exec','-i','-u','postgres','WeKnora-postgres','psql','-X','-w',
    '-U','postgres','-d',database,'-qAt','-v','ON_ERROR_STOP=1'],statement.encode()).strip()
  names=sql('postgres',"SELECT datname FROM pg_database WHERE lower(datname)='weknora'").splitlines()
  if len(names)!=1 or names[0] not in ('weknora','WeKnora'):raise RuntimeError('ambiguous_database')
  database=names[0]
  out['migration']=json.loads(sql(database,"SELECT json_build_object('version',version,'dirty',dirty) FROM schema_migrations"))
  query="""SELECT COALESCE(json_agg(x),'[]'::json) FROM (
   SELECT pc.tenant_id,pc.display_name,pc.allow_access_request,
    (SELECT count(*) FROM tenant_access_requests ar WHERE ar.tenant_id=pc.tenant_id AND ar.status='pending') AS pending_requests,
    (SELECT count(*) FROM tenant_members tm WHERE tm.tenant_id=pc.tenant_id AND tm.status='suspended' AND tm.deleted_at IS NULL) AS suspended_members,
    (SELECT count(*) FROM tenant_members tm JOIN users u ON u.id=tm.user_id
      WHERE tm.tenant_id=pc.tenant_id AND tm.role='owner' AND tm.status='active'
      AND tm.deleted_at IS NULL AND u.deleted_at IS NULL AND u.is_active) AS active_owners
   FROM tenant_portal_configs pc JOIN tenants t ON t.id=pc.tenant_id
   WHERE pc.status='published' AND t.status='active' AND t.deleted_at IS NULL ORDER BY pc.tenant_id) x"""
  out['published_spaces']=json.loads(sql(database,query))
 else:
  rows=[]
  for line in run(['docker','ps','-a','--format','{{json .}}']).splitlines():
   row=json.loads(line)
   if any(x in row['Names'].lower() for x in ('knowhub','weknora','feishu')):
    rows.append({k:row.get(k) for k in ('Names','Image','Status','Ports')})
  out['containers']=rows[:100]
  out['build_images']=[]
  for line in run(['docker','images','--format','{{json .}}']).splitlines():
   row=json.loads(line)
   if any(x in row.get('Repository','') for x in ('golang','knowhub-build','node','nginx','paradedb','redis')):
    out['build_images'].append({k:row.get(k) for k in ('Repository','Tag','ID')})
  out['docker']=run(['docker','--version']).strip()
  out['compose']=run(['docker','compose','version']).strip()
 out['status']='PASS'
except Exception as error:
 out['status']='BLOCKED';out['error_class']=type(error).__name__
print(json.dumps(out,ensure_ascii=False))
'''


def connect(mode):
    spec = importlib.util.spec_from_file_location('crypto', 'E:/AIData/Codex/sourceid-ssh.py')
    crypto = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(crypto)
    logging.getLogger('paramiko').disabled = True
    client = paramiko.SSHClient()
    client.set_missing_host_key_policy(paramiko.RejectPolicy())
    if mode == 'production':
        host = '10.51.134.114'
        keys = paramiko.HostKeys('E:/AIData/Codex/sourceid-production-host-public-key.txt')
        key = keys.lookup(host)['ssh-ed25519']
        actual = 'SHA256:' + base64.b64encode(hashlib.sha256(key.asbytes()).digest()).decode().rstrip('=')
        if actual != 'SHA256:u7TLDQSRo5z7oGMjNYsPfENIlbXGI7KdslJCajR57qA':
            raise ValueError('pinned_host_key_mismatch')
        client.get_host_keys().add(host, 'ssh-ed25519', key)
        credential = 'sourceid-production-host.ssh.dpapi'
    else:
        host = '172.30.232.42'
        client.load_host_keys(str(Path.home() / '.ssh/known_hosts'))
        credential = 'sourceid-test-host.ssh.dpapi'
    password = crypto.crypt((Path('E:/AIData/Codex/secure') / credential).read_bytes(), False).decode()
    try:
        client.connect(host, username='aqyfzx', password=password, allow_agent=False,
                       look_for_keys=False, timeout=15, auth_timeout=15, banner_timeout=15)
    finally:
        password = None
    return client


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--mode', choices=['production', 'test'], required=True)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    if args.output.exists():
        raise ValueError('evidence_already_exists')
    client = None
    try:
        client = connect(args.mode)
        incoming, outgoing, _ = client.exec_command('python3 - ' + args.mode, timeout=90)
        incoming.write(REMOTE)
        incoming.channel.shutdown_write()
        raw = outgoing.read(2 * 1024 * 1024)
        if outgoing.channel.recv_exit_status():
            raise RuntimeError('remote_read_failed')
        result = json.loads(raw)
    except Exception as error:
        result = {'mode': args.mode, 'status': 'BLOCKED', 'read_only': True,
                  'production_changes': False, 'error_class': type(error).__name__,
                  'observed_utc': datetime.datetime.now(datetime.timezone.utc).isoformat()}
    finally:
        if client:
            client.close()
    args.output.parent.mkdir(parents=True, exist_ok=True)
    with args.output.open('x', encoding='utf-8') as output:
        json.dump(result, output, ensure_ascii=False, indent=2)
    summary = {key: result.get(key) for key in ['mode', 'status', 'error_class', 'migration']}
    if 'published_spaces' in result:
        rows = result['published_spaces']
        summary.update(published_spaces=len(rows), requestable=sum(bool(r['allow_access_request']) for r in rows),
                       closed=sum(not r['allow_access_request'] for r in rows))
    if 'containers' in result:
        summary['containers_observed'] = len(result['containers'])
    summary['local_evidence'] = str(args.output)
    print(json.dumps(summary))
    return 0 if result['status'] == 'PASS' else 1


if __name__ == '__main__':
    sys.exit(main())
