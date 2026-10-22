"""Compatibility suites on frozen source and disposable databases only."""
from pathlib import Path
import datetime,json,subprocess,sys
base=Path(sys.argv[1]).resolve();assert base.parent==Path('/home/aqyfzx/ai/knowhub-formal-c2-20261022')
state=json.loads((base/'private/state.json').read_text());candidate=Path(state['candidate']);attempt=sys.argv[2]if len(sys.argv)>2 else'initial';assert attempt in('initial','a2','a3','a4');out=base/'results'/('compatibility'if attempt=='initial'else'compatibility-'+attempt);out.mkdir(exist_ok=False);rows=[]
dev=Path('/home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-dev-20261022/090216bc1a7d0c54')
prior=json.loads((dev/'results/results.json').read_text());go=next(x['argv']for x in prior if x['name']=='new-real-PG');go=go[:go.index('go')]
go[go.index('--name')+1]+='-formal-compat'
pg=go[go.index('--network')+1];assert pg.startswith('container:knowhub-next-dev-')
pg=pg.split(':',1)[1]
def run(name,args,timeout=1500):
 row={'name':name,'argv':args,'started_utc':datetime.datetime.now(datetime.timezone.utc).isoformat()}
 with(out/(name+'.log')).open('wb')as f:
  try:row['exit_code']=subprocess.run(args,stdout=f,stderr=subprocess.STDOUT,timeout=timeout).returncode
  except subprocess.TimeoutExpired:row['exit_code']=124
 rows.append(row);(out/'commands.json').write_text(json.dumps(rows,indent=2));(out/(name+'.exit.txt')).write_text(str(row['exit_code']));return row['exit_code']
if attempt=='initial':run('create-scale-database',['docker','exec','-u','postgres',pg,'psql','-U','postgres','-d','postgres','-v','ON_ERROR_STOP=1','-c','CREATE DATABASE knowhub_m6'])
if attempt=='a4':
 pg='kh-formal-scale-090216bc1a7d-a4';sock=pg+'-socket'
 assert run('start-fresh-scale-PG',['docker','run','-d','--network','none','--name',pg,'--label','knowhub.formal=disposable-scale','-e','POSTGRES_HOST_AUTH_METHOD=trust','-e','POSTGRES_DB=postgres','-v',pg+'-data:/var/lib/postgresql/data','-v',sock+':/var/run/postgresql','paradedb/paradedb:v0.22.2-pg17'])==0
 import time
 for _ in range(90):
  if subprocess.run(['docker','exec',pg,'pg_isready','-U','postgres'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode==0:break
  time.sleep(1)
 else:raise RuntimeError('disposable PG readiness')
 assert run('fresh-scale-database',['docker','exec','-u','postgres',pg,'psql','-v','ON_ERROR_STOP=1','-U','postgres','-d','postgres','-c','CREATE DATABASE knowhub_m6'])==0
g=list(go);g[g.index('-w'):g.index('-w')]=['-v',str(dev/'results')+':/work/results','-v','knowhub-next-development-090216bc1a7d-socket:/work/pgsocket','-e','KNOWHUB_M6_POSTGRES_DSN=host=/work/pgsocket user=postgres dbname=knowhub_m6 sslmode=disable','-e','KNOWHUB_M7_PLAN_DIR=/work/results/formal-pg-plans']
if attempt=='a4':
 g[g.index('--network')+1]='none'
 for i,v in enumerate(g):
  if v=='knowhub-next-development-090216bc1a7d-socket:/work/pgsocket':g[i]=sock+':/work/pgsocket'
pattern='^TestPortalM7RealPostgresScale$'if attempt in('a3','a4')else'TestPortalM6RealPostgresPersonal|TestPortalM7RealPostgresScale|TestPortalM7PublishedDynamicRealPostgres|TestPortalV1LinksAndIndexDimensionsPostgres'
run('PG-scale-dimensions-model-failure',g+['go','test','-tags','sqlite_fts5,knowhub_m6_postgres','-p','2','-count=1','-v','./internal/router','-timeout','18m','-run',pattern])
if attempt not in('a3','a4'):run('PG-feishu-ingress',g+['go','test','-tags','sqlite_fts5,knowhub_m6_postgres','-p','2','-count=1','-v','./internal/application/service','-timeout','10m','-run','^TestPortalM7PublishedFeishuIngressPostgres$'])
if attempt!='initial':
 result={'exit_code':0 if all(x['exit_code']==0 for x in rows)else 1,'candidate_sha256':json.loads((candidate/'manifest.json').read_text())['candidate_sha256'],'unchanged_tests':True,'fix':'Use exact pre-existing disposable Unix socket contract; no assertion modified','commands':[{k:x[k]for k in('name','exit_code')}for x in rows]};(out/'result.json').write_text(json.dumps(result,indent=2));print(json.dumps(result));sys.exit(result['exit_code'])
pyimage='weknora-custom/mcp-candidate:v0.8.0-knowhub.24-rc1-854f3b95'
run('python-full',['docker','run','--rm','--network','none','-v',str(candidate/'source')+':/source:ro','-w','/source','-e','WEKNORA_API_KEY=','--entrypoint','python',pyimage,'-m','unittest','discover','-s','mcp-server','-p','test_*.py','-v'],600)
code=run('python-sdk-build',go+['go','test','-tags','sqlite_fts5,knowhub_m5_integration','-c','-o','/results/formal-python-sdk.test','./internal/router'])
if code==0:
 run('python-sdk-real-router',['docker','run','--rm','--network','none','-v','/home/aqyfzx/go/pkg/mod/github.com/yanyiwu/gojieba@v1.4.7/deps/cppjieba/dict:/testdict:ro','-e','JIEBA_DICT_DIR=/testdict','-v',str(dev/'results')+':/artifacts:ro','-v',str(candidate/'source')+':/source:ro','-w','/source/internal/router','-e','WEKNORA_API_KEY=','--entrypoint','/artifacts/formal-python-sdk.test',pyimage,'-test.v','-test.count=1','-test.run','TestDocumentM5PythonActualREST'],600)
r={'exit_code':0 if all(x['exit_code']==0 for x in rows)else 1,'candidate_sha256':json.loads((candidate/'manifest.json').read_text())['candidate_sha256'],'runtime_source_sha256':'090216bc1a7d0c547235df30eef6f425050b9260fadba9791329b6c267a24783','synthetic_only':True,'real_model':'BLOCKED_CREDENTIAL_APPROVAL','real_SID':'NOT_REPEATED_APPROVED_RC3_EVIDENCE','commands':[{k:x[k]for k in('name','exit_code')}for x in rows]}
(out/'result.json').write_text(json.dumps(r,indent=2));print(json.dumps(r));sys.exit(r['exit_code'])
