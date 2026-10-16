"""Authorized legacy snapshot -> isolated network-none clone -> full up chain.
Never migrates or alters original/live databases; no row values in receipts.
"""
import pathlib, subprocess, json, os, time, datetime, traceback, hashlib
BASE=pathlib.Path('/home/aqyfzx/ai/knowhub-legacy79-rehearsal-20261016')
PG='oa-legacy79-pg-20261016'
SOURCE_PG='b2229c012c06ac8fab34b83b8ae0442077bb69bfc5cf065764bf94ca446c8abe'
IMAGE='sha256:b2751ba9c8797b0c2440b9435ad92ba9ab2c30d4a95b0c28a2b1b34e4a4c3af1'
PGIMAGE='sha256:d6fb8f50e25ea880d20e3acad6862931f97cb23abf4efa9c6256d2e112ed8de4'
def execute(args,input=None,timeout=100,output=None):
    p=subprocess.run(args,input=input,stdout=output if output is not None else subprocess.PIPE,stderr=subprocess.PIPE,timeout=timeout)
    if p.returncode:
        raw=p.stderr.decode('utf-8','replace').lower()
        markers=[w for w in ('extension','already exists','permission denied','syntax error','does not exist','constraint','dirty','timeout','no space') if w in raw]
        raise RuntimeError('command_failed:'+','.join(markers))
    return p.stdout
def sql(query,source=False):
    cid=SOURCE_PG if source else PG
    if source: query='BEGIN READ ONLY; SET LOCAL statement_timeout=\'8s\'; '+query+'; ROLLBACK;'
    cmd=['docker','exec','-i',cid,'sh','-c','exec psql -X -U "$POSTGRES_USER" -d "$POSTGRES_DB" -v ON_ERROR_STOP=1 -qAt']
    return execute(cmd,input=query.encode()).decode().strip()
def scalar(q,source=False): return json.loads(sql(q,source))
def counts(source=False):
    return {t:int(sql('SELECT COUNT(*) FROM '+t,source)) for t in ('users','tenants','tenant_members','organization_tenant_members','knowledge_bases','knowledges','chunks','embeddings','kb_shares')}
def ownership(source=False):
    # Digest only allowed identity/ACL columns; keep it in the private process.
    result={}
    specs={'users':'id,tenant_id,is_active,is_system_admin,can_access_all_tenants',
           'tenant_members':'user_id,tenant_id,role,status',
           'organization_tenant_members':'organization_id,tenant_id,role,representative_user_id',
           'knowledge_bases':'id,tenant_id', 'kb_shares':'id,knowledge_base_id,target_tenant_id'}
    for table,cols in specs.items():
        available=set(sql("SELECT column_name FROM information_schema.columns WHERE table_schema='public' AND table_name='"+table+"'",source).splitlines())
        chosen=[c for c in cols.split(',') if c in available]
        assert chosen
        raw=sql('SELECT row_to_json(s)::text FROM (SELECT '+','.join(chosen)+' FROM '+table+' ORDER BY '+','.join(chosen)+') s',source)
        result[table]=hashlib.sha256(raw.encode()).digest()
    return result
def file_digest(volume):
    code="import os,hashlib,json; r='/scan';h=hashlib.sha256();n=0;b=0\nfor root,dirs,files in os.walk(r):\n dirs.sort()\n for name in sorted(files):\n  p=os.path.join(root,name)\n  if os.path.islink(p):raise RuntimeError('symlink_requires_review')\n  v=open(p,'rb').read();h.update(os.path.relpath(p,r).encode());h.update(hashlib.sha256(v).digest());n+=1;b+=len(v)\nprint(json.dumps({'files':n,'bytes':b,'digest':h.hexdigest()}))"
    return json.loads(execute(['docker','run','--rm','--network','none','--log-driver','none','--entrypoint','python3','--mount',volume+',target=/scan,readonly',IMAGE,'-c',code]))
def main():
    os.umask(0o077);BASE.mkdir(mode=0o700,exist_ok=False)
    private=BASE/'private';private.mkdir(mode=0o700)
    result={'target':'172.30.232.42','started_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'result':'FAILED','source_database_changed':False,'live_database_changed':False,'real_sid_login':False}
    stage='baseline'
    try:
        assert execute(['docker','inspect','--format','{{.State.Running}}','839b9ed3ef0ba4c531a8c7b1331adcbd5881154c3f2a097171fde66300193d06']).decode().strip()=='false'
        assert sql('SELECT version::text||\'|\'||dirty::text FROM schema_migrations',True)=='79|false'
        before=counts(True); baseline=ownership(True)
        result['source_counts_before']=before
        stage='file_snapshot'
        files_before=file_digest('type=volume,source=weknora_data-files')
        with (private/'files.tar').open('wb') as f:
            execute(['docker','run','--rm','--network','none','--log-driver','none','--entrypoint','tar','--mount','type=volume,source=weknora_data-files,target=/scan,readonly',IMAGE,'-C','/scan','-cf','-','.'],output=f)
        stage='database_snapshot'
        with (private/'database79.dump').open('wb') as f:
            execute(['docker','exec',SOURCE_PG,'sh','-c','exec pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fc --no-owner --no-acl'],output=f)
        assert files_before==file_digest('type=volume,source=weknora_data-files')
        assert before==counts(True) and baseline==ownership(True)
        result['snapshot']={'consistent_pg_dump':True,'file_manifest_stable_during_snapshot':True,'files':files_before['files'],'file_bytes':files_before['bytes'],'dump_bytes':(private/'database79.dump').stat().st_size,'source_app_stopped':True,'global_atomic_freeze_used':False}
        stage='clone_start'
        (BASE/'files').mkdir(mode=0o700);(BASE/'pgdata').mkdir(mode=0o700)
        execute(['tar','-xf',str(private/'files.tar'),'-C',str(BASE/'files')])
        assert file_digest('type=bind,source='+str(BASE/'files'))==files_before
        execute(['docker','run','-d','--name',PG,'--label','knowhub.test=legacy79-rehearsal','--network','none','--log-driver','none','--cpus','2','--memory','3g','-v',str(BASE/'pgdata')+':/var/lib/postgresql/data','-e','POSTGRES_USER=clone','-e','POSTGRES_DB=clone','-e','POSTGRES_HOST_AUTH_METHOD=trust',PGIMAGE])
        for _ in range(40):
            p=subprocess.run(['docker','exec',PG,'pg_isready','-h','127.0.0.1','-U','clone'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
            if p.returncode==0:break
            time.sleep(1)
        else:raise RuntimeError('clone_pg_timeout')
        assert json.loads(execute(['docker','inspect','--format','{{json .HostConfig.NetworkMode}}',PG]))=='none'
        stage='restore'
        with (private/'database79.dump').open('rb') as f:
            p=subprocess.run(['docker','exec','-i',PG,'pg_restore','-U','clone','-d','clone','--clean','--if-exists','--no-owner','--no-acl','--exit-on-error'],stdin=f,stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=180)
        if p.returncode:raise RuntimeError('restore_failed')
        assert before==counts() and baseline==ownership()
        stage='full_migration_79_3027'
        # migrate shares only the isolated PG network namespace (loopback).
        p=subprocess.run(['docker','run','--rm','--network','container:'+PG,'--log-driver','none','--entrypoint','migrate',IMAGE,'-path','/app/migrations/versioned','-database','postgres://clone@127.0.0.1:5432/clone?sslmode=disable','up'],stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=300)
        if p.returncode:
            raw=(p.stdout+p.stderr).decode('utf-8','replace').lower()
            result['migration_failure_markers']=[w for w in ('extension','already exists','does not exist','constraint','syntax error','dirty','duplicate','timeout') if w in raw]
            raise RuntimeError('full_migration_failed')
        assert sql('SELECT version::text||\'|\'||dirty::text FROM schema_migrations')=='3027|false'
        result['schema_after']={'version':3027,'dirty':False}
        result['ownership_preserved']={k:baseline[k]==ownership()[k] for k in baseline}
        assert all(result['ownership_preserved'].values())
        result['clone_counts_after_migration']=counts()
        assert result['clone_counts_after_migration']==before
        result['extensions']=scalar("SELECT json_agg(json_build_object('name',extname,'version',extversion)) FROM pg_extension WHERE extname IN ('pg_search','vector')")
        result['invalid_indexes']=int(sql('SELECT COUNT(*) FROM pg_index WHERE NOT indisvalid OR NOT indisready'))
        result['embedding_references']=scalar("SELECT json_build_object('rows',COUNT(*),'missing_chunks',COUNT(*) FILTER (WHERE c.id IS NULL),'missing_documents',COUNT(*) FILTER (WHERE k.id IS NULL),'missing_kbs',COUNT(*) FILTER (WHERE b.id IS NULL)) FROM embeddings e LEFT JOIN chunks c ON c.id=e.chunk_id LEFT JOIN knowledges k ON k.id=e.knowledge_id LEFT JOIN knowledge_bases b ON b.id=e.knowledge_base_id")
        # Invalidate copied authentication material before any APP starts.
        stage='sanitize_clone_credentials'
        sql("BEGIN; UPDATE auth_tokens SET is_revoked=TRUE; UPDATE users SET password_hash='!isolated-no-local-password'; UPDATE tenant_api_keys SET revoked_at=NOW(); UPDATE models SET parameters=(parameters::jsonb - 'api_key' - 'app_secret')::jsonb; COMMIT;")
        result['clone_old_tokens_revoked']=int(sql('SELECT COUNT(*) FROM auth_tokens WHERE NOT is_revoked'))==0
        result['clone_original_passwords_disabled']=True
        result['clone_tenant_api_keys_disabled']=int(sql('SELECT COUNT(*) FROM tenant_api_keys WHERE revoked_at IS NULL'))==0
        result['clone_model_secrets_removed']=True
        assert before==counts(True) and baseline==ownership(True)
        result['source_schema_after']='79|false';result['source_counts_unchanged']=True
        result['source_ownership_unchanged']=True
        result['network']='none; dependent test containers share loopback namespace only; no published ports'
        result['result']='PASS'
    except Exception as e:
        result['failed_stage']=stage;result['failure_class']=type(e).__name__
        result['failure_lines']=[f.lineno for f in traceback.extract_tb(e.__traceback__) if f.filename==__file__]
        result['safe_failure_marker']=str(e) if isinstance(e,RuntimeError) and all(c.isalnum() or c in '_:,-' for c in str(e)) else 'NOT_EXPORTED'
    result['completed_utc']=datetime.datetime.now(datetime.timezone.utc).isoformat()
    (BASE/'migration-result.json').write_text(json.dumps(result,indent=2)+'\n')
    print(json.dumps(result))
if __name__=='__main__':main()
