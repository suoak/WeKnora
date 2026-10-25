"""Upgrade only the existing isolated test project, preserving users and OAuth.

Requires five passing exact-source gates, verified images, a stopped-worker
database snapshot and a successful restore. Original public entry is untouched.
Credentials are read through stdin; reports never include environment values.
"""
import argparse
import base64
import hashlib
import json
import re
import sys
import textwrap
from pathlib import Path, PurePosixPath

import paramiko

REMOTE = r"""
import json,pathlib,subprocess,hashlib,re,datetime,collections
root=pathlib.Path(ROOT);site=root/'isolated-site'
proof=json.loads((site/'preparation.json').read_text());project='knowhub-feishu-v2-'+OLD_SHA[:12]
if proof['project']!=project or root.is_symlink():raise ValueError('isolated_project_mismatch')
compose=['docker','compose','--project-name',project,'--file',str(site/'compose.json')]
def run(args,timeout=30,input=None):return subprocess.run(args,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True,input=input,timeout=timeout)
def query(q):
 p=run(compose+['exec','-T','postgres','psql','-qX','-v','ON_ERROR_STOP=1','-U','postgres','-d','knowhub_feishu_v2_test','-At'],input="BEGIN READ ONLY; SET LOCAL statement_timeout='10s'; "+q+"; COMMIT;")
 if p.returncode:raise ValueError('readonly_query_failed')
 return json.loads(p.stdout.strip() or 'null')
schema=query("SELECT json_object_agg(table_name,names) FROM (SELECT table_name,json_agg(column_name) names FROM information_schema.columns WHERE table_schema='public' GROUP BY table_name) x")
def cols(table):return schema.get(table,[])
def quoted(v):
 if not re.fullmatch('[a-zA-Z0-9_-]{1,64}',str(v)):raise ValueError('unexpected_identifier')
 return "'"+str(v)+"'"
def rows(table,fields,where):
 available=cols(table);fields=[f for f in fields if f in available]
 return query('SELECT coalesce(json_agg(q),\'[]\') FROM (SELECT '+','.join(fields)+' FROM '+table+' WHERE '+where+' LIMIT 100) q')
def mask(v):return hashlib.sha256(str(v).encode()).hexdigest()[:12]
def safe(rows):
 result=[]
 for row in rows:
  value={}
  for k,v in row.items():
   if k=='manifest_key':value[k]=bool(v)
   elif k=='id' or k.endswith('_id') or k in ['scope_id']:value[k]=mask(v) if v else None
   else:value[k]=v
  result.append(value)
 return result
users=rows('users',['id','is_active'],"username='test' AND deleted_at IS NULL")
if len(users)!=1:raise ValueError('expected_test_user_missing')
uid=users[0]['id']
batches=rows('feishu_import_batches',['id','kb_id','auth_mode','created_at'],"owner_id="+quoted(uid)+' ORDER BY created_at DESC')[:5]
bids=','.join(quoted(r['id']) for r in batches) or "''"
items=rows('feishu_import_batch_items',['id','batch_id','ordinal','status','error_category','import_id','retry_count','lease_until','updated_at'], 'batch_id IN ('+bids+') ORDER BY updated_at DESC')
iids=','.join(quoted(r['import_id']) for r in items if r.get('import_id')) or "''"
imports=rows('feishu_personal_imports',['id','kb_id','knowledge_id','resource_type','status','updated_at','error_category'], 'id IN ('+iids+')')
canonical_count=query('SELECT to_json(count(DISTINCT (resource_type,document_token))) FROM feishu_personal_imports WHERE id IN ('+iids+')')
kb_list=','.join(quoted(v['kb_id']) for v in imports) or "''"
wiki_configuration=query("SELECT coalesce(json_agg(q),'[]') FROM (SELECT id AS kb_id,coalesce((indexing_strategy->>'wiki_enabled')::boolean,false) AS wiki_enabled FROM knowledge_bases WHERE id IN ("+kb_list+") ) q")
targets=query("SELECT coalesce(json_agg(q),'[]') FROM (SELECT kb.id,kb.tenant_id,kb.creator_id,"+
 "coalesce(fp.owner_id='"+uid+"',false) AS proven_private_owner,"+
 "EXISTS(SELECT 1 FROM tenant_members tm WHERE tm.user_id='"+uid+"' AND tm.tenant_id=kb.tenant_id AND tm.status='active' AND tm.deleted_at IS NULL) AS active_member "+
 "FROM knowledge_bases kb LEFT JOIN feishu_private_kbs fp ON fp.kb_id=kb.id WHERE kb.name='test001' AND kb.deleted_at IS NULL LIMIT 10) q")
private_dependencies=query("SELECT coalesce(json_agg(q),'[]') FROM (SELECT fp.kb_id,"+
 "(SELECT count(*) FROM knowledges k WHERE k.knowledge_base_id=fp.kb_id AND k.deleted_at IS NULL) AS knowledge_count,"+
 "(SELECT count(*) FROM feishu_import_batches b WHERE b.kb_id=fp.kb_id) AS batch_count,"+
 "(SELECT count(*) FROM feishu_personal_imports m WHERE m.kb_id=fp.kb_id) AS import_count,"+
 "(SELECT count(*) FROM chunks c WHERE c.knowledge_base_id=fp.kb_id AND c.deleted_at IS NULL) AS chunk_count,"+
 "(SELECT count(*) FROM embeddings e WHERE e.knowledge_base_id=fp.kb_id) AS vector_count "+
 "FROM feishu_private_kbs fp WHERE fp.owner_id='"+uid+"' LIMIT 10) q")
attempts=rows('feishu_personal_attempts',['id','import_id','logical_knowledge_id','candidate_version_id','manifest_key','status','retry_after','retry_count','last_error_category','lease_until','created_at','updated_at','manifest_size'], 'import_id IN ('+iids+')')
kids=sorted({r.get('knowledge_id') for r in imports if r.get('knowledge_id')}|{r.get('logical_knowledge_id') for r in attempts if r.get('logical_knowledge_id')})
kids=sorted(set(kids)|{r.get('candidate_version_id') for r in attempts if r.get('candidate_version_id')})
klist=','.join(quoted(x) for x in kids) or "''"
knowledge=rows('knowledges',['id','knowledge_base_id','parse_status','enable_status','summary_status','created_at','updated_at','process_started_at','process_completed_at'], 'id IN ('+klist+')')
ops=rows('task_pending_ops',['id','scope','scope_id','task_type','op','fail_count','enqueued_at','claimed_at'], 'scope_id IN ('+klist+')'+(' OR '+ ' OR '.join("payload::text LIKE '%"+x+"%'" for x in kids) if kids else ''))
chunks=query('SELECT coalesce(json_agg(q),\'[]\') FROM (SELECT knowledge_id,count(*) AS count FROM chunks WHERE knowledge_id IN ('+klist+') GROUP BY knowledge_id) q')
vectors=query('SELECT coalesce(json_agg(q),\'[]\') FROM (SELECT knowledge_id,dimension,count(*) AS count FROM embeddings WHERE knowledge_id IN ('+klist+') GROUP BY knowledge_id,dimension) q')
bm25=[]
for kid in sorted({r.get('knowledge_id') for r in imports if r.get('knowledge_id')}):
 term=query("SELECT to_json(coalesce(substring(content from '[A-Za-z]{4,}'),substring(content from '[一-鿿]{2,8}'))) FROM embeddings WHERE knowledge_id="+quoted(kid)+" AND content IS NOT NULL LIMIT 1")
 if term and re.fullmatch(r'[A-Za-z]{4,}|[\u4e00-\u9fff]{2,8}',term):
  count=query("SELECT to_json(count(*)) FROM embeddings WHERE knowledge_id="+quoted(kid)+" AND content ||| '"+term+"'")
  bm25.append({'knowledge_id':kid,'matching_index_rows':count,'status':'PASS' if count else 'NO_MATCH'})
 else:bm25.append({'knowledge_id':kid,'status':'NO_SAFE_QUERY_TERM'})
logreports={}
for service in []:
 p=run(compose+['logs','--no-color','--since','30m','--tail','1500',service],45)
 lines=(p.stdout+p.stderr).splitlines()
 matching=[line for line in lines if any(x in line for x in [uid]+[r['id'] for r in attempts]+kids)]
 patterns={'reconcile_errors':r'(?i)reconcil.*(error|fail)|recover.*(error|fail)', 'metadata_api':r'operation=(resolve_personal_resource|get_wiki_node)', 'download_export':r'operation=.*(export|download)', 'processing':r'(?i)processdocument|manualprocess|postprocess','auth_error':r'(?i)forbidden|permission|unauthorized|reconnect','index_error':r'(?i)(embed|index).*(error|fail)','db_missing_column':r'(?i)column .* does not exist','not_found':r'(?i)record not found'}
 logreports[service]={'scanned_lines':len(lines),'related_lines':len(matching),'all_log_categories':{k:sum(bool(re.search(v,l)) for l in lines) for k,v in patterns.items()},'related_categories':{k:sum(bool(re.search(v,l)) for l in matching) for k,v in patterns.items()}}
 # Only stable failure identifiers are emitted, never raw logs or document URLs.
 codes=collections.Counter()
 for line in lines:
  for code in re.findall(r'(?:category=|error_category[=: ]+)([a-z_]{3,64})',line):codes[code]+=1
 logreports[service]['failure_categories']=dict(codes)
 known=['permission denied','context deadline exceeded','personal_resource_revision_changed','feishu_invalid_office_download','personal manifest ownership mismatch','empty personal candidate','connection refused','no space left','invalid personal manifest key']
 logreports[service]['known_errors']={v:sum(v in l.lower() for l in lines) for v in known}
 logreports[service]['api_events']=[{'operation':m.group(1),'http_status':int(m.group(2)),'bytes':int(m.group(3))} for l in lines for m in [re.search(r'operation=([a-z_]+) status=(\d+) bytes=(\d+)',l)] if m][-20:]
 health=run(compose+['exec','-T',service,'/bin/sh','-c','id -u; id -g; stat -c "%u:%g:%a" /data/feishu-staging; if test -w /data/feishu-staging; then echo writable; else echo not_writable; fi'])
 logreports[service]['staging_access']=health.stdout.strip().splitlines()

# Read only Redis protocol inspection; credentials remain in memory.
import socket
redisid=run(compose+['ps','--quiet','redis']).stdout.strip()
rrecord=json.loads(run(['docker','inspect',redisid]).stdout)[0]
ip=list(rrecord['NetworkSettings']['Networks'].values())[0]['IPAddress']
secret=re.search(r'^requirepass (.+)$',(site/'redis.conf').read_text(),re.M).group(1)
connection=socket.create_connection((ip,6379),timeout=10);stream=connection.makefile('rb')
def redis(*args):
 parts=[str(a).encode() for a in args];connection.sendall(b'*'+str(len(parts)).encode()+b'\r\n'+b''.join(b'$'+str(len(x)).encode()+b'\r\n'+x+b'\r\n' for x in parts))
 def read():
  line=stream.readline();kind=line[:1];value=line[1:-2]
  if kind==b'+':return value
  if kind==b'-':raise ValueError('redis_readonly_command_failed')
  if kind==b':':return int(value)
  if kind==b'$':
   size=int(value)
   if size<0:return None
   data=stream.read(size);stream.read(2);return data
  if kind==b'*':return [read() for _ in range(int(value))]
  raise ValueError('redis_response_invalid')
 return read()
redis('AUTH',secret)
queue=[];task_errors=[];cursor=0
for page in range(20):
 cursor,keys=redis('SCAN',cursor,'MATCH','*asynq*','COUNT',200)
 for raw in keys:
  key=raw.decode();kind=redis('TYPE',key)
  if kind in [b'list',b'zset']:
   queue.append({'queue_key_hash':mask(key),'kind':kind.decode(),'count':redis('LLEN' if kind==b'list' else 'ZCARD',key)})
  if kind==b'hash' and any(a['id'] in key for a in attempts):
   values=redis('HGETALL',key);values=dict(zip(values[::2],values[1::2]))
   err=values.get(b'error',values.get(b'err',values.get(b'last_err',b''))).decode(errors='replace')
   combined=b' '.join(values.values()).lower()
   proto=values.get(b'msg',b'');offset=0;wire_errors=[]
   def varint():
    global offset
    value=0;shift=0
    while True:
     b=proto[offset];offset+=1;value|=(b&127)<<shift
     if b<128:return value
     shift+=7
   while offset<len(proto):
    tag=varint();field=tag>>3;kind=tag&7
    if kind==0:varint()
    elif kind==2:
     size=varint();part=proto[offset:offset+size];offset+=size
     if field!=2 and b'permission denied' in part.lower():
      text=part.decode(errors='replace')
      wire_errors.append({'field':field,'permission_denied':True,'operation':next((op for op in ['mkdir','open','rename','write','read','stat'] if text.startswith(op+' ')),'other'),
       'staging_path': '/data/feishu-staging' in text, 'tmp_path':'/tmp/' in text,'path_component':next((x for x in ['manifest.json','content','assets'] if x in text),'other')})
    elif kind==1:offset+=8
    elif kind==5:offset+=4
    else:break

   known=['personal_resource_revision_changed_during_fetch','personal_resource_revision_changed','permission denied','context deadline exceeded','feishu_invalid_office_download','invalid personal manifest key','personal manifest ownership mismatch','personal recovery fence rejected','personal attempt fence rejected','personal_resource_revision_changed','personal manifest content mismatch','feishu_html_download_rejected','file exists','context canceled']
   category=next((x for x in known if x.encode() in combined),'unclassified' if err else '')
   task_errors.append({'task_id_hash':mask(key),'status':values.get(b'state',b'').decode(errors='replace'),'error_category':category,'error_fingerprint':mask(err) if err else None,'fields':[v.decode() for v in values.keys()],'wire_error_categories':wire_errors})
 if cursor==b'0':break
connection.close()
logreports['redis']={'queues':queue,'tasks':task_errors}
for service in ['app','worker2']:
 cid=run(compose+['ps','--quiet',service]).stdout.strip()
 process=run(compose+['exec','-T',service,'/bin/sh','-c','for p in /proc/[0-9]*/status; do grep -E "^(Name|Uid|Gid):" "$p" 2>/dev/null; done'])
 logreports[service]={'process_users':process.stdout.strip().splitlines(), 'staging_owner_mode':run(compose+['exec','-T',service,'stat','-c','%u:%g:%a','/data/feishu-staging']).stdout.strip()}


metadata=[]
if APPROVED_LINKS:
 import base64,urllib.request,urllib.error,urllib.parse
 from cryptography.hazmat.primitives.ciphers.aead import AESGCM
 links=[]
 for value in APPROVED_LINKS:
  u=urllib.parse.urlsplit(value)
  if u.scheme!='https' or u.netloc!='ruijie.feishu.cn' or not re.fullmatch('/wiki/[A-Za-z0-9]+',u.path) or u.query or u.fragment:raise ValueError('approved_wiki_url_required')
  links.append(u.path.rsplit('/',1)[1])
 if len(links)>10:raise ValueError('approved_link_limit')
 record=query("SELECT row_to_json(c) FROM (SELECT tokens,expires_at FROM feishu_connections WHERE user_id="+quoted(uid)+") c")
 if not record:metadata=[{'ordinal':i,'error_category':'authorization_missing'} for i in range(len(links))]
 elif datetime.datetime.fromisoformat(record['expires_at'])<=datetime.datetime.now(datetime.timezone.utc):
  metadata=[{'ordinal':i,'error_category':'authorization_expired','api_called':False} for i in range(len(links))]
 else:
  environment=dict(line.split('=',1) for line in (site/'app.env').read_text().splitlines() if '=' in line)
  cipher=record['tokens']
  if not cipher.startswith('enc:v1:'):raise ValueError('encrypted_connection_required')
  raw=base64.urlsafe_b64decode(cipher[7:]+'='*((-len(cipher[7:]))%4))
  credential=json.loads(AESGCM(environment['SYSTEM_AES_KEY'].encode()).decrypt(raw[:12],raw[12:],None))
  if credential.get('owner')!=uid:raise ValueError('connection_owner_mismatch')
  class NoRedirect(urllib.request.HTTPRedirectHandler):
   def redirect_request(self,*args,**kwargs):return None
  opener=urllib.request.build_opener(NoRedirect())
  for i,token in enumerate(links):
   request=urllib.request.Request('https://open.feishu.cn/open-apis/wiki/v2/spaces/get_node?token='+token,headers={'Authorization':'Bearer '+credential['access_token']})
   try:
    with opener.open(request,timeout=15) as response:
     result=json.loads(response.read(1024*1024));code=result.get('code')
     metadata.append({'ordinal':i,'http_status':response.status,'feishu_code':code,'resource_type':result.get('data',{}).get('node',{}).get('obj_type'),'status':'PASS' if code==0 else 'REJECTED'})
   except urllib.error.HTTPError as error:metadata.append({'ordinal':i,'http_status':error.code,'error_category':'feishu_api_rejected'})
   except urllib.error.URLError:metadata.append({'ordinal':i,'error_category':'network_failure'})
  credential.clear();environment.clear()
state={'user_id':uid,'batches':batches,'items':items,'imports':imports,'attempts':attempts,'knowledge':knowledge}
# Store identifiers/status only in the existing private site; do not alter jobs.
path=site/'real-batch-readonly-state.json';path.write_text(json.dumps(state));path.chmod(0o600)
print(json.dumps({'status':'READ_ONLY','canonical_resource_count':canonical_count,'wiki_configuration':safe(wiki_configuration),'private_kb_dependencies':safe(private_dependencies),'selected_target_check':safe(targets),'batches':safe(batches),'items':safe(items),'imports':safe(imports),'attempts':safe(attempts),'knowledge':safe(knowledge),'outbox':safe(ops),'chunks':safe(chunks),'vectors':safe(vectors),'bm25':safe(bm25),'approved_link_metadata':metadata,'logs':logreports}))
"""


def read_json(path):
    raw = path.read_bytes()
    return json.loads(raw.decode('utf-16') if raw.startswith((b'\xff\xfe', b'\xfe\xff')) else raw.decode('utf-8-sig'))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--manifest', type=Path, required=True)
    parser.add_argument('--candidate-manifest', type=Path, required=True)
    parser.add_argument('--release-gates', type=Path)
    parser.add_argument('--test-username')
    parser.add_argument('--approved-links-file',type=Path)
    parser.add_argument('--mode', choices=['prepare', 'apply', 'finish', 'status', 'rollback'], required=True)
    args = parser.parse_args()
    old, new = read_json(args.manifest), read_json(args.candidate_manifest)
    for manifest in [old, new]:
        sha = manifest['source_sha']
        root = PurePosixPath(manifest['source_directory']).parent
        if (not re.fullmatch('[a-f0-9]{40}', sha) or root.parent != PurePosixPath('/mnt/sata0/knowhub-feishu-v2-builds')
                or not re.fullmatch(sha[:12] + r'-[a-f0-9]{8}', root.name)):
            raise SystemExit('invalid source manifest')
    client = paramiko.SSHClient()
    try:
        credential = json.loads(sys.stdin.read(16384))
        if credential.get('host') != '172.30.232.42' or credential.get('user') != 'aqyfzx':
            raise ValueError('unexpected_target')
        client.load_system_host_keys()
        known = Path.home() / '.ssh' / 'known_hosts'
        if known.is_file():
            client.load_host_keys(str(known))
        client.set_missing_host_key_policy(paramiko.RejectPolicy())
        client.connect(credential['host'], username=credential['user'], password=credential['password'],
                       timeout=15, auth_timeout=15, banner_timeout=15, allow_agent=False, look_for_keys=False)
        credential.clear()
        values = {'ROOT': str(PurePosixPath(old['source_directory']).parent), 'CANDIDATE': str(PurePosixPath(new['source_directory']).parent),
                  'OLD_SHA': old['source_sha'], 'SHA': new['source_sha'], 'MODE': args.mode,
                  'GATES': read_json(args.release_gates) if args.release_gates else {}, 'TEST_USERNAME':args.test_username,
                  'APPROVED_LINKS':read_json(args.approved_links_file) if args.approved_links_file else []}
        guarded = 'try:\n' + textwrap.indent(REMOTE, ' ')
        guarded += "\nexcept Exception as error:\n print(json.dumps({'status':'BLOCKED','reason':str(error) if isinstance(error,ValueError) else 'isolated_upgrade_operation_failed','error_type':type(error).__name__}))\n raise SystemExit(2)\n"
        code = base64.b64encode(('\n'.join(k + '=' + repr(v) for k, v in values.items()) + '\n' + guarded).encode()).decode()
        _, out, _ = client.exec_command("python3 -c \"import base64;exec(base64.b64decode('" + code + "'))\"", timeout=600)
        result = json.loads(out.read(65536))
        print(json.dumps(result, indent=2))
        return 2 if result.get('status') == 'BLOCKED' else 0
    except (OSError, ValueError, KeyError, TypeError, paramiko.SSHException) as error:
        print(json.dumps({'status': 'BLOCKED', 'reason': 'verified_isolated_upgrade_unavailable', 'error_type': type(error).__name__}))
        return 2
    finally:
        client.close()


if __name__ == '__main__':
    sys.exit(main())
