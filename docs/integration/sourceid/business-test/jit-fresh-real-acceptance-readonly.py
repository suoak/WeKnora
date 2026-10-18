"""Safe post-window receipt and database evidence only; no authentication."""
import sys
R=pathlib.Path('/home/aqyfzx/ai/knowhub-jit-fresh-5f988be51-20261018');W=R/'real-window'
sys.path.insert(0,str(W));from runtime import fresh_meta,sql,jq,baseline,env,health
receipt=json.loads((W/'evidence.json').read_text());recovery=json.loads((W/'recovery.json').read_text());status=json.loads((W/'status.json').read_text())
events=receipt.get('events',[]);http=receipt.get('http_events',[]);gateway=receipt.get('gateway_events',[])
stages={s:('PASS' if any(x['stage']==s and x['pass'] for x in events) else 'NOT_CAPTURED') for s in ('authorize','callback_state','token','id_token','userinfo','identity_mapping','local_session','callback_result')}
for s in stages:
 if any(x['stage']==s and not x['pass'] for x in events):stages[s]='FAIL'
callback_traces={x['trace'] for x in events if x['stage']!='authorize'};authorize_traces={x['trace'] for x in events if x['stage']=='authorize'}
token_rows=[row for x in http if x['event']=='sourceid_token_http' for row in x['http'] if row['stage']=='token']
token_summary=[{'status':x['status'],'duration_ms':x['duration_ms'],'observed_write_attempts':x.get('transport',{}).get('observed_write_attempts'),'retry_observed':x.get('transport',{}).get('retry_observed')} for x in token_rows]
m=fresh_meta();assert m['users']==m['identities']==m['jit']==m['authorizations']==m['consumed_authorizations']==1
assert m['members']==m['kbs']==m['privileged']==m['active_sessions']==m['active_access']==m['refresh']==0
oldid=json.loads(pathlib.Path('/home/aqyfzx/ai/knowhub-company-entry-20261017/private/first-account-preparation/selected-account.json').read_text())['existing_user_id']
checks=jq("SELECT json_build_object('tenantless',bool_and(u.tenant_id IS NULL OR u.tenant_id=0),'ordinary',bool_and(NOT u.is_system_admin AND NOT u.can_access_all_tenants),'different_original_id',bool_and(u.id<>'"+oldid+"'),'all_sessions_revoked',NOT EXISTS(SELECT 1 FROM source_id_sessions WHERE NOT revoked),'all_access_revoked',NOT EXISTS(SELECT 1 FROM auth_tokens WHERE NOT is_revoked),'jit_record_preserved',COUNT(*)=1) FROM users u JOIN source_id_jit_registrations j ON j.user_id=u.id")
original_same=baseline()==json.loads((W/'original-baseline.json').read_text())
before=json.loads((W/'original-baseline.json').read_text());after=baseline()
original_business_same=all(before[t]==after[t] for t in before if t not in ('external_identities','source_id_sessions'))
def stable_binding(rows):return [{k:v for k,v in row['row'].items() if k not in ('last_login_at','updated_at')} for row in rows]
original_business_same=original_business_same and stable_binding(before['external_identities'])==stable_binding(after['external_identities'])
original_health=health('company-test-verified-email-db3030');new=env('knowhub-jit-fresh-app-1');assert new['OIDC_AUTH_ENABLE']=='false' and new['SOURCEID_JIT_ENABLE']=='false' and new['SOURCEID_PROTOCOL_ONLY']=='true'
trace_complete=all(x=='PASS' for x in stages.values()) and len(callback_traces)==len(authorize_traces)==1 and len(token_summary)==1 and token_summary[0]['status']==200 and token_summary[0]['observed_write_attempts']==1 and token_summary[0]['retry_observed'] is False
out={'result':'JIT_AND_RESTORE_PASS_WITH_AUDIT_DELTA' if all(checks.values()) and original_business_same else 'REVIEW_REQUIRED','run_id':status['run_id'],'trace':'PASS_SINGLE_WINDOW_CORRELATION' if trace_complete else 'PARTIAL_NOT_CAPTURED','stages':stages,'nonce':'PASS_STRICT_ID_TOKEN_PATH' if stages['id_token']=='PASS' else 'NOT_CAPTURED','unique_authorize_and_callback_traces':len(authorize_traces)==len(callback_traces)==1,'token_http':token_summary,'gateway_statuses':{kind:sorted({x['status'] for x in gateway if x['kind']==kind}) for kind in ('auth_me','logout','oidc_result','oidc_callback')},'database':m,'checks':checks,'new_business_gates_closed':True,'original_80_8080_restored':all(v in (200,302,401) for v in original_health.values()),'recovery':recovery['result'],'original_data_baseline_exact_match_now':original_same,'original_accounts_permissions_knowledge_and_binding_preserved':original_business_same,'user_portal_confirmation':'user_confirmed_normal_entry_and_logout','scope':'same_sid_identity_fresh_instance_only','real_sid_requests_by_agent':0}
(W/'acceptance-safe.json').write_text(json.dumps(out));print(json.dumps(out))
