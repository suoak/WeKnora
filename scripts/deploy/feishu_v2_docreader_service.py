"""Build and add the missing structured DocReader only to the existing isolated site."""
import argparse
import base64
import hashlib
import json
import re
import sys
from pathlib import Path, PurePosixPath

import paramiko

REMOTE = r'''
import json,os,pathlib,subprocess,re,hashlib
root=pathlib.Path(ROOT);source=pathlib.Path(SOURCE);site=root/'isolated-site'
project='knowhub-feishu-v2-'+BASE_SHA[:12]
if root.is_symlink() or source.is_symlink() or json.loads((site/'preparation.json').read_text())['project']!=project:raise ValueError('isolated_project_mismatch')
compose_path=site/'compose.json';plan=json.loads(compose_path.read_text())
compose=['docker','compose','--project-name',project,'--file',str(compose_path)]
work=source.parent/('structured-docreader' if ATTEMPT==1 else 'structured-docreader-grpc-tools');work.mkdir(mode=0o700,exist_ok=True)
if work.is_symlink():raise ValueError('unexpected_docreader_directory')
image='knowhub-feishu-v2/docreader:'+SHA
def run(args,timeout=30):return subprocess.run(args,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True,timeout=timeout)
def checked(args,timeout=30):
 p=run(args,timeout)
 if p.returncode:raise ValueError('isolated_docreader_operation_failed')
 return p.stdout.strip()
def inspect(name):return json.loads(checked(['docker','inspect',name]))[0]
for name in ['app','worker2']:
 record=inspect(checked(compose+['ps','--quiet',name]))
 if record['Config']['Labels'].get('com.docker.compose.project')!=project:raise ValueError('container_project_mismatch')
if MODE=='build' and not (work/'build.pid').exists():
 dockerfile=source/'docker/Dockerfile.docreader'
 if ATTEMPT==2:
  prior=source.parent/'structured-docreader/build.exit'
  if not prior.exists() or int(prior.read_text())==0:raise ValueError('failed_first_build_required')
  text=dockerfile.read_text()
  begin=text.index('RUN if [ -z "$TARGETARCH" ]; then')
  end=text.index('COPY docreader/pyproject.toml docreader/uv.lock ./',begin)
  text=text[:begin]+text[end:]
  dockerfile=work/'Dockerfile.grpc-tools'
  dockerfile.write_text(text)
 script=work/'build.sh'
 script.write_text('set -eu\numask 077\ncd '+str(source)+'\ndocker build --label org.opencontainers.image.revision='+SHA+' --label knowhub.environment=isolated-test --label knowhub.docreader-dockerfile-sha256='+hashlib.sha256(dockerfile.read_bytes()).hexdigest()+' -f '+str(dockerfile)+' -t '+image+' . > '+str(work/'build.log')+' 2>&1\n')
 script.chmod(0o700)
 wrapper='umask 077; bash '+str(script)+'; result=$?; printf "%s" "$result" > '+str(work/'build.exit')
 process=subprocess.Popen(['/bin/bash','-c',wrapper],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,stdin=subprocess.DEVNULL,start_new_session=True)
 (work/'build.pid').write_text(str(process.pid))
result={'source_sha':SHA,'project':project,'host_ports_added':False,'scope':'existing_isolated_project_only'}
incoming_archive=source.parent/'docreader-package/docreader-image.tar.gz'
if MODE=='status' and incoming_archive.is_file() and not incoming_archive.is_symlink():result['received_archive_bytes']=incoming_archive.stat().st_size
if MODE in ['build','status']:
 done=work/'build.exit'
 result['status']='NOT_STARTED' if not (work/'build.pid').exists() else 'BUILDING'
 if done.exists():
  result['build_exit_code']=int(done.read_text());result['status']='BUILT' if result['build_exit_code']==0 else 'BUILD_FAILED'
  if result['build_exit_code']==0:
   record=inspect(image)
   if record['Config']['Labels'].get('org.opencontainers.image.revision')!=SHA:raise ValueError('image_source_mismatch')
   result['image_id']=record['Id']
  else:
   text=(work/'build.log').read_text(errors='replace')
   result['failure_markers']=[v for v in ['Could not resolve','Connection refused','failed to solve','No matching distribution','ResolutionImpossible','No space left','timed out','TLS','uv.lock','libreoffice'] if v in text]
   tail=re.sub(r'https?://[^\s]+','[url]',text[-3000:])
   result['build_log_tail']='\n'.join('[sensitive_error_omitted]' if re.search(r'password|authorization|secret',line,re.I) else line for line in tail.splitlines())
elif MODE=='load':
 package=source.parent/'docreader-package';archive=package/'docreader-image.tar.gz'
 if archive.is_symlink() or archive.stat().st_size!=PACKAGE['archive_bytes']:raise ValueError('image_archive_mismatch')
 digest=hashlib.sha256()
 with archive.open('rb') as f:
  for part in iter(lambda:f.read(1024*1024),b''):digest.update(part)
 if digest.hexdigest()!=PACKAGE['archive_sha256']:raise ValueError('image_archive_checksum_mismatch')
 checked(['docker','load','--input',str(archive)],600)
 record=inspect(PACKAGE['image_id'])
 if record['Config']['Labels'].get('org.opencontainers.image.revision')!=SHA:raise ValueError('image_source_mismatch')
 (work/'ci-image.id').write_text(record['Id']);result.update(status='CI_IMAGE_VERIFIED',image_id=record['Id'])
elif MODE=='start':
 ci=work/'ci-image.id'
 if not ci.exists() and (not (work/'build.exit').exists() or int((work/'build.exit').read_text())!=0):raise ValueError('verified_docreader_build_required')
 record=inspect(ci.read_text().strip() if ci.exists() else image)
 if record['Config']['Labels'].get('org.opencontainers.image.revision')!=SHA:raise ValueError('image_source_mismatch')
 backup=work/'compose.before.json'
 if not backup.exists():
  fd=os.open(str(backup),os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
  with os.fdopen(fd,'wb') as out:out.write(compose_path.read_bytes())
 health="import grpc; from grpc_health.v1 import health_pb2,health_pb2_grpc; c=grpc.insecure_channel('127.0.0.1:50051'); assert health_pb2_grpc.HealthStub(c).Check(health_pb2.HealthCheckRequest(),timeout=3).status==1"
 service={'image':record['Id'],'restart':'unless-stopped','expose':['50051'],'environment':{'DOCREADER_GRPC_PORT':'50051','DOCREADER_GRPC_MAX_WORKERS':'2','DOCREADER_IMAGE_OUTPUT_DIR':'/tmp/docreader','SSRF_WHITELIST':''},'volumes':['docreader-tmp:/tmp/docreader'],'tmpfs':['/tmp:rw,nosuid,nodev,size=512m'],'read_only':True,'cap_drop':['ALL'],'security_opt':['no-new-privileges:true'],'mem_limit':'2g','cpus':1,'pids_limit':256,'healthcheck':{'test':['CMD','/app/.venv/bin/python','-c',health],'interval':'10s','timeout':'5s','retries':12,'start_period':'30s'},'labels':{'knowhub.source-sha':SHA,'knowhub.environment':'isolated-test'}}
 if 'networks' in plan['services']['app']:service['networks']=plan['services']['app']['networks']
 plan['services']['docreader']=service
 plan.setdefault('volumes',{}).setdefault('docreader-tmp',{})
 for name in ['app','worker2']:
  target=plan['services'][name]
  target.setdefault('environment',{}).update({'DOCREADER_ADDR':'docreader:50051','DOCREADER_TRANSPORT':'grpc'})
  mount='docreader-tmp:/tmp/docreader:ro'
  if mount not in target.setdefault('volumes',[]):target['volumes'].append(mount)
  target.setdefault('depends_on',{})['docreader']={'condition':'service_healthy'}
 compose_path.write_text(json.dumps(plan,indent=2)+'\n');compose_path.chmod(0o600)
 checked(compose+['config','--quiet'])
 checked(compose+['up','-d','--no-build','--pull','never','docreader'],120)
 result['status']='STARTED_APP_RECREATE_PENDING'
elif MODE=='enable-updates':
 if inspect(checked(compose+['ps','--quiet','app']))['Config']['Labels'].get('knowhub.source-sha')!=SHA:raise ValueError('current_app_source_mismatch')
 environment_path=site/'app.env';environment=dict(line.split('=',1) for line in environment_path.read_text().splitlines() if '=' in line)
 if environment.get('FEISHU_SAFE_INCREMENTAL_ENABLED')!='true' or environment.get('FEISHU_SYNC_SAFETY_ENABLED')!='true':raise ValueError('sync_safety_required')
 sql="BEGIN READ ONLY; SELECT coalesce(json_agg(q),'[]') FROM (SELECT m.id,m.resource_type,d.status,d.sync_mode,d.sync_schedule FROM feishu_personal_imports m JOIN users u ON u.id=m.user_id JOIN knowledge_bases k ON k.id=m.kb_id JOIN data_sources d ON d.id=m.id WHERE u.username='test' AND u.deleted_at IS NULL AND k.name='test001' AND k.deleted_at IS NULL AND m.auth_mode='user_oauth' AND m.status='source_managed') q; COMMIT;"
 response=run(compose+['exec','-T','postgres','psql','-qX','-v','ON_ERROR_STOP=1','-U','postgres','-d','knowhub_feishu_v2_test','-At','-c',sql])
 if response.returncode:raise ValueError('readonly_source_check_failed')
 expected={'6d1698a6e0d9','1dcf458346ba'}
 sources=[v for v in json.loads(response.stdout.strip()) if hashlib.sha256(v['id'].encode()).hexdigest()[:12] in expected]
 if len(sources)!=2 or {v['resource_type'] for v in sources}!={'sheet','bitable'} or any(v['status']!='active' or v['sync_mode']!='incremental' or v['sync_schedule']!='0 */15 * * * *' or not re.fullmatch('[a-f0-9-]{36}',v['id']) for v in sources):raise ValueError('approved_active_sources_required')
 allowed=','.join(sorted(v['id'] for v in sources));prior=environment.get('FEISHU_LEGACY_SYNC_ALLOWED_SOURCE_IDS')
 if prior not in ['none',allowed]:raise ValueError('existing_sync_scope_requires_review')
 if prior=='none':
  backup=work/'app.before-source-allowlist.env'
  fd=os.open(str(backup),os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
  with os.fdopen(fd,'wb') as output:output.write(environment_path.read_bytes())
  updated='\n'.join('FEISHU_LEGACY_SYNC_ALLOWED_SOURCE_IDS='+allowed if line.startswith('FEISHU_LEGACY_SYNC_ALLOWED_SOURCE_IDS=') else line for line in environment_path.read_text().splitlines())+'\n'
  environment_path.write_text(updated);environment_path.chmod(0o600)
  checked(compose+['up','-d','--no-build','--pull','never','app','worker2'],180)
 result.update(status='SOURCE_LIMITED_UPDATES_ENABLED',source_count=2,source_hashes=sorted(expected),other_sources_allowed=False,source_cursor_reset=False)
elif MODE=='health':
 record=inspect(checked(compose+['ps','--quiet','docreader']))
 result.update(status='HEALTHY' if record['State'].get('Health',{}).get('Status')=='healthy' else 'STARTING',health=record['State'].get('Health',{}).get('Status'),image_id=record['Image'])
print(json.dumps(result))
'''


def read_json(path):
    raw = path.read_bytes()
    return json.loads(raw.decode('utf-16') if raw.startswith((b'\xff\xfe', b'\xfe\xff')) else raw.decode('utf-8-sig'))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--manifest', type=Path, required=True)
    parser.add_argument('--source-manifest', type=Path, required=True)
    parser.add_argument('--mode', choices=['build', 'status', 'load', 'start', 'health', 'enable-updates'], required=True)
    parser.add_argument('--attempt', type=int, choices=[1, 2], default=1)
    parser.add_argument('--image-package', type=Path)
    args = parser.parse_args()
    base, source = read_json(args.manifest), read_json(args.source_manifest)
    for value in [base, source]:
        directory = PurePosixPath(value['source_directory']).parent
        if not re.fullmatch('[a-f0-9]{40}', value['source_sha']) or directory.parent != PurePosixPath('/mnt/sata0/knowhub-feishu-v2-builds'):
            raise ValueError('unexpected_source_manifest')
    client = paramiko.SSHClient()
    try:
        credential = json.loads(sys.stdin.read(16384))
        if credential['host'] != '172.30.232.42' or credential['user'] != 'aqyfzx':
            raise ValueError('unexpected_target')
        client.load_system_host_keys()
        known = Path.home() / '.ssh' / 'known_hosts'
        if known.is_file():
            client.load_host_keys(str(known))
        client.set_missing_host_key_policy(paramiko.RejectPolicy())
        client.connect(credential['host'], username=credential['user'], password=credential['password'], timeout=15,
                       auth_timeout=15, banner_timeout=15, allow_agent=False, look_for_keys=False)
        credential.clear()
        package = None
        if args.mode == 'load':
            if args.image_package is None:
                raise ValueError('image_package_required')
            package = read_json(args.image_package / 'manifest.json')
            archive = args.image_package / 'docreader-image.tar.gz'
            if package.get('source_sha') != source['source_sha'] or package.get('target') != 'linux/amd64' or package.get('qualification') != 'source_pinned_isolated_structured_docreader':
                raise ValueError('image_package_provenance_mismatch')
            digest = hashlib.sha256()
            with archive.open('rb') as incoming:
                for part in iter(lambda: incoming.read(1024 * 1024), b''):
                    digest.update(part)
            if archive.is_symlink() or not 0 < archive.stat().st_size <= 4 * 1024**3 or archive.stat().st_size != package['archive_bytes'] or digest.hexdigest() != package['archive_sha256']:
                raise ValueError('local_archive_checksum_mismatch')
            transfer = client.open_sftp()
            directory = str(PurePosixPath(source['source_directory']).parent / 'docreader-package')
            try:
                try:
                    transfer.mkdir(directory, 0o700)
                except OSError:
                    if not __import__('stat').S_ISDIR(transfer.lstat(directory).st_mode):
                        raise ValueError('unexpected_package_directory')
                remote = directory + '/docreader-image.tar.gz'
                try:
                    existing = transfer.lstat(remote)
                    if not __import__('stat').S_ISREG(existing.st_mode) or existing.st_size != archive.stat().st_size:
                        raise ValueError('existing_package_payload_mismatch')
                except FileNotFoundError:
                    with transfer.open(remote, 'wx') as output, archive.open('rb') as incoming:
                        output.set_pipelined(True)
                        while part := incoming.read(1024 * 1024):
                            output.write(part)
                transfer.chmod(remote, 0o600)
            finally:
                transfer.close()
        values = {'ROOT': str(PurePosixPath(base['source_directory']).parent), 'SOURCE': source['source_directory'],
                  'BASE_SHA': base['source_sha'], 'SHA': source['source_sha'], 'MODE': args.mode, 'ATTEMPT': args.attempt,
                  'PACKAGE': package}
        prefix = '\n'.join(k + '=' + repr(v) for k, v in values.items()) + '\n'
        import textwrap
        code = prefix + 'try:\n' + textwrap.indent(REMOTE, ' ') + "\nexcept Exception as e:\n print(json.dumps({'status':'BLOCKED','error_type':type(e).__name__,'reason':str(e) if isinstance(e,ValueError) else 'isolated_docreader_failed'}))\n"
        encoded = base64.b64encode(code.encode()).decode()
        _, out, _ = client.exec_command("python3 -c \"import base64;exec(base64.b64decode('" + encoded + "'))\"", timeout=900)
        result = json.loads(out.read(65536))
        print(json.dumps(result, indent=2))
        return 2 if result['status'] in ['BLOCKED', 'BUILD_FAILED'] else 0
    finally:
        client.close()


if __name__ == '__main__':
    sys.exit(main())
