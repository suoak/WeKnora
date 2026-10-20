"""Assert clean candidate/source/live boundaries, protect artifacts, stop candidate PG."""
import sys,stat,shutil
sys.path.insert(0,'/home/aqyfzx/ai/knowhub-legacy-clean-target-20261016')
from support import *
receipt=json.loads((BASE/'acceptance-result.json').read_text());assert receipt['result']=='PASS'
initial=json.loads((BASE/'preflight.json').read_text())
baseline=json.loads((BASE/'private/selected-data-baseline.json').read_text())
assert signatures(baseline['columns'])==baseline['signatures']==signatures(baseline['columns'],source=True)
extra={}
for table,wanted in {'organization_members':'id,organization_id,tenant_id,user_id,role,status,deleted_at','users':'id,tenant_id,is_active,is_system_admin,can_access_all_tenants,deleted_at'}.items():
 available=set(sql("SELECT column_name FROM information_schema.columns WHERE table_schema='public' AND table_name='"+table+"'",source=True).splitlines())
 if not available:extra[table]='NOT_PRESENT_IN_SOURCE';continue
 fields=[c for c in wanted.split(',') if c in available]
 extra[table]=signatures({table:fields})==signatures({table:fields},source=True)
 assert extra[table] is True
projects={}
for project in ('weknora','knowhub-sourceid-business','weknora-segment-test'):
 ids=run(['docker','ps','-a','--filter','label=com.docker.compose.project='+project,'--format','{{.ID}}']).decode().split()
 projects[project]={x['service']:x for x in (metadata(cid) for cid in ids)}
def canonical(value):
 if isinstance(value,dict):return {k:canonical(v) for k,v in value.items()}
 if isinstance(value,list):return sorted((canonical(v) for v in value),key=lambda x:json.dumps(x,sort_keys=True))
 return value
assert canonical(projects)==canonical(initial['projects'])
live=projects['knowhub-sourceid-business'];app=live['app'];front=live['frontend']
assert app['health']=='healthy' and app['image_id']==IMAGE and app['running'] and front['running']
assert app['id']=='842f61a9825fe013dfbd87285cfc6846c65bd086131bf26dbdcbd192c13c4e78'
flags={}
for key in ('SOURCEID_BASIC_ENCODING','SOURCEID_PROTOCOL_ONLY','SOURCEID_CONTRACT_CONFIRMED','SOURCEID_EMAIL_BINDING_ENABLE','SOURCEID_AUTO_BINDING_ENABLE','SOURCEID_SESSION_MODE','SOURCEID_LOGOUT_ENABLE','OIDC_AUTH_ENABLE','SOURCEID_DIAGNOSTICS_ENABLE','AUTO_MIGRATE'):
 v=run(['docker','exec',app['id'],'sh','-c','printf "%s" "${'+key+'-NOT_SET}"']).decode();assert v in ('true','false','oauth_form','external','NOT_SET');flags[key]=v
assert flags['SOURCEID_BASIC_ENCODING']=='oauth_form' and flags['SOURCEID_PROTOCOL_ONLY']=='true'
assert all(flags[k]=='false' for k in ('SOURCEID_CONTRACT_CONFIRMED','SOURCEID_EMAIL_BINDING_ENABLE','SOURCEID_AUTO_BINDING_ENABLE','SOURCEID_LOGOUT_ENABLE','AUTO_MIGRATE'))
q="BEGIN READ ONLY; SELECT json_build_object('version',version,'dirty',dirty) FROM schema_migrations; SELECT json_build_object('users',(SELECT COUNT(*) FROM users),'tenants',(SELECT COUNT(*) FROM tenants),'members',(SELECT COUNT(*) FROM tenant_members),'identities',(SELECT COUNT(*) FROM external_identities),'sessions',(SELECT COUNT(*) FROM source_id_sessions),'local_sessions',(SELECT COUNT(*) FROM source_id_local_sessions),'active_kbs',(SELECT COUNT(*) FROM knowledge_bases WHERE deleted_at IS NULL)); ROLLBACK;"
p=subprocess.run(['docker','exec','-i',live['postgres']['id'],'sh','-c','exec psql -X -U "$POSTGRES_USER" -d "$POSTGRES_DB" -qAt -v ON_ERROR_STOP=1'],input=q.encode(),stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=20);assert p.returncode==0
current_db=[json.loads(x) for x in p.stdout.decode().splitlines() if x];assert current_db[0]=={'version':3027,'dirty':False}
assert current_db[1]=={'users':2,'tenants':1,'members':2,'identities':0,'sessions':0,'local_sessions':0,'active_kbs':0}
private=BASE/'private'
for path in [private]+list(private.rglob('*')):
 assert not path.is_symlink();path.resolve().relative_to(BASE.resolve())
 path.chmod(0o700 if path.is_dir() else 0o600)
assert stat.S_IMODE(private.stat().st_mode)==0o700
protected={name:{'bytes':(private/name).stat().st_size,'mode':oct(stat.S_IMODE((private/name).stat().st_mode))} for name in ('source79-clean.dump','source79-files.tar','sanitized-3027-clean.dump','ready-3027-clean.dump','candidate.env','postgres.env')}
assert all(v['mode']=='0o600' and v['bytes']>0 for v in protected.values())
assert sql("SELECT version::text||'|'||dirty::text FROM schema_migrations",source=True)=='79|false'
assert sql("SELECT version::text||'|'||dirty::text FROM schema_migrations")=='3027|false'
assert not inspect(PG,'.HostConfig.PortBindings') and inspect(PG,'.HostConfig.NetworkMode')=='none'
remaining=json.loads(execute(['docker','inspect','--format','{{json .HostConfig.RestartPolicy}}',PG]));assert remaining['Name']=='no'
execute(['docker','stop',PG]);assert not inspect(PG,'.State.Running')
for name in ('knowhub-legacy-clean-20261016-app','knowhub-legacy-clean-20261016-front','knowhub-legacy-clean-20261016-redis'):
 assert subprocess.run(['docker','inspect',name],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode!=0
out={'result':'PASS','utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'projects_unchanged':True,'live_app_id':app['id'],'live_frontend_id':front['id'],'live_app_image':app['image_id'],'live_frontend_image':front['image_id'],'live_flags':flags,'live_database':current_db,'original_schema':'79|false','candidate_schema':'3027|false','additional_original_role_comparison':extra,'protected_artifacts':protected,'candidate_postgres_stopped':True,'candidate_app_frontend_redis_removed':True,'candidate_volume_retained':{'postgres':PGVOL,'files':FILEVOL},'candidate_network':'none','candidate_published_ports':False,'disk_free_bytes':shutil.disk_usage(str(BASE)).free,'real_sid_login':False,'live_database_switched':False,'production_accessed':False}
(BASE/'closeout-result.json').write_text(json.dumps(out,indent=2)+'\n');print(json.dumps(out))
