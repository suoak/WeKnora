"""Concentrated acceptance against the single deployed synthetic V1 candidate."""
from pathlib import Path
import datetime,json,os,subprocess,sys,time,urllib.request,urllib.error

base=Path(sys.argv[1]).resolve()
assert base.parent==Path('/home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022')
m=json.loads((base/'manifest.json').read_text());sha=m['candidate_sha256'];assert base.name==sha[:16]
instance=json.loads((base/'results/deploy/instance.json').read_text());project=instance['project'];origin=instance['url']
out=base/'results/acceptance';out.mkdir(exist_ok=False);rows=[];actions=[]
compose=['docker','compose','--project-name',project,'--project-directory',str(base),'-f',str(base/'compose.yml')]
def run(name,args,timeout=1800):
    row={'name':name,'argv':args,'started_utc':datetime.datetime.now(datetime.timezone.utc).isoformat()}
    with (out/(name+'.log')).open('wb')as f:
        try:row['exit_code']=subprocess.run(args,stdout=f,stderr=subprocess.STDOUT,timeout=timeout).returncode
        except subprocess.TimeoutExpired:row['exit_code']=124
    rows.append(row);(out/'results.json').write_text(json.dumps(rows,indent=2));(out/(name+'.exit.txt')).write_text(str(row['exit_code']))
    return row['exit_code']
def request(method,path,body=None,token=None,tenant=None):
    headers={'Content-Type':'application/json','Accept':'application/json, text/event-stream'}
    if token:headers['Authorization']='Bearer '+token
    if tenant:headers['X-Tenant-ID']=str(tenant)
    req=urllib.request.Request(origin+path,data=json.dumps(body).encode()if body is not None else None,headers=headers,method=method)
    try:
        with urllib.request.urlopen(req,timeout=45)as r:status=r.status;text=r.read().decode()
    except urllib.error.HTTPError as e:status=e.code;text=e.read().decode()
    if text.startswith(('event:','data:')):text=next(line[5:]for line in text.splitlines()if line.startswith('data:'))
    return status,json.loads(text)if text else None
env=dict(line.split('=',1)for line in (base/'private/app.env').read_text().splitlines())
def login(user):
    status,data=request('POST','/api/v1/auth/login',{'email':user+'@example.invalid','password':env['KNOWHUB_V1_TEST_PASSWORD']})
    assert status==200 and data.get('token'), 'synthetic real login failed: '+str(status)
    return data

try:
    loginA=login('A');admin=login('reviewer');owner=login('owner');jwt=loginA['token']
    status,old=request('GET','/api/v1/mcp-api-keys',token=jwt);assert status==200
    (out/'legacy-before.json').write_text(json.dumps(old,indent=2))
    status,options=request('GET','/api/v1/mcp-api-keys/scope-options',token=jwt);assert status==200
    scopes=[{'tenant_id':o['tenant_id'],'kb_scope_mode':'all','knowledge_bases':[]}for o in options['data']]
    assert {s['tenant_id']for s in scopes}>={1,7}
    status,created=request('POST','/api/v1/mcp-api-keys',{'personal_scope_mode':'dynamic_all','name':'V1 concentrated acceptance','client_type':'generic','capabilities':['retrieve','enterprise_retrieve'],'tenant_scopes':scopes},jwt)
    assert status==201 and created['data'].get('expires_at')is None
    key=created['data'];token=key['token'];rpc=0
    def call(name,args=None,keytoken=None):
        global rpc
        started=time.monotonic()
        rpc+=1;status,data=request('POST','/api/v1/mcp/personal',{'jsonrpc':'2.0','id':rpc,'method':'tools/call','params':{'name':name,'arguments':args or {}}},keytoken or token)
        actions.append({'rpc_id':rpc,'tool':name,'http_status':status,'elapsed_ms':1000*(time.monotonic()-started),'is_error':bool(data and (data.get('error')or data.get('result',{}).get('isError')))})
        return status,data
    def value(response):
        status,data=response;assert status==200 and not data.get('error')and not data['result'].get('isError'), 'MCP request denied'
        return json.loads(data['result']['content'][0]['text'])
    status,tools=request('POST','/api/v1/mcp/personal',{'jsonrpc':'2.0','id':1000,'method':'tools/list'},token=token)
    assert status==200 and {t['name']for t in tools['result']['tools']}=={'list_knowledge_bases','search_knowledge','grep_chunks','list_documents','read_document','wiki_search','wiki_read_page','wiki_index'}
    initial=value(call('list_knowledge_bases'));ids={b['id']for b in initial};assert ids=={'public-kb','public-two','private-home','private-other'}
    for mode in ['keyword','semantic','hybrid']:
        hits=value(call('search_knowledge',{'query':'PUBLIC','mode':mode,'limit':30}));assert hits['status']=='complete'and len({h['knowledge_base_id']for h in hits['results']})==4
        assert 'V1_URL_SECRET'not in json.dumps(hits)
    value(call('grep_chunks',{'query':'PUBLIC','limit':30}));value(call('list_documents',{'knowledge_base_id':'public-kb'}))
    for _ in range(5):
        measured=value(call('search_knowledge',{'query':'PUBLIC','mode':'hybrid','limit':30}))
        assert measured['status']=='complete'and len({h['knowledge_base_id']for h in measured['results']})==4
    text=value(call('read_document',{'knowledge_id':'public-doc'}));assert 'ordinary Office policy manual'in json.dumps(text)and 'V1_URL_SECRET'not in json.dumps(text)
    for tool,args in [('read_document',{'knowledge_id':'sensitive'}),('read_document',{'knowledge_id':'nonexistent'}),('add_document',{}),('delete_document',{}),('ask',{})]:
        status,data=call(tool,args);assert data.get('error')or data.get('result',{}).get('isError')

    # Eight-tool Wiki reads use known synthetic generation proofs. No actual
    # cloud LLM generation is claimed by this test fixture.
    wiki=value(call('wiki_search',{'query':'verified','knowledge_base_ids':['public-two','private-home']}))
    assert {p['knowledge_base_id']for p in wiki['pages']}=={'public-two','private-home'}
    assert 'UNTRUSTED_WIKI_CANARY'not in json.dumps(wiki)
    ambiguous=value(call('wiki_read_page',{'slug':'concept/verified'}))
    assert ambiguous['status']=='ambiguous'and all(not p.get('content')for p in ambiguous['pages'])
    page=value(call('wiki_read_page',{'slug':'concept/verified','knowledge_base_id':'public-two'}))
    assert len(page['pages'])==1 and 'ordinary content'in page['pages'][0]['content']
    def sql(q):
        return subprocess.check_output(compose+['exec','-T','postgres','psql','-v','ON_ERROR_STOP=1','-U','postgres','-d','knowhub_v1','-Atc',q]).decode().strip()
    before=sql("SELECT count(*) FROM wiki_pages WHERE page_type='index'")
    directory=value(call('wiki_index',{'knowledge_base_id':'public-two','limit':10}))
    assert before=='0'and sql("SELECT count(*) FROM wiki_pages WHERE page_type='index'")=='0'
    assert 'UNTRUSTED_WIKI_CANARY'not in json.dumps(directory)
    for args in [{'slug':'concept/unknown','knowledge_base_id':'public-two'},{'slug':'concept/verified','knowledge_base_id':'not-authorized'}]:
        _,denied=call('wiki_read_page',args);assert denied.get('error')or denied['result'].get('isError')
    # Historical fixed mode is explicit and remains restricted after future
    # private membership is added. B is another real synthetic local user.
    status,fixed=request('POST','/api/v1/mcp-api-keys',{'name':'fixed scope control','client_type':'generic','capabilities':['retrieve','enterprise_retrieve'],'tenant_scopes':scopes},jwt)
    assert status==201 and fixed['data']['personal_scope_mode']=='fixed'
    bLogin=login('B')
    status,bOptions=request('GET','/api/v1/mcp-api-keys/scope-options',token=bLogin['token']);assert status==200
    bScopes=[{'tenant_id':o['tenant_id'],'kb_scope_mode':'all','knowledge_bases':[]}for o in bOptions['data']]
    status,bKey=request('POST','/api/v1/mcp-api-keys',{'name':'dynamic B control','personal_scope_mode':'dynamic_all','client_type':'generic','capabilities':['retrieve','enterprise_retrieve'],'tenant_scopes':bScopes},bLogin['token']);assert status==201
    assert run('future-private-fixture',['docker','run','--rm','--network',project+'_isolated','--env-file',str(base/'private/app.env'),'-e','KNOWHUB_V1_SEED=SYNTHETIC_V1_ONLY','-v',str(base/'artifacts')+':/artifacts','--entrypoint','/artifacts/seed.test',json.loads((base/'results/build/done.json').read_text())['app'],'-test.v','-test.run','^TestV1NextDynamicPrivateFixtureDeployed$'])==0
    for keytoken in [token,bKey['data']['token'],fixed['data']['token']]:
        _,denied=call('read_document',{'knowledge_id':'future-private-doc'},keytoken);assert denied.get('error')or denied['result'].get('isError')
    sql("INSERT INTO tenant_members(user_id,tenant_id,role,status)VALUES('A',1207,'viewer','active')")
    assert 'future-private'in {k['id']for k in value(call('list_knowledge_bases'))}
    for mode in ['keyword','semantic','hybrid']:
        new=value(call('search_knowledge',{'query':'FUTURE_PRIVATE_CANARY','mode':mode,'limit':30}))
        assert 'future-private-doc'in {h['knowledge_id']for h in new['results']}
    for keytoken in [bKey['data']['token'],fixed['data']['token']]:
        _,denied=call('read_document',{'knowledge_id':'future-private-doc'},keytoken);assert denied.get('error')or denied['result'].get('isError')
    sql("UPDATE tenant_members SET status='inactive' WHERE user_id='A'AND tenant_id=1207")
    _,denied=call('read_document',{'knowledge_id':'future-private-doc'});assert denied.get('error')or denied['result'].get('isError')
    assert 'future-private'not in {k['id']for k in value(call('list_knowledge_bases'))}

    # Real Portal route, not an authorization override.
    status,_=request('POST','/api/v1/system/admin/portal/spaces/99/unpublish',token=admin['token']);assert status==200
    after=value(call('list_knowledge_bases'));assert 'public-kb'not in {b['id']for b in after}and {'private-home','private-other'}<={b['id']for b in after}
    status,data=call('read_document',{'knowledge_id':'public-doc'});assert data.get('error')or data['result'].get('isError')
    status,_=request('POST','/api/v1/system/admin/portal/spaces/99/publish',token=admin['token']);assert status==200
    status,rotated=request('POST','/api/v1/mcp-api-keys/'+str(key['id'])+'/rotate',{},jwt);assert status==200
    assert call('list_knowledge_bases',keytoken=token)[0]==401
    token=rotated['data']['token'];assert value(call('list_knowledge_bases'))
    status,_=request('DELETE','/api/v1/mcp-api-keys/'+str(key['id']),token=jwt);assert status==200
    assert call('list_knowledge_bases')[0]==401
    status,afterKeys=request('GET','/api/v1/mcp-api-keys',token=jwt);assert status==200
    beforeBy={k['id']:k for k in old['data']};afterBy={k['id']:k for k in afterKeys['data']}
    for kid,k in beforeBy.items():
        for field in ['capabilities','tenant_scopes','expires_at','revoked_at']:assert k.get(field)==afterBy[kid].get(field)
    # Private connection material for a later real-browser/CLI run, never evidence.
    private={'candidate_sha256':sha,'url':origin,'login':loginA,'owner_jwt':owner['token'],'admin_jwt':admin['token']}
    (base/'private/client.json').write_text(json.dumps(private));os.chmod(base/'private/client.json',0o600)
    (out/'http-business.json').write_text(json.dumps({'exit_code':0,'candidate_sha256':sha,'initial_kbs':sorted(ids),'actions':actions,'old_keys_unchanged':True,'SID':'BLOCKED_NOT_CONFIGURED','formal_embedding':'BLOCKED_NOT_CONFIGURED','model':'SYNTHETIC_1024'},indent=2))
except Exception as error:
    (out/'http-business.json').write_text(json.dumps({'exit_code':1,'error_class':type(error).__name__,'message':str(error),'actions':actions},indent=2))

# Same independent database server; none of these DBs are production copies.
for name in ['knowhub_m6','knowhub_m2_test','knowhub_m3_chain','knowhub_m3_fresh','knowhub_m3_empty','knowhub_m3_failure','feishu_atomic_test']:
    run('create-'+name,compose+['exec','-T','postgres','psql','-U','postgres','-d','postgres','-c','CREATE DATABASE '+name])
volume='knowhub-v1-wiki-dynamic-dev-'+sha[:16];socketvolume=project+'_pgsocket';legacysocket='/home/aqyfzx/ai/knowhub-mcp-m4-20261021-a1b2c3/pgsocket-1'
go=['docker','run','--rm','--label','knowhub.v1=acceptance','--network','none',
    '-v',str(base/'source')+':/source:ro','-v',str(base/'artifacts')+':/artifacts','-v',str(base/'artifacts')+':/work/results',
    '-v',volume+':/v1','-v','knowhub-mcp-b7-20261021-102200-data:/prior',
    '-v',socketvolume+':/work/pgsocket','-v',socketvolume+':'+legacysocket,
    '-v','/home/aqyfzx/go/pkg/mod:/home/aqyfzx/go/pkg/mod:ro',
    '-e','GOPATH=/home/aqyfzx/go','-e','GOCACHE=/prior/go-cache','-e','GOTMPDIR=/v1/tmp',
    '-e','GOTOOLCHAIN=local','-e','CGO_CFLAGS=-O2 -g -I/v1/sqlite-headers',
    '-e','KNOWHUB_M6_POSTGRES_DSN=host=/work/pgsocket user=postgres dbname=knowhub_m6 sslmode=disable',
    '-e','KNOWHUB_M2_TEST_POSTGRES_DSN=host='+legacysocket+' user=postgres dbname=knowhub_m2_test sslmode=disable',
    '-e','KNOWHUB_M7_PLAN_DIR=/work/results/pg-plans',
    '-w','/source','knowhub-build/golang:1.26-feishu-py311-ensurepip-20261022']
run('postgres-security-scale',go+['go','test','-tags','sqlite_fts5,knowhub_m6_postgres','-p','2','-count=1','-v','./internal/router','-timeout','18m','-run','TestPortalM6RealPostgresPersonal|TestPortalM7RealPostgresScale|TestPortalM7PublishedDynamicRealPostgres|TestPortalV1LinksAndIndexDimensionsPostgres'],1200)
run('postgres-feishu-ingress',go+['go','test','-tags','sqlite_fts5,knowhub_m6_postgres','-p','2','-count=1','-v','./internal/application/service','-timeout','10m','-run','^TestPortalM7PublishedFeishuIngressPostgres$'],720)
# Canonical index publication, stale-worker fencing and parser fault injection
# run against fresh per-test DBs on this project's PG, not the APP database.
atomic_go=list(go)
idx=atomic_go.index('-w')
atomic_go[idx:idx]=['-e','FEISHU_LEGACY_TEST_DSN=postgresql://postgres@/feishu_atomic_test?host=%2Fwork%2Fpgsocket&sslmode=disable']
for race_suffix,race_args in [('',[]),('-race',['-race'])]:
    for package,pattern,label in [('internal/application/repository','^TestFeishuLegacyAtomicPostgres$','atomic'),('internal/application/service','^TestFeishuLegacyRealParserEmbeddingPostgres$','parser')]:
        run('feishu-canonical-'+label+race_suffix,atomic_go+['go','test']+race_args+['-tags','sqlite_fts5','-p','2','-count=1','-v','./'+package,'-timeout','15m','-run',pattern],1000)
run('formal-pg-migration-race',go+['go','test','-race','-tags','sqlite_fts5,knowhub_m3_postgres','-p','2','-count=1','-v','./internal/application/repository','-run','^TestEnterpriseM2Postgres|^TestDocumentM3Postgres.*MigrationRunner$'],1200)
run('python-full',['docker','run','--rm','--network','none','-v',str(base/'source')+':/source:ro','-w','/source','-e','WEKNORA_API_KEY=','--entrypoint','python','weknora-custom/mcp-candidate:v0.8.0-knowhub.24-rc1-854f3b95','-m','unittest','discover','-s','mcp-server','-p','test_*.py','-v'],600)
pyimage='weknora-custom/mcp-candidate:v0.8.0-knowhub.24-rc1-854f3b95'
run('python-sdk-build',go+['go','test','-tags','sqlite_fts5,knowhub_m5_integration','-c','-o','/artifacts/python-sdk.test','./internal/router'])
dictionary=['-v','/home/aqyfzx/go/pkg/mod/github.com/yanyiwu/gojieba@v1.4.7/deps/cppjieba/dict:/testdict:ro','-e','JIEBA_DICT_DIR=/testdict']
run('python-sdk',['docker','run','--rm','--network','none']+dictionary+['-v',str(base/'source')+':/source:ro','-v',str(base/'artifacts')+':/artifacts','-w','/source/internal/router','-e','WEKNORA_API_KEY=','--entrypoint','/artifacts/python-sdk.test',pyimage,'-test.v','-test.count=1','-test.run','TestDocumentM5PythonActualREST'],600)
regression_redis=project+'-regression-redis'
run('regression-redis',['docker','run','-d','--name',regression_redis,'--restart','always','--label','knowhub.v1=acceptance','--network',project+'_isolated','--network-alias','knowhub-p1-dispatch-redis-20261018','redis:7.0-alpine','redis-server','--save','','--appendonly','no'])
for package,name in [('internal/application/service','service'),('internal/agent/tools','agent')]:
    code=run(name+'-build',go+['go','test','-tags','sqlite_fts5','-c','-o','/artifacts/'+name+'.test','./'+package])
    if code==0:
        run(name+'-full',['docker','run','--rm','--network',project+'_isolated']+dictionary+[
            '-v',str(base/'source')+':/source:ro','-v',str(base/'artifacts')+':/artifacts',
            '-v',socketvolume+':/work/pgsocket','--add-host','example.com:8.8.8.8','--add-host','api.e2b.app:8.8.8.8',
            '-w','/source/'+package,'-e','WEKNORA_API_KEY=',
            '-e','SYNC_EXECUTION_POSTGRES_DSN=postgresql://postgres@/knowhub_m6?host=%2Fwork%2Fpgsocket&sslmode=disable',
            '-e','REQUIRE_SYNC_EXECUTION_POSTGRES=1','-e','FEISHU_DISPATCH_REDIS_ADDR=knowhub-p1-dispatch-redis-20261018:6379',
            '--entrypoint','/artifacts/'+name+'.test',pyimage,'-test.v','-test.count=1'],1800)
run('regression-redis-stop',['docker','stop','--time','3',regression_redis])
# Database dump/restore stays protected on the build host; publish only status.
backup=base/'private/synthetic-db.dump'
with backup.open('wb')as f:code=subprocess.run(compose+['exec','-T','postgres','pg_dump','-U','postgres','-Fc','knowhub_v1'],stdout=f,stderr=subprocess.PIPE).returncode
os.chmod(backup,0o600)
if code==0:
    run('create-restore-db',compose+['exec','-T','postgres','psql','-U','postgres','-d','postgres','-c','CREATE DATABASE knowhub_v1_restored TEMPLATE template0'])
    with backup.open('rb')as f:
        code=subprocess.run(compose+['exec','-T','postgres','pg_restore','-U','postgres','-d','knowhub_v1_restored'],stdin=f,stdout=subprocess.DEVNULL,stderr=subprocess.PIPE).returncode
    run('restore-check',compose+['exec','-T','postgres','psql','-U','postgres','-d','knowhub_v1_restored','-Atc','SELECT version,dirty FROM schema_migrations; SELECT count(*) FROM knowledges; SELECT count(*) FROM tenant_api_keys;'])
(out/'backup-restore.json').write_text(json.dumps({'synthetic_only':True,'restore_exit_code':code,'file_retained_privately':True}))
(out/'done.json').write_text(json.dumps({'candidate_sha256':sha,'commands_passed':all(r['exit_code']==0 for r in rows),'http_business':json.loads((out/'http-business.json').read_text())['exit_code'],'real_sid':'BLOCKED','formal_embedding':'BLOCKED'}))
