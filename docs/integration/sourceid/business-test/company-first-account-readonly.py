"""Protected stdin own-account lookup; no binding, credential or policy writes."""
import json,sys,subprocess,pathlib,os,datetime
os.umask(0o077)
data=json.load(sys.stdin);assert set(data)=={'email'} and isinstance(data['email'],str)
email=data['email'].strip().lower();assert '@' in email and len(email)<=254 and not any(ord(c)<32 for c in email)
PG='knowhub-company-test-postgres-1';APP='knowhub-company-test-app-1'
def command(args,raw=None):
 z=subprocess.run(args,input=raw,stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=25)
 if z.returncode:raise RuntimeError('controlled_query_failed')
 return z.stdout
def query(sql):
 prefix="BEGIN READ ONLY;SET LOCAL statement_timeout='5s';SET LOCAL log_statement='none';SET LOCAL log_min_error_statement='panic';SET LOCAL log_parameter_max_length_on_error=0;"
 return json.loads(command(['docker','exec','-i',PG,'psql','-X','-U','clean_owner','-d','knowhub_oa_sso_pilot','-qAt','-v','ON_ERROR_STOP=1'],(prefix+sql+';ROLLBACK;').encode()))
labels=json.loads(command(['docker','inspect','--format','{{json .Config.Labels}}',APP]));assert labels['com.docker.compose.project']=='knowhub-company-test'
schema=query("SELECT json_build_object('version',version,'dirty',dirty) FROM schema_migrations");assert schema=={'version':3029,'dirty':False}
expression="convert_from(decode('"+email.encode().hex()+"','hex'),'UTF8')"
rows=query("SELECT COALESCE(json_agg(s),'[]') FROM (SELECT id,is_active,deleted_at IS NULL AS not_deleted,is_system_admin,can_access_all_tenants FROM users WHERE LOWER(TRIM(email))="+expression+") s")
out={'project':'knowhub-company-test','schema':schema,'matched_accounts':len(rows),'employee_values_exported':False,'queries_read_only':True,'identity_or_session_writes':False,'real_sid_requests':0,'ownership_ready':False,'ownership_reason':'INDEPENDENT_RJGH_HISTORY_EVIDENCE_NOT_SUPPLIED'}
if len(rows)==1:
 row=rows[0];uid=row['id'];assert __import__('re').fullmatch('[a-f0-9-]{36}',uid)
 permission=query("SELECT json_build_object('memberships',(SELECT COUNT(*) FROM tenant_members WHERE user_id='"+uid+"' AND deleted_at IS NULL),'owner_memberships',(SELECT COUNT(*) FROM tenant_members WHERE user_id='"+uid+"' AND deleted_at IS NULL AND role='owner'),'active_knowledge_bases',(SELECT COUNT(*) FROM knowledge_bases WHERE deleted_at IS NULL),'existing_ownership',(SELECT COUNT(*) FROM source_id_account_ownership WHERE user_id='"+uid+"'),'existing_identities',(SELECT COUNT(*) FROM external_identities WHERE user_id='"+uid+"'),'active_kbs_in_member_spaces',(SELECT COUNT(*) FROM knowledge_bases k WHERE k.deleted_at IS NULL AND EXISTS(SELECT 1 FROM tenant_members m WHERE m.user_id='"+uid+"' AND m.tenant_id=k.tenant_id AND m.deleted_at IS NULL AND m.status='active')),'active_kbs_outside_member_spaces',(SELECT COUNT(*) FROM knowledge_bases k WHERE k.deleted_at IS NULL AND NOT EXISTS(SELECT 1 FROM tenant_members m WHERE m.user_id='"+uid+"' AND m.tenant_id=k.tenant_id AND m.deleted_at IS NULL AND m.status='active')))")
 out.update(active=row['is_active'] and row['not_deleted'],system_admin=row['is_system_admin'],cross_tenant=row['can_access_all_tenants'],permission_counts=permission)
 # Internal random UID only, private preparatory input. No employee identifier,
 # email, guessed RJGH, approval, identity or business session is written.
 target=pathlib.Path('/home/aqyfzx/ai/knowhub-company-entry-20261017/private/first-account-preparation');target.mkdir(mode=0o700,exist_ok=True)
 f=target/'selected-account.json';f.write_text(json.dumps({'project':'knowhub-company-test','existing_user_id':uid,'identity_evidence_status':'NOT_SUPPLIED'}));f.chmod(0o600)
out['utc']=datetime.datetime.now(datetime.timezone.utc).isoformat();email=expression=data=rows=None
print(json.dumps(out))
