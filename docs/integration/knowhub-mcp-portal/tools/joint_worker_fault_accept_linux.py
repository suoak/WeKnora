"""Bounded real APP/Worker crash and Redis fault probe on the new instance.

Uses only synthetic sources. Lease expiry is explicitly injected by aging one
test log; this is not evidence that 30 minutes elapsed in wall-clock time.
"""
from pathlib import Path
import json, subprocess, sys, time, traceback, urllib.error, urllib.request

base = Path(sys.argv[1]).resolve()
assert base.parent == Path('/home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-feishu-20261022')
instance = json.loads((base/'results/deploy/instance.json').read_text())
assert instance['project'].startswith('knowhub-v1-feishu-')
source_run=json.loads((base/'feishu-source-current.json').read_text())
assert source_run['candidate_sha256']==instance['candidate_sha256']
assert json.loads((base/'results'/source_run['results_dir']/'result.json').read_text())['exit_code'] == 0
out = base/'results/worker-fault'
out.mkdir(exist_ok=False)
compose = ['docker','compose','--project-name',instance['project'],'--project-directory',str(base),'-f',str(base/'compose.yml')]
env = dict(line.split('=',1) for line in (base/'private/app.env').read_text().splitlines())
rows = []

def command(name,args,timeout=240):
    with (out/(name+'.log')).open('wb') as f:
        code = subprocess.run(args,stdout=f,stderr=subprocess.STDOUT,timeout=timeout).returncode
    rows.append({'name':name,'argv':args,'exit_code':code})
    (out/'commands.json').write_text(json.dumps(rows,indent=2))
    assert code == 0,name

def sql(query):
    return subprocess.check_output(compose+['exec','-T','postgres','psql','-v','ON_ERROR_STOP=1','-U','postgres','-d','knowhub_v1','-Atc',query]).decode().strip()

def http(method,path,body=None,token=None,tenant=None):
    headers={'Content-Type':'application/json','Accept':'application/json, text/event-stream'}
    if token: headers['Authorization']='Bearer '+token
    if tenant: headers['X-Tenant-ID']=str(tenant)
    req=urllib.request.Request(instance['url']+path,data=json.dumps(body).encode() if body is not None else None,method=method,headers=headers)
    try:
        with urllib.request.urlopen(req,timeout=30) as response: status,raw=response.status,response.read().decode()
    except urllib.error.HTTPError as error: status,raw=error.code,error.read().decode()
    if raw.startswith(('data:','event:')): raw=next(line[5:] for line in raw.splitlines() if line.startswith('data:'))
    return status,json.loads(raw) if raw else None

def fixture(changes=None):
    if changes is None:
        code="import urllib.request;print(urllib.request.urlopen('http://127.0.0.1:8000/state').read().decode())"
    else:
        code="import urllib.request;urllib.request.urlopen(urllib.request.Request('http://127.0.0.1:8000/control/wiki',data="+repr(json.dumps(changes).encode())+",headers={'Content-Type':'application/json'},method='POST')).read()"
    raw=subprocess.check_output(compose+['exec','-T','feishu','python','-c',code])
    return json.loads(raw)['wiki'] if changes is None else None

def wait_terminal(log):
    deadline=time.monotonic()+180
    while time.monotonic()<deadline:
        status=sql("SELECT status FROM sync_logs WHERE id='"+log+"'")
        if status!='running': return status
        time.sleep(1)
    raise AssertionError('task did not terminate within 180 seconds')

app_stopped=False
redis_stopped=False
try:
    status,data=http('POST','/api/v1/auth/login',{'email':'owner@example.invalid','password':env['KNOWHUB_V1_TEST_PASSWORD']})
    assert status==200
    owner=data['token']
    ds=source_run['sources']['wiki']['source_id']
    kid=sql("SELECT knowledge_id FROM feishu_legacy_resources WHERE data_source_id='"+ds+"'")
    assert ds and kid
    cursor=sql("SELECT md5(last_sync_cursor::text) FROM data_sources WHERE id='"+ds+"'")
    digest_sql="SELECT md5(coalesce(string_agg(content,',' ORDER BY chunk_id),'')) FROM embeddings WHERE knowledge_id='"+kid+"'"
    before=sql(digest_sql)
    seen=fixture()['blocks']
    fixture({'delay':10,'revision':1792555380,'text':'wiki restartcanary verified source text'})
    status,data=http('POST','/api/v1/datasource/'+ds+'/sync',{},owner,99)
    assert status==200
    log=data['id']
    deadline=time.monotonic()+60
    while time.monotonic()<deadline and fixture()['blocks']==seen: time.sleep(.1)
    assert fixture()['blocks']>seen,'real worker never entered connector HTTP'
    generation=int(sql("SELECT execution_generation FROM sync_logs WHERE id='"+log+"'"))
    assert generation>0
    duplicate,_=http('POST','/api/v1/datasource/'+ds+'/sync',{},owner,99)
    assert duplicate==400,'concurrent admission must not create another authoritative run'
    command('kill-only-candidate-app',compose+['kill','-s','SIGKILL','app'])
    app_stopped=True
    assert sql(digest_sql)==before
    assert sql("SELECT md5(last_sync_cursor::text) FROM data_sources WHERE id='"+ds+"'")==cursor
    # Explicit time fault on this synthetic log, without changing generation,
    # publication, cursor, credentials, or security policy to force a PASS.
    sql("UPDATE sync_logs SET updated_at=clock_timestamp()-interval '31 minutes',started_at=clock_timestamp()-interval '31 minutes' WHERE id='"+log+"'")
    fixture({'delay':0})
    command('restart-current-candidate',compose+['up','-d','--wait','--wait-timeout','180','app','frontend'])
    app_stopped=False
    recovered=sql("SELECT status||':'||execution_generation||':'||execution_claimed FROM sync_logs WHERE id='"+log+"'")
    assert recovered=='failed:'+str(generation+1)+':false',recovered
    status,data=http('POST','/api/v1/datasource/'+ds+'/sync',{},owner,99)
    assert status==200 and wait_terminal(data['id'])=='success'
    assert sql("SELECT count(*) FROM knowledges WHERE metadata->>'datasource_id'='"+ds+"'")=='1'
    assert sql("SELECT knowledge_id FROM feishu_legacy_resources WHERE data_source_id='"+ds+"'")==kid
    assert sql("SELECT count(*) FROM embeddings WHERE knowledge_id='"+kid+"' AND content ||| 'restartcanary'")!='0'
    cursor=sql("SELECT md5(last_sync_cursor::text) FROM data_sources WHERE id='"+ds+"'")
    before=sql(digest_sql)
    fixture({'revision':1792555440,'text':'wiki redisrecoverycanary verified source text'})
    command('stop-only-candidate-redis',compose+['stop','--timeout','5','redis'])
    redis_stopped=True
    status,data=http('POST','/api/v1/datasource/'+ds+'/sync',{},owner,99)
    assert status in (200,400,500,503)
    assert sql(digest_sql)==before
    assert sql("SELECT md5(last_sync_cursor::text) FROM data_sources WHERE id='"+ds+"'")==cursor
    command('restart-only-candidate-redis',compose+['up','-d','redis'])
    redis_stopped=False
    outage_http_status=status
    recovery_path='admitted-outbox'
    if status==200:
        assert wait_terminal(data['id'])=='success'
    else:
        # The DB outbox may recover the original intent before a manual retry.
        # Wait for its actual terminal state; do not assert a two-second race.
        latest=sql("SELECT id FROM sync_logs WHERE data_source_id='"+ds+"' ORDER BY created_at DESC LIMIT 1")
        terminal=wait_terminal(latest)
        published=sql("SELECT count(*) FROM embeddings WHERE knowledge_id='"+kid+"' AND content ||| 'redisrecoverycanary'")!='0'
        if terminal=='success' and published:
            recovery_path='durable-outbox-recovered'
        else:
            recovery_path='explicit-retry-after-terminal-failure'
            status,data=http('POST','/api/v1/datasource/'+ds+'/sync',{},owner,99)
            assert status==200 and wait_terminal(data['id'])=='success'
    assert sql("SELECT count(*) FROM knowledges WHERE metadata->>'datasource_id'='"+ds+"'")=='1'
    assert sql("SELECT count(*) FROM embeddings WHERE knowledge_id='"+kid+"' AND content ||| 'redisrecoverycanary'")!='0'
    result={'exit_code':0,'candidate_sha256':instance['candidate_sha256'],'stable_knowledge_id':kid,'real_APP_worker_killed':True,'fault_boundary':'connector HTTP in flight, before staging','lease_expiry':'synthetic single-log 31 minute age injection; no wall-clock claim','old_execution_fenced':recovered,'real_private_Redis_outage':True,'outage_http_status':outage_http_status,'redis_recovery_path':recovery_path,'production_changes':False}
except Exception as error:
    result={'exit_code':1,'candidate_sha256':instance['candidate_sha256'],'error_class':type(error).__name__,'message':str(error),'traceback':traceback.format_exc()}
finally:
    if redis_stopped: command('restore-candidate-redis-after-failure',compose+['up','-d','redis'])
    if app_stopped: command('restore-candidate-app-after-failure',compose+['up','-d','--wait','--wait-timeout','180','app','frontend'])
    (out/'result.json').write_text(json.dumps(result,indent=2))
print(json.dumps(result))
sys.exit(result['exit_code'])
