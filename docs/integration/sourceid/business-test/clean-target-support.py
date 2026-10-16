"""Private packaging clean-target primitives. Never print data/credentials."""
import pathlib,subprocess,json,datetime,time,os,hashlib
BASE=pathlib.Path('/home/aqyfzx/ai/knowhub-legacy-clean-target-20261016')
PG='knowhub-legacy-clean-20261016-pg'
DB='knowhub_legacy_clean';VALIDATION='knowhub_legacy_validation';REFERENCE='snapshot79_reference'
PGVOL='knowhub-legacy-clean-20261016-pgdata';FILEVOL='knowhub-legacy-clean-20261016-files'
SOURCE='b2229c012c06ac8fab34b83b8ae0442077bb69bfc5cf065764bf94ca446c8abe'
IMAGE='sha256:2d91336c3f5ba1ba330820801c7bf0573b1c9c45b9280994dd0862d016435286'
PGIMAGE='sha256:d6fb8f50e25ea880d20e3acad6862931f97cb23abf4efa9c6256d2e112ed8de4'
def execute(args,input=None,output=None,timeout=100):
    p=subprocess.run(args,input=input,stdout=output if output is not None else subprocess.PIPE,stderr=subprocess.PIPE,timeout=timeout)
    if p.returncode:raise RuntimeError('controlled_command_failed')
    return p.stdout
def inspect(cid,field):return json.loads(execute(['docker','inspect','--format','{{json '+field+'}}',cid]))
def sql(q,db=DB,source=False):
    if source:
        args=['docker','exec','-i',SOURCE,'sh','-c','exec psql -X -U "$POSTGRES_USER" -d "$POSTGRES_DB" -qAt -v ON_ERROR_STOP=1']
        q="BEGIN READ ONLY; SET LOCAL statement_timeout='15s'; "+q+';ROLLBACK;'
    else:
        assert db in (DB,VALIDATION,REFERENCE,'postgres')
        args=['docker','exec','-i',PG,'psql','-X','-U','clean_owner','-d',db,'-qAt','-v','ON_ERROR_STOP=1']
        q="SET log_statement='none'; SET log_min_error_statement='panic'; SET log_parameter_max_length_on_error=0; SET log_min_messages='panic'; "+q
    return execute(args,input=q.encode()).decode().strip()
def scalar(q,db=DB,source=False):return json.loads(sql(q,db,source))
def counts(db=DB,source=False):
    return {t:int(sql('SELECT COUNT(*) FROM '+t,db,source)) for t in ('users','tenants','tenant_members','organization_tenant_members','knowledge_bases','knowledges','chunks','embeddings','kb_shares','resources','resource_bindings')}
SPECS={
 'users':'id,tenant_id,email,is_active,is_system_admin,can_access_all_tenants,deleted_at',
 'tenants':'id,name,status,deleted_at',
 'tenant_members':'id,user_id,tenant_id,role,status,invited_by,deleted_at',
 'organization_tenant_members':'organization_id,tenant_id,role,representative_user_id,deleted_at',
 'knowledge_bases':'id,tenant_id,creator_id,type,embedding_model_id,summary_model_id,deleted_at',
 'knowledges':'id,tenant_id,knowledge_base_id,type,parse_status,enable_status,embedding_model_id,file_path,file_size,file_hash,deleted_at',
 'chunks':'id,knowledge_id,knowledge_base_id,content,chunk_index,status,deleted_at',
 'embeddings':'id,chunk_id,knowledge_id,knowledge_base_id,dimension,embedding',
 'kb_shares':'id,knowledge_base_id,organization_id,source_tenant_id,permission,shared_by_user_id,deleted_at',
 'resources':'id,handle,tenant_id,storage_backend_id,provider,physical_path,state,size,deleted_at',
 'resource_bindings':'id,resource_id,owner_type,owner_id,relation',
 'models':'id,tenant_id,type,status,deleted_at'}
def columns():
    out={}
    for t,cols in SPECS.items():
        available=set(sql("SELECT column_name FROM information_schema.columns WHERE table_schema='public' AND table_name='"+t+"'",source=True).splitlines())
        out[t]=[c for c in cols.split(',') if c in available];assert out[t]
    return out
def signatures(cols,db=DB,source=False):
    # DB computes digests of authorized noncredential data; never exports row
    # text, vectors, emails, or any digest outside this private process.
    result={}
    for table,fields in cols.items():
        order=fields[0] if fields[0]=='id' else ','.join(fields)
        q="SELECT COALESCE(md5(string_agg(md5(row_to_json(s)::text),'')),'') FROM (SELECT "+','.join(fields)+' FROM '+table+' ORDER BY '+order+') s'
        result[table]=sql(q,db,source)
    return result
def files(volume):
    code="import os,hashlib,json;h=hashlib.sha256();n=b=0\nfor root,dirs,names in os.walk('/scan'):\n dirs.sort()\n for name in sorted(names):\n  p=os.path.join(root,name)\n  if os.path.islink(p):raise RuntimeError('symlink')\n  data=open(p,'rb').read();h.update(os.path.relpath(p,'/scan').encode());h.update(hashlib.sha256(data).digest());n+=1;b+=len(data)\nprint(json.dumps({'files':n,'bytes':b,'digest':h.hexdigest()}))"
    return json.loads(execute(['docker','run','--rm','--network','none','--log-driver','none','--entrypoint','python3','--mount','type=volume,source='+volume+',target=/scan,readonly',IMAGE,'-c',code]))
def restore(path,db):
    assert db in (DB,VALIDATION,REFERENCE)
    with pathlib.Path(path).open('rb') as f:
        p=subprocess.run(['docker','exec','-i',PG,'pg_restore','-U','clean_owner','-d',db,'--clean','--if-exists','--no-owner','--no-acl','--exit-on-error'],stdin=f,stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=180)
    if p.returncode:raise RuntimeError('protected_restore_failed')
def state(**kw):(BASE/'preparation-status.json').write_text(json.dumps(dict(utc=datetime.datetime.now(datetime.timezone.utc).isoformat(),**kw),indent=2)+'\n')
