"""Real authenticated HTTP/MCP acceptance against ONLY the new private stack.

Run on the verified host after staging this file and the public/synthetic sample
directory below the owned task root. No production login or OAuth is consumed.
"""
import base64
import concurrent.futures
import datetime
import hashlib
import json
import os
from pathlib import Path
import secrets
import socket
import subprocess
import time
import threading
import urllib.error
import urllib.request
import uuid

ROOT=Path('/mnt/sata0/knowhub-feishu-v2-builds/office-guard-4ffd7442d-20261125')
SITE=ROOT/'site'
SHA=json.loads((SITE/'deployment-proof.json').read_text())['source_sha']
assert SHA in ('4ffd7442da82675bba2b63f3f0dfe31995712070','ca1c058c0887b9b2c00899bb6c7da6201761415a','668d067ec75a2a351ace4f7f0797be77af2b24ae')
PROJECT='knowhub-office-guard-test'
BASE='http://127.0.0.1:39781'
COMPOSE=['docker','compose','-p',PROJECT,'-f',str(SITE/'compose.json')]
RESULTS=[]
assert socket.gethostname()=='aqyfzx10' and os.geteuid()==1000 and ROOT.resolve()==ROOT
proof=json.loads((SITE/'deployment-proof.json').read_text())
assert proof['project']==PROJECT and proof['source_sha']==SHA

def private(path,data):
    fd=os.open(path,os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
    with os.fdopen(fd,'w') as f:json.dump(data,f)

def http(method,path,body=None,token=None,headers=None,raw=None):
    assert path.startswith(('/api/v1/','/internal/mcp/isolated/personal','/health'))
    h={'Content-Type':'application/json','Accept':'application/json'}
    if token:h['Authorization']='Bearer '+token
    h.update(headers or {})
    payload=raw if raw is not None else json.dumps(body).encode() if body is not None else None
    opener=urllib.request.build_opener(urllib.request.ProxyHandler({}))
    base=BASE
    if path.startswith('/internal/mcp/isolated/'):
        cid=subprocess.check_output(COMPOSE+['ps','-q','app'],text=True).strip()
        container=json.loads(subprocess.check_output(['docker','inspect',cid],text=True))[0]
        assert container['Config']['Labels']['com.docker.compose.project']==PROJECT
        ip=container['NetworkSettings']['Networks'][PROJECT+'-private']['IPAddress']
        assert ip.startswith('10.254.222.')
        base='http://'+ip+':8080'
    req=urllib.request.Request(base+path,data=payload,headers=h,method=method)
    start=time.monotonic()
    try:
        with opener.open(req,timeout=90) as r:status=r.status;data=r.read(4*1024*1024);rh=dict(r.headers)
    except urllib.error.HTTPError as e:status=e.code;data=e.read(65536);rh=dict(e.headers)
    try:value=json.loads(data)
    except ValueError:value={'raw_text':data.decode('utf-8','replace')[:8192]}
    return {'status':status,'body':value,'elapsed_ms':round((time.monotonic()-start)*1000,2),'headers':rh}

def sql(query):
    p=subprocess.run(COMPOSE+['exec','-T','postgres','psql','-X','-U','postgres','-d','office_guard_test','-At','-v','ON_ERROR_STOP=1'],
        input=query,text=True,capture_output=True,timeout=30)
    assert p.returncode==0,p.stderr[-1200:]
    return p.stdout.strip()

def snapshot():
    tables=json.loads(sql("SELECT json_agg(tablename) FROM pg_tables WHERE schemaname='public' AND (tablename IN ('knowledges','chunks','temporary_documents') OR tablename LIKE 'embeddings%');")) or []
    rows={}
    for name in sorted(tables):
        assert all(c.isalnum() or c=='_' for c in name)
        data=sql('SELECT COALESCE(json_agg(x ORDER BY x::text)::text,\'[]\') FROM (SELECT to_jsonb(t) AS x FROM "'+name+'" t) s;')
        rows[name]={'sha256':hashlib.sha256(data.encode()).hexdigest(),'count':len(json.loads(data))}
    files={}
    for p in sorted((SITE/'files').rglob('*')):
        if p.is_file() and not p.is_symlink():files[str(p.relative_to(SITE/'files'))]=hashlib.sha256(p.read_bytes()).hexdigest()
    return {'tables':rows,'files':files}

def resources():
    ids=subprocess.check_output(COMPOSE+['ps','-q'],text=True).split()
    p=subprocess.run(['docker','stats','--no-stream','--format','{{json .}}',*ids],text=True,capture_output=True,timeout=15)
    stats=[json.loads(line) for line in p.stdout.splitlines() if line.startswith('{')]
    peak=subprocess.run(COMPOSE+['exec','-T','app','sh','-c',
        'for f in /sys/fs/cgroup/memory.peak /sys/fs/cgroup/memory.max /sys/fs/cgroup/memory/memory.max_usage_in_bytes /sys/fs/cgroup/memory/memory.limit_in_bytes; do if test -r "$f"; then printf "%s=" "$f"; cat "$f"; fi; done'],text=True,capture_output=True,timeout=15)
    states=json.loads(subprocess.check_output(['docker','inspect',*ids],text=True))
    return {'stats':stats,'app_cgroup_peak_and_limit':peak.stdout.splitlines(),
        'states':[{'name':r['Name'],'oom_killed':r['State']['OOMKilled'],'restart_count':r['RestartCount'],'running':r['State']['Running']} for r in states]}

def record(name,status,**details):
    row={'case':name,'result':status,'executed_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'source_sha':SHA,**details}
    RESULTS.append(row)
    print(json.dumps(row,ensure_ascii=True),flush=True)

def upload(account,kb,filename,content,extras=None,path=None):
    boundary='OfficeAcceptance'+uuid.uuid4().hex
    chunks=[]
    for k,v in (extras or {}).items():chunks.append(('--'+boundary+'\r\nContent-Disposition: form-data; name="'+k+'"\r\n\r\n'+v+'\r\n').encode())
    chunks.extend([('--'+boundary+'\r\nContent-Disposition: form-data; name="file"; filename="'+filename+'"\r\nContent-Type: application/octet-stream\r\n\r\n').encode(),content,('\r\n--'+boundary+'--\r\n').encode()])
    return http('POST',path or '/api/v1/knowledge-bases/'+kb+'/knowledge/file',token=account['token'],headers={'Content-Type':'multipart/form-data; boundary='+boundary},raw=b''.join(chunks))

def register(name):
    path=SITE/(name+'-account.json')
    if path.exists():account=json.loads(path.read_text())
    else:
        account={'username':'office-'+name,'email':'office-'+name+'@example.invalid','password':secrets.token_urlsafe(16)+'aA!9'}
        private(path,account)
        r=http('POST','/api/v1/auth/register',account)
        assert r['status']==201,{'registration_status':r['status'],'body':r['body']}
    r=http('POST','/api/v1/auth/login',{'email':account['email'],'password':account['password']})
    if r['status']==401:
        # A previous transport failure may have occurred before registration.
        created=http('POST','/api/v1/auth/register',{k:account[k] for k in ('username','email','password')})
        assert created['status']==201,created['body']
        r=http('POST','/api/v1/auth/login',{'email':account['email'],'password':account['password']})
    assert r['status']==200 and r['body'].get('token'),r['body']
    account.update(r['body'])
    return account

def create_kb(account):
    path=SITE/('acceptance-kb-'+SHA[:9]+'.json')
    if path.exists():return json.loads(path.read_text())['id']
    model=http('POST','/api/v1/models',{'name':'office-test-embedding','type':'Embedding','source':'remote',
        'parameters':{'provider':'generic','base_url':'http://model-fixture:9000/v1','api_key':'isolated-nonsecret-fixture','embedding_parameters':{'dimension':1024}}},account['token'])
    assert model['status']==201,model['body']
    chat=http('POST','/api/v1/models',{'name':'office-test-chat','type':'KnowledgeQA','source':'remote',
        'parameters':{'provider':'generic','base_url':'http://model-fixture:9000/v1','api_key':'isolated-nonsecret-fixture'}},account['token'])
    assert chat['status']==201,chat['body']
    kb=http('POST','/api/v1/knowledge-bases',{'name':'Office encryption isolated acceptance','type':'document',
        'embedding_model_id':model['body']['data']['id'],
        'summary_model_id':chat['body']['data']['id'],
        'chunking_config':{'chunk_size':512,'chunk_overlap':50},
        'indexing_strategy':{'vector_enabled':True,'keyword_enabled':True,'wiki_enabled':False,'graph_enabled':False}},account['token'])
    assert kb['status']==201,kb['body']
    value=kb['body']['data'];private(path,{'id':value['id']});return value['id']

def brief(r):return {'http_status':r['status'],'response':r['body'],'elapsed_ms':r['elapsed_ms']}

def business_code(r):return r['body'].get('error',r['body']).get('code')

def mcp(token,tool,args,path='/api/v1/mcp/personal'):
    headers={'Accept':'application/json, text/event-stream','MCP-Protocol-Version':'2024-11-05'}
    init=http('POST',path,{'jsonrpc':'2.0','id':1,'method':'initialize','params':{'protocolVersion':'2024-11-05','capabilities':{},'clientInfo':{'name':'office-isolated-acceptance','version':'1'}}},token,headers)
    session=next((v for k,v in init['headers'].items() if k.lower()=='mcp-session-id'),None)
    if session:headers['Mcp-Session-Id']=session
    http('POST',path,{'jsonrpc':'2.0','method':'notifications/initialized'},token,headers)
    return http('POST',path,{'jsonrpc':'2.0','id':2,'method':'tools/call','params':{'name':tool,'arguments':args}},token,headers)

def shared_permissions(a,other,kb,encrypted):
    org=http('POST','/api/v1/organizations',{'name':'Office isolated share '+uuid.uuid4().hex[:8]},a['token'])
    if org['status'] not in (200,201):return record('shared_permissions_setup','FAIL',**brief(org))
    oid=org['body']['data']['id']
    invite=http('POST','/api/v1/organizations/'+oid+'/invite-code',{},a['token'])
    join=http('POST','/api/v1/organizations/join',{'invite_code':invite['body']['data']['invite_code']},other['token'])
    member=http('PUT','/api/v1/organizations/'+oid+'/members/'+str(other['user']['tenant_id']),{'role':'editor'},a['token'])
    share=http('POST','/api/v1/knowledge-bases/'+kb+'/shares',{'organization_id':oid,'permission':'editor'},a['token'])
    if share['status'] not in (200,201):return record('shared_permissions_setup','FAIL',join_http=join['status'],member_http=member['status'],**brief(share))
    sid=share['body']['data']['id']
    r=upload(other,kb,'shared-editor.txt',b'officecanary shared editor '+uuid.uuid4().hex.encode())
    record('shared_editor_normal_upload','PASS' if r['status'] in (200,201) else 'FAIL',**brief(r))
    r=upload(other,kb,'shared-encrypted.docx',encrypted)
    record('shared_editor_encrypted_rejected','PASS' if r['status']==400 and business_code(r)==2400 else 'FAIL',**brief(r))
    changed=http('PUT','/api/v1/knowledge-bases/'+kb+'/shares/'+sid,{'permission':'viewer'},a['token'])
    r=upload(other,kb,'shared-viewer.txt',b'officecanary viewer denied')
    record('shared_viewer_cannot_upload','PASS' if changed['status'] in (200,204) and r['status'] in (403,404) else 'FAIL',**brief(r))
    removed=http('DELETE','/api/v1/knowledge-bases/'+kb+'/shares/'+sid,token=a['token'])
    r=upload(other,kb,'shared-revoked.txt',b'officecanary revoked share')
    record('revoked_share_cannot_upload','PASS' if removed['status'] in (200,204) and r['status'] in (403,404) else 'FAIL',**brief(r))

def main():
    print(json.dumps({'phase':'acceptance_start','source_sha':SHA,'time_utc':datetime.datetime.now(datetime.timezone.utc).isoformat()}),flush=True)
    a=register('owner');other=register('other')
    kb=create_kb(a)
    # The operator is a normal registered local user and owner of their own KB.
    record('dedicated_accounts_and_kb','PASS',kb_id=kb,system_admin=bool(a['user'].get('is_system_admin')))
    private_state=SITE/('browser-session-'+SHA[:9]+'.json')
    if not private_state.exists():private(private_state,{'source_sha':SHA,'token':a['token'],'user':a['user'],'tenant':a.get('tenant') or a.get('active_tenant'),'kb_id':kb,'memberships':a.get('memberships',[])})
    for name in ('normal.docx','normal.xlsx','normal.pptx','sheet-protected.xlsx','modify-protected.docx','plain.doc','plain.xls','plain.ppt','WPS_Spout_test.xlsx'):
        p=ROOT/'samples'/name
        if not p.exists():record('upload_'+name,'NOT_RUN',reason='sample unavailable');continue
        r=upload(a,kb,name,p.read_bytes())
        record('upload_'+name,'PASS' if r['status'] in (200,201) else 'FAIL',**brief(r),sample_sha256=hashlib.sha256(p.read_bytes()).hexdigest())
    # Wait for real asynchronous parsing/indexing; failures remain failures.
    deadline=time.monotonic()+240
    while True:
        pending=sql("SELECT count(*) FROM knowledges WHERE knowledge_base_id='"+kb+"' AND parse_status IN ('pending','processing','finalizing');")
        if pending=='0' or time.monotonic()>deadline:break
        time.sleep(3)
    rows=json.loads(sql("SELECT COALESCE(json_agg(json_build_object('id',id,'title',title,'parse_status',parse_status,'error_message',error_message)),'[]') FROM knowledges WHERE knowledge_base_id='"+kb+"';"))
    record('real_parse_status','PASS' if rows and all(x['parse_status']=='completed' for x in rows) else 'FAIL',documents=rows)
    search=http('POST','/api/v1/knowledge-bases/'+kb+'/hybrid-search',{'query_text':'officecanary normal document','vector_threshold':0,'match_count':20},a['token'])
    record('real_index_retrieval','PASS' if search['status']==200 and search['body'].get('data') else 'FAIL',**brief(search))
    before=snapshot()
    if not SHA.startswith('4ffd7442d'):
        for name in ('drm.xlsx','user-drm.xlsx','drm-disguised.txt'):
            p=ROOT/'samples'/name
            r=upload(a,kb,name,p.read_bytes())
            ok=r['status']==400 and business_code(r)==2400 and 'DRM' in json.dumps(r['body'])
            record('http_drm_'+name,'PASS' if ok else 'FAIL',**brief(r),sample_sha256=hashlib.sha256(p.read_bytes()).hexdigest())
    for name in ('example_password.docx','example_password.xlsx','rc4cryptoapi_password.doc','rc4cryptoapi_password.xls','rc4cryptoapi_password.ppt','ecma376standard_password.docx'):
        p=ROOT/'samples'/name
        r=upload(a,kb,name,p.read_bytes())
        ok=r['status']==400 and business_code(r)==2400
        record('http_encrypted_'+name,'PASS' if ok else 'FAIL',**brief(r),sample_sha256=hashlib.sha256(p.read_bytes()).hexdigest())
    encrypted=(ROOT/'samples/example_password.docx').read_bytes()
    for extras in ({},{'channel':'feishu_drive'},{'channel':'confluence'}):
        r=upload(a,kb,'disguised.txt',encrypted,extras)
        record('http_disguised_'+extras.get('channel','default'),'PASS' if r['status']==400 and business_code(r)==2400 else 'FAIL',**brief(r))
    r=upload(a,kb,'normal.docx',encrypted)
    record('same_filename_encrypted_reupload','PASS' if r['status']==400 and business_code(r)==2400 else 'FAIL',**brief(r),coverage='HTTP same-name upload; ReplaceKnowledgeFile has no deployed caller')
    after=snapshot()
    record('rejection_no_knowledge_chunk_index_file_mutation','PASS' if before==after else 'FAIL',before=before,after=after)
    r=upload(other,kb,'other.txt',b'officecanary unauthorized')
    record('unrelated_account_cannot_upload','PASS' if r['status'] in (403,404) else 'FAIL',**brief(r))
    shared_permissions(a,other,kb,encrypted)
    session=http('POST','/api/v1/sessions',{'title':'Office isolated attachment acceptance'},a['token'])
    if session['status'] in (200,201):
        session_id=session['body']['data']['id']
        for name in ('normal.docx','example_password.docx','user-drm.xlsx'):
            p=ROOT/'samples'/name
            if not p.exists():continue
            result=upload(a,kb,name,p.read_bytes(),path='/api/v1/sessions/'+session_id+'/attachments')
            ok=result['status'] in (200,201,202) if name=='normal.docx' else result['status']==400 and business_code(result)==2400
            record('temporary_chat_attachment_'+name,'PASS' if ok else 'FAIL',**brief(result))
            if name=='normal.docx' and ok:
                attachment_id=result['body']['data']['id'];deadline=time.monotonic()+60
                while time.monotonic()<deadline:
                    state=http('GET','/api/v1/sessions/'+session_id+'/attachments/'+attachment_id,token=a['token'])
                    if state['body'].get('data',{}).get('status') in ('ready','failed'):break
                    time.sleep(1)
                record('temporary_chat_normal_parse','PASS' if state['body'].get('data',{}).get('status')=='ready' else 'FAIL',**brief(state))
    else:record('temporary_chat_session_create','FAIL',**brief(session))
    r=http('POST','/api/v1/mcp-api-keys',{'name':'Office isolated acceptance','client_type':'generic',
        'capabilities':['retrieve','add_content'],'never_expires':True,'personal_scope_mode':'fixed',
        'tenant_scopes':[{'tenant_id':a['user']['tenant_id'],'kb_scope_mode':'selected','knowledge_bases':[{'knowledge_base_id':kb,'source_type':'owned'}]}]},a['token'])
    if r['status'] not in (200,201):
        record('personal_mcp_key_create','FAIL',**brief(r))
    else:
        key=r['body']['data'];token=key.get('token') or key.get('credential',{}).get('token')
        assert token,'credential missing in normal key issuance'
        private(SITE/('mcp-key-'+str(key.get('id',uuid.uuid4()))+'.json'),key)
        args={'knowledge_base_id':kb,'filename':'encrypted.docx','content_base64':base64.b64encode(encrypted).decode(),'idempotency_key':'office-encrypted-'+uuid.uuid4().hex}
        result=mcp(token,'upload_document',args)
        record('personal_mcp_encrypted','PASS' if '2400' in json.dumps(result['body']) else 'FAIL',**brief(result))
        for filename,content in [('clean.txt',b'officecanary MCP clean file'),('eicar.txt',bytes.fromhex('58354f2150254041505b345c505a58353428505e2937434329377d2445494341522d5354414e444152442d414e544956495255532d544553542d46494c452124482b482a'))]:
            args={'knowledge_base_id':kb,'filename':filename,'content_base64':base64.b64encode(content).decode(),'idempotency_key':'office-'+uuid.uuid4().hex}
            result=mcp(token,'upload_document',args)
            error=result['body'].get('result',{}).get('isError',False)
            record('personal_mcp_'+filename,'PASS' if result['status']==200 and error==(filename=='eicar.txt') else 'FAIL',**brief(result))
        for filename,content in [('clean.txt',b'officecanary readonly'),('encrypted.docx',encrypted)]:
            readonly_before=snapshot()
            result=mcp(token,'upload_document',{'knowledge_base_id':kb,'filename':filename,'content_base64':base64.b64encode(content).decode(),'idempotency_key':'readonly-'+uuid.uuid4().hex},'/internal/mcp/isolated/personal')
            denied=result['status'] in (400,403) or result['body'].get('error') or result['body'].get('result',{}).get('isError')
            record('isolated_readonly_mcp_rejects_'+filename,'PASS' if denied else 'FAIL',**brief(result),transport='direct isolated app network; strict read-only protocol boundary')
        if not SHA.startswith('4ffd7442d'):
            result=mcp(token,'upload_document',{'knowledge_base_id':kb,'filename':'drm.xlsx','content_base64':base64.b64encode((ROOT/'samples/user-drm.xlsx').read_bytes()).decode(),'idempotency_key':'drm-'+uuid.uuid4().hex})
            record('personal_mcp_drm','PASS' if 'DRM' in json.dumps(result['body']) and '2400' in json.dumps(result['body']) else 'FAIL',**brief(result))
        key_id=key.get('id') or key.get('credential',{}).get('id')
        revoked=http('DELETE','/api/v1/mcp-api-keys/'+str(key_id),token=a['token'])
        result=mcp(token,'upload_document',{'knowledge_base_id':kb,'filename':'revoked.txt','content_base64':base64.b64encode(b'officecanary revoked').decode(),'idempotency_key':'revoked-'+uuid.uuid4().hex})
        record('revoked_mcp_key_rejected','PASS' if revoked['status'] in (200,204) and result['status'] in (401,403) else 'FAIL',revoke_http=revoked['status'],**brief(result))
    def one(i):
        content=encrypted if i%2==0 else ('officecanary concurrent '+str(i)).encode()
        r=upload(a,kb,('concurrent-'+str(i)+('.docx' if i%2==0 else '.txt')),content)
        return {'i':i,'expected_encrypted':i%2==0,**brief(r)}
    resource_before=resources()
    started=time.monotonic()
    with concurrent.futures.ThreadPoolExecutor(max_workers=6) as pool:requests=list(pool.map(one,range(18)))
    valid=all((x['http_status']==400 and x['response'].get('error',x['response']).get('code')==2400) if x['expected_encrypted'] else x['http_status'] in (200,201) for x in requests)
    duration=round(time.monotonic()-started,3)
    resource_after=resources()
    latencies=sorted(r['elapsed_ms'] for r in requests)
    record('concurrent_18_requests_6_workers','PASS' if valid else 'FAIL',elapsed_seconds=duration,requests=requests,
        latency_ms={'min':latencies[0],'p50':latencies[len(latencies)//2],'p95':latencies[int(len(latencies)*.95)],'max':latencies[-1]},
        resources_before=resource_before,resources_after=resource_after)
    for name in ('broken.docx','cycle.doc','unknown-ole.doc','header-only.xls','unknown.wps'):
        r=upload(a,kb,name,(ROOT/'samples'/name).read_bytes())
        record('abnormal_'+name,'OBSERVED',**brief(r))
    path=SITE/('acceptance-'+datetime.datetime.now(datetime.timezone.utc).strftime('%Y%m%dT%H%M%SZ')+'.json')
    private(path,RESULTS)
    print(json.dumps({'evidence':str(path),'PASS':sum(x['result']=='PASS' for x in RESULTS),'FAIL':sum(x['result']=='FAIL' for x in RESULTS)}))

if __name__=='__main__':main()
