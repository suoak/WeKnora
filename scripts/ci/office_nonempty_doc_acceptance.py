"""Nonempty public legacy DOC control, pinned to an Apache POI Git blob."""
import base64,hashlib,json,stat,urllib.request
from office_test_remote import ROOT,REMOTE_ROOT,connect,execute
BLOB='ac0384ac25269d12d778a0d65ce48e74e0541462'
PRIVATE=ROOT/'docs/qa/office-test-deployment/private'
request=urllib.request.Request('https://api.github.com/repos/apache/poi/git/blobs/'+BLOB,headers={'User-Agent':'KnowHub-isolated-acceptance'})
with urllib.request.urlopen(request,timeout=30) as response:metadata=json.loads(response.read(1024*1024))
data=base64.b64decode(metadata['content']);assert len(data)==19456
assert hashlib.sha1(b'blob '+str(len(data)).encode()+b'\0'+data).hexdigest()==BLOB
provenance={'repository':'https://github.com/apache/poi','path':'test-data/document/simple.doc','git_blob':BLOB,
    'sha256':hashlib.sha256(data).hexdigest(),'bytes':len(data),'license':'Apache-2.0; https://github.com/apache/poi/blob/trunk/LICENSE'}
(PRIVATE/'samples/apache-simple.doc').write_bytes(data)
with connect() as client:
    with client.open_sftp() as sftp:
        target=REMOTE_ROOT+'/samples/apache-simple.doc'
        try:existing=sftp.lstat(target);assert stat.S_ISREG(existing.st_mode) and existing.st_size==len(data)
        except FileNotFoundError:
            with sftp.open(target,'wx') as f:f.write(data)
            sftp.chmod(target,0o600)
    code='ROOT='+repr(REMOTE_ROOT)+'\nPROVENANCE='+repr(provenance)+'\n'+r'''
import datetime,json,pathlib,runpy,time
n=runpy.run_path(ROOT+'/office_site_acceptance.py');assert n['SHA']=='668d067ec75a2a351ace4f7f0797be77af2b24ae'
site=pathlib.Path(ROOT)/'site';kb=json.loads((site/'acceptance-kb-668d067ec.json').read_text())['id'];account=n['register']('owner')
p=pathlib.Path(ROOT)/'samples/apache-simple.doc'
r=n['upload'](account,kb,p.name,p.read_bytes());assert r['status'] in (200,201),r['body']
kid=r['body']['data']['id'];deadline=time.monotonic()+90
while time.monotonic()<deadline:
 status=n['sql']("SELECT parse_status FROM knowledges WHERE id='"+kid+"';")
 if status not in ('pending','processing','finalizing'):break
 time.sleep(2)
chunks=int(n['sql']("SELECT count(*) FROM chunks WHERE knowledge_id='"+kid+"';"))
indices=int(n['sql']("SELECT count(*) FROM embeddings WHERE knowledge_id='"+kid+"';"))
search=n['http']('POST','/api/v1/knowledge-bases/'+kb+'/hybrid-search',{'query_text':'simple','match_count':50,'vector_threshold':0},account['token'])
found=any(x.get('knowledge_id')==kid for x in search['body'].get('data',[]))
result={'case':'nonempty_legacy_doc_parse_and_retrieve','result':'PASS' if status=='completed' and chunks>0 and indices>0 and found else 'FAIL',
 'source_sha':n['SHA'],'executed_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'sample':PROVENANCE,
 'knowledge_id':kid,'http_status':r['status'],'parse_status':status,'chunks':chunks,'indices':indices,'retrieval_http':search['status'],'retrieval_found':found}
(site/'legacy-doc-control.json').write_text(json.dumps(result,indent=2));print(json.dumps(result))
'''
    raise SystemExit(execute(client,code,150))
