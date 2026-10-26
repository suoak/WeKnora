"""Read-only Hybrid search of the test owner's existing real imports.

The supplied personal credential stays in stdin. No login is minted and no
document text, query term, URL, token or raw provider response is reported.
"""
import argparse
import base64
import json
import re
import sys
from pathlib import Path, PurePosixPath

import paramiko

REMOTE = r'''
import json,pathlib,subprocess,re,hashlib,urllib.request,urllib.error
secret=json.loads(__import__('sys').stdin.read(16384))['mcp_token']
root=pathlib.Path(ROOT);site=root/'isolated-site';project='knowhub-feishu-v2-'+SHA[:12]
if root.is_symlink() or json.loads((site/'preparation.json').read_text())['project']!=project:raise ValueError('isolated_project_mismatch')
compose=['docker','compose','--project-name',project,'--file',str(site/'compose.json')]
app_id=subprocess.run(compose+['ps','--quiet','app'],stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True,timeout=15).stdout.strip()
app_record=json.loads(subprocess.run(['docker','inspect',app_id],stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True,timeout=15).stdout)[0]
deployed_sha=app_record['Config']['Labels'].get('knowhub.source-sha')
if not re.fullmatch('[a-f0-9]{40}',deployed_sha or ''):raise ValueError('source_provenance_missing')
def query(sql):
 p=subprocess.run(compose+['exec','-T','postgres','psql','-qX','-v','ON_ERROR_STOP=1','-U','postgres','-d','knowhub_feishu_v2_test','-At'],input='BEGIN READ ONLY; SET LOCAL statement_timeout=\'10s\'; '+sql+'; COMMIT;',text=True,stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=20)
 if p.returncode:raise ValueError('readonly_query_failed')
 return json.loads(p.stdout.strip() or 'null')
state=json.loads((site/'real-batch-readonly-state.json').read_text());uid=state['user_id']
if not re.fullmatch('[a-zA-Z0-9-]{1,64}',uid):raise ValueError('invalid_owner_identifier')
tenants=query("SELECT coalesce(json_agg(tenant_id),'[]') FROM tenant_members WHERE user_id='"+uid+"' AND status='active' AND deleted_at IS NULL")
imports=state['imports'];kids={v['knowledge_id']:v['kb_id'] for v in imports if v.get('knowledge_id')};python_inputs=[]
class NoRedirect(urllib.request.HTTPRedirectHandler):
 def redirect_request(self,*args,**kwargs):return None
opener=urllib.request.build_opener(NoRedirect())
def rpc(tenant,method,params):
 request=urllib.request.Request('http://127.0.0.1:28480/api/v1/mcp/personal',method='POST',headers={'Authorization':'Bearer '+secret,'X-Tenant-ID':str(tenant),'Content-Type':'application/json','Accept':'application/json, text/event-stream'},data=json.dumps({'jsonrpc':'2.0','id':1,'method':method,'params':params}).encode())
 try:
  with opener.open(request,timeout=45) as response:
   raw=response.read(2*1024*1024+1)
   if len(raw)>2*1024*1024:raise ValueError('response_limit')
   text=raw.decode()
   result=json.loads(text) if text.lstrip().startswith('{') else next(json.loads(line[5:].strip()) for line in text.splitlines() if line.startswith('data:'))
   return response.status,result
 except urllib.error.HTTPError as error:return error.code,{}
checks=[]
case_path=site/'real-table-query-cases.json'
table_cases=json.loads(case_path.read_text()) if case_path.is_file() and not case_path.is_symlink() else []
def matching_rows(value,kid):
 if isinstance(value,dict):
  if value.get('knowledge_id')==kid:return [value]
  result=[]
  for child in value.values():result.extend(matching_rows(child,kid))
  return result
 if isinstance(value,list):return [row for child in value for row in matching_rows(child,kid)]
 if isinstance(value,str) and value.lstrip().startswith(('{','[')):
  try:return matching_rows(json.loads(value),kid)
  except ValueError:return []
 return []
for tenant in tenants:
 status,result=rpc(tenant,'initialize',{'protocolVersion':'2024-11-05','capabilities':{},'clientInfo':{'name':'knowhub-real-owner-probe','version':'1'}})
 if status!=200 or result.get('error'):
  checks.append({'operation':'initialize','http_status':status,'status':'DENIED'});continue
 status,catalog=rpc(tenant,'tools/call',{'name':'list_knowledge_bases','arguments':{}})
 serialized=json.dumps(catalog)
 for kid,kb in kids.items():
  if not re.fullmatch('[a-zA-Z0-9-]{1,64}',kid):raise ValueError('invalid_knowledge_identifier')
  masked=hashlib.sha256(kid.encode()).hexdigest()[:12]
  if kb not in serialized:
   checks.append({'knowledge_id_hash':masked,'operation':'catalog','status':'OUT_OF_CREDENTIAL_SCOPE'});continue
  term=query("SELECT to_json(coalesce(substring(content from '[A-Za-z]{4,}'),substring(content from '[一-鿿]{2,8}'))) FROM embeddings WHERE knowledge_id='"+kid+"' AND content IS NOT NULL LIMIT 1")
  if not term or not re.fullmatch(r'[A-Za-z]{4,}|[\u4e00-\u9fff]{2,8}',term):raise ValueError('no_safe_search_term')
  cases=[c for c in table_cases if c['kid']==kid] or [{'query':term,'expected':[]}]
  for case_ordinal,case in enumerate(cases):
   python_inputs.append({'tenant':tenant,'kid':kid,'kb':kb,'query':case['query'],'expected':case['expected'],'case_ordinal':case_ordinal})
   for mode in ['keyword','semantic','hybrid']:
    status,found=rpc(tenant,'tools/call',{'name':'search_knowledge','arguments':{'query':case['query'],'mode':mode,'knowledge_base_ids':[kb],'limit':20}})
    hits=matching_rows(found,kid)
    correct=any(all(v in json.dumps(hit,ensure_ascii=False) for v in case['expected']) for hit in hits)
    checks.append({'knowledge_id_hash':masked,'case_ordinal':case_ordinal,'operation':mode+'_search','http_status':status,'authorized_source_hit':bool(hits),'expected_value_count':len(case['expected']),'same_hit_contains_expected_values':correct,'status':'PASS' if status==200 and correct and not found.get('error') and not found.get('result',{}).get('isError') else 'NO_CORRECT_AUTHORIZED_HIT'})
logs={}
for service in ['app','worker2']:
 p=subprocess.run(compose+['logs','--no-color','--since','5m','--tail','1500',service],stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True,timeout=20)
 text=p.stdout+p.stderr
 known=['column','does not exist','embedding','no model','model not found','retrieve','SQLSTATE','dimension','permission denied','Unauthorized','401','403','connection refused','context deadline','invalid','error','failed']
 logs[service]={word:len(re.findall(re.escape(word),text,re.I)) for word in known}
 logs[service]['sqlstates']=sorted(set(re.findall(r'SQLSTATE ([A-Z0-9]{5})',text)))
 logs[service]['query_dimensions']=re.findall(r'Vector retrieval: dim=(\d+)',text)[-10:]
 logs[service]['dimension_mismatch_count']=text.count('dimension does not match the bound model')
shapes=[]
for kb in set(kids.values()):
 if not re.fullmatch('[a-zA-Z0-9-]{1,64}',kb):raise ValueError('invalid_kb_identifier')
 shapes.append({'kb_hash':hashlib.sha256(kb.encode()).hexdigest()[:12],
  'bound_model':query("SELECT row_to_json(q) FROM (SELECT m.is_builtin,m.tenant_id=kb.tenant_id AS same_tenant,m.type,m.status,(m.parameters->'embedding_parameters'->>'dimension')::int AS dimension FROM knowledge_bases kb JOIN models m ON m.id=kb.embedding_model_id WHERE kb.id='"+kb+"') q"),
  'dimensions':query("SELECT coalesce(json_agg(q),'[]') FROM (SELECT dimension,count(*) AS count FROM embeddings WHERE knowledge_base_id='"+kb+"' GROUP BY dimension) q")})
secret=None
probe_path=site/'real-python-mcp-input.json'
probe_path.write_text(json.dumps(python_inputs));probe_path.chmod(0o600)
print(json.dumps({'status':'CHECKED','deployed_source_sha':deployed_sha,'qualification':'real_owner_personal_go_mcp','checks':checks,'index_shapes':shapes,'log_categories':logs,'imports_created':False,'python_mcp':'NOT_EXECUTED','portal':'NOT_EXECUTED'}))
'''


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--manifest',type=Path)
    parser.add_argument('--python-adapter',action='store_true')
    parser.add_argument('--include-python',action='store_true')
    args=parser.parse_args()
    if args.python_adapter:
        return python_adapter()
    if not args.manifest:parser.error('--manifest required')
    raw=args.manifest.read_bytes()
    manifest=json.loads(raw.decode('utf-16') if raw.startswith((b'\xff\xfe',b'\xfe\xff')) else raw.decode('utf-8-sig'))
    sha=manifest['source_sha'];root=PurePosixPath(manifest['source_directory']).parent
    if not re.fullmatch('[a-f0-9]{40}',sha) or root.parent!=PurePosixPath('/mnt/sata0/knowhub-feishu-v2-builds'):raise SystemExit('invalid_manifest')
    credential=json.loads(sys.stdin.read(16384));client=paramiko.SSHClient()
    try:
        if credential.get('host')!='172.30.232.42' or credential.get('user')!='aqyfzx':raise ValueError('unexpected_host')
        client.load_system_host_keys();client.load_host_keys(str(Path.home()/'.ssh'/'known_hosts'))
        client.set_missing_host_key_policy(paramiko.RejectPolicy())
        client.connect(credential['host'],username=credential['user'],password=credential['password'],timeout=15,allow_agent=False,look_for_keys=False)
        code=base64.b64encode(('ROOT='+repr(str(root))+'\nSHA='+repr(sha)+'\n'+REMOTE).encode()).decode()
        incoming,out,_=client.exec_command('python3 -c "import base64;exec(base64.b64decode(\''+code+'\'))"',timeout=180)
        mcp_token=credential['mcp_token']
        incoming.write(json.dumps({'mcp_token':mcp_token}));incoming.channel.shutdown_write();credential.clear()
        report=out.read(65536)
        if out.channel.recv_exit_status():raise ValueError('scoped_probe_failed')
        result=json.loads(report)
        if args.include_python:
            import subprocess
            with client.open_sftp() as transfer:
                with transfer.open(str(root/'isolated-site'/'real-python-mcp-input.json')) as source:
                    items=json.loads(source.read(65536))
            process=subprocess.run(['uv','run','--no-project','--python','3.13','--with','mcp>=2,<3',
                '--with','requests','--with','starlette','--with','uvicorn','--with','paramiko','python',str(Path(__file__).resolve()),
                '--python-adapter'],input=json.dumps({'token':mcp_token,'items':items}),text=True,
                stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=120)
            result['python_mcp']=json.loads(process.stdout) if process.returncode==0 else {
                'status':'BLOCKED','reason':'python_adapter_failed',
                'error_classes':re.findall(r'\b([A-Za-z]+Error):',process.stderr),
                'missing_module':re.findall(r"No module named '([A-Za-z0-9_.]+)'",process.stderr)}
        mcp_token=None
        print(json.dumps(result,indent=2))
    except Exception as error:
        print(json.dumps({'status':'BLOCKED','reason':str(error) if isinstance(error,ValueError) else 'scoped_probe_failed','error_type':type(error).__name__}))
        return 2
    finally:client.close()
    return 0


def python_adapter():
    import hashlib
    import logging
    logging.disable(logging.CRITICAL)
    sys.path.insert(0,str(Path(__file__).resolve().parents[2]/'mcp-server'))
    import weknora_mcp_server as adapter
    payload=json.loads(sys.stdin.read(65536));checks=[]
    def matching_rows(value,kid):
        if isinstance(value,dict):
            if value.get('knowledge_id')==kid:return [value]
            return [row for child in value.values() for row in matching_rows(child,kid)]
        if isinstance(value,list):return [row for child in value for row in matching_rows(child,kid)]
        return []
    for row in payload['items']:
        adapter.WEKNORA_TENANT_ID=str(row['tenant'])
        adapter.client=adapter.WeKnoraClient('http://172.30.232.42/api/v1',payload['token'])
        try:
            result=adapter.hybrid_search(row['kb'],row['query'],vector_threshold=-1,keyword_threshold=0,match_count=20)
            hits=matching_rows(result,row['kid'])
            hit=any(all(v in json.dumps(found,ensure_ascii=False) for v in row.get('expected',[])) for found in hits)
            checks.append({'knowledge_id_hash':hashlib.sha256(row['kid'].encode()).hexdigest()[:12],
                'case_ordinal':row.get('case_ordinal'),'expected_value_count':len(row.get('expected',[])),
                'same_hit_contains_expected_values':hit,
                'status':'PASS' if hit else 'NO_AUTHORIZED_HIT'})
        except Exception as error:
            checks.append({'knowledge_id_hash':hashlib.sha256(row['kid'].encode()).hexdigest()[:12],
                'status':'BLOCKED','error_type':type(error).__name__})
    payload.clear()
    print(json.dumps({'qualification':'actual_python_mcp_hybrid_tool_real_owner','checks':checks}))
    return 0


if __name__=='__main__':sys.exit(main())
