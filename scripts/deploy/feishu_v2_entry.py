"""Prepare, switch or roll back the explicitly authorized test 80/8080 entry.

The existing entry container is stopped and retained, never removed. Its original
configuration is copied privately and fingerprinted. Other stacks are untouched.
"""
import argparse
import base64
import json
import re
import sys
import textwrap
from pathlib import Path, PurePosixPath

import paramiko

REMOTE = r"""
import hashlib,ipaddress,json,os,pathlib,subprocess,urllib.request
root=pathlib.Path(ROOT)
if root.is_symlink() or str(root.resolve().parent)!='/mnt/sata0/knowhub-feishu-v2-builds':
 raise ValueError('unexpected_build_root')
release=root/'isolated-site'
proof=json.loads((release/'preparation.json').read_text())
project='knowhub-feishu-v2-'+SHA[:12]
if proof['source_sha']!=SHA or proof['project']!=project:raise ValueError('project_identity_mismatch')
if not (release/'bootstrap-ready.json').exists() or not (release/'restore-verification.json').exists():
 raise ValueError('isolated_bootstrap_and_restore_required')
compose_path=release/'compose.json'
compose=['docker','compose','--project-name',project,'--file',str(compose_path)]
def run(args,timeout=45):
 return subprocess.run(args,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True,timeout=timeout)
def inspect(name):
 p=run(['docker','inspect',name])
 if p.returncode:raise ValueError('expected_entry_container_unavailable')
 return json.loads(p.stdout)[0]
def own_container(service):
 p=run(compose+['ps','--all','--quiet',service])
 if p.returncode or not p.stdout.strip():return None
 result=inspect(p.stdout.strip())
 if result['Config'].get('Labels',{}).get('com.docker.compose.project')!=project:
  raise ValueError('entry_project_identity_mismatch')
 return result
def private_write(path,content):
 fd=os.open(str(path),os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
 with os.fdopen(fd,'w') as out:out.write(content)
statefile=release/'entry-state.json'
if MODE=='prepare':
 old=inspect('knowhub-company-entry-entry-1')
 if old['Config'].get('Labels',{}).get('com.docker.compose.project')!='knowhub-company-entry':
  raise ValueError('original_entry_project_changed')
 original_image='sha256:ab7845c89fadeb17ad2b68bd924e892f948b893781a50ab79e48385c319ddbe1'
 if old['Image']!=original_image:raise ValueError('original_entry_image_changed')
 ports=old['HostConfig'].get('PortBindings',{})
 if ports!={'80/tcp':[{'HostIp':'','HostPort':'80'}],'8080/tcp':[{'HostIp':'','HostPort':'8080'}]}:
  # Docker may canonicalize an unspecified binding as 0.0.0.0.
  if set(ports)!={'80/tcp','8080/tcp'} or any(len(values)!=1 or values[0].get('HostPort')!=key.split('/')[0]
   or values[0].get('HostIp') not in ('','0.0.0.0') for key,values in ports.items()):
   raise ValueError('original_entry_ports_changed')
 expected=pathlib.Path('/mnt/sata0/knowhub-formal-c2-20261022t072133z-a3/private/entry-current.conf')
 mounts=[m for m in old['Mounts'] if m.get('Destination')=='/etc/nginx/nginx.conf']
 if len(mounts)!=1 or mounts[0].get('Source')!=str(expected) or mounts[0].get('RW'):
  raise ValueError('original_entry_config_mount_changed')
 original=expected.read_bytes()
 original_sha=hashlib.sha256(original).hexdigest()
 if not statefile.exists():
  backup=release/'entry-original.conf'
  fd=os.open(str(backup),os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
  with os.fdopen(fd,'wb') as output:output.write(original)
  state={'source_sha':SHA,'old_container_id':old['Id'],'old_image':old['Image'],
   'old_config_path':str(expected),'old_config_sha256':original_sha,'active':False,
   'rollback_container_preserved':True}
  private_write(statefile,json.dumps(state,indent=2)+'\n')
 else:
  state=json.loads(statefile.read_text())
  if state['old_container_id']!=old['Id'] or state['old_config_sha256']!=original_sha:
   raise ValueError('original_entry_changed_since_backup')
 client=os.environ.get('SSH_CONNECTION','').split()[0]
 ip=ipaddress.ip_address(client)
 if ip.version!=4 or not ip.is_private:raise ValueError('private_client_address_required')
 subnet=ipaddress.ip_network(proof['network_subnet'])
 gateway=str(subnet.network_address+1)
 def nginx(preview):
  allowed=[client,'172.30.232.42','127.0.0.1']+([gateway] if preview else [])
  policy='\n'.join('        allow '+value+';' for value in allowed)+'\n        deny all;'
  servers=[]
  for port,upstream in [(80,'frontend:80'),(8080,'app:8080')]:
   servers.append('''    server {
        listen '''+str(port)+''';
        server_name _;
        client_max_body_size 101M;
'''+policy+'''
        add_header Referrer-Policy no-referrer always;
        location / {
            proxy_pass http://'''+upstream+''';
            proxy_set_header Host $host;
            proxy_set_header X-Real-IP $remote_addr;
            proxy_set_header X-Forwarded-For $remote_addr;
            proxy_set_header X-Forwarded-Proto http;
            proxy_http_version 1.1;
            proxy_read_timeout 120s;
            proxy_hide_header Referrer-Policy;
        }
    }
''')
  return '''worker_processes 1;
error_log /dev/null;
pid /var/run/nginx.pid;
events { worker_connections 512; }
http {
    log_format no_query '$remote_addr $request_method $uri $status $request_time';
    access_log /dev/stdout no_query;
'''+''.join(servers)+'}\n'
 for name,preview in [('entry.conf',False),('entry-preview.conf',True)]:
  path=release/name;content=nginx(preview)
  if path.exists() and path.read_text()!=content:raise ValueError('existing_entry_plan_changed')
  if not path.exists():private_write(path,content)
  path.chmod(0o644)
 plan=json.loads(compose_path.read_text())
 for service,configuration,bindings,profile in [
  ('entry','entry.conf',['172.30.232.42:80:80','172.30.232.42:8080:8080'],'cutover'),
  ('entry-preview','entry-preview.conf',['127.0.0.1:28580:80','127.0.0.1:28581:8080'],'entry-preview')]:
  plan['services'][service]={'image':proof['frontend_image'],'entrypoint':['nginx'],'command':['-g','daemon off;'],
   'volumes':['./'+configuration+':/etc/nginx/nginx.conf:ro'],'ports':bindings,'profiles':[profile],
   'restart':'unless-stopped','mem_limit':'128m','cpus':1,
   'labels':{'knowhub.source-sha':SHA,'knowhub.environment':'isolated-test'}}
 temporary=release/'compose.entry.json'
 with temporary.open('x') as output:json.dump(plan,output,indent=2)
 temporary.chmod(0o600)
 if run(['docker','compose','--project-name',project,'--file',str(temporary),'config','--quiet']).returncode:
  raise ValueError('entry_compose_plan_invalid')
 temporary.replace(compose_path)
 tested=run(['docker','run','--rm','--pull=never','--network',project+'-network','--cap-drop','ALL','--cap-add','CHOWN',
  '--volume',str(release/'entry.conf')+':/etc/nginx/nginx.conf:ro','--entrypoint','nginx',
  proof['frontend_image'],'-t'],60)
 if tested.returncode:
  print(json.dumps({'status':'BLOCKED','reason':'isolated_entry_nginx_configuration_failed',
   'diagnostic':(tested.stdout+tested.stderr)[-2500:]}))
  raise SystemExit(2)
 state['candidate_config_sha256']=hashlib.sha256((release/'entry.conf').read_bytes()).hexdigest()
 statefile.write_text(json.dumps(state,indent=2)+'\n')
 p=run(compose+['up','-d','--no-build','--pull','never','entry-preview'],60)
 if p.returncode:raise ValueError('isolated_entry_preview_start_failed')
 print(json.dumps({'status':'PREPARED','source_sha':SHA,'client_allow_address':client,
  'old_entry_config_sha256':original_sha,'old_container_retained':True,'public_entry_changed':False,
  'preview_frontend':'http://127.0.0.1:28580','preview_backend':'http://127.0.0.1:28581'}))
 raise SystemExit(0)
state=json.loads(statefile.read_text())
if state['source_sha']!=SHA:raise ValueError('entry_state_source_mismatch')
old=inspect(state['old_container_id'])
if old['Name']!='/knowhub-company-entry-entry-1' or old['Image']!=state['old_image']:
 raise ValueError('rollback_container_identity_changed')
if hashlib.sha256(pathlib.Path(state['old_config_path']).read_bytes()).hexdigest()!=state['old_config_sha256']:
 raise ValueError('original_entry_configuration_changed')
if MODE=='cutover':
 smoke=json.loads((release/'startup-smoke.json').read_text())
 if smoke.get('status')!='PASS' or smoke.get('source_sha')!=SHA:raise ValueError('passing_preview_smoke_required')
 if hashlib.sha256((release/'entry.conf').read_bytes()).hexdigest()!=state.get('candidate_config_sha256'):
  raise ValueError('candidate_entry_configuration_changed')
 for service in ['app','worker2','frontend']:
  record=own_container(service)
  if not record or record['State']['Status']!='running':raise ValueError('healthy_candidate_services_required')
  if service in ['app','worker2'] and record['State'].get('Health',{}).get('Status')!='healthy':
   raise ValueError('both_candidate_workers_must_be_healthy')
 if old['State']['Running'] and run(['docker','stop','--time','15',old['Id']],30).returncode:
  raise ValueError('authorized_old_entry_stop_failed')
 p=run(compose+['up','-d','--no-build','--pull','never','entry'],90)
 if p.returncode:
  run(compose+['stop','entry'])
  restored=run(['docker','start',old['Id']])
  print(json.dumps({'status':'FAILED','reason':'candidate_entry_start_failed','old_entry_restored':restored.returncode==0}))
  raise SystemExit(2)
 state['active']=True
 status='CUTOVER_STARTED'
else:
 current=own_container('entry')
 if current and run(compose+['stop','entry']).returncode:raise ValueError('candidate_entry_stop_failed')
 if run(['docker','start',old['Id']]).returncode:raise ValueError('original_entry_restore_failed')
 state['active']=False
 status='ROLLED_BACK'
statefile.write_text(json.dumps(state,indent=2)+'\n')
print(json.dumps({'status':status,'source_sha':SHA,'candidate_entry_active':state['active'],
 'old_container_retained':True,'old_configuration_unchanged':True,'application_data_modified':False}))
"""


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--manifest',type=Path,required=True)
    parser.add_argument('--mode',choices=['prepare','cutover','rollback'],required=True)
    args=parser.parse_args();raw=args.manifest.read_bytes()
    manifest=json.loads(raw.decode('utf-16') if raw.startswith((b'\xff\xfe',b'\xfe\xff')) else raw.decode('utf-8-sig'))
    sha=manifest['source_sha'];root=PurePosixPath(manifest['source_directory']).parent
    if (not re.fullmatch('[a-f0-9]{40}',sha) or root.parent!=PurePosixPath('/mnt/sata0/knowhub-feishu-v2-builds')
            or not re.fullmatch(sha[:12]+r'-[a-f0-9]{8}',root.name)):raise SystemExit('invalid staged source manifest')
    client=paramiko.SSHClient()
    try:
        credential=json.loads(sys.stdin.read(16384))
        if credential.get('host')!='172.30.232.42' or credential.get('user')!='aqyfzx':raise ValueError('unexpected_target')
        client.load_system_host_keys();known=Path.home()/'.ssh'/'known_hosts'
        if known.is_file():client.load_host_keys(str(known))
        client.set_missing_host_key_policy(paramiko.RejectPolicy())
        client.connect(credential['host'],username=credential['user'],password=credential['password'],
                       timeout=15,auth_timeout=15,banner_timeout=15,allow_agent=False,look_for_keys=False)
        credential.clear()
        guarded='try:\n'+textwrap.indent(REMOTE,' ')
        guarded+="\nexcept Exception as error:\n print(json.dumps({'status':'BLOCKED','error_type':type(error).__name__,"
        guarded+="'reason':str(error) if isinstance(error,ValueError) else 'authorized_test_entry_operation_failed'}))\n raise SystemExit(2)\n"
        prefix='ROOT='+repr(str(root))+'\nSHA='+repr(sha)+'\nMODE='+repr(args.mode)+'\n'
        code=base64.b64encode((prefix+guarded).encode()).decode()
        _,out,_=client.exec_command("python3 -c \"import base64;exec(base64.b64decode('"+code+"'))\"",timeout=240)
        result=json.loads(out.read(65536));print(json.dumps(result,indent=2))
        return 2 if result.get('status') in ('BLOCKED','FAILED') else 0
    except (OSError,ValueError,KeyError,TypeError,paramiko.SSHException) as error:
        print(json.dumps({'status':'BLOCKED','reason':'verified_test_entry_operation_unavailable',
                          'error_type':type(error).__name__}));return 2
    finally:client.close()


if __name__=='__main__':sys.exit(main())
