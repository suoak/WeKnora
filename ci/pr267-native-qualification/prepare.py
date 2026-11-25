"""Create ONLY a new owned, isolated APP qualification stack on the test host."""
import datetime,hashlib,ipaddress,json,os,pathlib,secrets,socket,subprocess,sys
from cryptography.hazmat.primitives.asymmetric import ed25519
from cryptography.hazmat.primitives import serialization
ROOT=pathlib.Path(os.environ['QUALIFICATION_ROOT'])
PROJECT='knowhub-pr267-'+os.environ['GITHUB_RUN_ID']
SHA=os.environ['SOURCE_SHA'];assert len(SHA)==40 and all(c in '0123456789abcdef' for c in SHA)
assert os.environ.get('GITHUB_ACTIONS')=='true' and os.environ.get('SOURCE_SHA')==SHA
assert ROOT.parent.is_dir() and not ROOT.exists(),'Do not overwrite an existing site'
image=sys.argv[1];assert image.startswith('sha256:') and len(image)==71
def run(*args):return subprocess.check_output(args,text=True,timeout=45).strip()
meta=json.loads(run('docker','image','inspect',image))[0]
assert meta['Config']['Labels']['org.opencontainers.image.revision']==SHA
assert ROOT.resolve().parent==pathlib.Path(os.environ['RUNNER_TEMP']).resolve()
v=os.statvfs(ROOT.parent);assert v.f_bavail*v.f_frsize>8*1024**3
mem={k:int(v.split()[0]) for k,v in (line.split(':',1) for line in pathlib.Path('/proc/meminfo').read_text().splitlines())}
assert mem['MemAvailable']>5*1024**2,'Insufficient memory headroom: stop rather than weaken dependencies'
networks=json.loads(run('docker','network','inspect',*run('docker','network','ls','-q').split()))
used=[ipaddress.ip_network(c['Subnet'],strict=False) for n in networks for c in (n.get('IPAM',{}).get('Config') or []) if c.get('Subnet')]
routes=json.loads(run('ip','-j','route','show','table','all'))
used += [ipaddress.ip_network(r['dst'],strict=False) for r in routes if r.get('dst') not in (None,'default')]
subnet=next((str(n) for n in ipaddress.ip_network('10.244.240.0/20').subnets(new_prefix=28) if not any(n.version==u.version and n.overlaps(u) for u in used)),None)
assert subnet,'No safe isolated subnet available'
with socket.socket() as s:s.bind(('127.0.0.1',39881))
ROOT.mkdir(mode=0o700)
def write(name,body,mode=0o600):
 p=ROOT/name;fd=os.open(p,os.O_CREAT|os.O_EXCL|os.O_WRONLY,mode)
 with os.fdopen(fd,'w') as f:f.write(body if isinstance(body,str) else json.dumps(body,indent=2))
for name in ['files','staging','docreader-tmp']:(ROOT/name).mkdir(mode=0o700)
dbpass=secrets.token_urlsafe(32);redispass=secrets.token_urlsafe(32)
write('postgres.env',f'POSTGRES_USER=postgres\nPOSTGRES_DB=consumer_native_test\nPOSTGRES_PASSWORD={dbpass}\n')
write('redis.conf',f'bind 0.0.0.0\nprotected-mode yes\nappendonly yes\nrequirepass {redispass}\n',0o644)
key=ed25519.Ed25519PrivateKey.generate()
write('approval.pub',key.public_key().public_bytes(serialization.Encoding.PEM,serialization.PublicFormat.SubjectPublicKeyInfo).decode(),0o644)
del key
env={'DB_DRIVER':'postgres','DB_HOST':'postgres','DB_PORT':'5432','DB_USER':'postgres','DB_NAME':'consumer_native_test','DB_PASSWORD':dbpass,
 'REDIS_ADDR':'redis:6379','REDIS_PASSWORD':redispass,'REDIS_DB':'0','REDIS_PREFIX':'consumer-native-pr267-'+os.environ['GITHUB_RUN_ID'],'WEKNORA_REDIS_NAMESPACE':'consumer-native-pr267-'+os.environ['GITHUB_RUN_ID'],
 'STORAGE_TYPE':'local','LOCAL_STORAGE_BASE_DIR':'/data/files','AUTO_MIGRATE':'true','GIN_MODE':'release','TZ':'Asia/Shanghai','RETRIEVE_DRIVER':'postgres',
 'SSRF_WHITELIST':'model-fixture',
 'JWT_SECRET':secrets.token_urlsafe(48),'SYSTEM_AES_KEY':secrets.token_hex(16),'SYSTEM_SIGNING_KEY':secrets.token_urlsafe(48),
 'DOCREADER_ADDR':'docreader:50051','DISABLE_REGISTRATION':'false','WEKNORA_AUTH_COMPLEX_PASSWORD_ENABLED':'true',
 'LANGFUSE_ENABLED':'false','LLM_DEBUG_LOG':'false','FEISHU_PERSONAL_STAGING_DIR':'/data/feishu-staging',
 'FEISHU_SYNC_SAFETY_ENABLED':'true','FEISHU_SYNC_SAFETY_MODE':'automatic','FEISHU_LEGACY_SYNC_ALLOWED_SOURCE_IDS':'all',
 'FEISHU_ACCESS_V2_ENABLED':'true','FEISHU_PERSONAL_MVP_FIRST_CREATE':'true','FEISHU_SAFE_INCREMENTAL_ENABLED':'true',
 'FEISHU_APPROVED_SYNC_LANE_ENABLED':'1','FEISHU_SYNC_CONTROL_DIAGNOSTICS':'1','FEISHU_SYNC_RECOVERY_APPROVAL_PUBLIC_KEY_FILE':'/app/approval.pub',
 'FEISHU_SYNC_SAFETY_POLICY_JSON':json.dumps({'Namespace':'native-pr267-'+os.environ['GITHUB_RUN_ID'],'EndpointHost':'model-fixture','InFlight':1,'RPM':30,'RPS':1,'TPM':30000,'WaitBudgetDuration':65000000000,'TokenBoundVerified':True,'TokenOverhead':128},separators=(',',':'))}
for k in ['CORE','ENRICHMENT','SHARED','POSTPROCESS']:env['WEKNORA_ASYNQ_'+k+'_CONCURRENCY']='1'
env['WEKNORA_MODEL_MAX_CONCURRENCY']='2';env['WEKNORA_ASYNQ_MAINTENANCE_CONCURRENCY']='4';env['WEKNORA_WIKI_ASYNQ_CONCURRENCY']='1'
write('app.env',''.join(k+'='+v+'\n' for k,v in env.items()))
# Read current candidate's built-in config; never copy an existing site's secrets.
cid=run('docker','create','--network','none',image)
try:subprocess.run(['docker','cp',cid+':/app/config/config.yaml',str(ROOT/'config.yaml')],check=True,timeout=30)
finally:subprocess.run(['docker','rm',cid],check=True,stdout=subprocess.DEVNULL,timeout=15)
config=(ROOT/'config.yaml').read_text();assert 'enabled: false' in config
config=config.replace('personal_mcp:\n  enabled: false','personal_mcp:\n  enabled: true',1)
import re
config=re.sub(r'(policy_reference:)\s*"[^"]*"',r'\1 "consumer-native-pr267-isolated"',config,count=1)
config=re.sub(r'(upload_scanner_address:)\s*"[^"]*"',r'\1 "scanner:3310"',config,count=1)
(ROOT/'config.yaml').write_text(config);os.chmod(ROOT/'config.yaml',0o644)
base={'restart':'no','logging':{'driver':'json-file','options':{'max-size':'10m','max-file':'2'}},'pids_limit':512,'networks':['private'],'pull_policy':'never','labels':{'knowhub.source-sha':SHA,'knowhub.environment':'isolated-native-qualification'}}
def service(**kw):return {**base,**kw}
services={
 'postgres':service(image=run('docker','image','inspect','--format','{{.Id}}','paradedb/paradedb@sha256:af585013f97f622715de01e48d00558f7edf17055d7b40deafc9f98ca8d99a56'),env_file=['postgres.env'],volumes=['postgres:/var/lib/postgresql/data'],mem_limit='768m',cpus=1,healthcheck={'test':['CMD','pg_isready','-h','127.0.0.1','-U','postgres','-d','consumer_native_test'],'interval':'5s','timeout':'5s','retries':30}),
 'redis':service(ports=['127.0.0.1:16381:6379'],image=run('docker','image','inspect','--format','{{.Id}}','redis:7.0.15'),command=['redis-server','/usr/local/etc/redis/redis.conf'],volumes=['redis:/data','./redis.conf:/usr/local/etc/redis/redis.conf:ro'],mem_limit='256m',cpus=1,healthcheck={'test':['CMD','sh','-c','export REDISCLI_AUTH="$(sed -n "s/^requirepass //p" /usr/local/etc/redis/redis.conf)"; redis-cli ping'],'interval':'3s','timeout':'3s','retries':30}),
 'scanner':service(image=run('docker','image','inspect','--format','{{.Id}}','clamav/clamav:stable'),environment={'CLAMAV_NO_FRESHCLAMD':'true'},mem_limit='2g',cpus=2),
 'docreader':service(healthcheck={'test':['CMD','/bin/grpc_health_probe','-addr=127.0.0.1:50051'],'interval':'5s','timeout':'5s','retries':40,'start_period':'30s'},image=run('docker','image','inspect','--format','{{.Id}}','knowhub-pr267-docreader:'+os.environ['DOCREADER_SHA']),mem_limit='768m',cpus=1,volumes=['./docreader-tmp:/tmp/docreader'],environment={'DOCREADER_GRPC_MAX_WORKERS':'2','DOCREADER_MARKITDOWN_MAX_WORKERS':'1','DOCREADER_IMAGE_OUTPUT_DIR':'/tmp/docreader'}),
 'model-fixture':service(image=image,user='1000:1000',entrypoint=['python3','/fixture.py'],volumes=['./model-fixture.py:/fixture.py:ro'],read_only=True,cap_drop=['ALL'],mem_limit='128m',cpus=1),
 'app':service(image=image,env_file=['app.env'],mem_limit='1536m',cpus=2,volumes=['./files:/data/files','./staging:/data/feishu-staging','./docreader-tmp:/tmp/docreader:ro','./config.yaml:/app/config/config.yaml:ro','./approval.pub:/app/approval.pub:ro'],ports=['127.0.0.1:39881:8080'],depends_on={'postgres':{'condition':'service_healthy'},'redis':{'condition':'service_healthy'},'scanner':{'condition':'service_healthy'},'docreader':{'condition':'service_healthy'}},healthcheck={'test':['CMD','curl','-fsS','http://127.0.0.1:8080/health'],'interval':'10s','timeout':'5s','retries':18,'start_period':'90s'})}
compose={'name':PROJECT,'services':services,'networks':{'private':{'name':PROJECT+'-private','internal':True,'ipam':{'config':[{'subnet':subnet}]}}},'volumes':{n:{'name':PROJECT+'-'+n} for n in ['postgres','redis']}}
write('compose.json',compose)
# Data paths exist only in this newly created ephemeral runner workspace.
subprocess.run(['sudo','chown','1000:1000',str(ROOT/'files'),str(ROOT/'staging'),str(ROOT/'docreader-tmp')],check=True)

write('deployment-proof.json',{'project':PROJECT,'source_sha':SHA,'image_id':image,'subnet':subnet,'created_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'config_sha256':hashlib.sha256(config.encode()).hexdigest(),'production_resources':False,'approval_private_key_discarded':True})
print(json.dumps({'prepared':str(ROOT),'project':PROJECT,'subnet':subnet,'image_id':image,'production_connections':False}))
