"""Approved APP-only immutable-image deployment; no migration or database switch.
EXPECTED, LOCAL_FIXTURE and reviewed business HTTP assertions are injected by
the private packaging-only controller. Stdout contains safe receipt fields only.
"""
import fcntl,os,shutil,time,urllib.error,urllib.request,yaml,traceback,hashlib
LIVE=pathlib.Path('/home/aqyfzx/ai/knowhub-sourceid-business-test-db1b2cd05')
ROOT=pathlib.Path('/home/aqyfzx/ai/knowhub-allroles-035f35e1d')
PROJECT='knowhub-sourceid-business'
NEW='sha256:2d91336c3f5ba1ba330820801c7bf0573b1c9c45b9280994dd0862d016435286'
OLD='sha256:b2751ba9c8797b0c2440b9435ad92ba9ab2c30d4a95b0c28a2b1b34e4a4c3af1'
SHA='035f35e1d388a2f02615d35cf826bbdcc22e56b0'
PIN=ROOT/'app-release-pin.yml';ROLLBACK=ROOT/'app-rollback-pin.yml'
opener=urllib.request.build_opener(urllib.request.ProxyHandler({}))
no_redirect=urllib.request.build_opener(urllib.request.ProxyHandler({}),type('NoRedirect',(urllib.request.HTTPRedirectHandler,),{'redirect_request':lambda *args:None})())
VERIFY_LOG_SINCE=datetime.datetime.now(datetime.timezone.utc).isoformat()
def execute(args,timeout=120,input=None,output=None):
    p=subprocess.run(args,input=input,stdout=output if output is not None else subprocess.PIPE,stderr=subprocess.PIPE,timeout=timeout)
    if p.returncode:raise RuntimeError('controlled_command_failed')
    return p.stdout
def snapshot():
    ids=execute(['docker','ps','-a','--filter','label=com.docker.compose.project='+PROJECT,'--format','{{.ID}}']).decode().split()
    return {x['service']:x for x in (metadata(cid) for cid in ids)}
def canon(items):
    return sorted([dict(x,mounts=sorted(x['mounts'],key=lambda v:v['Destination'])) for x in items],key=lambda v:v['id'])
def flags(cid):
    out={}
    for key in EXPECTED['flags']:
        val=execute(['docker','exec',cid,'sh','-c','printf "%s" "${'+key+'-NOT_SET}"']).decode()
        assert val in ('true','false','oauth_form','external','NOT_SET');out[key]=val
    return out
def dbtarget(cid):
    # Private comparison only. No DB credentials or identifying configuration
    # values appear in the receipt, logs or command arguments.
    code="import os,json;print(json.dumps({k:os.getenv(k) for k in ('DB_HOST','DB_PORT','DB_NAME','DB_USER','DB_DRIVER')}))"
    return json.loads(execute(['docker','exec',cid,'python3','-c',code]))
def sql(pg,q):
    return execute(['docker','exec','-i',pg,'sh','-c','exec psql -X -U "$POSTGRES_USER" -d "$POSTGRES_DB" -v ON_ERROR_STOP=1 -qAt'],input=('BEGIN READ ONLY;'+q+';ROLLBACK;').encode()).decode().strip()
def counts(pg):
    return {t:int(sql(pg,'SELECT COUNT(*) FROM '+t)) for t in ('users','tenants','tenant_members','external_identities','source_id_binding_reviews','source_id_sessions','source_id_local_sessions','feishu_connections','feishu_personal_imports')}
def ownership(pg):
    # Fingerprints stay inside this private process and are never emitted.
    result={}
    for table,cols in {'users':'id,tenant_id,is_active,is_system_admin,can_access_all_tenants','tenant_members':'id,user_id,tenant_id,role,status,deleted_at','organization_tenant_members':'id,organization_id,tenant_id,role,representative_user_id,deleted_at'}.items():
        available=set(sql(pg,"SELECT column_name FROM information_schema.columns WHERE table_schema='public' AND table_name='"+table+"'").splitlines())
        cols=','.join(c for c in cols.split(',') if c in available);assert cols
        raw=sql(pg,'SELECT row_to_json(s)::text FROM (SELECT '+cols+' FROM '+table+' ORDER BY '+cols+') s')
        result[table]=hashlib.sha256(raw.encode()).digest()
    return result
def compose(pin):
    cmd=['docker','compose','--project-name',PROJECT,'--project-directory',str(LIVE)]
    for path in EXPECTED['compose_files_in_order']+[str(pin)]:cmd+=['-f',path]
    return cmd
def healthy(image):
    for _ in range(45):
        try:
            a=snapshot()['app']
            if a['running'] and a['image_id']==image and a['health']=='healthy':
                with opener.open('http://127.0.0.1:8080/health',timeout=3) as r:
                    if r.status==200:return
        except Exception:pass
        time.sleep(2)
    raise RuntimeError('app_health_timeout')
def main():
    os.umask(0o077)
    out={'target':'172.30.232.42','target_commit':SHA,'result':'FAILED','upgrade_completed':False,'rolled_back':False,'real_sid_login':'NOT_EXECUTED','real_employee_binding':'NOT_EXECUTED','database_migration':'NOT_EXECUTED','legacy_database_reuse':'NOT_EXECUTED'}
    stopped=replaced=False;phase='precheck';backup=None
    lock=(LIVE/'private/allroles-app-upgrade.lock').open('a');fcntl.flock(lock,fcntl.LOCK_EX|fcntl.LOCK_NB)
    try:
        before=snapshot();assert canon(before.values())==canon(EXPECTED['containers'].values())
        assert before['app']['image_id']==OLD and flags(before['app']['id'])==EXPECTED['flags']
        pg=before['postgres']['id'];assert sql(pg,'SELECT version,dirty FROM schema_migrations')=='3027|f'
        assert execute(['docker','image','inspect','--format','{{index .Config.Labels "org.opencontainers.image.revision"}}',NEW]).decode().strip()==SHA
        baseline=counts(pg);original_ownership=ownership(pg);target=dbtarget(before['app']['id'])
        assert baseline['external_identities']==baseline['source_id_sessions']==baseline['source_id_local_sessions']==0
        out['containers_before']=before;out['flags_before']=flags(before['app']['id']);out['counts_before']=baseline
        phase='baseline_local_auth'
        out['pre_upgrade_local_auth']=business_acceptance('431226c3b',extended=False)
        assert counts(pg)==baseline
        phase='protected_backups'
        backup=LIVE/'private/allroles-app-upgrade-backups'/datetime.datetime.now(datetime.timezone.utc).strftime('%Y%m%dT%H%M%SZ')
        backup.mkdir(mode=0o700,parents=True,exist_ok=False)
        files=[pathlib.Path(p) for p in EXPECTED['compose_files_in_order']]
        base=yaml.safe_load((LIVE/'compose.yml').read_text())
        for service in ('app','frontend'):
            entries=base['services'][service].get('env_file',[])
            if isinstance(entries,str):entries=[entries]
            for entry in entries:
                p=pathlib.Path(entry['path'] if isinstance(entry,dict) else entry)
                files.append(p if p.is_absolute() else LIVE/p)
        if (LIVE/'.env').exists():files.append(LIVE/'.env')
        originals={}
        for i,p in enumerate(dict.fromkeys(files)):
            assert p.is_file() and not p.is_symlink()
            dest=backup/(str(i)+'-'+p.name);shutil.copyfile(p,dest);dest.chmod(0o600);originals[str(p)]=p.read_bytes()
        # Preserve mounted configuration material without touching file volumes.
        for i,mount in enumerate(before['app']['mounts']):
            if mount['Type']=='bind' and mount['Destination'].startswith(('/app/config','/fixture/')):
                p=pathlib.Path(mount['Source']);assert p.is_file() and not p.is_symlink()
                dest=backup/('mounted-'+str(i)+'-'+p.name);shutil.copyfile(p,dest);dest.chmod(0o600)
        (backup/'containers-before.json').write_text(json.dumps(before,indent=2))
        out['backup_directory']=str(backup);out['configuration_backup_count']=len(originals)
        PIN.write_text(yaml.safe_dump({'services':{'app':{'image':NEW,'pull_policy':'never'}}}));PIN.chmod(0o600)
        ROLLBACK.write_text(yaml.safe_dump({'services':{'app':{'image':OLD,'pull_policy':'never'}}}));ROLLBACK.chmod(0o600)
        cfg=json.loads(execute(compose(PIN)+['config','--format','json']))
        assert cfg['services']['app']['image']==NEW and cfg['services']['frontend']['image']==before['frontend']['image_id']
        assert set(yaml.safe_load(PIN.read_text())['services'])=={'app'}
        assert cfg['services']['app']['environment'].get('SOURCEID_BASIC_ENCODING','oauth_form')=='oauth_form'
        phase='stop_app_and_fresh_db_backup'
        out['maintenance_started_utc']=datetime.datetime.now(datetime.timezone.utc).isoformat()
        stopped=True;execute(['docker','stop',before['app']['id']])
        assert inspect(before['frontend']['id'],'.State.Running')
        dump=backup/'current-db-3027.dump'
        with dump.open('wb') as f:execute(['docker','exec',pg,'sh','-c','exec pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fc --no-owner --no-acl'],output=f)
        dump.chmod(0o600);assert dump.stat().st_size>0
        p=subprocess.run(['docker','exec','-i',pg,'pg_restore','--list'],input=dump.read_bytes(),stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=30);assert p.returncode==0
        assert counts(pg)==baseline and ownership(pg)==original_ownership
        assert all(pathlib.Path(p).read_bytes()==raw for p,raw in originals.items())
        out['fresh_database_backup']={'path':str(dump),'bytes':dump.stat().st_size,'mode':'0600','catalog_parse_pass':True,'app_writes_stopped':True}
        phase='replace_app_only';replaced=True
        execute(compose(PIN)+['up','-d','--no-deps','--no-build','--pull','never','--force-recreate','app'])
        healthy(NEW);out['maintenance_ended_utc']=datetime.datetime.now(datetime.timezone.utc).isoformat()
        phase='business_acceptance'
        after=snapshot();assert after['app']['image_id']==NEW and after['app']['id']!=before['app']['id']
        assert canon(x for k,x in after.items() if k!='app')==canon(x for k,x in before.items() if k!='app')
        assert flags(after['app']['id'])==EXPECTED['flags'] and dbtarget(after['app']['id'])==target
        assert sorted(after['app']['mounts'],key=lambda x:x['Destination'])==sorted(before['app']['mounts'],key=lambda x:x['Destination'])
        out['acceptance']=business_acceptance('035f35e1d',extended=True)
        assert all(not v['sensitive_pattern_or_test_credential_found'] and v['scan_completed'] for v in out['acceptance']['new_container_log_privacy'].values())
        assert out['acceptance']['cleanup']['local_token_revoked']
        assert counts(pg)==baseline and ownership(pg)==original_ownership
        assert sql(pg,'SELECT version,dirty FROM schema_migrations')=='3027|f'
        assert all(pathlib.Path(p).read_bytes()==raw for p,raw in originals.items())
        out.update(result='PASS',upgrade_completed=True,containers_after=snapshot(),flags_after=flags(after['app']['id']),counts_after=counts(pg),database_target_unchanged=True,original_user_roles_unchanged=True,non_app_containers_unchanged=True,app_mounts_unchanged=True,configuration_sources_unchanged=True,compose_files_in_order=EXPECTED['compose_files_in_order']+[str(PIN)],app_pin=str(PIN),rollback_pin=str(ROLLBACK))
    except Exception as e:
        out['failure_stage']=phase;out['failure_class']=type(e).__name__;out['failure_lines']=[f.lineno for f in traceback.extract_tb(e.__traceback__) if f.filename=='<stdin>']
        if stopped:
            try:
                if replaced:
                    execute(compose(ROLLBACK)+['up','-d','--no-deps','--no-build','--pull','never','--force-recreate','app'])
                else:execute(['docker','start',before['app']['id']])
                healthy(OLD)
                restored=snapshot();assert flags(restored['app']['id'])==EXPECTED['flags']
                assert canon(x for k,x in restored.items() if k!='app')==canon(x for k,x in before.items() if k!='app')
                out['rollback_acceptance']=business_acceptance('431226c3b',extended=False)
                out['rolled_back']=True;out['recovery']='OLD_APP_RESTORED_OAUTH_FORM_AND_DB3027_RETAINED'
            except Exception as recovery:
                out['recovery']='FAILED_REQUIRES_TARGETED_INSPECTION';out['recovery_failure_class']=type(recovery).__name__
    finally:
        out['completed_utc']=datetime.datetime.now(datetime.timezone.utc).isoformat()
        (ROOT/'app-deployment-result.json').write_text(json.dumps(out,indent=2)+'\n')
        if backup:(backup/'deployment-result.json').write_text(json.dumps(out,indent=2)+'\n')
        print(json.dumps(out));fcntl.flock(lock,fcntl.LOCK_UN);lock.close()
    return 0 if out['result']=='PASS' else 2
if __name__=='__main__':raise SystemExit(main())
