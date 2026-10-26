"""Deploy a CI-qualified frontend without restarting isolated backend workers."""
import argparse
import base64
import hashlib
import json
import re
import sys
import textwrap
from pathlib import Path, PurePosixPath

import paramiko

from feishu_v2_docreader_service import read_json

REMOTE = r'''
import hashlib,json,os,pathlib,subprocess,time,urllib.request
base=pathlib.Path('/mnt/sata0/knowhub-feishu-v2-builds')
root=pathlib.Path(ROOT);candidate=pathlib.Path(CANDIDATE);backend=pathlib.Path(BACKEND)
for directory in [root,candidate,backend]:
 if directory.is_symlink() or directory.resolve().parent!=base:raise ValueError('unexpected_release_root')
site=root/'isolated-site';project='knowhub-feishu-v2-'+BASE_SHA[:12]
if site.is_symlink() or json.loads((site/'preparation.json').read_text())['project']!=project:raise ValueError('isolated_project_mismatch')
compose_path=site/'compose.json';compose=['docker','compose','--project-name',project,'--file',str(compose_path)]
work=candidate/'frontend-upgrade';work.mkdir(mode=0o700,exist_ok=True)
if work.is_symlink():raise ValueError('unexpected_upgrade_directory')
def checked(args,timeout=30):
 p=subprocess.run(args,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True,timeout=timeout)
 if p.returncode:raise ValueError('isolated_frontend_operation_failed')
 return p.stdout.strip()
def inspect(value):return json.loads(checked(['docker','inspect',value]))[0]
def container(name):
 record=inspect(checked(compose+['ps','--quiet',name]))
 if record['Config']['Labels'].get('com.docker.compose.project')!=project:raise ValueError('container_project_mismatch')
 return record
def private_write(path,data):
 fd=os.open(str(path),os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
 with os.fdopen(fd,'wb') as output:output.write(data)
def code_fingerprint(source):
 files=[]
 for name in ['internal','cmd','config','migrations','pkg']:
  directory=source/name
  if directory.exists():files.extend(p for p in directory.rglob('*') if p.is_file())
 files.extend(source/name for name in ['go.mod','go.sum'])
 digest=hashlib.sha256()
 for path in sorted(files):
  if path.is_symlink():raise ValueError('unexpected_backend_source_symlink')
  digest.update(str(path.relative_to(source)).encode());digest.update(hashlib.sha256(path.read_bytes()).digest())
 return digest.hexdigest()
if code_fingerprint(candidate/'source')!=code_fingerprint(backend/'source'):raise ValueError('frontend_only_backend_source_changed')
fixed=['app','worker2','docreader','postgres','redis'];before={name:container(name)['Id'] for name in fixed}
for name in ['app','worker2']:
 if container(name)['Config']['Labels'].get('knowhub.source-sha')!=BACKEND_SHA:raise ValueError('backend_source_drift')
env_hash=hashlib.sha256((site/'app.env').read_bytes()).hexdigest()
state_path=work/'state.json';state=json.loads(state_path.read_text()) if state_path.exists() else {'phase':'NEW','source_sha':SHA,'backend_sha':BACKEND_SHA}
if state['source_sha']!=SHA or state['backend_sha']!=BACKEND_SHA:raise ValueError('upgrade_identity_mismatch')
services=['frontend','entry','entry-preview']
if MODE=='prepare':
 if GATE.get('headSha')!=SHA or GATE.get('name')!='frontend' or GATE.get('status')!='completed' or GATE.get('conclusion')!='success':raise ValueError('exact_source_frontend_gate_required')
 archive=candidate/'frontend-package/frontend-image.tar.gz'
 if archive.is_symlink() or hashlib.sha256(archive.read_bytes()).hexdigest()!=PACKAGE['archive_sha256']:raise ValueError('frontend_archive_mismatch')
 if PACKAGE['source_sha']!=SHA or PACKAGE.get('qualification')!='frontend_tests_typecheck_build_and_pinned_dockerfile':raise ValueError('frontend_provenance_mismatch')
 if hashlib.sha256((candidate/'source/frontend/Dockerfile').read_bytes()).hexdigest()!=PACKAGE['dockerfile_sha256']:raise ValueError('frontend_dockerfile_mismatch')
 checked(['docker','load','--input',str(archive)],180)
 if inspect(PACKAGE['image_id'])['Config']['Labels'].get('org.opencontainers.image.revision')!=SHA:raise ValueError('image_source_mismatch')
 if state['phase']=='NEW':
  private_write(work/'compose.before.json',compose_path.read_bytes())
  state.update(phase='PREPARED',frontend_image=PACKAGE['image_id'],backend_containers=before,environment_sha256=env_hash,gate=GATE)
elif MODE=='apply':
 if state['phase']!='PREPARED' or before!=state['backend_containers'] or env_hash!=state['environment_sha256']:raise ValueError('prepared_backend_state_changed')
 if compose_path.read_bytes()!=(work/'compose.before.json').read_bytes():raise ValueError('compose_changed_since_prepare')
 plan=json.loads(compose_path.read_text())
 for name in services:
  plan['services'][name]['image']=state['frontend_image']
  plan['services'][name].setdefault('labels',{})['knowhub.source-sha']=SHA
 temporary=work/'compose.after.json';private_write(temporary,(json.dumps(plan,indent=2)+'\n').encode())
 compose_path.write_bytes(temporary.read_bytes())
 try:
  checked(compose+['config','--quiet'])
  checked(compose+['up','-d','--no-deps','--no-build','--pull','never']+services,120)
  checks=[]
  for port,path in [(28481,'/'),(28580,'/'),(28581,'/health'),(80,'/')]:
   good=False
   for attempt in range(12):
    try:
     with urllib.request.urlopen('http://127.0.0.1:'+str(port)+path,timeout=5) as response:good=response.status==200
    except OSError:pass
    if good:break
    time.sleep(1)
   if not good:raise ValueError('frontend_health_failed')
   checks.append({'port':port,'path':path,'status':200})
  if before!={name:container(name)['Id'] for name in fixed} or env_hash!=hashlib.sha256((site/'app.env').read_bytes()).hexdigest():raise ValueError('backend_mutation_detected')
  if any(container(name)['Image']!=state['frontend_image'] for name in services):raise ValueError('frontend_image_drift')
  state.update(phase='RUNNING',checks=checks)
 except Exception:
  compose_path.write_bytes((work/'compose.before.json').read_bytes())
  checked(compose+['up','-d','--no-deps','--no-build','--pull','never']+services,120)
  raise
state_path.write_text(json.dumps(state,indent=2)+'\n');state_path.chmod(0o600)
print(json.dumps({'status':state['phase'],'frontend_source_sha':SHA,'backend_source_sha':BACKEND_SHA,'backend_containers_preserved':before==state.get('backend_containers'),'backend_environment_preserved':env_hash==state.get('environment_sha256'),'checks':state.get('checks',[]),'project':project,'database_writes':False,'sync_locks_modified':False}))
'''


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ['manifest', 'candidate-manifest', 'backend-manifest']:
        parser.add_argument('--' + name, type=Path, required=True)
    parser.add_argument('--frontend-package', type=Path)
    parser.add_argument('--frontend-gate', type=Path)
    parser.add_argument('--mode', choices=['prepare', 'apply', 'status'], required=True)
    args = parser.parse_args()
    base, candidate, backend = [read_json(p) for p in [args.manifest, args.candidate_manifest, args.backend_manifest]]
    for value in [base, candidate, backend]:
        directory = PurePosixPath(value['source_directory']).parent
        if not re.fullmatch('[a-f0-9]{40}', value['source_sha']) or directory.parent != PurePosixPath('/mnt/sata0/knowhub-feishu-v2-builds'):
            raise ValueError('unexpected_source_manifest')
    client = paramiko.SSHClient()
    try:
        credential = json.loads(sys.stdin.read(16384))
        if credential['host'] != '172.30.232.42' or credential['user'] != 'aqyfzx':raise ValueError('unexpected_target')
        client.load_system_host_keys()
        known = Path.home() / '.ssh' / 'known_hosts'
        if known.is_file():client.load_host_keys(str(known))
        client.set_missing_host_key_policy(paramiko.RejectPolicy())
        client.connect(credential['host'], username=credential['user'], password=credential['password'], timeout=15, allow_agent=False, look_for_keys=False)
        credential.clear()
        package = read_json(args.frontend_package / 'manifest.json') if args.frontend_package else None
        if args.mode == 'prepare':
            if not package or package['source_sha'] != candidate['source_sha']:raise ValueError('source_pinned_package_required')
            archive = args.frontend_package / 'frontend-image.tar.gz'
            if archive.is_symlink() or not 0 < archive.stat().st_size < 1024**3 or hashlib.sha256(archive.read_bytes()).hexdigest() != package['archive_sha256']:raise ValueError('frontend_checksum_mismatch')
            with client.open_sftp() as transfer:
                directory = str(PurePosixPath(candidate['source_directory']).parent / 'frontend-package')
                transfer.mkdir(directory, 0o700)
                with transfer.open(directory + '/frontend-image.tar.gz', 'wx') as output, archive.open('rb') as incoming:
                    output.set_pipelined(True)
                    while part := incoming.read(1024 * 1024):output.write(part)
                transfer.chmod(directory + '/frontend-image.tar.gz', 0o600)
        values = {'ROOT':str(PurePosixPath(base['source_directory']).parent),'CANDIDATE':str(PurePosixPath(candidate['source_directory']).parent),'BACKEND':str(PurePosixPath(backend['source_directory']).parent),'BASE_SHA':base['source_sha'],'SHA':candidate['source_sha'],'BACKEND_SHA':backend['source_sha'],'MODE':args.mode,'PACKAGE':package,'GATE':read_json(args.frontend_gate) if args.frontend_gate else {}}
        guarded = 'try:\n' + textwrap.indent(REMOTE, ' ') + "\nexcept Exception as error:\n print(json.dumps({'status':'BLOCKED','reason':str(error) if isinstance(error,ValueError) else 'isolated_frontend_upgrade_failed','error_type':type(error).__name__}))\n"
        encoded = base64.b64encode(('\n'.join(k+'='+repr(v) for k,v in values.items())+'\n'+guarded).encode()).decode()
        _, out, _ = client.exec_command("python3 -c \"import base64;exec(base64.b64decode('"+encoded+"'))\"", timeout=300)
        result = json.loads(out.read(65536));print(json.dumps(result,indent=2))
        return 2 if result['status'] == 'BLOCKED' else 0
    finally:client.close()


if __name__ == '__main__':sys.exit(main())
