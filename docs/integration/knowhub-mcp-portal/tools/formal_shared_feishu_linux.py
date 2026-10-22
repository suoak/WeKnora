"""Actual candidate APP + queue worker + synthetic Feishu HTTP + PG acceptance.

Run only after the inherited V1 business acceptance, on this new isolated DB.
This script never calls real Feishu or production endpoints.
"""
from pathlib import Path
import datetime, hashlib, hmac, json, os, re, subprocess, sys, time, traceback, urllib.error, urllib.request, urllib.parse

base=Path(sys.argv[1]).resolve()
assert base.parent==Path('/home/aqyfzx/ai/knowhub-formal-c2-20261022')
import importlib.util
cfg=json.loads((base/'formal-runtime-current.json').read_text());hp=Path(cfg['path']);assert hashlib.sha256(hp.read_bytes()).hexdigest()==cfg['sha256']
spec=importlib.util.spec_from_file_location('formal_runtime',hp);mod=importlib.util.module_from_spec(spec);spec.loader.exec_module(mod);r=mod.Runtime(base)
state=r.state;candidate=r.candidate;private=r.private;backup_root=Path(state['backup_path']);m=json.loads((candidate/'manifest.json').read_text())
attempt=sys.argv[2]if len(sys.argv)>2 else'initial';assert re.fullmatch(r'[a-z0-9-]{1,12}',attempt)
suffix=''if attempt=='initial'else'-'+attempt
out=base/'results'/('feishu'+suffix);out.mkdir(exist_ok=False)
events=[];sources={};rpc_frames=[];origin=r.origin
compose=state['upgrade_compose_argv'];env=dict(x.split('=',1)for x in(private/'app.env').read_text().splitlines()if'='in x)
env.update(dict(x.split('=',1)for x in(private/'formal-signing.env').read_text().splitlines()if'='in x))
def sql(query):return r.sql(query)

def request(method, path, body=None, token=None, tenant=None):
    headers = {'Content-Type':'application/json', 'Accept':'application/json, text/event-stream'}
    if token: headers['Authorization'] = 'Bearer '+token
    if tenant: headers['X-Tenant-ID'] = str(tenant)
    req = urllib.request.Request(origin+path, data=json.dumps(body).encode() if body is not None else None, headers=headers, method=method)
    try:
        with urllib.request.urlopen(req, timeout=45) as r:
            request.last_scope = {'scope':r.headers.get('X-KnowHub-Knowledge-Scope'),'warning':r.headers.get('Warning')}; globals()['r'].headers={k.lower():v for k,v in r.headers.items()}
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
    subprocess.run(compose+['exec','-T','app','curl','--fail','--silent','-H','Content-Type: application/json','-d',json.dumps(changes),'http://formal-feishu:8000/control/'+kind],check=True,stdout=subprocess.DEVNULL)

def signed_file(kid):
    path = sql("SELECT coalesce((SELECT physical_path FROM resources WHERE 'resource://' || handle=k.file_path), k.file_path) FROM knowledges k WHERE id='"+kid+"'")
    expires = str(int(time.time())+86400)
    payload = 'file_path='+path+'&tenant_id=820099&expires='+expires
    signature = hmac.new((env.get('SYSTEM_SIGNING_KEY')or env['SYSTEM_AES_KEY']).encode(),payload.encode(),hashlib.sha256).hexdigest()
    return '/api/v1/files/presigned?'+urllib.parse.urlencode(dict(file_path=path,tenant_id='820099',expires=expires,sig=signature))

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
    status,data = request('POST','/api/v1/auth/login',{'email':'fm-'+user+'@example.invalid','password':r.env['FORMAL_TEST_PASSWORD']})
    assert status == 200
    return data['token']

try:
    retry_source=backup_root/'operator-source/cmd/formalretry';retry_source.mkdir(exist_ok=True)
    (retry_source/'main.go').write_text('// Synthetic test orchestration only. Never compiled into the APP or MCP.\npackage main\nimport (\n "encoding/json"\n "fmt"\n "os"\n "regexp"\n "github.com/hibiken/asynq"\n)\nfunc main(){\n if os.Getenv("KNOWHUB_SYNTHETIC_RETRY_CONTROL")!="SYNTHETIC_ONLY" || os.Getenv("REDIS_ADDR")!="WeKnora-redis:6379" || len(os.Args)!=3 {panic("requires explicit private synthetic fixture")}\n valid:=regexp.MustCompile(`^[a-f0-9-]{36}$`)\n if !valid.MatchString(os.Args[1])||!valid.MatchString(os.Args[2]){panic("exact source and run UUID required")}\n inspector:=asynq.NewInspector(asynq.RedisClientOpt{Addr:os.Getenv("REDIS_ADDR"),Password:os.Getenv("REDIS_PASSWORD")});defer inspector.Close()\n tasks,err:=inspector.ListRetryTasks("sync",asynq.PageSize(1000));if err!=nil{panic(err)}\n changes:=[]map[string]interface{}{}\n for _,task:=range tasks{\n  if task.Type!="datasource:sync"{continue}\n  var payload struct{Source string `json:"data_source_id"`;Run string `json:"sync_log_id"`;Tenant uint64 `json:"tenant_id"`}\n  if json.Unmarshal(task.Payload,&payload)!=nil || payload.Source!=os.Args[1]||payload.Run!=os.Args[2]||payload.Tenant!=820099 {continue}\n  if task.MaxRetry!=5 || task.Retried<1 || task.Retried>5 {panic("unexpected retry budget; never modify it")}\n  err=inspector.RunTask("sync",task.ID)\n  row:=map[string]interface{}{"task_id":task.ID,"retried":task.Retried,"max_retry":task.MaxRetry,"immediate_retry":err==nil,"payload_changed":false,"authorization_changed":false}\n  if err!=nil{row["race_or_error"]=err.Error()}\n  changes=append(changes,row)\n }\n encoded,err:=json.Marshal(changes);if err!=nil{panic(err)};fmt.Println(string(encoded))\n}\n')
    args=['docker','run','--rm','--network','none','-v',str(backup_root/'operator-source')+':/source:ro','-v',str(backup_root/'operators')+':/ops','-v',str(backup_root/'operator-temp')+':/work','-v','/home/aqyfzx/go/pkg/mod:/home/aqyfzx/go/pkg/mod:ro','-v','knowhub-mcp-b7-20261021-102200-data:/prior','-e','GOPATH=/home/aqyfzx/go','-e','GOCACHE=/prior/go-cache','-e','GOTOOLCHAIN=local','-e','GOTMPDIR=/work','-w','/source','knowhub-build/golang:1.26-feishu-py311-ensurepip-20261022','go','build','-o','/ops/formal-retry','./cmd/formalretry']
    with(out/'build-retry.log').open('wb')as f:code=subprocess.run(args,stdout=f,stderr=subprocess.STDOUT,timeout=300).returncode
    assert code==0,'scoped retry helper build'
    retry_env=private/('formal-retry'+suffix+'.env')
    retry_env.write_text('KNOWHUB_SYNTHETIC_RETRY_CONTROL=SYNTHETIC_ONLY\nREDIS_ADDR=WeKnora-redis:6379\nREDIS_PASSWORD='+env.get('REDIS_PASSWORD','')+'\n')
    stamp('scoped-retry-helper',source_sha256=hashlib.sha256('// Synthetic test orchestration only. Never compiled into the APP or MCP.\npackage main\nimport (\n "encoding/json"\n "fmt"\n "os"\n "regexp"\n "github.com/hibiken/asynq"\n)\nfunc main(){\n if os.Getenv("KNOWHUB_SYNTHETIC_RETRY_CONTROL")!="SYNTHETIC_ONLY" || os.Getenv("REDIS_ADDR")!="WeKnora-redis:6379" || len(os.Args)!=3 {panic("requires explicit private synthetic fixture")}\n valid:=regexp.MustCompile(`^[a-f0-9-]{36}$`)\n if !valid.MatchString(os.Args[1])||!valid.MatchString(os.Args[2]){panic("exact source and run UUID required")}\n inspector:=asynq.NewInspector(asynq.RedisClientOpt{Addr:os.Getenv("REDIS_ADDR"),Password:os.Getenv("REDIS_PASSWORD")});defer inspector.Close()\n tasks,err:=inspector.ListRetryTasks("sync",asynq.PageSize(1000));if err!=nil{panic(err)}\n changes:=[]map[string]interface{}{}\n for _,task:=range tasks{\n  if task.Type!="datasource:sync"{continue}\n  var payload struct{Source string `json:"data_source_id"`;Run string `json:"sync_log_id"`;Tenant uint64 `json:"tenant_id"`}\n  if json.Unmarshal(task.Payload,&payload)!=nil || payload.Source!=os.Args[1]||payload.Run!=os.Args[2]||payload.Tenant!=820099 {continue}\n  if task.MaxRetry!=5 || task.Retried<1 || task.Retried>5 {panic("unexpected retry budget; never modify it")}\n  err=inspector.RunTask("sync",task.ID)\n  row:=map[string]interface{}{"task_id":task.ID,"retried":task.Retried,"max_retry":task.MaxRetry,"immediate_retry":err==nil,"payload_changed":false,"authorization_changed":false}\n  if err!=nil{row["race_or_error"]=err.Error()}\n  changes=append(changes,row)\n }\n encoded,err:=json.Marshal(changes);if err!=nil{panic(err)};fmt.Println(string(encoded))\n}\n'.encode()).hexdigest(),binary_sha256=hashlib.sha256((backup_root/'operators/formal-retry').read_bytes()).hexdigest(),wall_backoff_verified=False)

    owner, employee = login('owner'), login('A')
    status,options = request('GET','/api/v1/mcp-api-keys/scope-options',token=employee)
    assert status == 200
    scopes = [{'tenant_id':o['tenant_id'],'kb_scope_mode':'all','knowledge_bases':[]} for o in options['data']]
    status,data = request('POST','/api/v1/mcp-api-keys',{'name':'Joint Feishu synthetic acceptance','client_type':'generic','capabilities':['retrieve','enterprise_retrieve'],'tenant_scopes':scopes,'personal_scope_mode':'dynamic_all'},employee)
    assert status == 201
    key = data['data']; token = key['token']; rpc = 2000
    # A separate legitimate source Owner's read-only personal credential probes
    # old Python REST access; it never impersonates the nonmember employee.
    status,data = request('POST','/api/v1/mcp-api-keys',{'name':'Legacy Python source regression','client_type':'generic','capabilities':['retrieve','enterprise_retrieve'],'tenant_scopes':[{'tenant_id':820099,'kb_scope_mode':'all','knowledge_bases':[]}]},owner)
    assert status == 201
    legacy_key = data['data']
    probe_env = private/('python-source'+suffix+'.env')
    probe_path=private/('python-probe'+suffix+'.py')
    probe_path.write_text('"""Real legacy Python SSE/HTTP SDK calls to the candidate APP, synthetic only."""\nimport asyncio\nfrom contextlib import AsyncExitStack\nimport json, os, socket, subprocess, sys, tempfile, time\nfrom pathlib import Path\nfrom mcp import ClientSession\nfrom mcp.client.sse import sse_client\nfrom mcp.client.streamable_http import streamable_http_client\nfrom mcp.shared._httpx_utils import create_mcp_http_client\n\nroot = Path(\'/source\')\nkid, expected = sys.argv[1], sys.argv[2] == \'allow\'\nassert kid and expected in (True,False)\nkey = os.environ[\'JOINT_PYTHON_PROBE_KEY\']\nresults = []\n\nasync def exercise(url, transport):\n    headers = {\'Authorization\':\'Bearer \'+key,\'X-Tenant-ID\':\'820099\',\'X-From-MCP\':\'false\'}\n    async with AsyncExitStack() as stack:\n        if transport == \'sse\':\n            read,write = await stack.enter_async_context(sse_client(url+\'/sse\',headers=headers))\n        else:\n            client = await stack.enter_async_context(create_mcp_http_client(headers=headers))\n            streams = await stack.enter_async_context(streamable_http_client(url+\'/mcp\',http_client=client))\n            read,write = streams[0],streams[1]\n        session = await stack.enter_async_context(ClientSession(read,write))\n        await session.initialize()\n        for tool in [\'get_knowledge\',\'list_chunks\']:\n            started = time.monotonic()\n            result = await asyncio.wait_for(session.call_tool(tool,{\'knowledge_id\':kid}),15)\n            raw = result.model_dump_json()\n            if expected:\n                assert not result.is_error and kid in raw, \'allowed legacy read did not return the requested document identity\'\n                if tool == \'list_chunks\':\n                    assert \'initialcanary\' in raw or \'updatedcanary\' in raw, \'actual source Chunk text missing\'\n                else:\n                    assert \'Synthetic Wiki\' in raw or \'Synthetic Drive\' in raw, \'actual source detail/title missing\'\n            else:\n                assert result.is_error or \'403\' in raw or \'404\' in raw, \'legacy source read was not denied\'\n                assert not any(value in raw for value in [\'initialcanary\',\'updatedcanary\',\'Synthetic Wiki\',\'Synthetic Drive\']), \'source body or title escaped HOLD\'\n            results.append({\'transport\':transport,\'tool\':tool,\'expected_allow\':expected,\'seconds\':round(time.monotonic()-started,3),\'pass\':True})\n\nfor transport in [\'sse\',\'http\']:\n    with socket.socket() as sock:\n        sock.bind((\'127.0.0.1\',0)); port = sock.getsockname()[1]\n    env = dict(os.environ,MCP_AUTH_MODE=\'weknora_api_key\',WEKNORA_API_KEY=\'\',WEKNORA_BASE_URL=\'http://p3app:8080/api/v1\',WEKNORA_TENANT_ID=\'\',PYTHONUNBUFFERED=\'1\')\n    with tempfile.TemporaryFile() as log:\n        process = subprocess.Popen([sys.executable,str(root/\'mcp-server/weknora_mcp_server.py\'),\'--transport\',transport,\'--host\',\'127.0.0.1\',\'--port\',str(port)],env=env,stdout=log,stderr=subprocess.STDOUT)\n        try:\n            for _ in range(100):\n                if process.poll() is not None: raise RuntimeError(\'legacy candidate process terminated\')\n                try:\n                    with socket.create_connection((\'127.0.0.1\',port),timeout=.2): break\n                except OSError: time.sleep(.1)\n            else: raise RuntimeError(\'legacy transport readiness timed out\')\n            asyncio.run(asyncio.wait_for(exercise(\'http://127.0.0.1:\'+str(port),transport),90))\n        finally:\n            process.terminate()\n            try: process.wait(timeout=10)\n            except subprocess.TimeoutExpired: process.kill(); process.wait(timeout=10)\nprint(json.dumps({\'exit_code\':0,\'real_legacy_sdk\':True,\'real_candidate_APP\':True,\'synthetic_data\':True,\'calls\':results}))\n')
    probe_env.write_text('JOINT_PYTHON_PROBE_KEY='+legacy_key['token']+'\n')
    os.chmod(probe_env,0o600)
    def python_probe(kid, allowed, phase):
        argv = ['docker','run','--rm','--network','weknora_WeKnora-network','--env-file',str(probe_env),'-v',str(candidate/'source')+':/source:ro','-v',str(probe_path)+':/probe.py:ro','-w','/source','--entrypoint','python','weknora-custom/mcp-candidate:v0.8.0-knowhub.24-rc1-854f3b95','/probe.py',kid,'allow' if allowed else 'deny']
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
        healthy=json.loads(text(call('search_knowledge',{'query':'PUBLIC','mode':'hybrid','limit':30})))
        assert healthy['status']=='partial'and any(h['knowledge_id']=='fm-public-two-doc'for h in healthy['results'])
        assert not any(h['knowledge_id']==kid for h in healthy['results'])
        assert 'fm-public-two-doc'in text(call('read_document',{'knowledge_id':'fm-public-two-doc'}))
        assert 'concept/verified'in text(call('wiki_read_page',{'slug':'concept/verified','knowledge_base_id':'fm-public-two'}))
        rest_status,rest_data=request('GET','/api/v1/knowledge/fm-private-home-doc',token=employee,tenant=820001)
        assert rest_status==200 and 'fm-private-home-doc'in json.dumps(rest_data), 'healthy REST read was poisoned by another source'
        list_status,list_data=request('GET','/api/v1/knowledge-bases',token=owner,tenant=820099)
        assert list_status==200, 'source HOLD poisoned the whole REST KB list'
        assert kb not in [row.get('id')for row in list_data.get('data',[])]
        assert r.headers.get('x-knowhub-knowledge-scope')=='partial'and '299'in(r.headers.get('warning')or'')
        stamp('REST-source-isolation',phase=phase,list_status=list_status,healthy_read_status=rest_status,scope_headers={k:v for k,v in r.headers.items()if k in('x-knowhub-knowledge-scope','warning')})
        stamp('source-MCP-deny-boundaries',knowledge_base_id=kb,knowledge_id=kid,phase=phase,operations=['read','list','keyword','semantic','hybrid'])
    def immediate_synthetic_retry(ds,log):
        argv=['docker','run','--rm','--network','weknora_WeKnora-network','--env-file',str(retry_env),'-v',str(backup_root/'operators')+':/ops:ro','--entrypoint','/ops/formal-retry',json.loads((candidate/'results/build/done.json').read_text())['app'],ds,log]
        r=subprocess.run(argv,capture_output=True,timeout=30)
        assert r.returncode==0,'synthetic retry controller failed'
        changes=json.loads(r.stdout)
        if changes:stamp('synthetic-immediate-retry',source=ds,log_id=log,changes=changes,wall_backoff_verified=False)
    def run_sync(ds, success):
        status,data = request('POST','/api/v1/datasource/'+ds+'/sync',{},owner,820099)
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
    # Earlier failed attempts are retained. Recover only these synthetic test
    # sources through the real owner API/worker; never clear their guard in SQL.
    prior = sql("SELECT id||':'||type FROM data_sources WHERE tenant_id=820099 AND name IN ('Formal synthetic wiki','Formal synthetic drive') AND legacy_access_state='hold' AND deleted_at IS NULL")
    for row in prior.splitlines():
        prior_id, prior_type = row.split(':')
        kind = 'wiki' if prior_type == 'feishu' else 'drive'
        assert prior_type in ('feishu','feishu_drive')
        fixture(kind,revision=1792556000,text=kind+' priorattemptrecovery ordinary text',missing=False,status=200,block_status=200,delay=0)
        pending=sql("SELECT id FROM sync_logs WHERE data_source_id='"+prior_id+"' AND status='running'")
        for prior_log in pending.splitlines():
            deadline=time.monotonic()+180
            while time.monotonic()<deadline:
                if sql("SELECT status FROM sync_logs WHERE id='"+prior_log+"'")!='running':break
                immediate_synthetic_retry(prior_id,prior_log)
                time.sleep(1)
            assert sql("SELECT status FROM sync_logs WHERE id='"+prior_log+"'")!='running','prior synthetic retry did not terminate'
        run_sync(prior_id,True)
        stamp('prior-synthetic-attempt-recovered',source_id=prior_id,via='real-owner-sync-worker',guard_sql_override=False)
    for kind,connector in [('wiki','feishu'),('drive','feishu_drive')]:
        kb = 'fm-feishu-'+kind+suffix
        fixture(kind,revision=1792555200,text=kind+' initialcanary ordinary text',missing=False,status=200,block_status=200,delay=0)
        sql("INSERT INTO knowledge_bases(id,name,tenant_id,embedding_model_id,summary_model_id,type,chunking_config,indexing_strategy) SELECT '"+kb+"','Synthetic Feishu "+kind+"',820099,embedding_model_id,summary_model_id,type,chunking_config,indexing_strategy FROM knowledge_bases WHERE id='fm-public-kb'")
        status,data = request('POST','/api/v1/datasource',{'name':'Formal synthetic '+kind,'type':connector,'knowledge_base_id':kb,'sync_mode':'incremental','sync_deletions':True,'config':{'type':connector,'credentials':{'app_id':'synthetic','app_secret':'synthetic-not-secret','base_url':'http://formal-feishu:8000'},'resource_ids':['root']}},owner,820099)
        assert status == 201, 'create source status '+str(status)
        ds = data['id']
        # Synthetic pre-existing RC3-shaped content, before any resource is
        # enrolled. Exercise the first suspended-source replay through the
        # actual APP worker, not only a newly created Knowledge path.
        baseline_kid = 'fm-legacy-'+kind+suffix
        baseline_chunk = baseline_kid+'-chunk'
        metadata = json.dumps({'datasource_id':ds,'external_id':kind+'-document'})
        sql("INSERT INTO knowledges(id,tenant_id,knowledge_base_id,type,title,source,file_name,file_hash,parse_status,enable_status,channel,metadata) VALUES('"+baseline_kid+"',820099,'"+kb+"','file','RC3 synthetic baseline','synthetic legacy before suspension','legacy.md','rc3-before-suspension','completed','enabled','feishu','"+metadata+"'::jsonb)")
        sql("INSERT INTO chunks(id,tenant_id,knowledge_base_id,knowledge_id,content,chunk_type,content_revision,is_enabled,chunk_index,start_at,end_at) VALUES('"+baseline_chunk+"',820099,'"+kb+"','"+baseline_kid+"','rc3baselinecanary old source text','text',1,true,0,0,33)")
        vector = '['+','.join(['1']+['0']*1023)+']'
        sql("INSERT INTO embeddings(source_id,source_type,chunk_id,knowledge_id,knowledge_base_id,content,dimension,embedding,is_enabled) VALUES('"+baseline_chunk+"',0,'"+baseline_chunk+"','"+baseline_kid+"','"+kb+"','rc3baselinecanary old source text',1024,'"+vector+"'::halfvec,true)")
        assert 'rc3baselinecanary' in text(call('read_document',{'knowledge_id':baseline_kid}))
        run_sync(ds,True)
        kid = sql("SELECT id FROM knowledges WHERE metadata->>'datasource_id'='"+ds+"'")
        assert kid and '\n' not in kid
        assert kid == baseline_kid, 'first source replay must preserve the pre-existing RC3 Knowledge ID'
        assert sql("SELECT count(*) FROM embeddings WHERE knowledge_id='"+kid+"' AND content ||| 'rc3baselinecanary'") == '0'
        assert 'initialcanary' in text(call('read_document',{'knowledge_id':kid}))
        python_probe(kid,True,kind+'-active')
        old_file = signed_file(kid)
        assert file_status(old_file)[0] == 200
        old_attempt = sql("SELECT active_attempt FROM feishu_legacy_resources WHERE data_source_id='"+ds+"'")
        run_sync(ds,True)
        assert old_attempt == sql("SELECT active_attempt FROM feishu_legacy_resources WHERE data_source_id='"+ds+"'")
        fixture(kind,revision=1792555260,text=kind+' updatedcanary ordinary linked [policy](https://example.test/guide)')
        run_sync(ds,True)
        assert kid == sql("SELECT id FROM knowledges WHERE metadata->>'datasource_id'='"+ds+"'")
        for mode in ['keyword','semantic','hybrid']:
            result = json.loads(text(call('search_knowledge',{'query':'updatedcanary','mode':mode,'limit':30})))
            target_hits=[hit for hit in result['results'] if hit['knowledge_id']==kid]
            assert target_hits, 'updated source document missing from '+mode
            assert all(hit['tenant_id']==820099 and hit['knowledge_base_id']==kb for hit in target_hits)
            assert 'updatedcanary' in json.dumps(target_hits) and 'initialcanary' not in json.dumps(target_hits), 'one source publication mixed old and new text'
            # The controlled embedding intentionally returns a constant vector;
            # other authorized KBs may correctly have semantic matches too.
        assert file_status(old_file)[0] in (403,404), 'old unexpired anonymous link must be denied after version switch'
        current_file = signed_file(kid)
        assert file_status(current_file)[0] == 200
        good_attempt = sql("SELECT active_attempt FROM feishu_legacy_resources WHERE data_source_id='"+ds+"'")
        cursor = sql("SELECT md5(last_sync_cursor::text) FROM data_sources WHERE id='"+ds+"'")
        fixture(kind,revision=1792555320,block_status=500)
        run_sync(ds,False)
        assert good_attempt == sql("SELECT active_attempt FROM feishu_legacy_resources WHERE data_source_id='"+ds+"'")
        assert cursor == sql("SELECT md5(last_sync_cursor::text) FROM data_sources WHERE id='"+ds+"'")
        assert 'updatedcanary' in text(call('read_document',{'knowledge_id':kid}))
        fixture(kind,block_status=200,revision=1792555260,status=429)
        run_sync(ds,False)
        assert cursor == sql("SELECT md5(last_sync_cursor::text) FROM data_sources WHERE id='"+ds+"'")
        assert 'updatedcanary' in text(call('read_document',{'knowledge_id':kid})), 'rate limit must preserve old readable content'
        fixture(kind,block_status=200,revision=1792555260,status=403)
        run_sync(ds,False)
        source_denied(kb,kid,'403-hold')
        assert file_status(current_file)[0] in (403,404), 'current unexpired anonymous link must be denied on HOLD'
        python_probe(kid,False,kind+'-hold')
        status,_ = request('GET','/api/v1/knowledge/'+kid,token=owner,tenant=820099)
        assert status in (403,404), 'REST must deny held document'
        status,_ = request('GET','/api/v1/datasource/'+ds+'/logs',token=owner,tenant=820099)
        assert status == 403, 'historical diagnostics must not bypass HOLD'
        fixture(kind,status=200)
        run_sync(ds,True)
        assert 'updatedcanary' in text(call('read_document',{'knowledge_id':kid}))
        fixture(kind,missing=True)
        run_sync(ds,False)
        source_denied(kb,kid,'unknown-absence')
        assert sql("SELECT count(*) FROM knowledges WHERE id='"+kid+"'") == '1'
        fixture(kind,missing=False)
        run_sync(ds,True)
        assert 'updatedcanary' in text(call('read_document',{'knowledge_id':kid}))
        current_file = signed_file(kid)
        assert file_status(current_file)[0] == 200
        stamp('source-lifecycle-pass',kind=kind,stable_knowledge_id=kid,source_id=ds,real_cloud=False)
        sources[kind]={'source_id':ds,'knowledge_base_id':kb,'knowledge_id':kid}
    # Finish the browser/real CLI flow on this same live APP/database while
    # both sources are valid, then explicitly release the destructive deny phase.
    ready=base/('feishu-active-ready'+suffix+'.json')
    ready.write_text(json.dumps({'candidate_sha256':m['candidate_sha256'],'sources':sources,'pending':'browser/CLI then source revoke'},indent=2))
    release=base/('feishu-release-revoke'+suffix)
    deadline=time.monotonic()+1200
    while not release.exists() and time.monotonic()<deadline:time.sleep(1)
    assert release.exists(),'browser/CLI orchestration not released within 1200 seconds'
    stamp('browser-CLI-orchestration-released',sources_active=True)
    file_reference=private/('revoked-file'+suffix+'.txt')
    file_reference.write_text(current_file);os.chmod(file_reference,0o600)
    # Source deletion is an authenticated trusted local event, independent of
    # speculative cloud absence. Preserve content rows but deny new reads.
    status,_ = request('DELETE','/api/v1/datasource/'+ds,token=owner,tenant=820099)
    assert status == 204
    source_denied(kb,kid,'trusted-local-delete')
    assert file_status(current_file)[0] in (403,404)
    python_probe(kid,False,'drive-revoked')
    status,_ = request('DELETE','/api/v1/mcp-api-keys/'+str(legacy_key['id']),token=owner)
    assert status == 200
    assert sql("SELECT legacy_access_state FROM data_sources WHERE id='"+ds+"'") == 'revoked'
    backup = private/('feishu-revoked'+suffix+'.dump')
    with backup.open('wb')as f:
        subprocess.run(['docker','exec','-u','postgres','WeKnora-postgres','pg_dump','-U','postgres','-Fc','WeKnora'],stdout=f,check=True)
    os.chmod(backup,0o600)
    stamp('durable-revocation-backup',schema=3037,dump_sha256=hashlib.sha256(backup.read_bytes()).hexdigest(),restore_pending=True)
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
    # Full logs remain private on the server; public results contain only synthetic tool frames.
print(json.dumps(result))
sys.exit(result['exit_code'])
