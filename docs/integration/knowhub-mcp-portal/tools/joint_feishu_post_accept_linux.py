"""Actual candidate APP + queue worker + synthetic Feishu HTTP + PG acceptance.

Run only after the inherited V1 business acceptance, on this new isolated DB.
This script never calls real Feishu or production endpoints.
"""
from pathlib import Path
import datetime, hashlib, hmac, json, os, re, subprocess, sys, time, traceback, urllib.error, urllib.request, urllib.parse

base = Path(sys.argv[1]).resolve()
assert base.parent == Path('/home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-feishu-20261022')
m = json.loads((base/'manifest.json').read_text())
instance = json.loads((base/'results/deploy/instance.json').read_text())
assert m['candidate_sha256'] == instance['candidate_sha256']
assert instance['project'].startswith('knowhub-v1-feishu-')
attempt=sys.argv[2] if len(sys.argv)>2 else 'post-a4'
assert re.fullmatch(r'[a-z0-9-]{1,12}',attempt)
suffix='' if attempt=='initial' else '-'+attempt
out = base/'results'/('feishu-app'+suffix)
out.mkdir(exist_ok=False)
events = []
sources = {}
rpc_frames = []
compose = ['docker','compose','--project-name',instance['project'],'--project-directory',str(base),'-f',str(base/'compose.yml')]
origin = instance['url']
env = dict(line.split('=',1) for line in (base/'private/app.env').read_text().splitlines())

def sql(query):
    raw = subprocess.check_output(compose+['exec','-T','postgres','psql','-v','ON_ERROR_STOP=1','-U','postgres','-d','knowhub_v1','-Atc',query])
    return raw.decode().strip()

def request(method, path, body=None, token=None, tenant=None):
    headers = {'Content-Type':'application/json', 'Accept':'application/json, text/event-stream'}
    if token: headers['Authorization'] = 'Bearer '+token
    if tenant: headers['X-Tenant-ID'] = str(tenant)
    req = urllib.request.Request(origin+path, data=json.dumps(body).encode() if body is not None else None, headers=headers, method=method)
    try:
        with urllib.request.urlopen(req, timeout=45) as r:
            status, raw = r.status, r.read().decode()
    except urllib.error.HTTPError as e:
        status, raw = e.code, e.read().decode()
    if raw.startswith(('event:','data:')):
        raw = next(line[5:] for line in raw.splitlines() if line.startswith('data:'))
    try:
        data = json.loads(raw) if raw else None
    except json.JSONDecodeError:
        # net/http authorization errors are deliberately plain text. Preserve
        # the actual status and denial instead of hiding it behind a parser error.
        assert status >= 400, 'unexpected successful non-JSON response'
        data = {'error': {'http_status': status, 'message': raw[:500]}}
    return status, data

def fixture(kind, **changes):
    subprocess.run(compose+['exec','-T','app','curl','--fail','--silent','-H','Content-Type: application/json','-d',json.dumps(changes),'http://feishu:8000/control/'+kind],check=True,stdout=subprocess.DEVNULL)

def signed_file(kid):
    path = sql("SELECT coalesce((SELECT physical_path FROM resources WHERE 'resource://' || id=k.file_path), k.file_path) FROM knowledges k WHERE id='"+kid+"'")
    expires = str(int(time.time())+86400)
    payload = 'file_path='+path+'&tenant_id=99&expires='+expires
    signature = hmac.new(env['SYSTEM_SIGNING_KEY'].encode(),payload.encode(),hashlib.sha256).hexdigest()
    return '/api/v1/files/presigned?'+urllib.parse.urlencode(dict(file_path=path,tenant_id='99',expires=expires,sig=signature))

def file_status(path):
    try:
        with urllib.request.urlopen(origin+path,timeout=15) as response:
            raw = response.read()
            assert response.headers.get('Cache-Control') == 'private, no-store'
            return response.status,raw
    except urllib.error.HTTPError as error:
        return error.code,error.read()

def stamp(name, **facts):
    events.append(dict(name=name, utc=datetime.datetime.now(datetime.timezone.utc).isoformat(), **facts))
    (out/'events.json').write_text(json.dumps(events,indent=2))

def login(user):
    status,data = request('POST','/api/v1/auth/login',{'email':user+'@example.invalid','password':env['KNOWHUB_V1_TEST_PASSWORD']})
    assert status == 200
    return data['token']

try:
    owner, employee = login('owner'), login('A')
    status,options = request('GET','/api/v1/mcp-api-keys/scope-options',token=employee)
    assert status == 200
    scopes = [{'tenant_id':o['tenant_id'],'kb_scope_mode':'all','knowledge_bases':[]} for o in options['data']]
    status,data = request('POST','/api/v1/mcp-api-keys',{'name':'Joint Feishu synthetic acceptance','client_type':'generic','capabilities':['retrieve','enterprise_retrieve'],'tenant_scopes':scopes},employee)
    assert status == 201
    key = data['data']; token = key['token']; rpc = 2000
    # A separate legitimate source Owner's read-only personal credential probes
    # old Python REST access; it never impersonates the nonmember employee.
    status,data = request('POST','/api/v1/mcp-api-keys',{'name':'Legacy Python source regression','client_type':'generic','capabilities':['retrieve','enterprise_retrieve'],'tenant_scopes':[{'tenant_id':99,'kb_scope_mode':'all','knowledge_bases':[]}]},owner)
    assert status == 201
    legacy_key = data['data']
    probe_env = base/('private/python-source'+suffix+'.env')
    probe = json.loads((base/'python-source-probe-current.json').read_text())
    probe_path = Path(probe['path']).resolve()
    assert probe_path.parent == base and hashlib.sha256(probe_path.read_bytes()).hexdigest() == probe['sha256']
    probe_env.write_text('JOINT_PYTHON_PROBE_KEY='+legacy_key['token']+'\n')
    os.chmod(probe_env,0o600)
    def python_probe(kid, allowed, phase):
        argv = ['docker','run','--rm','--network',instance['project']+'_isolated','--env-file',str(probe_env),'-v',str(base/'source')+':/source:ro','-v',str(probe_path)+':/probe.py:ro','-w','/source','--entrypoint','python','weknora-custom/mcp-candidate:v0.8.0-knowhub.24-rc1-854f3b95','/probe.py',kid,'allow' if allowed else 'deny']
        result = subprocess.run(argv,capture_output=True,timeout=240)
        raw = result.stdout+result.stderr
        raw = raw.replace(legacy_key['token'].encode(),b'[REDACTED_SYNTHETIC_CREDENTIAL]')
        (out/(phase+'-python-sdk.log')).write_bytes(raw)
        (out/(phase+'-python-sdk.exit.txt')).write_text(str(result.returncode))
        assert result.returncode == 0,'legacy Python probe failed: '+phase

    def call(tool, arguments):
        global rpc
        rpc += 1
        response=request('POST','/api/v1/mcp/personal',{'jsonrpc':'2.0','id':rpc,'method':'tools/call','params':{'name':tool,'arguments':arguments}},token)
        frame=json.dumps({'rpc_id':rpc,'tool':tool,'arguments':arguments,'http_status':response[0],'response':response[1]})
        for secret in [owner,employee,token,legacy_key['token']]: frame=frame.replace(secret,'[REDACTED_SYNTHETIC_CREDENTIAL]')
        rpc_frames.append(json.loads(frame))
        (out/'mcp-responses.json').write_text(json.dumps({'synthetic_only':True,'frames':rpc_frames},indent=2))
        return response
    def text(result):
        status,data = result
        assert status == 200 and not data.get('error') and not data.get('result',{}).get('isError'), 'MCP read rejected'
        return json.dumps(json.loads(data['result']['content'][0]['text']))
    def denied(result):
        status,data = result
        return status in (401,403,404) or bool(data.get('error') or data.get('result',{}).get('isError'))
    def source_denied(kb,kid,phase):
        assert denied(call('read_document',{'knowledge_id':kid}))
        listed=call('list_documents',{'knowledge_base_id':kb})
        if not denied(listed):
            assert kid not in [x['knowledge_id']for x in json.loads(text(listed))], 'held document still listed'
        for mode in ['keyword','semantic','hybrid']:
            searched=call('search_knowledge',{'query':'updatedcanary','mode':mode,'knowledge_base_ids':[kb],'limit':10})
            if not denied(searched):
                data=json.loads(text(searched))
                assert not any(x.get('knowledge_id')==kid for x in data['results']), 'held candidate escaped '+mode
                assert 'updatedcanary'not in json.dumps(data['results']), 'held content escaped '+mode
        stamp('source-MCP-deny-boundaries',knowledge_base_id=kb,knowledge_id=kid,phase=phase,operations=['read','list','keyword','semantic','hybrid'])
    def immediate_synthetic_retry(ds,log):
        argv=['docker','run','--rm','--network',instance['project']+'_isolated','-e','KNOWHUB_SYNTHETIC_RETRY_CONTROL=SYNTHETIC_ONLY','-e','REDIS_ADDR=redis:6379','-v',str(base/'artifacts')+':/artifacts:ro','--entrypoint','/artifacts/retry-control',json.loads((base/'results/build/done.json').read_text())['app'],ds,log]
        r=subprocess.run(argv,capture_output=True,timeout=30)
        assert r.returncode==0,'synthetic retry controller failed'
        changes=json.loads(r.stdout)
        if changes:stamp('synthetic-immediate-retry',source=ds,log_id=log,changes=changes,wall_backoff_verified=False)
    def run_sync(ds, success):
        status,data = request('POST','/api/v1/datasource/'+ds+'/sync',{},owner,99)
        assert status == 200, 'enqueue status '+str(status)
        log = data['id']
        deadline = time.monotonic()+180
        while time.monotonic()<deadline:
            state = sql("SELECT status FROM sync_logs WHERE id='"+log+"'")
            if state != 'running': break
            if not success:immediate_synthetic_retry(ds,log)
            time.sleep(1)
        assert state != 'running', 'worker did not finish within 180s'
        assert (state == 'success') == success, 'unexpected sync state '+state
        stamp('worker-sync', source=ds, log_id=log, state=state, expected_success=success)
        return log
    # Resume the exact source state after the prior harness mistook a real
    # successful HTTP 204 DELETE for HTTP 200. Do not replay or undo deletion.
    prior=base/'results/feishu-app-a4'
    failure=json.loads((prior/'result.json').read_text())
    assert failure['exit_code']==1 and 'line 266' in failure['traceback']
    prior_events=json.loads((prior/'events.json').read_text())
    assert {e['kind']for e in prior_events if e['name']=='source-lifecycle-pass'}=={'wiki','drive'}
    ready=json.loads((base/'feishu-active-ready-a4.json').read_text())
    assert ready['candidate_sha256']==m['candidate_sha256']
    sources=ready['sources'];ds=sources['drive']['source_id'];kb=sources['drive']['knowledge_base_id'];kid=sources['drive']['knowledge_id']
    assert any('method=DELETE path=/api/v1/datasource/'+ds in line and 'status_code=204' in line for line in (prior/'app.log').read_text().splitlines())
    assert sql("SELECT legacy_access_state FROM data_sources WHERE id='"+ds+"'")=='revoked'
    current_file=(base/'private/revoked-file-a4.txt').read_text()
    stamp('resume-after-observed-204',prior_results='feishu-app-a4',lifecycle_scope='real APP/worker synthetic Wiki and Drive',source_revocation_not_undone=True)
    file_reference=base/('private/revoked-file'+suffix+'.txt')
    file_reference.write_text(current_file);os.chmod(file_reference,0o600)
    # Source deletion is an authenticated trusted local event, independent of
    # speculative cloud absence. Preserve content rows but deny new reads.
    source_denied(kb,kid,'trusted-local-delete')
    assert file_status(current_file)[0] in (403,404)
    python_probe(kid,False,'drive-revoked')
    status,_ = request('DELETE','/api/v1/mcp-api-keys/'+str(legacy_key['id']),token=owner)
    assert status == 200
    assert sql("SELECT legacy_access_state FROM data_sources WHERE id='"+ds+"'") == 'revoked'
    backup = base/('private/feishu-revoked'+suffix+'.dump')
    with backup.open('wb') as f:
        subprocess.run(compose+['exec','-T','postgres','pg_dump','-U','postgres','-Fc','knowhub_v1'],stdout=f,check=True)
    os.chmod(backup,0o600)
    restore_db='knowhub_joint_revoked_restore'+suffix.replace('-','_')
    sql('CREATE DATABASE '+restore_db+' TEMPLATE template0')
    with backup.open('rb') as f:
        subprocess.run(compose+['exec','-T','postgres','pg_restore','-U','postgres','-d',restore_db],stdin=f,check=True)
    restored = subprocess.check_output(compose+['exec','-T','postgres','psql','-U','postgres','-d',restore_db,'-Atc',"SELECT version||':'||dirty FROM schema_migrations; SELECT legacy_access_state FROM data_sources WHERE id='"+ds+"'"]).decode().strip()
    assert '3036:false' in restored and 'revoked' in restored
    restore_override = base/('restore-validation'+suffix+'.compose.json')
    restore_override.write_text(json.dumps({'services':{'app':{'environment':{'DB_NAME':restore_db}}}}))
    try:
        subprocess.run(compose+['-f',str(restore_override),'up','-d','--force-recreate','--wait','--wait-timeout','180','app'],check=True,timeout=240,stdout=subprocess.DEVNULL)
        assert denied(call('read_document',{'knowledge_id':kid})), 'restored APP must retain source denial'
        status,_ = request('GET','/api/v1/knowledge/'+kid,token=owner,tenant=99)
        assert status in (403,404)
        assert file_status(current_file)[0] in (403,404)
    finally:
        subprocess.run(compose+['up','-d','--force-recreate','--wait','--wait-timeout','180','app'],check=True,timeout=240,stdout=subprocess.DEVNULL)
    stamp('durable-revocation-backup-restore',schema=3036,revocation_preserved=True,production_data=False)
    status,_ = request('DELETE','/api/v1/mcp-api-keys/'+str(key['id']),token=employee)
    assert status == 200
    assert denied(call('list_knowledge_bases',{}))
    (base/'feishu-source-current.json').write_text(json.dumps({'candidate_sha256':m['candidate_sha256'],'results_dir':out.name,'sources':sources,'backup':str(backup),'revoked_file_reference':str(file_reference),'revoked_file_sha256':hashlib.sha256(current_file.encode()).hexdigest()},indent=2))
    result = {'exit_code':0,'candidate_sha256':m['candidate_sha256'],'runtime_source_sha256':m['runtime_source_sha256'],'real_app_worker':True,'real_postgres_paradedb':True,'cloud':'SYNTHETIC_HTTP_ONLY','formal_model':'NOT_TESTED','real_SID':'NOT_TESTED'}
except Exception as error:
    details=traceback.format_exc()
    for name in ['owner','employee','token']:
        secret=locals().get(name)
        if isinstance(secret,str) and secret: details=details.replace(secret,'[REDACTED_SYNTHETIC_CREDENTIAL]')
    if 'legacy_key' in locals(): details=details.replace(legacy_key.get('token','unconfigured'),'[REDACTED_SYNTHETIC_CREDENTIAL]')
    result = {'exit_code':1,'error_class':type(error).__name__,'message':str(error),'traceback':details,'candidate_sha256':m['candidate_sha256']}
finally:
    (out/'result.json').write_text(json.dumps(result,indent=2))
    with (out/'app.log').open('wb') as f:
        subprocess.run(compose+['logs','--no-color','app'],stdout=f,stderr=subprocess.STDOUT,timeout=30)
print(json.dumps(result))
sys.exit(result['exit_code'])
