import sys
B=pathlib.Path('/home/aqyfzx/ai/knowhub-verified-email-candidate-5f988be51');s=json.loads((B/'real-business-operation.json').read_text());P=pathlib.Path(s['prepared_directory']);inputs=json.loads((P/'approved-inputs.json').read_text());uid=inputs['existing_user_id'];ref=inputs['company_policy_ref']
sys.path.insert(0,str(B/'monitor/business-test'))
from business_monitor_pilot import DockerPilotPort
p=DockerPilotPort.__new__(DockerPilotPort)
def q(sql):return json.loads(p.query(sql))
tx=q("SELECT json_build_object('authorization_total',COUNT(*) FILTER(WHERE nonce<>''),'authorization_consumed',COUNT(*) FILTER(WHERE nonce<>'' AND consumed),'login_result_total',COUNT(*) FILTER(WHERE nonce=''),'login_result_consumed',COUNT(*) FILTER(WHERE nonce='' AND consumed),'active_authorizations',COUNT(*) FILTER(WHERE nonce<>'' AND NOT consumed AND expires_at>NOW())) FROM oidc_transactions")
sessions=q("SELECT json_build_object('total',COUNT(*),'active',COUNT(*) FILTER(WHERE NOT revoked AND expires_at>NOW()),'expected_original_user',COALESCE(bool_and(user_id='"+uid+"'),false)) FROM source_id_sessions")
policy=q("SELECT json_build_object('decisions',COUNT(*),'original_user',COALESCE(bool_and(r.user_id='"+uid+"'),false),'approved',COALESCE(bool_and(r.status='approved'),false),'policy_matches',COALESCE(bool_and(d.policy_ref='"+ref+"'),false)) FROM source_id_link_decisions d JOIN source_id_binding_reviews r ON r.id=d.review_id")
records=q("SELECT json_build_object('users',(SELECT COUNT(*) FROM users),'bindings',(SELECT COUNT(*) FROM external_identities),'jit',(SELECT COUNT(*) FROM source_id_jit_registrations),'ownership',(SELECT COUNT(*) FROM source_id_account_ownership),'schema',(SELECT version FROM schema_migrations),'dirty',(SELECT dirty FROM schema_migrations))")
events=json.loads((pathlib.Path(s['monitor_directory'])/'before-restore.json').read_text())
flags=p.flags()
print(json.dumps({'result':'READ_ONLY','transactions_by_safe_presence_only':tx,'sessions':sessions,'policy':policy,'records':records,'captured_safe_stage_events':len(events.get('events',[])),'captured_safe_http_events':len(events.get('http_events',[])),'current_business_flags':{k:flags.get(k) for k in ('OIDC_AUTH_ENABLE','SOURCEID_PROTOCOL_ONLY','SOURCEID_ACCOUNT_LINK_MODE','SOURCEID_CONTRACT_CONFIRMED','SOURCEID_AUTO_BINDING_ENABLE','SOURCEID_SESSION_MODE')},'employee_values_read_or_exported':False}))
