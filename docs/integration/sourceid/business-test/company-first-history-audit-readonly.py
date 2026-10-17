"""Selected account only, original test DB vs green; never export identities."""
import json,pathlib,subprocess,re,datetime,os
os.umask(0o077)
BASE=pathlib.Path('/home/aqyfzx/ai/knowhub-company-entry-20261017')
uid=json.loads((BASE/'private/first-account-preparation/selected-account.json').read_text())['existing_user_id'];assert re.fullmatch('[a-f0-9-]{36}',uid)
def command(args,raw=None):
 z=subprocess.run(args,input=raw,stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=25)
 if z.returncode:raise RuntimeError('controlled_history_query_failed')
 return z.stdout
def query(pg,sql):
 prefix="BEGIN READ ONLY;SET LOCAL statement_timeout='5s';SET LOCAL log_statement='none';SET LOCAL log_min_error_statement='panic';SET LOCAL log_parameter_max_length_on_error=0;"
 value=command(['docker','exec','-i',pg,'sh','-c','exec psql -X -U "$POSTGRES_USER" -d "$POSTGRES_DB" -qAt -v ON_ERROR_STOP=1'],(prefix+sql+';ROLLBACK;').encode())
 return json.loads(value)
out={'result':'PASS','new_sid_requests':0,'database_writes':False,'employee_values_exported':False,'databases':{},'history_records':{}}
rows={};members={};knowledge={}
for name,pg in [('original','WeKnora-postgres'),('new','knowhub-company-test-postgres-1')]:
 cols=query(pg,"SELECT COALESCE(json_agg(column_name),'[]') FROM information_schema.columns WHERE table_name='users' AND table_schema='public'")
 common=[x for x in ('id','email','created_at','is_active','is_system_admin','can_access_all_tenants','tenant_id','deleted_at') if x in cols]
 data=query(pg,"SELECT COALESCE(json_agg(s),'[]') FROM (SELECT "+','.join(common)+" FROM users WHERE id='"+uid+"') s");assert len(data)==1;rows[name]=data[0]
 out['databases'][name]={'schema':query(pg,"SELECT json_build_object('version',version,'dirty',dirty) FROM schema_migrations"),'selected_account_found':True,'identity_field_columns_present':any(x.lower() in ('rjgh','employee_no','employee_id','staff_id') for x in cols)}
 tables=query(pg,"SELECT COALESCE(json_agg(table_name),'[]') FROM information_schema.tables WHERE table_schema='public'")
 available=[x for x in ('audit_logs','external_identities','source_id_account_ownership','source_id_binding_reviews') if x in tables]
 evidence={}
 for table in available:
  columns=query(pg,"SELECT COALESCE(json_agg(column_name),'[]') FROM information_schema.columns WHERE table_schema='public' AND table_name='"+table+"'")
  # Audit details, UserInfo and tokens are deliberately not requested.
  field='user_id' if 'user_id' in columns else 'actor_user_id' if 'actor_user_id' in columns else None
  if field:evidence[table]={'selected_account_rows':query(pg,"SELECT COUNT(*) FROM "+table+" WHERE "+field+"='"+uid+"'"),'raw_details_read':False}
 out['history_records'][name]=evidence
 member_cols=query(pg,"SELECT COALESCE(json_agg(column_name),'[]') FROM information_schema.columns WHERE table_schema='public' AND table_name='tenant_members'")
 fields=[x for x in ('id','user_id','tenant_id','role','status','deleted_at') if x in member_cols]
 members[name]=query(pg,"SELECT COALESCE(json_agg(s),'[]') FROM (SELECT "+','.join(fields)+" FROM tenant_members WHERE user_id='"+uid+"' ORDER BY id) s")
 knowledge[name]=query(pg,"SELECT COALESCE(json_agg(s),'[]') FROM (SELECT id,tenant_id,deleted_at FROM knowledge_bases ORDER BY id) s")
out['comparisons']={key:rows['original'].get(key)==rows['new'].get(key) for key in ('id','email','created_at','is_active','is_system_admin','tenant_id') if key in rows['original'] and key in rows['new']}
assert all(out['comparisons'].values())
out['memberships_identical']=members['original']==members['new'];out['knowledge_space_metadata_identical']=knowledge['original']==knowledge['new']
assert out['memberships_identical'] and out['knowledge_space_metadata_identical']
out['creation_record_present']=bool(rows['original'].get('created_at'))
review=BASE/'private/first-account-preparation/history-review.json'
review.write_text(json.dumps({'purpose':'ACCOUNT_METADATA_REVIEW_ONLY_NOT_EMPLOYEE_ATTESTATION','source_test_database':'schema79 original','candidate_project':'knowhub-company-test','selected_original_account':rows['original'],'selected_candidate_account':rows['new'],'selected_original_memberships':members['original'],'selected_candidate_memberships':members['new'],'rjgh_evidence':'NOT_SUPPLIED'},default=str));review.chmod(0o600)
out['private_account_review_bundle_saved']=True
out['confirmed_from_db']='ORIGINAL_ACCOUNT_AND_METADATA_CONTINUITY_ONLY'
out['rjgh_person_link']='NOT_ESTABLISHED_BY_DATABASE_OR_COUNTED_AUDIT_RECORDS'
out['utc']=datetime.datetime.now(datetime.timezone.utc).isoformat();rows=members=knowledge=None
print(json.dumps(out))
