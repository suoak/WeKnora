"""Read-only candidate volume/source inventory; no employee data or secrets."""
import shutil,stat
root=pathlib.Path('/home/aqyfzx/ai/knowhub-legacy-clean-target-20261016')
out={'utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'target':'172.30.232.42','runtime_changed':False,'candidate_started':False}
out['artifacts']={}
for name in ('source79-clean.dump','source79-files.tar','sanitized-3027-clean.dump','ready-3027-clean.dump'):
 p=root/'private'/name
 out['artifacts'][name]={'exists':p.is_file(),'bytes':p.stat().st_size if p.is_file() else None,'private_mode':stat.S_IMODE(p.stat().st_mode)==0o600 if p.is_file() else False}
out['candidate_pg']={k:metadata('knowhub-legacy-clean-20261016-pg')[k] for k in ('id','running','image_id','mounts')}
out['candidate_network_mode']=inspect('knowhub-legacy-clean-20261016-pg','.HostConfig.NetworkMode')
out['volumes']={}
for name in ('knowhub-legacy-clean-20261016-pgdata','knowhub-legacy-clean-20261016-files'):
 out['volumes'][name]={'exists':bool(run(['docker','volume','inspect','--format','{{.Name}}',name]).decode().strip())}
q="BEGIN READ ONLY;SELECT json_build_object('version',version,'dirty',dirty) FROM schema_migrations;SELECT json_build_object('users',(SELECT COUNT(*) FROM users),'system_admins',(SELECT COUNT(*) FROM users WHERE is_system_admin),'cross_tenant_admins',(SELECT COUNT(*) FROM users WHERE can_access_all_tenants),'active_kbs',(SELECT COUNT(*) FROM knowledge_bases WHERE deleted_at IS NULL),'active_documents',(SELECT COUNT(*) FROM knowledges WHERE deleted_at IS NULL),'nonempty_chunks',(SELECT COUNT(*) FROM chunks WHERE deleted_at IS NULL AND content<>''),'embeddings',(SELECT COUNT(*) FROM embeddings));ROLLBACK;"
p=subprocess.run(['docker','exec','-i','b2229c012c06ac8fab34b83b8ae0442077bb69bfc5cf065764bf94ca446c8abe','sh','-c','exec psql -X -U "$POSTGRES_USER" -d "$POSTGRES_DB" -qAt -v ON_ERROR_STOP=1'],input=q.encode(),stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=20)
assert p.returncode==0
out['source_aggregate']=[json.loads(x) for x in p.stdout.decode().splitlines() if x]
assert out['source_aggregate'][0]=={'version':79,'dirty':False}
def source_query(sql):
 p=subprocess.run(['docker','exec','-i','b2229c012c06ac8fab34b83b8ae0442077bb69bfc5cf065764bf94ca446c8abe','sh','-c','exec psql -X -U "$POSTGRES_USER" -d "$POSTGRES_DB" -qAt -v ON_ERROR_STOP=1'],input=('BEGIN READ ONLY;'+sql+';ROLLBACK;').encode(),stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=15)
 assert p.returncode==0
 return p.stdout.decode().strip()
out['source_membership_tables']={}
for table in ('tenant_members','organization_tenant_members'):
 exists=source_query("SELECT to_regclass('public."+table+"') IS NOT NULL")=='t'
 out['source_membership_tables'][table]={'exists':exists}
 if exists:
  role_counts=source_query("SELECT COALESCE(json_agg(s),'[]') FROM (SELECT CASE WHEN role IN ('owner','admin','contributor','viewer') THEN role ELSE 'OTHER' END AS role_class,COUNT(*) FROM "+table+" GROUP BY 1) s")
  out['source_membership_tables'][table]['aggregate_roles']=json.loads(role_counts)
out['default_gate_flags']={}
for key in ('SOURCEID_FORCE_REAUTH_PROMPT_LOGIN','SOURCEID_PROTOCOL_ONLY','SOURCEID_CONTRACT_CONFIRMED','SOURCEID_EMAIL_BINDING_ENABLE','SOURCEID_AUTO_BINDING_ENABLE','SOURCEID_BASIC_ENCODING'):
 value=run(['docker','exec','knowhub-sourceid-business-app-1','sh','-c','printf "%s" "${'+key+'-NOT_SET}"']).decode()
 assert value in ('true','false','oauth_form','NOT_SET')
 out['default_gate_flags'][key]=value
out['default_app']={k:metadata('knowhub-sourceid-business-app-1')[k] for k in ('id','image_id','health','ports')}
out['frontend_ports']=inspect('knowhub-sourceid-business-frontend-1','.NetworkSettings.Ports')
out['disk_free_bytes']=shutil.disk_usage('/home/aqyfzx/ai').free
out['new_candidate_compatibility']='NOT_EXECUTED_PENDING_CANDIDATE'
out['allowed_denied_source_network_validation']='NOT_EXECUTED_NO_NETWORK_RULE_CHANGE'
out['candidate_database_current_rows']='NOT_QUERIED_STOPPED_CONTAINER'
print(json.dumps(out))
