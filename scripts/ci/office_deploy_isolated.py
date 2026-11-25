"""Stage verified CI archives and prepare/start only the Office test project.

Uses ordinary aqyfzx via the existing pinned DPAPI connector. No old env files,
credentials, queues, bind mounts or production resources are read/copied.
"""
import argparse
import hashlib
import json
import re
import stat
import subprocess
from pathlib import Path
from office_test_remote import ROOT, REMOTE_ROOT, connect, execute

SHA='4ffd7442da82675bba2b63f3f0dfe31995712070'
PROJECT='knowhub-office-guard-test'
PRIVATE=ROOT/'docs/qa/office-test-deployment/private'

REMOTE=r'''
import datetime,hashlib,json,os,pathlib,secrets,socket,subprocess
root=pathlib.Path(ROOT)
assert socket.gethostname()=='aqyfzx10' and os.geteuid()==1000
assert root.resolve()==root and root.stat().st_uid==os.geteuid()
def run(args,timeout=60):
 p=subprocess.run(args,capture_output=True,text=True,timeout=timeout)
 if p.returncode:raise RuntimeError('command failed: '+str(args[:4])+': '+p.stderr[-3000:])
 return p.stdout
def write(path,body,mode=0o600):
 assert not path.is_symlink()
 if path.exists():
  assert path.read_text()==body,'refusing to overwrite existing release config'
 else:
  fd=os.open(path,os.O_WRONLY|os.O_CREAT|os.O_EXCL,mode)
  with os.fdopen(fd,'w') as f:f.write(body)
def image(ref):return json.loads(run(['docker','image','inspect',ref]))[0]
manifest=json.loads((root/'image-manifest.json').read_text())
assert manifest['source_sha']==SHA
for name,m in manifest['images'].items():
 archive=root/(name+'-image.tar.gz')
 assert not archive.is_symlink()
 h=hashlib.sha256()
 with archive.open('rb') as f:
  while data:=f.read(1024*1024):h.update(data)
 assert h.hexdigest()==m['archive_sha256'],'archive hash mismatch'
 found=subprocess.run(['docker','image','inspect',m['id']],capture_output=True)
 if found.returncode:
  print(json.dumps({'stage':'loading_image','component':name}),flush=True)
  run(['docker','load','-i',str(archive)],240)
 record=image(m['id'])
 assert record['Id']==m['id']
 if name=='app':assert record['Config']['Labels']['org.opencontainers.image.revision']==SHA
pg=image('paradedb/paradedb:v0.22.2-pg17')['Id']
redis=image('redis:7.0-alpine')['Id']
docreader=image('sha256:7ddcecd6854e3002d93a8643c689eeaa653173d2e44389bb44216c968398209d')['Id']
release=root/'site';assert not release.is_symlink();release.mkdir(mode=0o700,exist_ok=True)
compose_path=release/'compose.json'
command=['docker','compose','--project-name',PROJECT,'--file',str(compose_path)]
if not compose_path.exists():
 assert not run(['docker','ps','-aq','--filter','label=com.docker.compose.project='+PROJECT]).strip()
 for port in (39780,39781):
  with socket.socket() as s:s.bind(('127.0.0.1',port))
 for name in ('files','staging','docreader-tmp'):
  p=release/name;assert not p.exists();p.mkdir(mode=0o700)
 # These are owned, initially empty task directories. No host permission change.
 environment={
  'DB_DRIVER':'postgres','DB_HOST':'postgres','DB_PORT':'5432','DB_USER':'postgres',
  'DB_NAME':'office_guard_test','DB_PASSWORD':secrets.token_hex(24),
  'REDIS_ADDR':'redis:6379','REDIS_PASSWORD':secrets.token_hex(24),'REDIS_DB':'0',
  'REDIS_PREFIX':PROJECT+':','WEKNORA_REDIS_NAMESPACE':PROJECT,
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
  'WEKNORA_AUTH_COMPLEX_PASSWORD_ENABLED':'true','FRONTEND_BASE_URL':'http://127.0.0.1:39781',
  'LANGFUSE_ENABLED':'false','TZ':'Asia/Shanghai','DOCREADER_ADDR':'docreader:50051',
  'DOCREADER_TRANSPORT':'grpc','SSRF_WHITELIST':'model-fixture,confluence-fixture',
 }
 write(release/'app.env',''.join(k+'='+v+'\n' for k,v in environment.items()))
 write(release/'postgres.env','POSTGRES_USER=postgres\nPOSTGRES_DB=office_guard_test\nPOSTGRES_PASSWORD='+environment['DB_PASSWORD']+'\n')
 write(release/'redis.conf','bind 0.0.0.0\nprotected-mode yes\nappendonly yes\nrequirepass '+environment['REDIS_PASSWORD']+'\n',0o644)
 config=(root/'candidate-config.yaml').read_text()
 old='personal_mcp:\n  enabled: false\n  policy_reference: ""'
 assert config.count(old)==1
 config=config.replace(old,'personal_mcp:\n  enabled: true\n  policy_reference: "office-guard-isolated-acceptance-20261125"',1)
 assert config.count('upload_scanner_address: ""')==1
 config=config.replace('upload_scanner_address: ""','upload_scanner_address: "scanner:3310"',1)
 write(release/'config.yaml',config,0o644)
 labels={'knowhub.source-sha':SHA,'knowhub.environment':'isolated-office-test'}
 common={'restart':'no','logging':{'driver':'json-file','options':{'max-size':'10m','max-file':'3'}},'pids_limit':512,'labels':labels,'networks':['private']}
 def service(**kw):return {**common,**kw}
 services={
  'postgres':service(image=pg,env_file=['postgres.env'],volumes=['postgres:/var/lib/postgresql/data'],mem_limit='1g',cpus=2,
   healthcheck={'test':['CMD','pg_isready','-U','postgres','-d','office_guard_test'],'interval':'5s','timeout':'5s','retries':30}),
  'redis':service(image=redis,command=['redis-server','/usr/local/etc/redis/redis.conf'],
   volumes=['redis:/data','./redis.conf:/usr/local/etc/redis/redis.conf:ro'],mem_limit='256m',cpus=1),
  'scanner':service(image=manifest['images']['scanner']['id'],mem_limit='2g',cpus=2,
   environment={'CLAMAV_NO_FRESHCLAMD':'true'}),
  'model-fixture':service(image=manifest['images']['app']['id'],user='1000:1000',entrypoint=['python3','/fixture.py'],
   volumes=[str(root/'model-fixture.py')+':/fixture.py:ro'],read_only=True,cap_drop=['ALL'],mem_limit='128m',cpus=1),
  'docreader':service(image=docreader,mem_limit='768m',cpus=2,
   volumes=['./docreader-tmp:/tmp/docreader'],environment={'DOCREADER_GRPC_MAX_WORKERS':'2','DOCREADER_MARKITDOWN_MAX_WORKERS':'1','DOCREADER_IMAGE_OUTPUT_DIR':'/tmp/docreader'}),
  'app':service(image=manifest['images']['app']['id'],env_file=['app.env'],mem_limit='2g',cpus=2,
   volumes=['./files:/data/files','./staging:/data/feishu-staging','./docreader-tmp:/tmp/docreader:ro','./config.yaml:/app/config/config.yaml:ro'],
   ports=['127.0.0.1:39780:8080'],depends_on={'postgres':{'condition':'service_healthy'},'redis':{'condition':'service_started'}},
   healthcheck={'test':['CMD','curl','-fsS','http://127.0.0.1:8080/health'],'interval':'10s','timeout':'5s','retries':18,'start_period':'90s'}),
  'frontend':service(image=manifest['images']['frontend']['id'],mem_limit='128m',cpus=1,
   ports=['127.0.0.1:39781:80'],environment={'APP_HOST':'app','APP_PORT':'8080','APP_SCHEME':'http','MAX_FILE_SIZE_MB':'100','DEFAULT_LOCALE':'zh-CN'},
   depends_on={'app':{'condition':'service_healthy'}}),
 }
 plan={'name':PROJECT,'services':services,'networks':{'private':{'name':PROJECT+'-private','internal':True,'ipam':{'config':[{'subnet':'10.254.222.0/28'}]}}},
  'volumes':{n:{'name':PROJECT+'-'+n,'labels':labels} for n in ('postgres','redis')}}
 write(compose_path,json.dumps(plan,indent=2)+'\n')
 proof={'source_sha':SHA,'project':PROJECT,'release_directory':str(release),'images':manifest['images'],
  'postgres_image':pg,'redis_image':redis,'docreader_image':docreader,'backend_port':39780,'frontend_port':39781,
  'created_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'production_credentials_copied':False,
  'network_internal':True,'host_root_or_docker_socket_mount':False,'existing_projects_modified':False,
  'antivirus':'real ClamAV; isolated signature image, fail-closed upload scanner',
  'embedding':'deterministic owned fixture, not production semantic-quality validation',
  'rollback':'Stop only this project with docker compose stop; preserve all new volumes and evidence. Existing project remains unchanged.'}
 write(release/'deployment-proof.json',json.dumps(proof,indent=2)+'\n')
else:
 proof=json.loads((release/'deployment-proof.json').read_text());assert proof['source_sha']==SHA and proof['project']==PROJECT
 # A failed initial create exhausted Docker's default address pools. Only the
 # not-yet-created task network may receive an explicitly checked free subnet.
 plan=json.loads(compose_path.read_text())
 if not plan['networks']['private'].get('ipam'):
  assert not run(['docker','ps','-aq','--filter','label=com.docker.compose.project='+PROJECT]).strip()
  exists=subprocess.run(['docker','network','inspect',PROJECT+'-private'],capture_output=True)
  assert exists.returncode!=0,'existing network cannot be silently reconfigured'
  import ipaddress
  subnet=ipaddress.ip_network('10.254.222.0/28')
  netids=run(['docker','network','ls','-q']).split()
  networks=json.loads(run(['docker','network','inspect',*netids]))
  ranges=[ipaddress.ip_network(c['Subnet']) for n in networks for c in n['IPAM'].get('Config',[]) if c.get('Subnet')]
  routes=json.loads(run(['ip','-j','route','show','table','all']))
  ranges.extend(ipaddress.ip_network(r['dst']) for r in routes if r.get('dst') and r['dst']!='default')
  assert all(not subnet.overlaps(r) for r in ranges if r.version==4),'network conflict'
  plan['networks']['private']['ipam']={'config':[{'subnet':str(subnet)}]}
  temporary=release/'compose-network-checked.json'
  write(temporary,json.dumps(plan,indent=2)+'\n');temporary.replace(compose_path)
  print(json.dumps({'network_subnet':str(subnet),'checked_existing_docker_networks':len(networks),'checked_routes':len(routes),'existing_networks_changed':False}))
run(command+['config','--quiet'])
if START:
 # Every named volume must be new or already owned by this exact project.
 for n in ('postgres','redis'):
  name=PROJECT+'-'+n
  p=subprocess.run(['docker','volume','inspect',name],capture_output=True,text=True)
  if p.returncode==0:
   v=json.loads(p.stdout)[0];assert v['Labels'].get('knowhub.source-sha')==SHA and v['Labels'].get('com.docker.compose.project')==PROJECT
 run(command+['up','-d','--no-build','--pull','never'],240)
 proof['started_utc']=datetime.datetime.now(datetime.timezone.utc).isoformat()
 print(run(command+['ps','--format','json']))
print(json.dumps(proof))
'''

def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--start',action='store_true')
    args=parser.parse_args()
    manifest={'source_sha':SHA,'images':{}}
    uploads=[]
    for name in ('app','frontend','scanner'):
        directory=PRIVATE/name
        archive=directory/(name+'-image.tar.gz')
        if not archive.is_file():raise SystemExit('Missing completed CI archive: '+str(archive))
        if name!='scanner':assert (directory/'source-sha.txt').read_text().strip()==SHA
        checksum=(directory/(archive.name+'.sha256')).read_text().split()[0]
        hasher=hashlib.sha256()
        with archive.open('rb') as stream:
            while block:=stream.read(1024*1024):hasher.update(block)
        digest=hasher.hexdigest()
        assert checksum==digest
        image_id=(directory/'image-id.txt').read_text().strip()
        assert re.fullmatch(r'sha256:[0-9a-f]{64}',image_id)
        manifest['images'][name]={'id':image_id,'archive_sha256':digest,'bytes':archive.stat().st_size}
        uploads.append((archive,archive.name))
    PRIVATE.mkdir(exist_ok=True,parents=True)
    mf=PRIVATE/'image-manifest.json';mf.write_text(json.dumps(manifest,indent=2)+'\n')
    config=subprocess.check_output(['git','show',SHA+':config/config.yaml'],cwd=ROOT)
    config_path=PRIVATE/'candidate-config.yaml';config_path.write_bytes(config)
    uploads.extend([(mf,mf.name),(config_path,config_path.name),(ROOT/'scripts/ci/office_model_fixture.py','model-fixture.py')])
    with connect() as client:
        with client.open_sftp() as sftp:
            target=sftp.lstat(REMOTE_ROOT)
            assert stat.S_ISDIR(target.st_mode) and target.st_uid==1000
            for local,name in uploads:
                remote=REMOTE_ROOT+'/'+name
                try:
                    old=sftp.lstat(remote)
                    assert stat.S_ISREG(old.st_mode) and old.st_size==local.stat().st_size,'existing staged file mismatch'
                except FileNotFoundError:
                    print(json.dumps({'stage':'transfer','file':name,'bytes':local.stat().st_size}),flush=True)
                    with sftp.open(remote,'wx') as out,local.open('rb') as source:
                        out.set_pipelined(True)
                        while data:=source.read(1024*1024):out.write(data)
                    sftp.chmod(remote,0o600 if name!='model-fixture.py' else 0o644)
        code='ROOT='+repr(REMOTE_ROOT)+'\nSHA='+repr(SHA)+'\nPROJECT='+repr(PROJECT)+'\nSTART='+repr(args.start)+'\n'+REMOTE
        raise SystemExit(execute(client,code,900))

if __name__=='__main__':main()
