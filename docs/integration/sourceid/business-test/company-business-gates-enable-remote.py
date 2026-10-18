"""Explicitly approved existing-account business access; no SID request."""
import sys,os,uuid,time
os.umask(0o077)
B=pathlib.Path('/home/aqyfzx/ai/knowhub-verified-email-candidate-5f988be51')
state=json.loads((B/'real-business-operation.json').read_text());P=pathlib.Path(state['prepared_directory'])
inputs=json.loads((P/'approved-inputs.json').read_text())
sys.path.insert(0,str(B/'monitor/business-test'))
from business_monitor_pilot import DockerPilotPort,APP,PG,IMAGE,REVISION,command,atomic,inspect as docker_inspect
p=DockerPilotPort(inputs,'existing')
assert docker_inspect(APP,'.Image')==IMAGE and docker_inspect(IMAGE,'.Config.Labels')['org.opencontainers.image.revision']==REVISION
assert docker_inspect(APP,'.State.Health.Status')=='healthy'
flags=p.flags();assert flags['SOURCEID_PROTOCOL_ONLY']=='true' and flags['SOURCEID_BASIC_ENCODING']=='oauth_form'
schema=json.loads(p.query("SELECT json_build_object('version',version,'dirty',dirty) FROM schema_migrations"));assert schema=={'version':3030,'dirty':False}
assert not any(not t.consumed and t.expires>time.time() for t in p.transactions())
assert docker_inspect(APP,'.Config.Labels')['com.docker.compose.project.config_files'].split(',')==p.base_compose
before=p.snapshot();assert len(before['users'])==3 and not before['jit']
uid=inputs['existing_user_id'];ref=inputs['company_policy_ref']
# Select identifiers only, never token contents, email, employee ID or claims.
sessions=json.loads(p.query("SELECT COALESCE(json_agg(json_build_object('id',s.id,'user_id',s.user_id)),'[]') FROM source_id_sessions s JOIN source_id_local_link_policies l ON l.id=s.id WHERE NOT s.revoked AND s.expires_at>NOW() AND s.user_id='"+uid+"' AND l.policy_ref='"+ref+"'"))
assert len(sessions)<=1
total=int(p.query("SELECT COUNT(*) FROM source_id_sessions WHERE NOT revoked AND expires_at>NOW()"));assert total==len(sessions)
op=p.dir
atomic(op/'before-safe.json',{'revision':REVISION,'schema':schema,'gates':flags,'other_services':before['other_services']})
for index,name in enumerate(p.base_compose):
    target=op/('compose-'+str(index)+'.backup');target.write_bytes(pathlib.Path(name).read_bytes());target.chmod(0o600)
for s in sessions:
    p.revoke_exact_session_and_access(s);assert p.revocation_verified(s)
atomic(p.override,{'services':{'app':{'environment':p.env}}})
try:
    p.recreate(True)
    actual=p.flags();assert all(actual.get(k)==v for k,v in p.env.items())
    assert p.other_services()==before['other_services']
    after=p.snapshot()
    for key in ('users','members','org_members','tenants','jit','ownership'):
        assert before[key]==after[key]
    assert json.loads(p.query("SELECT json_build_object('version',version,'dirty',dirty) FROM schema_migrations"))==schema
    checks={}
    for port,path,expected in [(80,'/login',200),(8080,'/login',401),(80,'/api/v1/auth/oidc/config',200),(8080,'/api/v1/auth/oidc/config',200)]:
        status=command(['curl','--noproxy','*','-sS','-o','/dev/null','-w','%{http_code}','http://127.0.0.1:'+str(port)+path]).decode();assert status==str(expected);checks[str(port)+path]=int(status)
except BaseException:
    p.recreate(False)
    raise
atomic(B/'active-business-config.json',{'directory':str(op),'override':str(p.override),'base_compose':p.base_compose,'prepared_directory':str(P),'user_approved_open':True})
out={'result':'BUSINESS_GATES_ENABLED','revision':REVISION,'schema':3030,'dirty':False,'effective_config_matches':True,'health':checks,'original_accounts_and_permissions_preserved':True,'previous_test_sessions_revoked':len(sessions),'business_flags':{k:actual[k] for k in ('OIDC_AUTH_ENABLE','SOURCEID_PROTOCOL_ONLY','SOURCEID_CONTRACT_CONFIRMED','SOURCEID_EMAIL_BINDING_ENABLE','SOURCEID_EMAIL_CONTRACT_CONFIRMED','SOURCEID_AUTO_BINDING_ENABLE','SOURCEID_ACCOUNT_LINK_MODE','SOURCEID_VERIFIED_EMAIL_POLICY_CONFIRMED','SOURCEID_SESSION_MODE','SOURCEID_ADMISSION_MODE','SOURCEID_LOCAL_ACCESS_TTL','SOURCEID_LOCAL_ABSOLUTE_TTL','SOURCEID_LOCAL_IDLE_TTL','SOURCEID_ADMISSION_MAX_AGE','SOURCEID_LOCAL_REFRESH_ENABLE','SOURCEID_JIT_ENABLE','SOURCEID_FORCE_REAUTH_PROMPT_LOGIN')},'automatic_gate_closure_on_monitor_uncertainty':False,'real_sid_requests':0}
atomic(op/'enable-result.json',out);print(json.dumps(out))
