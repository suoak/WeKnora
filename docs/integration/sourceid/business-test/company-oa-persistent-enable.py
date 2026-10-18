"""Persistent test-only OA approval. APP-only, no migration or SID login."""
import os,sys,time,shlex
os.umask(0o077)
R=pathlib.Path('/home/aqyfzx/ai/knowhub-jit-fresh-5f988be51-20261018')
sys.path.insert(0,str(R/'real-window'))
from runtime import jq,env,health,IMAGE,REV,baseline,atomic
APP='knowhub-company-test-app-1';ENTRY='knowhub-company-entry-entry-1'
REF='user-conversation:2026-10-18:company-test-oa-persistent-v1'
assert any(a.get('local')=='172.30.232.42' for n in json.loads(run(['ip','-j','-4','address','show'])) for a in n.get('addr_info',[]))
assert inspect(APP,'.Image')==IMAGE and inspect(IMAGE,'.Config.Labels')['org.opencontainers.image.revision']==REV
assert inspect(APP,'.State.Health.Status')=='healthy'
assert jq("SELECT json_build_object('version',version,'dirty',dirty) FROM schema_migrations",False)=={'version':3030,'dirty':False}
assert jq("SELECT COUNT(*) FROM oidc_transactions WHERE NOT consumed AND expires_at>NOW()",False)==0
assert jq("SELECT COUNT(*) FROM source_id_sessions WHERE NOT revoked AND expires_at>NOW()",False)==0
health('company-test-verified-email-db3030')
labels=inspect(APP,'.Config.Labels');files=labels['com.docker.compose.project.config_files'].split(',')
root=labels['com.docker.compose.project.working_dir']
D=pathlib.Path('/home/aqyfzx/ai/knowhub-company-entry-20261017/private')/('oa-persistent-'+datetime.datetime.now(datetime.timezone.utc).strftime('%Y%m%dT%H%M%SZ'))
D.mkdir(mode=0o700);assert not (D/'result.json').exists()
e=env(APP)
assert e['OIDC_AUTH_REDIRECT_URI']=='http://knowhub.ruijie.com.cn/api/v1/auth/oidc/callback' and e['SOURCEID_BASIC_ENCODING']=='oauth_form'
assert inspect(APP,'.HostConfig.LogConfig')['Type']=='none', 'persistent_sensitive_log_storage_not_allowed'
base=['docker','compose','--project-name','knowhub-company-test','--project-directory',root]
for f in files:base+=['-f',f]
atomic(D/'compose-order.json',files);atomic(D/'original-app-env-protected.json',e)
for i,f in enumerate(files):(D/('compose-'+str(i)+'.backup')).write_bytes(pathlib.Path(f).read_bytes())
config=run(base+['config','--format','json']);(D/'resolved-compose-protected.json').write_bytes(config)
(D/'gateway.conf').write_bytes(pathlib.Path('/home/aqyfzx/ai/knowhub-company-entry-20261017/entry/nginx.conf').read_bytes())
atomic(D/'approval.json',{'reference':REF,'source':'current_user_explicit_message','approved_by':'requesting_user_for_test_environment','executor':'Codex controlled SSH process as aqyfzx','recorded_at':datetime.datetime.now(datetime.timezone.utc).isoformat(),'scope':'persistent OA and JIT in knowhub-company-test only','email_domains':['ruijie.com.cn'],'minutes':[60,60,30,60],'refresh':False,'force_prompt':False,'production_approval':False})
new=dict(OIDC_AUTH_ENABLE='true',SOURCEID_ENABLE='true',SOURCEID_PROTOCOL_ONLY='false',SOURCEID_CONTRACT_CONFIRMED='true',SOURCEID_CLAIMS_SOURCE='userinfo',SOURCEID_EMPLOYEE_CLAIM='attributes.RJGH',SOURCEID_EMAIL_BINDING_ENABLE='true',SOURCEID_EMAIL_CONTRACT_CONFIRMED='true',SOURCEID_AUTO_BINDING_ENABLE='true',SOURCEID_AUTO_BINDING_POLICY_REF=REF,SOURCEID_ACCOUNT_SCOPE='enterprise_all',SOURCEID_ACCOUNT_LINK_MODE='verified_email_policy',SOURCEID_VERIFIED_EMAIL_POLICY_CONFIRMED='true',SOURCEID_VERIFIED_EMAIL_POLICY_REF=REF,SOURCEID_SESSION_MODE='local_approved',SOURCEID_LOCAL_POLICY_REF=REF,SOURCEID_LOCAL_ACCESS_TTL='60m',SOURCEID_LOCAL_ABSOLUTE_TTL='60m',SOURCEID_LOCAL_IDLE_TTL='30m',SOURCEID_LOCAL_REFRESH_ENABLE='false',SOURCEID_ADMISSION_MODE='oidc_sso_approved',SOURCEID_ADMISSION_POLICY_REF=REF,SOURCEID_ADMISSION_MAX_AGE='60m',SOURCEID_SSO_POLICY_CONFIRMED='true',SOURCEID_FORCE_REAUTH_PROMPT_LOGIN='false',SOURCEID_BASIC_ENCODING='oauth_form',SOURCEID_JIT_ENABLE='true',SOURCEID_JIT_POLICY_CONFIRMED='true',SOURCEID_JIT_POLICY_REF=REF,SOURCEID_JIT_EMAIL_DOMAINS='ruijie.com.cn',SOURCEID_LOGOUT_ENABLE='false',SOURCEID_AUTHORIZATION_RECHECK_CONFIRMED='false',AUTO_MIGRATE='false')
pin=D/'persistent-oa.json';atomic(pin,{'services':{'app':{'image':IMAGE,'environment':new}}})
def resources():
 rows={}
 for cid in run(['docker','ps','-aq']).decode().split():
  n=inspect(cid,'.Name')
  if n=='/'+APP:continue
  rows[n]={'id':inspect(cid,'.Id'),'image':inspect(cid,'.Image'),'mounts':sorted([{k:v.get(k) for k in ('Type','Source','Destination','RW')} for v in inspect(cid,'.Mounts')],key=lambda v:(v['Destination'],v['Source'])),'ports':inspect(cid,'.HostConfig.PortBindings'),'networks':inspect(cid,'.NetworkSettings.Networks')}
 return rows
others=resources();atomic(D/'other-resources-protected.json',others)
oldmount=sorted(inspect(APP,'.Mounts'),key=lambda v:v['Destination'])
oldnet=inspect(APP,'.HostConfig.NetworkMode')
oldports=inspect(APP,'.HostConfig.PortBindings')
def start(args):
 run(args+['up','-d','--no-deps','--no-build','--pull','never','--force-recreate','app'],timeout=90)
 for _ in range(60):
  if inspect(APP,'.State').get('Health',{}).get('Status')=='healthy':break
  time.sleep(1)
 else:raise RuntimeError('app_health_failed')
 run(['docker','exec','knowhub-company-test-frontend-1','nginx','-s','reload'])
rollback='#!/bin/sh\nset -eu\n'+shlex.join(base+['up','-d','--no-deps','--no-build','--pull','never','--force-recreate','app'])+'\ndocker exec knowhub-company-test-frontend-1 nginx -s reload\n'
(D/'rollback-app-only.sh').write_text(rollback);(D/'rollback-app-only.sh').chmod(0o700)
stopped=False
try:
 run(['docker','stop','--time','20',APP]);stopped=True
 original=baseline();atomic(D/'baseline-protected.json',original)
 assert len(original['users'])==3 and len(original['external_identities'])==1 and len(original['source_id_jit_registrations'])==0
 with (D/'db3030.dump').open('wb') as f:
  z=subprocess.run(['docker','exec','knowhub-company-test-postgres-1','pg_dump','-U','clean_owner','-d','knowhub_oa_sso_pilot','-Fc','--no-owner','--no-acl'],stdout=f,stderr=subprocess.PIPE,timeout=120);assert z.returncode==0
 with (D/'files.tar').open('wb') as f:
  z=subprocess.run(['docker','run','--rm','--network','none','--log-driver','none','--mount','type=volume,source=knowhub-company-test-files,target=/scan,readonly','--entrypoint','tar',IMAGE,'-C','/scan','-cf','-','.'],stdout=f,stderr=subprocess.PIPE,timeout=90);assert z.returncode==0
 assert (D/'db3030.dump').stat().st_size>1000 and (D/'files.tar').stat().st_size>1024
 start(base+['-f',str(pin)])
 loaded=env(APP);assert all(loaded.get(k)==v for k,v in new.items())
 assert all(loaded.get(k)==v for k,v in e.items() if k not in new)
 assert inspect(APP,'.Image')==IMAGE and inspect(IMAGE,'.Config.Labels')['org.opencontainers.image.revision']==REV
 assert sorted(inspect(APP,'.Mounts'),key=lambda v:v['Destination'])==oldmount
 assert inspect(APP,'.HostConfig.NetworkMode')==oldnet and inspect(APP,'.HostConfig.PortBindings')==oldports
 assert resources()==others
 assert baseline()==original
 assert jq("SELECT json_build_object('version',version,'dirty',dirty) FROM schema_migrations",False)=={'version':3030,'dirty':False}
 routes=health('company-test-verified-email-db3030')
 result={'result':'PERSISTENT_TEST_OA_ENABLED','revision':REV,'image':IMAGE,'schema':3030,'dirty':False,'approval_ref':REF,'backup_directory':str(D),'compose_override':str(pin),'rollback_script':str(D/'rollback-app-only.sh'),'database_and_files_backed_up_same_frozen_generation':True,'original_accounts_permissions_bindings_and_knowledge_unchanged':True,'other_containers_resources_unchanged':True,'only_app_recreated':True,'routes':routes,'effective_flags':new,'real_sid_auth_calls':0,'continuous_enablement':True,'rollback_triggered':False}
 atomic(D/'result.json',result)
 atomic(pathlib.Path('/home/aqyfzx/ai/knowhub-company-entry-20261017/private/active-persistent-oa.json'),{'directory':str(D),'override':str(pin),'compose_files':files+[str(pin)],'approval_ref':REF})
 print(json.dumps(result))
except BaseException:
 recovery={'result':'FAILED','rollback_attempted':stopped}
 if stopped:
  try:
   start(base);assert env(APP)==e;health('company-test-verified-email-db3030');recovery['rollback']='PASS'
  except BaseException:recovery['rollback']='FAIL_REQUIRES_RECOVERY'
 atomic(D/'failure-safe.json',recovery)
 print(json.dumps(recovery))
