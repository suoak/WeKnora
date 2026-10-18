"""Post-user-confirmation metadata only. No requests using user credentials."""
import sys
B=pathlib.Path('/home/aqyfzx/ai/knowhub-verified-email-candidate-5f988be51')
a=json.loads((B/'active-business-config.json').read_text());inputs=json.loads((pathlib.Path(a['prepared_directory'])/'approved-inputs.json').read_text())
sys.path.insert(0,str(B/'monitor/business-test'))
from business_monitor_pilot import DockerPilotPort,APP,IMAGE,REVISION,inspect as di
p=DockerPilotPort.__new__(DockerPilotPort);uid=inputs['existing_user_id'];ref=inputs['company_policy_ref']
q=lambda sql:json.loads(p.query(sql))
account=q("SELECT json_build_object('original_user_exists',COUNT(*)=1,'active',bool_and(is_active AND deleted_at IS NULL),'system_admin',bool_and(is_system_admin),'cross_tenant',bool_and(can_access_all_tenants)) FROM users WHERE id='"+uid+"'")
records=q("SELECT json_build_object('users',(SELECT COUNT(*) FROM users),'bindings',(SELECT COUNT(*) FROM external_identities),'jit',(SELECT COUNT(*) FROM source_id_jit_registrations),'documents',(SELECT COUNT(*) FROM knowledges WHERE deleted_at IS NULL),'active_kbs',(SELECT COUNT(*) FROM knowledge_bases WHERE deleted_at IS NULL),'schema',(SELECT version FROM schema_migrations),'dirty',(SELECT dirty FROM schema_migrations))")
session=q("SELECT json_build_object('total',COUNT(*),'active',COUNT(*) FILTER(WHERE NOT revoked AND expires_at>NOW()),'revoked',COUNT(*) FILTER(WHERE revoked),'original_user',COALESCE(bool_and(user_id='"+uid+"'),false)) FROM source_id_sessions")
tokens=q("SELECT json_build_object('enterprise_refresh_records',COUNT(*) FILTER(WHERE token_type='refresh_token'),'active_enterprise_access',COUNT(*) FILTER(WHERE token_type='access_token' AND NOT is_revoked AND expires_at>NOW())) FROM auth_tokens WHERE session_id IN (SELECT id FROM source_id_sessions WHERE user_id='"+uid+"')")
policy=q("SELECT json_build_object('decisions',COUNT(*),'original_user',COALESCE(bool_and(r.user_id='"+uid+"'),false),'approved',COALESCE(bool_and(r.status='approved'),false),'policy_matches',COALESCE(bool_and(d.policy_ref='"+ref+"'),false)) FROM source_id_link_decisions d JOIN source_id_binding_reviews r ON r.id=d.review_id")
members=q("SELECT json_build_object('active_memberships',COUNT(*),'owner_memberships',COUNT(*) FILTER(WHERE role='owner')) FROM tenant_members WHERE user_id='"+uid+"' AND deleted_at IS NULL AND status='active'")
print(json.dumps({'result':'READ_ONLY','app_image_matches':di(APP,'.Image')==IMAGE,'app_revision':di(IMAGE,'.Config.Labels')['org.opencontainers.image.revision'],'healthy':di(APP,'.State.Health.Status')=='healthy','account':account,'records':records,'session':session,'tokens':tokens,'policy':policy,'memberships':members,'business_config_matches':all(p.flags().get(k)==v for k,v in inputs['approved_environment'].items()),'raw_identity_or_credentials_exported':False}))
