"""Prepare an isolated Compose plan and verified images, without starting services.

Credentials arrive through stdin. All new configuration lives below the staged
build directory. Existing routes, containers, databases and storage are untouched.
"""
import argparse
import base64
import hashlib
import json
import re
import sys
import textwrap
from pathlib import Path, PurePosixPath

import paramiko

REMOTE = r'''
import hashlib,json,os,pathlib,secrets,socket,subprocess
root=pathlib.Path(ROOT)
if root.is_symlink() or str(root.resolve().parent)!='/mnt/sata0/knowhub-feishu-v2-builds':
 raise ValueError('unexpected_build_root')
manifest=json.loads((root/'source-manifest.json').read_text())
if manifest['source_sha']!=SHA: raise ValueError('source_sha_mismatch')
source=root/'source'
def run(args,timeout=30):
 return subprocess.run(args,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True,timeout=timeout)
app=(root/'runtime-image.id').read_text().strip()
def inspect(image):
 p=run(['docker','image','inspect',image])
 if p.returncode: raise ValueError('required_image_unavailable:'+image)
 return json.loads(p.stdout)[0]
record=inspect(app)
if record['Config'].get('Labels',{}).get('org.opencontainers.image.revision')!=SHA:
 raise ValueError('runtime_image_source_mismatch')
package=root/'frontend-package'
archive=package/'frontend-image.tar.gz'
if archive.is_symlink() or hashlib.sha256(archive.read_bytes()).hexdigest()!=FRONTEND['archive_sha256']:
 raise ValueError('frontend_archive_checksum_mismatch')
if hashlib.sha256((source/'frontend/Dockerfile').read_bytes()).hexdigest()!=FRONTEND['dockerfile_sha256']:
 raise ValueError('frontend_dockerfile_mismatch')
existing=run(['docker','image','inspect',FRONTEND['image_id']])
if existing.returncode:
 loaded=run(['docker','load','--input',str(archive)],180)
 if loaded.returncode: raise ValueError('frontend_image_load_failed')
frontend=inspect(FRONTEND['image_id'])
if frontend['Config'].get('Labels',{}).get('org.opencontainers.image.revision')!=SHA:
 raise ValueError('frontend_image_source_mismatch')
manifest_path=package/'manifest.json'
if manifest_path.is_symlink():raise ValueError('unexpected_frontend_manifest_path')
if manifest_path.exists():
 if json.loads(manifest_path.read_text())!=FRONTEND:raise ValueError('existing_frontend_manifest_mismatch')
else:
 fd=os.open(str(manifest_path),os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
 with os.fdopen(fd,'w') as output:json.dump(FRONTEND,output,indent=2)
if IMAGES_ONLY:
 print(json.dumps({'status':'IMAGES_VERIFIED','source_sha':SHA,'app_image':app,
  'frontend_image':FRONTEND['image_id'],'services_started':False,'public_ports_exposed':False}))
 raise SystemExit(0)
pg=inspect('paradedb/paradedb:v0.22.2-pg17')['Id']
redis=inspect('redis:7.0-alpine')['Id']
project='knowhub-feishu-v2-'+SHA[:12]
release=root/'isolated-site'
if release.is_symlink(): raise ValueError('unexpected_release_path')
release.mkdir(mode=0o700,exist_ok=True)
if (release/'compose.json').exists():
 proof=json.loads((release/'preparation.json').read_text())
 if proof['source_sha']!=SHA or proof['app_image']!=app or proof['frontend_image']!=FRONTEND['image_id']:
  raise ValueError('existing_isolated_plan_mismatch')
 result=run(['docker','compose','--project-name',project,'--file',str(release/'compose.json'),'config','--quiet'])
 if result.returncode: raise ValueError('existing_isolated_plan_invalid')
 print(json.dumps(proof))
 raise SystemExit(0)
for port in [28480,28481]:
 sock=socket.socket()
 try: sock.bind(('127.0.0.1',port))
 finally: sock.close()
environment={
 'DB_DRIVER':'postgres','DB_HOST':'postgres','DB_PORT':'5432','DB_USER':'postgres',
 'DB_PASSWORD':secrets.token_hex(24),'DB_NAME':'knowhub_feishu_v2_test',
 'REDIS_ADDR':'redis:6379','REDIS_PASSWORD':secrets.token_hex(24),'REDIS_DB':'0',
 'REDIS_PREFIX':project+':','WEKNORA_REDIS_NAMESPACE':project,
 'JWT_SECRET':secrets.token_hex(32),'SYSTEM_AES_KEY':secrets.token_hex(16),
 'SYSTEM_SIGNING_KEY':secrets.token_hex(32),'KNOWHUB_ENVIRONMENT':'isolated-test',
 'FEISHU_ACCESS_V2_ENABLED':'1','FEISHU_PERSONAL_MVP_FIRST_CREATE':'1',
 'FEISHU_SAFE_INCREMENTAL_ENABLED':'true','FEISHU_SYNC_SAFETY_ENABLED':'true',
 'FEISHU_SYNC_SAFETY_MODE':'automatic','FEISHU_SYNC_SOURCE_SHA':SHA,
 'FEISHU_TEST_HTTP_CALLBACK_ENABLED':'0','FEISHU_LEGACY_SYNC_ALLOWED_SOURCE_IDS':'none',
 'WEKNORA_ASYNQ_CORE_CONCURRENCY':'2','WEKNORA_ASYNQ_POSTPROCESS_CONCURRENCY':'1',
 'WEKNORA_ASYNQ_ENRICHMENT_CONCURRENCY':'1','WEKNORA_ASYNQ_MAINTENANCE_CONCURRENCY':'1',
 'WEKNORA_ASYNQ_SHARED_CONCURRENCY':'1','WEKNORA_WIKI_ASYNQ_CONCURRENCY':'1',
 'CONCURRENCY_POOL_SIZE':'2','RETRIEVE_DRIVER':'postgres','STORAGE_TYPE':'local',
 'LOCAL_STORAGE_BASE_DIR':'/data/files','FEISHU_PERSONAL_STAGING_DIR':'/data/feishu-staging',
 'AUTO_MIGRATE':'true','GIN_MODE':'release','LOG_LEVEL':'info','LLM_DEBUG_LOG':'false',
 'WEKNORA_SANDBOX_DOCKER_ENABLED':'false','DISABLE_REGISTRATION':'false',
 'WEKNORA_AUTH_COMPLEX_PASSWORD_ENABLED':'true','FRONTEND_BASE_URL':'http://172.30.232.42',
 'LANGFUSE_ENABLED':'false','TZ':'Asia/Shanghai',
}
def write_private(path,content):
 fd=os.open(str(path),os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
 with os.fdopen(fd,'w') as f: f.write(content)
write_private(release/'app.env',''.join(key+'='+value+'\n' for key,value in environment.items()))
write_private(release/'postgres.env','POSTGRES_USER=postgres\nPOSTGRES_DB='+environment['DB_NAME']+
 '\nPOSTGRES_PASSWORD='+environment['DB_PASSWORD']+'\n')
write_private(release/'redis.conf','bind 0.0.0.0\nprotected-mode yes\nappendonly yes\nrequirepass '+
 environment['REDIS_PASSWORD']+'\n')
# Redis runs as its packaged user, and this new non-secret configuration mount
# must be readable there. The containing release directory remains private.
(release/'redis.conf').chmod(0o644)
config=(source/'config/config.yaml').read_text()
old='personal_mcp:\n  enabled: false\n  policy_reference: ""'
new='personal_mcp:\n  enabled: true\n  policy_reference: "feishu-v2-isolated-test-task"'
if old not in config: raise ValueError('personal_mcp_config_shape_changed')
write_private(release/'config.yaml',config.replace(old,new,1))
(release/'config.yaml').chmod(0o644)
health={'test':['CMD','curl','-fsS','http://127.0.0.1:8080/health'],
 'interval':'15s','timeout':'5s','retries':12,'start_period':'60s'}
common={'image':app,'env_file':['app.env'],'volumes':['files:/data/files','staging:/data/feishu-staging',
 './config.yaml:/app/config/config.yaml:ro'],'restart':'unless-stopped',
 'healthcheck':health,'mem_limit':'3g','cpus':2,'pids_limit':512,
 'depends_on':{'postgres':{'condition':'service_healthy'},'redis':{'condition':'service_started'}},
 'labels':{'knowhub.source-sha':SHA,'knowhub.environment':'isolated-test'}}
services={
 'postgres':{'image':pg,'env_file':['postgres.env'],'volumes':['postgres:/var/lib/postgresql/data'],
  'restart':'unless-stopped','mem_limit':'2g','cpus':2,
  'healthcheck':{'test':['CMD-SHELL','pg_isready -U postgres -d knowhub_feishu_v2_test'],
   'interval':'5s','timeout':'5s','retries':30}},
 'redis':{'image':redis,'command':['redis-server','/usr/local/etc/redis/redis.conf'],
  'volumes':['redis:/data','./redis.conf:/usr/local/etc/redis/redis.conf:ro'],
  'restart':'unless-stopped','mem_limit':'512m','cpus':1},
 'app':{**common,'ports':['127.0.0.1:28480:8080']},
 'worker2':{**common,'environment':{'AUTO_MIGRATE':'false'},
  'depends_on':{'app':{'condition':'service_healthy'}}},
 'frontend':{'image':FRONTEND['image_id'],'ports':['127.0.0.1:28481:80'],
  'environment':{'APP_HOST':'app','APP_PORT':'8080','APP_SCHEME':'http','MAX_FILE_SIZE_MB':'100'},
  'depends_on':{'app':{'condition':'service_healthy'}},'restart':'unless-stopped','mem_limit':'256m'},
}
compose={'name':project,'services':services,'volumes':{name:{} for name in ['postgres','redis','files','staging']},
 'networks':{'default':{'name':project+'-network'}}}
write_private(release/'compose.json',json.dumps(compose,indent=2)+'\n')
result=run(['docker','compose','--project-name',project,'--file',str(release/'compose.json'),'config','--quiet'])
if result.returncode: raise ValueError('isolated_compose_validation_failed')
proof={'source_sha':SHA,'project':project,'release_directory':str(release),'app_image':app,
 'frontend_image':FRONTEND['image_id'],'postgres_image':pg,'redis_image':redis,
 'services_started':False,'public_ports_exposed':False,'http_callback_exception_enabled':False,
 'status':'PREPARED','backup_restore_status':'NOT_EXECUTED','migration_status':'NOT_EXECUTED',
 'scope':'isolated_compose_plan_only'}
write_private(release/'preparation.json',json.dumps(proof,indent=2)+'\n')
print(json.dumps(proof))
'''


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--manifest', type=Path, required=True)
    parser.add_argument('--frontend-package', type=Path, required=True)
    parser.add_argument('--images-only', action='store_true')
    args = parser.parse_args()
    raw = args.manifest.read_bytes()
    staging = json.loads(raw.decode('utf-16') if raw.startswith((b'\xff\xfe', b'\xfe\xff')) else raw.decode('utf-8-sig'))
    sha = staging['source_sha']
    root = PurePosixPath(staging['source_directory']).parent
    if (not re.fullmatch('[a-f0-9]{40}', sha) or root.parent != PurePosixPath('/mnt/sata0/knowhub-feishu-v2-builds')
            or not re.fullmatch(sha[:12] + r'-[a-f0-9]{8}', root.name)):
        raise SystemExit('invalid staged source manifest')
    frontend = json.loads((args.frontend_package/'manifest.json').read_text(encoding='utf-8'))
    archive = args.frontend_package/'frontend-image.tar.gz'
    if (frontend.get('source_sha') != sha or archive.is_symlink() or archive.stat().st_size > 512*1024**2
            or hashlib.sha256(archive.read_bytes()).hexdigest() != frontend['archive_sha256']
            or not re.fullmatch(r'sha256:[a-f0-9]{64}', frontend['image_id'])
            or frontend.get('qualification') != 'frontend_tests_typecheck_build_and_pinned_dockerfile'):
        raise SystemExit('frontend image provenance mismatch')
    client = paramiko.SSHClient()
    try:
        credential = json.loads(sys.stdin.read(16384))
        if credential.get('host') != '172.30.232.42' or credential.get('user') != 'aqyfzx':
            raise ValueError('unexpected_target')
        client.load_system_host_keys()
        known = Path.home()/'.ssh'/'known_hosts'
        if known.is_file(): client.load_host_keys(str(known))
        client.set_missing_host_key_policy(paramiko.RejectPolicy())
        client.connect(credential['host'],username=credential['user'],password=credential['password'],
                       timeout=15,auth_timeout=15,banner_timeout=15,allow_agent=False,look_for_keys=False)
        credential.clear()
        directory = root/'frontend-package'
        check = 'import pathlib;p=pathlib.Path('+repr(str(root))+');'
        check += "assert not p.is_symlink() and str(p.resolve().parent)=='/mnt/sata0/knowhub-feishu-v2-builds';"
        check += "q=p/'frontend-package';assert not q.is_symlink();q.mkdir(mode=0o700,exist_ok=True)"
        code = base64.b64encode(check.encode()).decode()
        _, out, _ = client.exec_command("python3 -c \"import base64;exec(base64.b64decode('"+code+"'))\"",timeout=20)
        if out.channel.recv_exit_status(): raise ValueError('frontend_package_directory_unavailable')
        with client.open_sftp() as transfer:
            remote = str(directory/'frontend-image.tar.gz')
            try:
                import stat
                existing = transfer.lstat(remote)
                if not stat.S_ISREG(existing.st_mode) or existing.st_size != archive.stat().st_size:
                    raise ValueError('existing_frontend_package_mismatch')
            except FileNotFoundError:
                with transfer.open(remote,'wx') as output, archive.open('rb') as incoming:
                    output.set_pipelined(True)
                    while part := incoming.read(1024*1024): output.write(part)
        prefix = 'ROOT='+repr(str(root))+'\nSHA='+repr(sha)+'\nFRONTEND='+repr(frontend)+'\nIMAGES_ONLY='+repr(args.images_only)+'\n'
        guarded = 'try:\n'+textwrap.indent(REMOTE,' ')
        guarded += "\nexcept Exception as error:\n print(json.dumps({'status':'BLOCKED','error_type':type(error).__name__,"
        guarded += "'reason':str(error) if isinstance(error,ValueError) else 'isolated_site_prepare_failed'}))\n raise SystemExit(2)\n"
        code = base64.b64encode((prefix+guarded).encode()).decode()
        _, out, _ = client.exec_command("python3 -c \"import base64;exec(base64.b64decode('"+code+"'))\"",timeout=240)
        report = json.loads(out.read(65536))
        print(json.dumps(report,indent=2))
        return 0 if report.get('status') in ('PREPARED','IMAGES_VERIFIED') else 2
    except (OSError,ValueError,KeyError,TypeError,paramiko.SSHException):
        print(json.dumps({'status':'BLOCKED','reason':'verified_isolated_site_prepare_unavailable'}))
        return 2
    finally:
        client.close()


if __name__ == '__main__':
    sys.exit(main())
