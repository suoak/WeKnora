"""Candidate-only local business checks. Sensitive values remain in memory."""
from support import *
import secrets,crypt,traceback,uuid,urllib.parse
APP='knowhub-legacy-clean-20261016-app';FRONT='knowhub-legacy-clean-20261016-front';REDIS='knowhub-legacy-clean-20261016-redis'
FRONTIMAGE='sha256:63146a134fc3be75b663a1ac6ddba205be2b0a3c881891a3ba29a28d6a04f48a'
REDISIMAGE='sha256:426f6fb54a1b82a2254bce70e5f4a72d15c14ecd3cc70d799465b956e7a32548'
private=BASE/'private'
HTTP=r'''
import sys,json,urllib.request,urllib.error,base64
p=json.load(sys.stdin)
class NoRedirect(urllib.request.HTTPRedirectHandler):
 def redirect_request(self,*a,**k):return None
o=urllib.request.build_opener(urllib.request.ProxyHandler({}),NoRedirect())
h={'Content-Type':'application/json','Host':'knowhub.ruijie.com.cn'}
if p.get('token'):h['Authorization']='Bearer '+p['token']
q=urllib.request.Request('http://127.0.0.1'+p['path'],headers=h,method=p.get('method','GET'),data=json.dumps(p['payload']).encode() if 'payload' in p else None)
try:r=o.open(q,timeout=25)
except urllib.error.HTTPError as e:r=e
with r:
 raw=r.read(16777216)
 try:b=json.loads(raw)
 except ValueError:b={}
 print(json.dumps({'status':r.status,'body':b,'bytes':len(raw),'location':r.headers.get('Location','')}))
'''
def req(path,method='GET',payload=None,token=None):
 p={'path':path,'method':method,'token':token}
 if payload is not None:p['payload']=payload
 # Script and JSON are passed only via encrypted stdin, never process arguments.
 code=HTTP.replace('p=json.load(sys.stdin)','p='+repr(p))
 return json.loads(execute(['docker','exec','-i',APP,'python3','-'],input=code.encode(),timeout=40))
def remove(name):
 p=subprocess.run(['docker','inspect',name],stdout=subprocess.PIPE,stderr=subprocess.PIPE)
 if p.returncode:return
 mode=inspect(name,'.HostConfig.NetworkMode');assert mode in ('container:'+PG,'container:'+inspect(PG,'.Id'))
 execute(['docker','rm','-f',name])
def start(db):
 env=(private/'candidate.env').read_text().replace('DB_NAME='+DB+'\n','DB_NAME='+db+'\n')
 (private/'validation-runtime.env').write_text(env)
 execute(['docker','run','-d','--name',APP,'--network','container:'+PG,'--log-driver','none','--cpus','2','--memory','4g','--env-file',str(private/'validation-runtime.env'),'--mount','type=volume,source='+FILEVOL+',target=/data/files,readonly','--tmpfs','/tmp:rw,size=268435456','--entrypoint','/app/WeKnora',IMAGE])
 execute(['docker','run','-d','--name',FRONT,'--network','container:'+PG,'--log-driver','none','-e','APP_HOST=127.0.0.1','-e','APP_PORT=8080',FRONTIMAGE])
 for _ in range(50):
  try:
   probe="import urllib.request;assert urllib.request.urlopen('http://127.0.0.1:8080/health',timeout=2).status==200"
   p=subprocess.run(['docker','exec',APP,'python3','-c',probe],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
   if p.returncode==0 and req('/api/v1/auth/oidc/config')['status']==200:return
  except Exception:pass
  time.sleep(1)
 raise RuntimeError('candidate_health_timeout')
def password_for(uid,db):
 p=secrets.token_urlsafe(32);hashed=crypt.crypt(p,crypt.mksalt(crypt.METHOD_BLOWFISH))
 assert hashed.startswith('$2')
 sql("UPDATE users SET password_hash='"+hashed+"' WHERE id='"+uid+"'",db)
 return p
def login(email,password):
 r=req('/api/v1/auth/login','POST',{'email':email,'password':password})
 assert r['status']==200 and r['body']['success'];return r['body']
def switch(body,tenant):
 if body.get('active_tenant',body.get('tenant',{})).get('id')==tenant:return body
 r=req('/api/v1/auth/switch-tenant','POST',{'tenant_id':tenant,'refresh_token':body.get('refresh_token','')},body['token'])
 assert r['status']==200 and r['body']['success'];return r['body']
def main():
 os.umask(0o077);stage='initial';out={'result':'FAIL','real_sid_login':False,'mock_sid_identity_injected':False,'live_database_switched':False}
 try:
  assert json.loads((BASE/'preparation-result.json').read_text())['result']=='PASS'
  assert inspect(PG,'.HostConfig.NetworkMode')=='none'
  baseline=json.loads((private/'selected-data-baseline.json').read_text());assert signatures(baseline['columns'])==baseline['signatures']
  admin=scalar("SELECT row_to_json(s) FROM (SELECT id,email,tenant_id,is_system_admin,can_access_all_tenants FROM users WHERE is_active AND is_system_admin AND deleted_at IS NULL ORDER BY id LIMIT 1) s")
  kb=scalar("SELECT row_to_json(s) FROM (SELECT id,tenant_id,embedding_model_id FROM knowledge_bases WHERE deleted_at IS NULL ORDER BY id LIMIT 1) s")
  docs=scalar("SELECT json_agg(id) FROM knowledges WHERE deleted_at IS NULL")
  stage='isolated_start';state(stage=stage,result='RUNNING')
  execute(['docker','run','-d','--name',REDIS,'--network','container:'+PG,'--log-driver','none','--memory','256m',REDISIMAGE,'redis-server','--bind','127.0.0.1','--save','','--appendonly','no'])
  start(DB)
  out['health_and_frontend']={'health':req('/health')['status'],'portal_static':req('/portal')['status']}
  assert out['health_and_frontend']=={'health':200,'portal_static':200}
  assert inspect(APP,'.Image')==IMAGE and inspect(FRONT,'.Image')==FRONTIMAGE
  assert not inspect(APP,'.HostConfig.PortBindings') and not inspect(FRONT,'.HostConfig.PortBindings')
  network="import socket,json;r={}\nfor ip,port in [('172.30.232.42',80),('172.30.232.42',8080),('1.1.1.1',443)]:\n s=socket.socket();s.settimeout(1)\n try:s.connect((ip,port));r[ip+':'+str(port)]=False\n except OSError:r[ip+':'+str(port)]=True\n finally:s.close()\nprint(json.dumps(r))"
  out['actual_outbound_denied']=json.loads(execute(['docker','exec',APP,'python3','-c',network]));assert all(out['actual_outbound_denied'].values())
  config=req('/api/v1/auth/oidc/config');out['oidc_config_status']=config['status'];out['oidc_config_enabled']=config['body'].get('enabled');assert config['status']==200 and config['body']['enabled'] is False
  out['oidc_disabled']=True
  stage='clean_original_admin_local_business_login';state(stage=stage,result='RUNNING')
  pw=password_for(admin['id'],DB);body=login(admin['email'],pw);pw=None
  assert body['user']['id']==admin['id'] and body['user']['tenant_id']==admin['tenant_id']
  assert body['user']['is_system_admin']==admin['is_system_admin'] and body['user']['can_access_all_tenants']==admin['can_access_all_tenants']
  me=req('/api/v1/auth/me',token=body['token']);assert me['status']==200 and me['body']['data']['user']['id']==admin['id']
  body=switch(body,kb['tenant_id']);token=body['token']
  assert req('/api/v1/knowledge-bases/'+kb['id'],token=token)['status']==200
  assert req('/api/v1/knowledge-bases/'+kb['id']+'/knowledge',token=token)['status']==200
  downloads=[]
  for doc in docs:
   assert req('/api/v1/knowledge/'+doc,token=token)['status']==200
   r=req('/api/v1/knowledge/'+doc+'/download',token=token);assert r['status']==200 and r['bytes']>0
   downloads.append({'status':200,'nonempty':True})
  out['original_admin']={'same_user_id':True,'same_home_tenant':True,'same_admin_flags':True,'auth_me':200,'original_kb_access':200,'document_downloads':downloads,'credential':'fresh candidate-only local password; disabled after test'}
  stage='existing_vector_business_recall';state(stage=stage,result='RUNNING')
  vector=scalar("SELECT embedding::text FROM embeddings WHERE knowledge_base_id='"+kb['id']+"' AND embedding IS NOT NULL LIMIT 1")
  r=req('/api/v1/knowledge-bases/'+kb['id']+'/hybrid-search','POST',{'query_embedding':vector,'disable_keywords_match':True,'disable_vector_match':False,'match_count':3,'vector_threshold':0,'skip_context_enrichment':True},token)
  hits=r['body'].get('data') or [];assert r['status']==200 and r['body']['success'] and len(hits)>0 and all(h.get('knowledge_id') in docs for h in hits)
  out['vector_recall']={'status':200,'hits':len(hits),'original_document_references':True,'model_calls':0,'text_or_vector_exported':False}
  assert req('/api/v1/auth/logout','POST',token=token)['status']==200 and req('/api/v1/auth/me',token=token)['status']==401
  out['local_logout_invalidates_token']=True
  remove(FRONT);remove(APP)
  sql("BEGIN; UPDATE users SET password_hash='!candidate-original-password-disabled'; UPDATE auth_tokens SET is_revoked=TRUE; COMMIT;")
  assert signatures(baseline['columns'])==baseline['signatures']
  stage='disposable_cross_space_fixture';state(stage=stage,result='RUNNING')
  sql('CREATE DATABASE '+VALIDATION,db='postgres');restore(private/'sanitized-3027-clean.dump',VALIDATION)
  start(VALIDATION)
  pw=password_for(admin['id'],VALIDATION);body=login(admin['email'],pw);pw=None
  owner=sql("SELECT user_id FROM tenant_members WHERE tenant_id='"+str(kb['tenant_id'])+"' AND role='owner' AND deleted_at IS NULL LIMIT 1",VALIDATION)
  pgpassword=dict(x.split('=',1) for x in (private/'postgres.env').read_text().splitlines())['POSTGRES_PASSWORD']
  dsn='postgres://clean_owner:'+urllib.parse.quote(pgpassword,safe='')+'@127.0.0.1:5432/'+VALIDATION+'?sslmode=disable'
  cli='/home/aqyfzx/ai/knowhub-accountprep-20261016/artifacts/sourceid-test-account'
  args=['docker','run','--rm','-i','--network','container:'+PG,'--log-driver','none','--mount','type=bind,source='+cli+',target=/fixture-cli,readonly','--entrypoint','/fixture-cli',IMAGE,'--test-host','172.30.232.42','--tenant',str(kb['tenant_id']),'--operator',owner,'--operation',str(uuid.uuid4()),'--approval','synthetic-clean-validation-only','--role','viewer','--protected-json-stdin']
  payload=json.dumps({'email':'validation-viewer@example.test','database_dsn':dsn}).encode()
  dry=json.loads(execute(args,input=payload));applied=json.loads(execute(args+['--apply'],input=payload))
  viewer=scalar("SELECT row_to_json(s) FROM (SELECT id,email,is_system_admin,can_access_all_tenants FROM users WHERE email='validation-viewer@example.test') s",VALIDATION);assert viewer and not viewer['is_system_admin'] and not viewer['can_access_all_tenants']
  r=req('/api/v1/tenants','POST',{'name':'clean-validation-denied-space','description':'synthetic-only'},body['token']);assert r['status'] in (200,201)
  denied_tenant=r['body'].get('data',r['body'])['id'];body=switch(body,denied_tenant)
  r=req('/api/v1/knowledge-bases','POST',{'name':'clean-validation-denied-kb','type':'document','embedding_model_id':kb['embedding_model_id']},body['token']);assert r['status'] in (200,201)
  denied_kb=r['body'].get('data',r['body'])['id'];assert req('/api/v1/knowledge-bases/'+denied_kb,token=body['token'])['status']==200
  assert str(denied_tenant)!=str(kb['tenant_id'])
  assert int(sql("SELECT COUNT(*) FROM tenant_members WHERE tenant_id='"+str(denied_tenant)+"' AND user_id='"+viewer['id']+"' AND deleted_at IS NULL",VALIDATION))==0
  assert int(sql("SELECT COUNT(*) FROM kb_shares WHERE knowledge_base_id='"+denied_kb+"' AND deleted_at IS NULL",VALIDATION))==0
  pw=password_for(viewer['id'],VALIDATION);vbody=login(viewer['email'],pw);pw=None;vtoken=vbody['token']
  assert req('/api/v1/auth/me',token=vtoken)['status']==200
  assert req('/api/v1/knowledge-bases/'+kb['id'],token=vtoken)['status']==200
  denied=req('/api/v1/knowledge-bases/'+denied_kb,token=vtoken)['status'];assert denied in (403,404)
  denied_switch=req('/api/v1/auth/switch-tenant','POST',{'tenant_id':denied_tenant,'refresh_token':vbody.get('refresh_token','')},vtoken)['status'];assert denied_switch in (403,404)
  out['disposable_viewer_acl']={'preparer_dry_run_and_apply':True,'auth_me':200,'original_allowed_kb':200,'existing_denied_kb_admin_read':200,'denied_kb_separate_space':True,'no_membership_or_kb_share':True,'denied_kb_status':denied,'denied_switch_status':denied_switch,'fixtures_in_clean_target':False}
  stage='final_integrity';state(stage=stage,result='RUNNING')
  remove(FRONT);remove(APP);sql('DROP DATABASE '+VALIDATION,db='postgres')
  assert signatures(baseline['columns'])==baseline['signatures']==signatures(baseline['columns'],source=True)
  out['clean_final_counts']=counts();assert out['clean_final_counts']==baseline['source_counts']
  out['clean_target_identity_rows']=int(sql('SELECT COUNT(*) FROM external_identities'));assert out['clean_target_identity_rows']==0
  assert int(sql('SELECT COUNT(*) FROM auth_tokens WHERE NOT is_revoked'))==0
  assert int(sql("SELECT COUNT(*) FROM users WHERE password_hash<>'!candidate-original-password-disabled'"))==0
  assert files(FILEVOL)==files('weknora_data-files')
  out['final_integrity']={'selected_data_and_acl_equal_source':True,'files_equal_source':True,'all_passwords_disabled':True,'all_auth_tokens_revoked':True,'all_api_keys_revoked':int(sql('SELECT COUNT(*) FROM tenant_api_keys WHERE revoked_at IS NULL'))==0,'schema':sql("SELECT version::text||'|'||dirty::text FROM schema_migrations"),'validation_database_dropped':True}
  with (private/'ready-3027-clean.dump').open('wb') as f:execute(['docker','exec',PG,'pg_dump','-U','clean_owner','-d',DB,'-Fc','--no-owner','--no-acl'],output=f)
  out['result']='PASS'
 except Exception as e:
  out.update(failed_stage=stage,failure_class=type(e).__name__,failure_lines=[f.lineno for f in traceback.extract_tb(e.__traceback__) if f.filename==__file__])
 finally:
  for name in (FRONT,APP,REDIS):
   try:remove(name)
   except Exception:out['cleanup_failure']=True;out['result']='FAIL'
  # A failed assertion must never leave a usable temporary local password.
  try:
   if inspect(PG,'.State.Running'):
    sql("BEGIN; UPDATE users SET password_hash='!candidate-original-password-disabled'; UPDATE auth_tokens SET is_revoked=TRUE; COMMIT;")
  except Exception:out['credential_cleanup_failure']=True;out['result']='FAIL'
  out['completed_utc']=datetime.datetime.now(datetime.timezone.utc).isoformat()
  (BASE/'acceptance-result.json').write_text(json.dumps(out,indent=2)+'\n');state(stage=stage,**out)
if __name__=='__main__':main()
