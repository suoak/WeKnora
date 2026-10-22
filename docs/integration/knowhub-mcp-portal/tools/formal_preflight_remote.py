"""Allowlisted metadata-only environment inspection. Run over SSH stdin.

No deployment, migration, Redis command, model request or source-cloud call.
Every SQL transaction is READ ONLY; no bodies, identities, tokens or URLs.
"""
import datetime,hashlib,json,os,re,subprocess,sys,urllib.parse,urllib.request,urllib.error

mode=sys.argv[1];assert mode in ('test','test-active','production')
out={'observed_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'mode':mode,'read_only':True,
     'body_accessed':False,'model_or_Feishu_cloud_called':False,'changes':False,'errors':[],'commands':[]}

def run(args,timeout=30):
    try:
        p=subprocess.run(args,capture_output=True,timeout=timeout)
        out['commands'].append({'argv':args,'exit_code':p.returncode})
        if p.returncode:raise RuntimeError('command_exit_'+str(p.returncode))
        return p.stdout.decode('utf8','replace')
    except Exception as e:
        out['errors'].append({'operation':args[:3],'error_class':type(e).__name__})
        raise

def env(x):return dict(v.split('=',1)for v in x['Config'].get('Env',[])if'='in v)

def url_origin(v):
    try:
        p=urllib.parse.urlsplit(v);return p.scheme+'://'+(p.hostname or '')+(':'+str(p.port)if p.port else '')
    except ValueError:return 'INVALID'

def inspect(name):return json.loads(run(['docker','inspect',name]))[0]

allc=[json.loads(v)for v in run(['docker','ps','-a','--format','{{json .}}']).splitlines()]
names={v['Names']for v in allc}
if mode=='production':appname,pgname='WeKnora-app','WeKnora-postgres'
else:appname,pgname='knowhub-company-test-app-1','knowhub-company-test-postgres-1'
if mode=='test-active':appname,pgname='knowhub-mvp-p3-app-1','WeKnora-postgres'
if appname not in names and mode=='production':
    # Existing deployments have used both service spellings. Never guess a
    # different project; inspect exact WeKnora app label only.
    candidates=[v['Names']for v in allc if v['Names']in('WeKnora','WeKnora-app-1')]
    if len(candidates)==1:appname=candidates[0]
selected=[v for v in allc if(v['Names'].startswith('WeKnora')if mode=='production'else v['Names'].startswith('knowhub-company'))]
if mode=='test-active':selected=[v for v in allc if v['Names']in ('knowhub-mvp-p3-app-1','knowhub-mvp-p3-frontend-1','knowhub-company-entry-entry-1','WeKnora-postgres','WeKnora-redis','WeKnora-docreader','WeKnora-mcp','WeKnora-neo4j')]
out['services']=[];app=None
for row in selected:
    x=inspect(row['Names']);labels=x['Config'].get('Labels',{})or{}
    item={'name':row['Names'],'image':x['Config']['Image'],'image_id':x['Image'],'running':x['State']['Running'],
          'health':x['State'].get('Health',{}).get('Status'),'started_at':x['State']['StartedAt'],
          'compose_project':labels.get('com.docker.compose.project'),'compose_service':labels.get('com.docker.compose.service'),
          'working_dir':labels.get('com.docker.compose.project.working_dir'),'compose_files':labels.get('com.docker.compose.project.config_files'),
          'ports':x['NetworkSettings']['Ports'],'networks':list(x['NetworkSettings']['Networks']),
          'revision_labels':{k:labels[k]for k in ['org.opencontainers.image.revision','knowhub.source.sha256','knowhub.release']if k in labels},
          'mounts':[{'type':v['Type'],'name':v.get('Name'),'destination':v['Destination'],'rw':v['RW']}for v in x['Mounts']]}
    out['services'].append(item)
    if row['Names']==appname:app=x
assert app is not None,'Pinned APP service not found'
out['app_name']=appname;ae=env(app)
flags=['OIDC_AUTH_ENABLE','SOURCEID_ENABLE','SOURCEID_PROTOCOL_ONLY','SOURCEID_JIT_ENABLE','SOURCEID_CONTRACT_CONFIRMED',
       'SOURCEID_EMAIL_CONTRACT_CONFIRMED','SOURCEID_FORCE_REAUTH_PROMPT_LOGIN','SOURCEID_LOCAL_ACCESS_TTL',
       'FEISHU_LEGACY_SYNC_SUSPENDED','FEISHU_SAFE_INCREMENTAL_ENABLED','AUTO_MIGRATE','AUTO_RECOVER_DIRTY',
       'RETRIEVE_DRIVER','DUCKDB_SKIP_EXTENSION_LOAD','MCP_AUTH_MODE']
out['app_flags']={k:ae.get(k,'UNSET')for k in flags}
out['secret_material_presence']={k:bool(ae.get(k))for k in ['SYSTEM_AES_KEY','SYSTEM_SIGNING_KEY','JWT_SECRET','OIDC_CLIENT_SECRET','SOURCEID_CLIENT_SECRET']}
out['AES_material_validation']={'present':bool(ae.get('SYSTEM_AES_KEY')),'length':len(ae.get('SYSTEM_AES_KEY','')),'backup_material_verified':False,'exported':False}
out['identity_endpoint_origins']={k:url_origin(v)for k,v in ae.items()if k in ['OIDC_ISSUER','OIDC_ISSUER_URL','SOURCEID_ISSUER','OIDC_REDIRECT_URI','SOURCEID_REDIRECT_URI','SOURCEID_BASE_URL','MCP_PUBLIC_URL']}
out['identity_callback_paths']={k:urllib.parse.urlsplit(v).path for k,v in ae.items()if k in ['OIDC_REDIRECT_URI','SOURCEID_REDIRECT_URI']}
try:out['app_binary_sha256']=run(['docker','exec',appname,'sha256sum','/app/WeKnora']).split()[0]
except Exception:out['app_binary_sha256']='UNKNOWN'
out['nginx_routes']=[]
for row in selected:
    if not any(v in row['Names'].lower()for v in ['frontend','nginx','entry']):continue
    p=subprocess.run(['docker','exec',row['Names'],'nginx','-T'],capture_output=True,timeout=20)
    if p.returncode==0:
        lines=[]
        for line in p.stdout.decode('utf8','replace').splitlines():
            s=line.strip()
            if re.match(r'^(listen|server_name|location|proxy_pass|proxy_http_version|proxy_buffering|proxy_request_buffering|proxy_read_timeout|proxy_send_timeout|client_max_body_size)\b',s):lines.append(s)
            elif re.match(r'^proxy_set_header\s+(Authorization|Host|Connection|X-Forwarded-Proto|Mcp-Session-Id)\s+',s):
                name=s.split()[1];value=' '.join(s.split()[2:]);lines.append('proxy_set_header '+name+' '+(value if '$'in value or name!='Authorization'else'[REDACTED]'))
        out['nginx_routes'].append({'container':row['Names'],'nginx_T_exit':0,'safe_directives':lines})
out['host_disk']=run(['df','-h','/','/home/aqyfzx'])
out['listening_tcp']=run(['ss','-ltnH'])
pg=inspect(pgname);pe=env(pg);db=ae.get('DB_NAME')or pe.get('POSTGRES_DB')or'weknora';user=pe.get('POSTGRES_USER','postgres')
out['database_target']={'container':pgname,'name':db,'app_DB_HOST':ae.get('DB_HOST','UNSET'),'not_production_host':mode!='production','data_origin_verified':False}

def query(q):
    statement="BEGIN READ ONLY; SET LOCAL statement_timeout='20s'; SELECT COALESCE(json_agg(row_to_json(inspection)),'[]'::json)FROM("+q+")inspection;ROLLBACK;"
    args=['docker','exec','-i','-u','postgres',pgname,'psql','-X','-w','-U',user,'-d',db,'-qAt','-v','ON_ERROR_STOP=1']
    p=subprocess.run(args,input=statement.encode(),capture_output=True,timeout=30)
    out['commands'].append({'operation':'SQL metadata READ ONLY','query':q,'exit_code':p.returncode})
    if p.returncode:raise RuntimeError('read_only_SQL_failed')
    return json.loads(p.stdout)

schema=query("SELECT table_name,column_name,data_type FROM information_schema.columns WHERE table_schema='public'AND table_name IN('data_sources','sync_logs','knowledge_bases','tenants','models','tenant_portal_configs','users','system_settings','embeddings','feishu_legacy_resources','feishu_legacy_attempts')")
cols={}
for r in schema:cols.setdefault(r['table_name'],set()).add(r['column_name'])
out['schema_shape']=schema
out['schema_version']=query('SELECT version,dirty FROM schema_migrations')
out['extensions']=query("SELECT extname,extversion FROM pg_extension WHERE extname IN('vector','pg_search')")
out['account_counts']=query('SELECT count(*)AS accounts,count(*)FILTER(WHERE is_active)AS active_accounts FROM users')
base="FROM tenant_portal_configs pc JOIN tenants t ON t.id=pc.tenant_id JOIN knowledge_bases kb ON kb.tenant_id=t.id"
valid="pc.status='published'AND t.deleted_at IS NULL AND t.status='active'AND kb.deleted_at IS NULL"
out['portal_counts']=query("SELECT count(DISTINCT t.id)AS published_active_spaces,count(DISTINCT kb.id)AS live_KBs,count(DISTINCT kb.id)FILTER(WHERE kb.type='document')AS document_KBs,count(DISTINCT kb.id)FILTER(WHERE kb.type='document'AND NOT COALESCE(kb.is_temporary,false))AS non_temporary_document_KBs "+base+' WHERE '+valid)
out['portal_inventory']=query("SELECT substr(md5('knowhub-b7-space:'||t.id::text),1,16)AS space_ref,substr(md5('knowhub-b7-kb:'||kb.id),1,16)AS kb_ref,kb.type,kb.is_temporary,kb.deleted_at IS NOT NULL AS kb_deleted,t.deleted_at IS NOT NULL AS space_deleted,t.status AS space_status,pc.status AS portal_status,(SELECT count(*)FROM knowledges k WHERE k.knowledge_base_id=kb.id AND k.tenant_id=kb.tenant_id AND k.deleted_at IS NULL)AS documents "+base)
out['model_registry']=query("SELECT substr(md5(id),1,16)AS model_ref,type,source,status,is_default,parameters::jsonb->'embedding_parameters'->>'dimension'AS dimension,COALESCE(parameters::jsonb->>'api_key','')<>''AS credential_present,COALESCE(parameters::jsonb->>'base_url','')<>''AS endpoint_present FROM models WHERE deleted_at IS NULL")
out['KB_model_bindings']=query("SELECT substr(md5('knowhub-b7-kb:'||kb.id),1,16)AS kb_ref,substr(md5(kb.embedding_model_id),1,16)AS model_ref,m.deleted_at IS NOT NULL AS model_deleted,m.status AS model_status,m.parameters::jsonb->'embedding_parameters'->>'dimension'AS dimension "+base+" LEFT JOIN models m ON m.id=kb.embedding_model_id WHERE "+valid)
if 'dimension'in cols.get('embeddings',set()):out['vector_dimensions']=query('SELECT dimension,count(*)AS rows FROM embeddings GROUP BY dimension')
out['vector_index_dimensions']=query("SELECT indexname,substring(indexdef from 'halfvec\\(([0-9]+)\\)')AS halfvec_dimension FROM pg_indexes WHERE schemaname='public'AND tablename='embeddings'AND indexdef ILIKE '%hnsw%'")
if 'data_sources'in cols:
    extra=",legacy_access_state"if'legacy_access_state'in cols['data_sources']else",'SCHEMA_UNAVAILABLE'::text AS legacy_access_state"
    out['feishu_sources']=query("SELECT substr(md5('knowhub-formal-source:'||id),1,16)AS source_ref,type,status,sync_mode,sync_deletions,last_sync_at,EXTRACT(EPOCH FROM(now()-last_sync_at))/3600 AS hours_since_sync,deleted_at IS NOT NULL AS deleted,COALESCE(error_message,'')<>''AS has_error,last_sync_cursor IS NOT NULL AS has_cursor,md5(COALESCE(last_sync_cursor::text,''))AS cursor_fingerprint"+extra+" FROM data_sources WHERE type IN('feishu','feishu_drive')")
if {'data_source_id','status'}.issubset(cols.get('sync_logs',set())):
    out['feishu_sync_log_counts']=query("SELECT substr(md5('knowhub-formal-source:'||l.data_source_id),1,16)AS source_ref,l.status,count(*)AS rows FROM sync_logs l JOIN data_sources d ON d.id=l.data_source_id WHERE d.type IN('feishu','feishu_drive')GROUP BY l.data_source_id,l.status")
if 'feishu_legacy_resources'in cols:
    fields=[v for v in ['state','access_state','status']if v in cols['feishu_legacy_resources']]
    if fields:out['legacy_resource_states']=query('SELECT '+','.join(fields)+',count(*)AS rows FROM feishu_legacy_resources GROUP BY '+','.join(fields))
if 'system_settings'in cols:
    out['safe_runtime_settings']=query("SELECT key,value FROM system_settings WHERE key IN('auth.oidc.enabled','auth.sourceid.enabled','feishu.legacy_sync_suspended','feishu.safe_incremental_enabled')AND COALESCE(is_secret,false)=false")
print(json.dumps(out,indent=2))
