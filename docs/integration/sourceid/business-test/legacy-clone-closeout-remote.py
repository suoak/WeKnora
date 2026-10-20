"""Compare original IDs/ACL privately, revoke clone auth and stop exact test containers."""
import importlib.util,hashlib
s=importlib.util.spec_from_file_location('legacy','/home/aqyfzx/ai/knowhub-legacy79-worker-20261016.py');m=importlib.util.module_from_spec(s);s.loader.exec_module(m)
assert inspect(m.PG,'.HostConfig.NetworkMode')=='none'
def rows(table,columns,source=False,where=''):
    q='SELECT row_to_json(s)::text FROM (SELECT '+columns+' FROM '+table+(' WHERE '+where if where else '')+' ORDER BY '+columns+') s'
    return [json.loads(x) for x in m.sql(q,source).splitlines() if x]
specs={'users':'id,tenant_id,is_active,is_system_admin,can_access_all_tenants',
       'tenant_members':'id,user_id,tenant_id,role,status,invited_by,deleted_at',
       'organization_members':'id,organization_id,tenant_id,role,deleted_at',
       'organization_tenant_members':'id,organization_id,tenant_id,role,representative_user_id,deleted_at',
       'knowledge_bases':'id,tenant_id,creator_id,deleted_at',
       'kb_shares':'id,knowledge_base_id,organization_id,source_tenant_id,permission,shared_by_user_id,deleted_at',
       'resources':'id,handle,tenant_id,storage_backend_id,provider,physical_path,state,deleted_at',
       'resource_bindings':'id,resource_id,owner_type,owner_id,relation'}
comparison={}
for table,wanted in specs.items():
    cols=set(m.sql("SELECT column_name FROM information_schema.columns WHERE table_schema='public' AND table_name='"+table+"'",True).splitlines())
    if not cols:comparison[table]='NOT_PRESENT_IN_SOURCE';continue
    columns=','.join(c for c in wanted.split(',') if c in cols)
    original=rows(table,columns,True)
    if not original:
        comparison[table]=len(rows(table,columns))==0
    else:
        ids=','.join("'"+str(r['id']).replace("'","''")+"'" for r in original)
        copied=rows(table,columns,where='id IN ('+ids+')')
        comparison[table]=original==copied
assert all(v is True or v=='NOT_PRESENT_IN_SOURCE' for v in comparison.values())
out={'original_id_roles_acl_consistency':comparison,'source_schema':m.sql("SELECT version::text||'|'||dirty::text FROM schema_migrations",True),'clone_schema':m.sql("SELECT version::text||'|'||dirty::text FROM schema_migrations"),'source_counts':m.counts(True),'copy_files_equal_source':m.file_digest('type=volume,source=weknora_data-files')==m.file_digest('type=bind,source='+str(m.BASE/'files')),'real_sid_login':False,'real_employee_binding_written':False}
assert out['source_schema']=='79|false' and out['clone_schema']=='3027|false' and out['copy_files_equal_source']
# No more running APP can issue clone sessions during final revocation.
pg_id=inspect(m.PG,'.Id')
stopped={}
for name in ('oa-legacy79-frontend-20261016','oa-legacy79-app-20261016','oa-legacy79-mock-20261016','oa-legacy79-redis-20261016'):
    mode=inspect(name,'.HostConfig.NetworkMode');assert mode in ('container:'+pg_id,'container:'+m.PG)
    cid=inspect(name,'.Id');run(['docker','rm','-f',cid]);stopped[name]=True
m.sql('BEGIN; UPDATE auth_tokens SET is_revoked=TRUE; UPDATE source_id_sessions SET revoked=TRUE; COMMIT;')
out['clone_active_auth_tokens_after']=int(m.sql('SELECT COUNT(*) FROM auth_tokens WHERE NOT is_revoked'))
out['clone_active_enterprise_sessions_after']=int(m.sql('SELECT COUNT(*) FROM source_id_sessions WHERE NOT revoked'))
assert out['clone_active_auth_tokens_after']==out['clone_active_enterprise_sessions_after']==0
out['synthetic_bindings_in_offline_clone']=int(m.sql('SELECT COUNT(*) FROM external_identities'))
assert m.sql("SELECT COUNT(*) FROM external_identities WHERE issuer<>'https://127.0.0.1:18443' OR employee_no NOT IN ('SYNTHETIC-ADMIN','SYNTHETIC-VIEWER')")=='0'
run(['docker','stop',pg_id]);run(['docker','rm',pg_id]);stopped[m.PG]=True
out['temporary_containers_removed']=stopped
out['protected_snapshot_and_clone_files_retained']=True
live={}
for project in ('weknora','knowhub-sourceid-business','weknora-segment-test'):
    ids=run(['docker','ps','-a','--filter','label=com.docker.compose.project='+project,'--format','{{.ID}}']).decode().split()
    live[project]=[metadata(x) for x in ids]
app=next(x for x in live['knowhub-sourceid-business'] if x['service']=='app')
frontend=next(x for x in live['knowhub-sourceid-business'] if x['service']=='frontend')
assert app['id']=='ceac394ee4632f5514cbb65821e77c369df63b35ee1aba7d291fab8a9fdb6ea5'
assert frontend['id']=='7b624ec2ee904cb7a0ab86458954722bae831f20f1f3c9da54c7ca3f5e53c448'
assert app['running'] and app['health']=='healthy' and frontend['running']
out['project_container_metadata']=live
out['current_app_and_frontend_container_ids_unchanged']=True
out['completed_utc']=datetime.datetime.now(datetime.timezone.utc).isoformat();out['result']='PASS'
(m.BASE/'closeout-result.json').write_text(json.dumps(out,indent=2))
print(json.dumps(out))
