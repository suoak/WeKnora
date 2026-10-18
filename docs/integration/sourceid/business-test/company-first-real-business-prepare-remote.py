"""Prepare one authorized existing-account window; no enable or real login."""
import os,ipaddress,sys,uuid
os.umask(0o077)
B=pathlib.Path('/home/aqyfzx/ai/knowhub-verified-email-candidate-5f988be51');R=pathlib.Path('/home/aqyfzx/ai/knowhub-company-entry-20261017')
sys.path.insert(0,str(B/'monitor/business-test'))
from business_monitor_pilot import DockerPilotPort,protected,COMPOSE_FILES,APP,PG,IMAGE,REVISION,SCHEMA
from business_monitor_adapter import CLOSED
assert inspect(APP,'.Image')==IMAGE and inspect(APP,'.State.Health.Status')=='healthy'
entry_dir=pathlib.Path(json.loads((B/'oa-entry-operation.json').read_text())['private_directory']);entry=entry_dir/'approved-entry-only.json'
assert inspect(APP,'.Config.Labels')['com.docker.compose.project.config_files'].split(',')==COMPOSE_FILES+[str(entry)]
selected=protected(R/'private/first-account-preparation/selected-account.json');uid=selected['existing_user_id'];assert str(uuid.UUID(uid))==uid
source=str(ipaddress.ip_address(os.environ['SSH_CONNECTION'].split()[0]));assert 'allow '+source+';' in (R/'entry/nginx.conf').read_text()
ref='user-conversation:2026-10-18:existing-account-business-approval-v1'
env={'SOURCEID_CLAIMS_SOURCE':'userinfo','SOURCEID_EMPLOYEE_CLAIM':'attributes.RJGH','OIDC_AUTH_ENABLE':'true','SOURCEID_PROTOCOL_ONLY':'false','SOURCEID_CONTRACT_CONFIRMED':'true','SOURCEID_EMAIL_BINDING_ENABLE':'true','SOURCEID_EMAIL_CONTRACT_CONFIRMED':'true','SOURCEID_AUTO_BINDING_ENABLE':'true','SOURCEID_AUTO_BINDING_POLICY_REF':ref,'SOURCEID_ACCOUNT_SCOPE':'enterprise_all','SOURCEID_ACCOUNT_LINK_MODE':'verified_email_policy','SOURCEID_VERIFIED_EMAIL_POLICY_CONFIRMED':'true','SOURCEID_VERIFIED_EMAIL_POLICY_REF':ref,'SOURCEID_SESSION_MODE':'local_approved','SOURCEID_LOCAL_POLICY_REF':ref,'SOURCEID_LOCAL_ACCESS_TTL':'60m','SOURCEID_LOCAL_ABSOLUTE_TTL':'60m','SOURCEID_LOCAL_IDLE_TTL':'30m','SOURCEID_LOCAL_REFRESH_ENABLE':'false','SOURCEID_ADMISSION_MODE':'oidc_sso_approved','SOURCEID_ADMISSION_POLICY_REF':ref,'SOURCEID_ADMISSION_MAX_AGE':'60m','SOURCEID_SSO_POLICY_CONFIRMED':'true','SOURCEID_FORCE_REAUTH_PROMPT_LOGIN':'false','SOURCEID_BASIC_ENCODING':'oauth_form','SOURCEID_DIAGNOSTICS_ENABLE':'true','SOURCEID_JIT_ENABLE':'false','AUTO_MIGRATE':'false'}
P=R/'private'/('real-business-'+datetime.datetime.now(datetime.timezone.utc).strftime('%Y%m%dT%H%M%SZ'));P.mkdir(mode=0o700);observer=P/'observer';observer.mkdir(mode=0o700)
inputs={'existing_user_id':uid,'existing_entry_override':str(entry),'approved_environment':env,'business_approval_ref':ref,'scope_evidence_ref':'current-test-entry-approved-source-firewall-and-single-transaction-monitor','company_policy_ref':ref,'positive_scope_approval_ref':ref,'acceptance_scope':'A_positive','protected_observer_directory':str(observer),'observation_mode':'server_metadata','approved_client_address':source,'allowed_kb_ids':[],'allowed_document_ids':[]}
port=DockerPilotPort.__new__(DockerPilotPort)
q=lambda s:json.loads(port.query(s))
account=q("SELECT json_build_object('exists',COUNT(*)=1,'active',bool_and(is_active AND deleted_at IS NULL),'system_admin',bool_and(is_system_admin),'cross_space',bool_and(can_access_all_tenants)) FROM users WHERE id='"+uid+"'");assert account['exists'] and account['active'] and account['system_admin']
kbs=q("SELECT COALESCE(json_agg(id),'[]') FROM knowledge_bases k WHERE deleted_at IS NULL AND EXISTS(SELECT 1 FROM tenant_members m WHERE m.user_id='"+uid+"' AND m.tenant_id=k.tenant_id AND m.deleted_at IS NULL AND m.status='active')");assert kbs
docs=q("SELECT COALESCE(json_agg(id),'[]') FROM knowledges WHERE deleted_at IS NULL AND knowledge_base_id IN ("+','.join("'"+k+"'" for k in kbs)+")");assert len(docs)==3
inputs['allowed_kb_ids']=kbs;inputs['allowed_document_ids']=docs
(P/'approved-inputs.json').write_text(json.dumps(inputs));(P/'approval-basis.json').write_text(json.dumps({'reference':ref,'source':'current_user_message','candidate_minutes':[60,60,30,60],'test_only':True,'jit_approved':False,'full_negative_scope_approved':False}))
port=DockerPilotPort(inputs,'existing');i=port.inventory();before=port.snapshot();assert i['revision']==REVISION and i['schema']==3030 and not i['dirty'] and i['healthy'];assert all(i['flags'].get(k)==v for k,v in port.closed_baseline.items())
assert not any(not t.consumed and t.expires>datetime.datetime.now(datetime.timezone.utc).timestamp() for t in port.transactions())
assert not before['sessions'] and not before['jit'] and not before['ownership']
checks={k:i['flags'].get(k) for k in ('SOURCEID_ENABLE','SOURCEID_CLAIMS_SOURCE','SOURCEID_EMPLOYEE_CLAIM')}
# Non-secret claim-path prerequisites read only, not real employee claim values.
fields=['SOURCEID_ENABLE','SOURCEID_CLAIMS_SOURCE','SOURCEID_EMPLOYEE_CLAIM','SOURCEID_LOGOUT_ENABLE','SOURCEID_AUTHORIZATION_RECHECK_CONFIRMED']
claimcfg=json.loads(run(['docker','exec',APP,'python3','-c',"import os,json,sys;print(json.dumps({k:os.environ.get(k) for k in json.loads(sys.argv[1])}))",json.dumps(fields)]))
assert claimcfg['SOURCEID_ENABLE']=='true' and claimcfg['SOURCEID_CLAIMS_SOURCE'] in (None,'userinfo') and claimcfg['SOURCEID_EMPLOYEE_CLAIM'] in (None,'attributes.RJGH')
assert claimcfg['SOURCEID_LOGOUT_ENABLE']=='false' and claimcfg['SOURCEID_AUTHORIZATION_RECHECK_CONFIRMED']=='false'
(B/'real-business-operation.json').write_text(json.dumps({'prepared_directory':str(P),'monitor_directory':str(port.dir),'observer_directory':str(observer)}))
out={'result':'PREPARED_NOT_ENABLED','app_revision':REVISION,'schema':SCHEMA,'existing_account_active':account['active'],'original_system_admin':account['system_admin'],'original_cross_space':account['cross_space'],'allowed_knowledge_count':len(kbs),'allowed_document_count':len(docs),'monitor_baseline_and_routes':'PASS','verified_email_mode':True,'session_minutes':[60,60,30,60],'jit':False,'current_business_gates_opened':False,'real_sid_requests':0,'user_values_exported':False,'approval_reference':ref};(P/'preparation-result.json').write_text(json.dumps(out));print(json.dumps(out))
