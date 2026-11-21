"""Build and deploy only a new, loopback-only Portal acceptance instance.

Uses the existing pinned test-host connection. Never reuses an application
database, application environment, source cursor, or existing service name.
"""
import argparse
import hashlib
import json
from pathlib import Path
import re
import subprocess
import sys
import tarfile
import time

from remote_readonly import connect

REMOTE_COMMON = r'''
import datetime,hashlib,json,os,pathlib,re,subprocess,tarfile,time
root=pathlib.Path(ROOT)
allowed=pathlib.Path('/mnt/sata0/knowhub-feishu-v2-builds')
if root.parent!=allowed or root.is_symlink() or allowed.is_symlink():raise ValueError('unexpected_test_root')
if not re.fullmatch('portal-access-'+SHA[:12]+'-[0-9]{8}T[0-9]{6}Z',root.name):raise ValueError('unexpected_test_name')
if not re.fullmatch('[a-f0-9]{40}',SHA):raise ValueError('invalid_source_sha')
def run(args,timeout=30,data=None):
 p=subprocess.run(args,input=data,stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=timeout)
 if p.returncode:raise RuntimeError('command_failed:'+args[0]+':'+str(p.returncode))
 return p.stdout.decode().strip()
def inspect(name):return json.loads(run(['docker','inspect',name]))[0]
def digest(path):
 h=hashlib.sha256()
 with path.open('rb')as f:
  for part in iter(lambda:f.read(1048576),b''):h.update(part)
 return h.hexdigest()
def extract(archive,destination):
 destination.mkdir(mode=0o700)
 with tarfile.open(archive)as t:
  entries=t.getmembers()
  if len(entries)>50000 or sum(e.size for e in entries)>1024**3:raise ValueError('archive_size_limit')
  for e in entries:
   target=destination/e.name
   target.resolve().relative_to(destination.resolve())
   if not(e.isdir()or e.isfile()):raise ValueError('unsafe_archive_entry')
  t.extractall(destination)
'''

STAGE = r'''
proof=json.loads((root/'staging.json').read_text())
if proof['source_sha']!=SHA:raise ValueError('source_mismatch')
for filename in ['source.tar','frontend.tar.gz']:
 if digest(root/filename)!=proof['digests'][filename]:raise ValueError('archive_digest_mismatch')
extract(root/'source.tar',root/'source')
extract(root/'frontend.tar.gz',root/'frontend')
proof.update(status='STAGED',isolated=True,production_changes=False)
(root/'manifest.json').write_text(json.dumps(proof,indent=2))
print(json.dumps(proof))
'''

BUILD = r'''
proof=json.loads((root/'manifest.json').read_text())
if proof['source_sha']!=SHA:raise ValueError('source_mismatch')
image=json.loads(run(['docker','image','inspect','knowhub-build/golang:1.26-feishu-py311-ensurepip-20261022']))[0]
if image['Id']!='sha256:33b84179859a3a275976e22ecf184643e4835de0a230d635f6ad88e299634ef0':raise ValueError('builder_image_mismatch')
available=int(next(x.split()[1]for x in pathlib.Path('/proc/meminfo').read_text().splitlines()if x.startswith('MemAvailable:')))*1024
if available<8*1024**3:raise ValueError('insufficient_build_memory')
name='knowhub-portal-build-'+SHA[:12]
if subprocess.run(['docker','inspect',name],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode==0:
 record=inspect(name)
 if record['Config']['Labels'].get('portal.source')!=SHA:raise ValueError('build_identity_mismatch')
else:
 for folder in ['cache','output']: (root/folder).mkdir(mode=0o700)
 script=root/'compile.sh'
 script.write_text(SCRIPT)
 args=['docker','run','-d','--name',name,'--pull=never','--cpus','2','--memory','6g','--pids-limit','512',
  '--label','portal.source='+SHA,'--label','portal.phase=compile-only',
  '-v',str(root/'source')+':/workspace:ro','-v',str(root/'cache')+':/cache',
  '-v',str(root/'output')+':/output','-v',str(script)+':/compile.sh:ro',
  '-v','/home/aqyfzx/go/pkg/mod:/baseline-modcache:ro',
  '-e','GOMAXPROCS=2','-e','GOTOOLCHAIN=local','-e','GOENV=off',
  '-e','GOPATH=/cache/go','-e','GOCACHE=/cache/go-build','-e','GOSUMDB=sum.golang.org',
  '-e','GOPROXY=https://goproxy.cn|https://proxy.golang.org|direct',
  '-w','/workspace',image['Id'],'bash','/compile.sh']
 run(args)
 record=inspect(name)
proof.update(build_container=name,builder_image=image['Id'],build_status=record['State']['Status'])
(root/'manifest.json').write_text(json.dumps(proof,indent=2))
print(json.dumps({k:proof[k]for k in ['source_sha','build_container','builder_image','build_status']}))
'''

POLL = r'''
proof=json.loads((root/'manifest.json').read_text())
record=inspect(proof['build_container'])
state=record['State']
out={'source_sha':SHA,'status':state['Status'],'exit_code':state.get('ExitCode')}
if state['Status']=='running':
 p=subprocess.run(['docker','logs','--tail','5',proof['build_container']],capture_output=True,timeout=15)
 out['progress']=(p.stdout+p.stderr).decode('utf-8','replace')[-3000:]
 p=subprocess.run(['docker','exec',proof['build_container'],'python3','-c',"import pathlib,json;print(json.dumps([p.read_bytes().replace(bytes([0]),b' ').decode(errors='replace')[:200]for p in pathlib.Path('/proc').glob('[0-9]*/cmdline')][:8]))"],capture_output=True,timeout=15)
 out['build_processes']=json.loads(p.stdout)if p.returncode==0 else ['process_probe_unavailable']
if state['Status']=='exited'and state['ExitCode']==0:
 out['binary_sha256']=digest(root/'output/WeKnora')
 out['go_version']=(root/'output/go-version.txt').read_text().strip()
 out['binary_bytes']=(root/'output/WeKnora').stat().st_size
 proof.update(out);(root/'manifest.json').write_text(json.dumps(proof,indent=2))
elif state['Status']=='exited':
 out['build_log_tail']=run(['docker','logs','--tail','16',proof['build_container']])
print(json.dumps(out))
'''

COMPILE = r'''#!/bin/bash
set -euo pipefail
mkdir -p /cache/go/pkg/mod/cache/download /cache/go-build /cache/headers
python3 - <<'PY'
import pathlib,shutil
origin=pathlib.Path('/baseline-modcache/cache/download')
target=pathlib.Path('/cache/go/pkg/mod/cache/download')
def escape(s):return ''.join('!'+x.lower()if'A'<=x<='Z'else x for x in s)
for line in pathlib.Path('go.sum').read_text().splitlines():
 module,version,_=line.split();version=version.removesuffix('/go.mod')
 relative=pathlib.PurePosixPath(escape(module))/'@v'/escape(version)
 if relative.is_absolute()or'..'in relative.parts:raise ValueError('invalid_module_path')
 for ext in ['.info','.mod','.zip','.ziphash']:
  src=origin/(str(relative)+ext);dst=target/(str(relative)+ext)
  if src.is_file()and not src.is_symlink()and not dst.exists():
   src.resolve().relative_to(origin.resolve());dst.parent.mkdir(parents=True,exist_ok=True);shutil.copyfile(src,dst)
PY
go version | tee /output/go-version.txt
go mod download github.com/mattn/go-sqlite3
sqlite_module=$(go list -m -f '{{.Dir}}' github.com/mattn/go-sqlite3)
cp "$sqlite_module/sqlite3-binding.h" /cache/headers/sqlite3.h
cp "$sqlite_module/sqlite3ext.h" /cache/headers/sqlite3ext.h
export CPATH=/cache/headers
timeout 1800 go build -p 2 -trimpath -ldflags '-X github.com/Tencent/WeKnora/internal/handler.Version=portal-acceptance -X github.com/Tencent/WeKnora/internal/handler.CommitID=__SHA__' -o /output/WeKnora ./cmd/server
go mod verify
sha256sum /output/WeKnora | tee /output/binary.sha256
'''

DEPLOY = r'''
import secrets,shutil,socket,urllib.request,urllib.error,ipaddress
proof=json.loads((root/'manifest.json').read_text())
if proof.get('status')=='DEPLOYED':
 print(json.dumps(proof['site']));raise SystemExit(0)
builder=inspect(proof['build_container'])
if builder['State']['Status']!='exited'or builder['State']['ExitCode']!=0:raise ValueError('candidate_build_not_passed')
binary=root/'output/WeKnora'
if digest(binary)!=proof['binary_sha256']:raise ValueError('candidate_binary_mismatch')
appbase=json.loads(run(['docker','image','inspect','knowhub-feishu-v2/app:bb3e85d1214a3b390b360742d48c3c5f7e75b439']))[0]
if not appbase['Id'].startswith('sha256:4a5a8ed78e1a'):raise ValueError('runtime_base_mismatch')
nginx=json.loads(run(['docker','image','inspect','nginx:1.30.3-alpine']))[0]
pg=json.loads(run(['docker','image','inspect','paradedb/paradedb:v0.22.2-pg17']))[0]
redis=json.loads(run(['docker','image','inspect','redis:7.0-alpine']))[0]
project='knowhub-portal-'+SHA[:12]
for service in ['app','frontend','postgres','redis']:
 if subprocess.run(['docker','inspect',project+'-'+service+'-1'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode==0:
  raise ValueError('isolated_service_name_already_exists')
context=root/'runtime-context';context.mkdir(mode=0o700,exist_ok=True)
shutil.copy2(binary,context/'WeKnora')
for folder in ['config','migrations']:shutil.copytree(root/'source'/folder,context/folder,dirs_exist_ok=True)
(context/'Dockerfile').write_text('FROM '+appbase['Id']+'\nUSER root\nCOPY --chown=appuser:appuser WeKnora /app/WeKnora\nCOPY config /app/config\nCOPY migrations /app/migrations\nWORKDIR /app\nUSER appuser\nLABEL org.opencontainers.image.revision="'+SHA+'" portal.scope="isolated-test"\nENTRYPOINT ["/app/WeKnora"]\n')
appimage=project+':app';frontimage=project+':frontend'
def build(context,tag):
 p=subprocess.run(['docker','build','--network','none','--pull=false','-t',tag,str(context)],env={**os.environ,'DOCKER_BUILDKIT':'0'},stdout=subprocess.PIPE,stderr=subprocess.STDOUT,timeout=180)
 (root/(tag.rsplit(':',1)[1]+'-image-build.log')).write_bytes(p.stdout)
 if p.returncode:raise RuntimeError('isolated_image_build_failed')
build(context,appimage)
front=root/'frontend-context';front.mkdir(mode=0o700,exist_ok=True)
shutil.copytree(root/'frontend',front/'dist',dirs_exist_ok=True)
(front/'nginx.conf').write_text('server { listen 8080; server_name _; root /usr/share/nginx/html; client_max_body_size 10m; location /api/ { proxy_pass http://app:8080; proxy_set_header Host $host; proxy_set_header X-Forwarded-Proto $scheme; } location /health { proxy_pass http://app:8080; } location / { try_files $uri $uri/ /index.html; } }\n')
(front/'Dockerfile').write_text('FROM '+nginx['Id']+'\nCOPY dist /usr/share/nginx/html\nCOPY nginx.conf /etc/nginx/conf.d/default.conf\nLABEL org.opencontainers.image.revision="'+SHA+'" portal.scope="isolated-test"\n')
build(front,frontimage)
release=root/'site';release.mkdir(mode=0o700,exist_ok=True)
for folder in ['postgres','redis','files','backups']:(release/folder).mkdir(mode=0o700,exist_ok=True)
with socket.socket()as s:s.bind(('127.0.0.1',0));port=s.getsockname()[1]
password=secrets.token_urlsafe(32)
env={'GIN_MODE':'release','LOG_LEVEL':'warn','DB_DRIVER':'postgres','DB_HOST':'postgres','DB_PORT':'5432','DB_USER':'postgres',
 'DB_PASSWORD':password,'DB_NAME':'portal_notifications','RETRIEVE_DRIVER':'postgres','AUTO_MIGRATE':'true','REDIS_ADDR':'redis:6379',
 'JWT_SECRET':secrets.token_hex(32),'SYSTEM_AES_KEY':secrets.token_hex(16),'SYSTEM_SIGNING_KEY':secrets.token_hex(32),
 'DUCKDB_SKIP_EXTENSION_LOAD':'true','OIDC_AUTH_ENABLE':'false','SOURCEID_ENABLE':'false','SOURCEID_JIT_ENABLE':'false',
 'DISABLE_REGISTRATION':'false','DOCREADER_ADDR':'127.0.0.1:50051','WEKNORA_SANDBOX_DOCKER_ENABLED':'false'}
compose={'services':{
 'postgres':{'image':pg['Id'],'environment':{'POSTGRES_PASSWORD':password,'POSTGRES_DB':'portal_notifications'},'volumes':[str(release/'postgres')+':/var/lib/postgresql/data'],'networks':['isolated'],'mem_limit':'2g'},
 'redis':{'image':redis['Id'],'command':['redis-server','--appendonly','yes'],'volumes':[str(release/'redis')+':/data'],'networks':['isolated'],'mem_limit':'256m'},
 'app':{'image':appimage,'environment':env,'volumes':[str(release/'files')+':/data/files'],'networks':['isolated'],'mem_limit':'4g','cpus':2},
 'frontend':{'image':frontimage,'ports':['127.0.0.1:'+str(port)+':8080'],'networks':['isolated','preview'],'mem_limit':'256m'}},
 'networks':{'isolated':{'internal':True},'preview':{}}}
for service in compose['services'].values():service['labels']={'portal.scope':'isolated-test','portal.source':SHA}
path=release/'compose.json'
if path.exists():
 compose=json.loads(path.read_text())
 for service in compose['services'].values():
  if service['labels']!={'portal.scope':'isolated-test','portal.source':SHA}:raise ValueError('resume_scope_mismatch')
 port=int(compose['services']['frontend']['ports'][0].split(':')[1])
# The host's default Docker pools are exhausted. Allocate two small subnets
# after checking all existing Docker networks and non-default IPv4 routes.
used=[]
network_ids=run(['docker','network','ls','-q']).splitlines()
for network in json.loads(run(['docker','network','inspect']+network_ids)):
 for config in network['IPAM'].get('Config',[]):
  if config.get('Subnet'):used.append(ipaddress.ip_network(config['Subnet']))
for route in json.loads(run(['ip','-4','-j','route','show','table','all'])):
 if route.get('dst','default')!='default':used.append(ipaddress.ip_network(route['dst'],strict=False))
available=[]
for candidate in ipaddress.ip_network('10.244.253.0/24').subnets(new_prefix=28):
 if not any(n.version==4 and candidate.overlaps(n)for n in used):available.append(str(candidate))
if len(available)<2:raise ValueError('isolated_subnets_unavailable')
for name,subnet in zip(['isolated','preview'],available[:2]):compose['networks'][name]['ipam']={'config':[{'subnet':subnet}]}
fd=os.open(path,os.O_WRONLY|os.O_CREAT|os.O_TRUNC,0o600)
with os.fdopen(fd,'w')as f:json.dump(compose,f,indent=2)
cmd=['docker','compose','--project-name',project,'--file',str(path)]
run(cmd+['config','--quiet'])
run(cmd+['up','-d','postgres','redis'],90)
pgname=project+'-postgres-1'
for _ in range(60):
 p=subprocess.run(['docker','exec',pgname,'pg_isready','-h','127.0.0.1','-U','postgres','-d','portal_notifications'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
 if p.returncode==0:break
 time.sleep(1)
else:raise RuntimeError('isolated_database_not_ready')
backup=release/'backups/before-app.dump'
with backup.open('xb')as f:
 p=subprocess.run(['docker','exec','-u','postgres',pgname,'pg_dump','-Fc','-U','postgres','-d','portal_notifications'],stdout=f,stderr=subprocess.PIPE,timeout=60)
 if p.returncode:raise RuntimeError('empty_test_database_backup_failed')
run(['docker','exec','-u','postgres',pgname,'createdb','-T','template0','-U','postgres','portal_backup_restore'])
with backup.open('rb')as f:
 p=subprocess.run(['docker','exec','-i','-u','postgres',pgname,'pg_restore','--exit-on-error','--no-owner','-U','postgres','-d','portal_backup_restore'],stdin=f,stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=60)
 if p.returncode:raise RuntimeError('empty_test_database_restore_failed')
run(cmd+['up','-d','app','frontend'],90)
for _ in range(90):
 try:
  with urllib.request.urlopen('http://127.0.0.1:'+str(port)+'/health',timeout=2)as r:
   if r.status==200:break
 except Exception:pass
 state=inspect(project+'-app-1')['State']
 if not state['Running']:raise RuntimeError('isolated_app_exited')
 time.sleep(1)
else:raise RuntimeError('isolated_http_not_ready')
state=run(['docker','exec','-u','postgres',pgname,'psql','-X','-w','-U','postgres','-d','portal_notifications','-Atc',"SELECT version::text||':'||dirty::text FROM schema_migrations"])
if state!='3050:false':raise ValueError('isolated_migration_state_mismatch')
site={'project':project,'source_sha':SHA,'frontend_port':port,'url':'http://127.0.0.1:'+str(port),'exposure':'loopback-only',
 'database_container':pgname,'database':'portal_notifications','migration':'3050:false',
 'app_image':inspect(project+'-app-1')['Image'],'frontend_image':inspect(project+'-frontend-1')['Image'],
 'binary_sha256':proof['binary_sha256'],'empty_backup_sha256':digest(backup),'backup_restore_verified':True,
 'existing_services_changed':False,'production_changes':False,'native_parser_profile':'disabled_like_standard_APP_CI'}
proof.update(status='DEPLOYED',site=site);(root/'manifest.json').write_text(json.dumps(proof,indent=2))
print(json.dumps(site))
'''

SEED = r'''
import secrets,urllib.request,urllib.error
proof=json.loads((root/'manifest.json').read_text())
if proof.get('status')!='DEPLOYED':raise ValueError('isolated_site_not_deployed')
site=proof['site'];pgname=site['database_container']
if inspect(pgname)['Config']['Labels'].get('portal.scope')!='isolated-test':raise ValueError('database_scope_mismatch')
if site['database']!='portal_notifications':raise ValueError('database_name_mismatch')
def sql(statement):
 return run(['docker','exec','-i','-u','postgres',pgname,'psql','-X','-w','-U','postgres','-d','portal_notifications','-qAt','-v','ON_ERROR_STOP=1'],data=statement.encode())
def literal(value):return "'"+str(value).replace("'","''")+"'"
def api(method,path,body=None,token=None,tenant=None,expected=(200,201)):
 headers={'Content-Type':'application/json'}
 if token:headers['Authorization']='Bearer '+token
 if tenant:headers['X-Tenant-ID']=str(tenant)
 req=urllib.request.Request(site['url']+'/api/v1'+path,data=json.dumps(body,ensure_ascii=False).encode()if body is not None else None,headers=headers,method=method)
 try:
  with urllib.request.urlopen(req,timeout=25)as r:status=r.status;result=json.load(r)
 except urllib.error.HTTPError as e:status=e.code;result=json.load(e)
 if status not in expected:raise RuntimeError('test_api_failed:'+method+':'+path+':'+str(status)+':'+str(result.get('message',''))[:300])
 return result
private=root/'site/accounts.json'
if private.exists():accounts=json.loads(private.read_text())
else:
 accounts={role:{'username':'portal-'+role+'-'+SHA[:7],'email':'portal-'+role+'-'+SHA[:7]+'@example.invalid','password':secrets.token_urlsafe(18)+'Aa9!'}for role in ['owner','applicant']}
 fd=os.open(private,os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
 with os.fdopen(fd,'w')as f:json.dump(accounts,f)
for role,user in accounts.items():
 if not sql('BEGIN READ ONLY; SELECT count(*) FROM users WHERE email='+literal(user['email'])+'; ROLLBACK;').strip().endswith('1'):
  if len(user['password'])>32:
   user['password']=secrets.token_urlsafe(18)+'Aa9!'
   private.write_text(json.dumps(accounts))
  api('POST','/auth/register',user)
 result=api('POST','/auth/login',{'email':user['email'],'password':user['password']})
 user['token']=result['token'];user['user_id']=result['user']['id'];user['tenant_id']=result['active_tenant']['id']
owner=accounts['owner'];applicant=accounts['applicant'];tid=owner['tenant_id']
sql('BEGIN; UPDATE users SET is_system_admin=true WHERE id='+literal(owner['user_id'])+'; COMMIT;')
payload={'display_name':'概念2','description':'隔离门户访问申请与消息通知验收空间','category':'requirements_management','responsible_team':'Portal acceptance test','contact':'Isolated test Owner','stages':['concept_product'],'featured':True,'display_order':0,'interaction_organization_id':None}
admin='/system/admin/portal/spaces/'+str(tid)
config=api('PUT',admin,payload,owner['token'])['data']
if config['allow_access_request']is not True:raise ValueError('new_config_not_requestable')
api('POST',admin+'/publish',{},owner['token'])
payload['allow_access_request']=False
api('PUT',admin,payload,owner['token'])
api('POST','/portal/spaces/'+str(tid)+'/access-requests',{'reason':'Closed switch must reject'},applicant['token'],expected=(409,))
payload['allow_access_request']=True;api('PUT',admin,payload,owner['token'])
if 'fixture'not in proof:
 kb=api('POST','/knowledge-bases',{'name':'门户授权验收知识库','type':'document','embedding_model_id':'isolated-keyword-only-no-model-calls','summary_model_id':''},owner['token'])['data']
 doc=api('POST','/knowledge-bases/'+kb['id']+'/knowledge/manual',{'title':'PORTAL_ACCESS_CANARY 权限验收规范','content':'# 门户访问验收\nPORTAL_ACCESS_CANARY：审批通过后，Viewer 可以浏览知识并进行标题关键词检索。','status':'draft'},owner['token'])['data']
 # This is an explicit synthetic content fixture, not a claim of parser,
 # embedding-provider or semantic-retrieval acceptance. Authorization remains real.
 sql("BEGIN; UPDATE knowledges SET parse_status='completed',enable_status='enabled',metadata=jsonb_set(metadata::jsonb,'{status}','\"publish\"'::jsonb) WHERE id="+literal(doc['id'])+"; COMMIT;")
 proof['fixture']={'tenant_id':tid,'kb_id':kb['id'],'knowledge_id':doc['id'],'space_name':'概念2',
  'kb_name':'门户授权验收知识库','knowledge_title':'PORTAL_ACCESS_CANARY 权限验收规范','content_kind':'synthetic_manual_fixture','search_scope':'title_keyword_only_no_external_model_calls'}
fixture=proof['fixture']
api('GET','/knowledge-bases/'+fixture['kb_id'],token=applicant['token'],tenant=tid,expected=(403,404))
api('GET','/me/notifications',expected=(401,))
for user in accounts.values():user.pop('token',None)
fd=os.open(private,os.O_WRONLY|os.O_TRUNC,0o600)
with os.fdopen(fd,'w')as f:json.dump(accounts,f,indent=2)
(root/'manifest.json').write_text(json.dumps(proof,indent=2))
print(json.dumps({'source_sha':SHA,'fixture':fixture,'closed_switch_denied':True,'preapproval_content_denied':True,
 'unauthenticated_inbox_denied':True,'actual_password_accounts_created':2,'production_changes':False}))
'''


AUDIT = r'''
proof=json.loads((root/'manifest.json').read_text())
if proof.get('status')!='DEPLOYED':raise ValueError('isolated_site_not_deployed')
site=proof['site'];pgname=site['database_container']
if inspect(pgname)['Config']['Labels'].get('portal.source')!=SHA:raise ValueError('database_scope_mismatch')
if site['database']!='portal_notifications':raise ValueError('database_name_mismatch')
accounts=json.loads((root/'site/accounts.json').read_text())
tid=int(proof['fixture']['tenant_id'])
def literal(value):return "'"+str(value).replace("'","''")+"'"
uid=literal(accounts['applicant']['user_id'])
query="""SELECT json_build_object(
 'requests', (SELECT COALESCE(json_agg(x),'[]') FROM
   (SELECT status,count(*) FROM tenant_access_requests WHERE tenant_id=%s AND applicant_user_id=%s GROUP BY status) x),
 'membership', (SELECT json_build_object('role',role,'status',status) FROM tenant_members
   WHERE tenant_id=%s AND user_id=%s AND deleted_at IS NULL),
 'notifications', (SELECT COALESCE(json_agg(x),'[]') FROM
   (SELECT notification_type,is_read,count(*) FROM user_notifications WHERE user_id=%s GROUP BY notification_type,is_read) x),
 'duplicate_events', (SELECT count(*) FROM
   (SELECT 1 FROM user_notifications GROUP BY user_id,notification_type,reference_type,reference_id HAVING count(*)>1) x),
 'audit', (SELECT COALESCE(json_agg(x),'[]') FROM
   (SELECT action,count(*) FROM audit_logs WHERE tenant_id=%s AND target_user_id=%s GROUP BY action) x),
 'migration', (SELECT json_build_object('version',version,'dirty',dirty) FROM schema_migrations))"""%(tid,uid,tid,uid,uid,tid,uid)
statement="BEGIN READ ONLY; SET LOCAL statement_timeout='10s'; "+query+"; ROLLBACK;"
result=json.loads(run(['docker','exec','-i','-u','postgres',pgname,'psql','-X','-w','-U','postgres','-d',site['database'],'-qAt','-v','ON_ERROR_STOP=1'],data=statement.encode()))
result.update(source_sha=SHA,site=site,production_changes=False)
print(json.dumps(result))
'''


def remote(client, manifest, program, extra=''):
    prefix = 'ROOT=' + repr(manifest['remote_root']) + '\nSHA=' + repr(manifest['source_sha']) + '\n' + extra
    incoming, outgoing, errors = client.exec_command('python3 -', timeout=420)
    incoming.write(prefix + REMOTE_COMMON + program)
    incoming.channel.shutdown_write()
    raw = outgoing.read(2 * 1024 * 1024)
    status = outgoing.channel.recv_exit_status()
    if status:
        detail = errors.read(4096).decode('utf-8', 'replace')
        raise RuntimeError('isolated_operation_failed:' + detail[-1200:])
    return json.loads(raw)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('phase', choices=['stage', 'build', 'poll', 'deploy', 'seed', 'audit'])
    parser.add_argument('--manifest', type=Path, required=True)
    parser.add_argument('--source-root', type=Path)
    parser.add_argument('--sha')
    args = parser.parse_args()
    client = None
    try:
        if args.phase == 'stage':
            if args.manifest.exists() or not re.fullmatch('[a-f0-9]{40}', args.sha or ''):
                raise ValueError('new_manifest_and_exact_sha_required')
            revision = subprocess.check_output(['git', '-C', str(args.source_root), 'rev-parse', args.sha]).decode().strip()
            if revision != args.sha:
                raise ValueError('source_revision_mismatch')
            args.manifest.parent.mkdir(parents=True, exist_ok=True)
            source = args.manifest.parent / 'source.tar'
            subprocess.run(['git', '-c', 'core.autocrlf=false', '-C', str(args.source_root), 'archive', '--format=tar', '-o', str(source), args.sha], check=True)
            frontend = args.manifest.parent / 'frontend.tar.gz'
            if not (args.source_root / 'frontend/dist/index.html').is_file():
                raise ValueError('frontend_build_missing')
            with tarfile.open(frontend, 'w:gz') as output:
                for item in (args.source_root / 'frontend/dist').iterdir():
                    output.add(item, arcname=item.name)
            stamp = time.strftime('%Y%m%dT%H%M%SZ', time.gmtime())
            root = '/mnt/sata0/knowhub-feishu-v2-builds/portal-access-' + args.sha[:12] + '-' + stamp
            manifest = {'source_sha': args.sha, 'remote_root': root,
                        'digests': {p.name: hashlib.sha256(p.read_bytes()).hexdigest() for p in [source, frontend]}}
            client = connect('test')
            setup = "import pathlib; p=pathlib.Path(" + repr(root) + "); p.parent.resolve(strict=True); p.mkdir(mode=0o700)"
            incoming, outgoing, _ = client.exec_command('python3 -', timeout=30)
            incoming.write(setup); incoming.channel.shutdown_write()
            if outgoing.channel.recv_exit_status():
                raise RuntimeError('new_test_directory_unavailable')
            with client.open_sftp() as transfer:
                for path in [source, frontend]:
                    with transfer.open(root + '/' + path.name, 'wx') as output, path.open('rb') as incoming:
                        output.set_pipelined(True)
                        while part := incoming.read(1024 * 1024):
                            output.write(part)
                with transfer.open(root + '/staging.json', 'wx') as output:
                    output.write(json.dumps(manifest).encode())
            result = remote(client, manifest, STAGE)
            with args.manifest.open('x', encoding='utf-8') as output:
                json.dump(result, output, indent=2)
        else:
            manifest = json.loads(args.manifest.read_text(encoding='utf-8'))
            client = connect('test')
            programs = {'build': BUILD, 'poll': POLL, 'deploy': DEPLOY, 'seed': SEED, 'audit': AUDIT}
            result = remote(client, manifest, programs[args.phase],
                            'SCRIPT=' + repr(COMPILE.replace('__SHA__', manifest['source_sha'])) + '\n')
        print(json.dumps(result, indent=2))
        return 0
    except Exception as error:
        print(json.dumps({'status': 'BLOCKED', 'error_class': type(error).__name__,
                          'reason': str(error) if isinstance(error, (ValueError, RuntimeError)) else 'isolated_operation_failed'}))
        return 1
    finally:
        if client:
            client.close()


if __name__ == '__main__':
    sys.exit(main())
