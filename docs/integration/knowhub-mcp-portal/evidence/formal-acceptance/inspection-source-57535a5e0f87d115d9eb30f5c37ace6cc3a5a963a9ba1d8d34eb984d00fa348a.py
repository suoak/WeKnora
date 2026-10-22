"""Metadata supplement; HTTP probes never begin a SID login or model call."""
import datetime,hashlib,json,re,subprocess,sys,urllib.parse,urllib.request,urllib.error
mode=sys.argv[1];assert mode in ('test','test-active','production')
out={'observed_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'read_only':True,'mode':mode,'changes':False,'cloud_calls':False}
def run(args):
 p=subprocess.run(args,capture_output=True,timeout=30)
 assert p.returncode==0,'metadata command failed'
 return p.stdout.decode()
def inspect(n):return json.loads(run(['docker','inspect',n]))[0]
def env(c):return dict(v.split('=',1)for v in c['Config']['Env']if'='in v)
app='knowhub-company-test-app-1'if mode=='test'else'WeKnora-app';pg='knowhub-company-test-postgres-1'if mode=='test'else'WeKnora-postgres'
if mode=='test-active':app='knowhub-mvp-p3-app-1'
x=inspect(app);a=env(x);p=env(inspect(pg));db=a.get('DB_NAME')or p.get('POSTGRES_DB')or'weknora'
def query(sql):
 statement="BEGIN READ ONLY;SET LOCAL statement_timeout='15s';SELECT COALESCE(json_agg(row_to_json(x)),'[]'::json)FROM("+sql+")x;ROLLBACK;"
 r=subprocess.run(['docker','exec','-i','-u','postgres',pg,'psql','-X','-w','-U',p.get('POSTGRES_USER','postgres'),'-d',db,'-qAt','-v','ON_ERROR_STOP=1'],input=statement.encode(),capture_output=True,timeout=25)
 assert r.returncode==0,'read only query failed'
 return json.loads(r.stdout)
def public_url(raw):
 try:
  u=urllib.parse.urlsplit(raw);return u.scheme+'://'+(u.hostname or'')+(':'+str(u.port)if u.port else'')+u.path
 except ValueError:return'INVALID'
out['identity_config']={k:public_url(a[k])for k in ['OIDC_AUTH_ISSUER_URL','OIDC_AUTH_DISCOVERY_URL','OIDC_AUTH_REDIRECT_URI','OIDC_AUTH_AUTHORIZATION_ENDPOINT','OIDC_AUTH_TOKEN_ENDPOINT','OIDC_AUTH_USER_INFO_ENDPOINT','OIDC_AUTH_JWKS_URI']if a.get(k)}
out['identity_credential_presence']={k:bool(a.get(k))for k in ['OIDC_AUTH_CLIENT_ID','OIDC_AUTH_CLIENT_SECRET']}
out['identity_env_only_not_live_login']=True
out['model_endpoint_metadata']=[]
for row in query("SELECT type,source,status,parameters::jsonb->>'base_url' AS endpoint,COALESCE(parameters::jsonb->>'api_key','')<>''AS credential_present FROM models WHERE deleted_at IS NULL"):
 u=urllib.parse.urlsplit(row.pop('endpoint')or'');row['endpoint_origin']=u.scheme+'://'+(u.hostname or'')+(':'+str(u.port)if u.port else'');out['model_endpoint_metadata'].append(row)
out['identity_table_counts']={}
tables=query("SELECT table_name FROM information_schema.tables WHERE table_schema='public'AND(table_name LIKE '%sourceid%'OR table_name LIKE '%oidc%')")
for row in tables:
 t=row['table_name'];assert re.fullmatch(r'[a-z0-9_]+',t)
 out['identity_table_counts'][t]=query('SELECT count(*)AS rows FROM '+t)[0]['rows']
if mode=='production':
 out['changed_KB_metadata']=query("SELECT substr(md5('knowhub-b7-kb:'||kb.id),1,16)AS kb_ref,kb.deleted_at,kb.updated_at,(SELECT count(*)FROM knowledges k WHERE k.knowledge_base_id=kb.id AND k.deleted_at IS NOT NULL)AS deleted_documents,(SELECT count(*)FROM knowledges k WHERE k.knowledge_base_id=kb.id AND k.deleted_at IS NULL)AS live_documents FROM knowledge_bases kb WHERE substr(md5('knowhub-b7-kb:'||kb.id),1,16)='c2edb6e0b65ad3d9'")
else:
 out['http_probes']=[]
 for port in [80,8080,18091]:
  for path in ['/health','/api/v1/auth/oidc/config','/api/v1/mcp/personal']:
   url='http://127.0.0.1:'+str(port)+path
   try:
    with urllib.request.urlopen(url,timeout=4)as r:status=r.status
    out['http_probes'].append({'url':url,'status':status})
   except urllib.error.HTTPError as e:out['http_probes'].append({'url':url,'status':e.code})
   except OSError as e:out['http_probes'].append({'url':url,'error_class':type(e).__name__})
 entry='knowhub-company-entry-entry-1'
 r=subprocess.run(['docker','exec',entry,'nginx','-T'],capture_output=True,timeout=20)
 out['entry_nginx_T_exit']=r.returncode
 out['entry_safe_directives']=[m.group(0)for m in re.finditer(r'\b(?:listen|server_name|proxy_pass|proxy_http_version|proxy_buffering|proxy_read_timeout)\s+[^;{}]+;',r.stdout.decode('utf8','replace'))]if r.returncode==0 else[]
 out['entry_literal_upstreams']=sorted(set(re.findall(r'\b[a-zA-Z0-9_.-]+:[0-9]{2,5}\b',r.stdout.decode('utf8','replace'))))if r.returncode==0 else[]
 out['entry_network_members']=[]
 for name in inspect(entry)['NetworkSettings']['Networks']:
  net=json.loads(run(['docker','network','inspect',name]))[0]
  out['entry_network_members'].append({'network':name,'members':[{'name':v['Name'],'ip':v.get('IPv4Address')}for v in net.get('Containers',{}).values()]})
 # Only the existing frozen candidate is inspected, never restarted.
 candidate='knowhub-v1-wiki-dynamic-c2f260317fc1-app-1';ci=inspect(candidate)
 out['frozen_candidate']={'app_image_id':ci['Image'],'running':ci['State']['Running'],'binary_sha256':run(['docker','exec',candidate,'sha256sum','/app/WeKnora']).split()[0]}
 frontend=inspect('knowhub-v1-wiki-dynamic-c2f260317fc1-frontend-1');out['frozen_candidate']['frontend_image_id']=frontend['Image']
 source='/home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/c2f260317fc151fb/source.tar.gz'
 out['frozen_candidate']['source_archive_sha256']=run(['sha256sum',source]).split()[0]
 out['test_backup_evidence']={'latest_current_shared_backup':'UNKNOWN','restore_on_3037':'NOT_EXECUTED_ON_SHARED_TARGET','AES_escrow_verified':False,'no_backup_or_key_export':True}
print(json.dumps(out,indent=2))
