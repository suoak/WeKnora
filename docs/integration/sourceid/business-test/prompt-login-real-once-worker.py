"""One authorized operator transaction, bounded observation, finally restore false.
Shared read-only metadata helpers are prepended by the protected SSH launcher.
Never persist raw Docker logs, claims or credentials.
"""
import os,time,yaml,importlib.util,traceback,hashlib
ROOT=pathlib.Path('/home/aqyfzx/ai/knowhub-prompt-login-real-20261016')
LIVE=pathlib.Path('/home/aqyfzx/ai/knowhub-sourceid-business-test-db1b2cd05')
RELEASE=pathlib.Path('/home/aqyfzx/ai/knowhub-prompt-login-479416c90')
IMAGE='sha256:85f9d6c052298e7f52e7d7c4533e131ce19326d461521378e90c6df3b01fa49c'
FILES=json.loads((RELEASE/'app-deployment-result.json').read_text())['compose_files_in_order']
TEMP=ROOT/'private/protocol-once.yml'
def command(extra=False):
 a=['docker','compose','--project-name','knowhub-sourceid-business','--project-directory',str(LIVE)]
 for p in FILES+([str(TEMP)] if extra else []):a+=['-f',p]
 return a
def sql(q):
 return run(['docker','exec','-i','knowhub-sourceid-business-postgres-1','sh','-c','exec psql -X -U "$POSTGRES_USER" -d "$POSTGRES_DB" -qAt -v ON_ERROR_STOP=1']) if False else subprocess.run(['docker','exec','-i','knowhub-sourceid-business-postgres-1','sh','-c','exec psql -X -U "$POSTGRES_USER" -d "$POSTGRES_DB" -qAt -v ON_ERROR_STOP=1'],input=('BEGIN READ ONLY;'+q+';ROLLBACK;').encode(),stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=15)
def query(q):
 p=sql(q);assert p.returncode==0;return p.stdout.decode().strip()
def snapshot():
 ids=run(['docker','ps','-a','--filter','label=com.docker.compose.project=knowhub-sourceid-business','--format','{{.ID}}']).decode().split()
 return {m['service']:m for m in (metadata(i) for i in ids)}
def flags():
 keys=('SOURCEID_FORCE_REAUTH_PROMPT_LOGIN','SOURCEID_PROTOCOL_ONLY','SOURCEID_CONTRACT_CONFIRMED','SOURCEID_EMAIL_BINDING_ENABLE','SOURCEID_EMAIL_CONTRACT_CONFIRMED','SOURCEID_AUTO_BINDING_ENABLE','SOURCEID_AUTHORIZATION_RECHECK_CONFIRMED','SOURCEID_SESSION_MODE','SOURCEID_LOCAL_REFRESH_ENABLE','SOURCEID_BASIC_ENCODING','OIDC_AUTH_ENABLE','SOURCEID_DIAGNOSTICS_ENABLE','AUTO_MIGRATE')
 out={}
 for k in keys:
  v=run(['docker','exec','knowhub-sourceid-business-app-1','sh','-c','printf "%s" "${'+k+'-NOT_SET}"']).decode();assert v in ('true','false','NOT_SET','external','oauth_form');out[k]=v
 return out
def counts():
 return {t:int(query('SELECT COUNT(*) FROM '+t)) for t in ('users','tenant_members','external_identities','source_id_binding_reviews','source_id_sessions','source_id_local_sessions')}
def ownership():
 # Private baseline only, no digests exported; detects user and role changes.
 return [query("SELECT COALESCE(md5(string_agg(row_to_json(s)::text,'')),'') FROM (SELECT "+cols+' FROM '+t+' ORDER BY id) s') for t,cols in [('users','id,tenant_id,email,is_active,is_system_admin,can_access_all_tenants'),('tenant_members','id,user_id,tenant_id,role,status,deleted_at')]]
def healthy():
 for _ in range(60):
  s=snapshot()['app']
  if s['image_id']==IMAGE and s['health']=='healthy':return
  time.sleep(1)
 raise RuntimeError('health_timeout')
def state(phase,**kw):
 (ROOT/'status.json').write_text(json.dumps({'phase':phase,'utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),**kw},indent=2)+'\n')
def load(name,path):
 s=importlib.util.spec_from_file_location(name,path);m=importlib.util.module_from_spec(s);s.loader.exec_module(m);return m
def collect():
 ex=load('safe_export',ROOT/'diagnostics/business-test/export_token_diagnostics.py')
 rules=ex.rules
 p=subprocess.run(['docker','logs','--since',START,'--tail','3000','knowhub-sourceid-business-app-1'],stdout=subprocess.PIPE,stderr=subprocess.STDOUT,timeout=15)
 assert p.returncode==0
 out={'events':[],'stages':[],'schemas':{},'raw_logs_saved':False}
 for line in p.stdout.decode('utf-8','replace').splitlines():
  if len(line)>65536:continue
  event=ex.extract(line)
  if event is not None:
   if len(out['events'])<256:out['events'].append(event)
   continue
  start=line.find('{"event"')
  if start<0:continue
  try:e=json.JSONDecoder().raw_decode(line[start:])[0]
  except (ValueError,TypeError):continue
  if not isinstance(e,dict) or not re.fullmatch(r'[a-f0-9-]{36}',e.get('trace','')):continue
  if e.get('event')=='sourceid_stage' and e.get('stage') in {'authorize','callback_state','token','id_token','userinfo','identity_mapping','local_session','callback_result'} and e.get('category') in {'none','rejected','token_http_401','nonce_mismatch','enrollment_required','identity_conflict','account_disabled','identity_fields_missing','subject_mismatch','id_token_verification'} and type(e.get('pass')) is bool and rules.integer(e.get('duration_ms')):
   out['stages'].append({k:e[k] for k in ('trace','stage','pass','duration_ms','category')})
  elif e.get('event')=='sourceid_verified_schema' and e.get('stage') in {'id_token','userinfo'} and rules.schema(e.get('schema')):
   out['schemas'][e['stage']]=e['schema']
 return out
START=datetime.datetime.now(datetime.timezone.utc).isoformat()
out={'result':'FAIL','real_login_authorized_once':True,'started_utc':START,'operator_sso_and_route':'USER_CONFIRMED','protocol_only':True,'production_accessed':False}
changed=False;before=None
try:
 os.umask(0o077);before=snapshot();baseline=counts();own=ownership();original=flags()
 assert before['app']['image_id']==IMAGE and before['app']['health']=='healthy'
 assert original['SOURCEID_FORCE_REAUTH_PROMPT_LOGIN']=='false' and original['SOURCEID_PROTOCOL_ONLY']=='true'
 assert all(original[k]=='false' for k in ('SOURCEID_CONTRACT_CONFIRMED','SOURCEID_EMAIL_BINDING_ENABLE','SOURCEID_AUTO_BINDING_ENABLE','SOURCEID_AUTHORIZATION_RECHECK_CONFIRMED'))
 assert query("SELECT version::text||'|'||dirty::text FROM schema_migrations")=='3027|false'
 assert query('SELECT COUNT(*) FROM oidc_transactions WHERE NOT consumed AND expires_at>NOW()')=='0'
 before_transactions=int(query('SELECT COUNT(*) FROM oidc_transactions'))
 (ROOT/'private/before-metadata.json').write_text(json.dumps(before))
 (ROOT/'private/before-flags.json').write_text(json.dumps(original))
 TEMP.write_text(yaml.safe_dump({'services':{'app':{'environment':{'SOURCEID_FORCE_REAUTH_PROMPT_LOGIN':'true'}}}}));TEMP.chmod(0o600)
 changed=True;run(command(True)+['up','-d','--no-deps','--no-build','--pull','never','--force-recreate','app'],timeout=120)
 healthy();enabled=flags();assert enabled==dict(original,SOURCEID_FORCE_REAUTH_PROMPT_LOGIN='true')
 out['enabled_flags']=enabled;out['counts_before']=baseline
 state('READY_FOR_USER_SINGLE_CLICK',prompt_loaded=True,gates_preserved=True,no_authorization_requested_by_agent=True,auto_restore_seconds=900)
 deadline=time.monotonic()+900
 while time.monotonic()<deadline:
  d=collect()
  if any(e['stage']=='callback_result' for e in d['stages']):
   out['diagnostics']=d;out['completion_reason']='CALLBACK_FINISHED';break
  if sum(e['stage']=='authorize' for e in d['stages'])>1:
   out['diagnostics']=d;out['completion_reason']='MULTIPLE_AUTHORIZE_STOP';break
  time.sleep(1)
 else:
  out['diagnostics']=collect();out['completion_reason']='TIMEOUT_NO_AUTORETRY'
 out['transactions_added']=int(query('SELECT COUNT(*) FROM oidc_transactions'))-before_transactions
 # Metadata-only count proves actual persisted prompt request without reading
 # state, nonce, verifier, result payloads or authorization URL into output.
 out['live_unconsumed_authorization_count']=int(query("SELECT COUNT(*) FROM oidc_transactions WHERE NOT consumed AND expires_at>NOW() AND nonce<>''"))
 out['counts_after_transaction']=counts();out['user_and_roles_unchanged']=ownership()==own
 assert out['counts_after_transaction']==baseline and out['user_and_roles_unchanged']
 out['result']='OBSERVED_PENDING_OPERATOR_SID_CONFIRMATION'
except Exception as e:
 out.update(failure_class=type(e).__name__,failure_lines=[x.lineno for x in traceback.extract_tb(e.__traceback__) if x.filename==__file__])
finally:
 try:
  if changed:
   state('RESTORING_FALSE')
   run(command(False)+['up','-d','--no-deps','--no-build','--pull','never','--force-recreate','app'],timeout=120);healthy()
   assert flags()==original
  out['restored_flags']=flags();after=snapshot()
  def canonical(x):return json.dumps(x,sort_keys=True)
  assert all(canonical(after[k])==canonical(v) for k,v in before.items() if k!='app')
  assert counts()==baseline and ownership()==own
  out['non_app_containers_unchanged']=True;out['accounts_and_enterprise_records_unchanged']=True
  out['schema']=query("SELECT version::text||'|'||dirty::text FROM schema_migrations")
  out['restored_app_id']=after['app']['id'];out['restored_image']=after['app']['image_id'];out['restore']='PASS'
 except Exception as e:out['restore']='FAILED';out['restore_error_class']=type(e).__name__
 out['completed_utc']=datetime.datetime.now(datetime.timezone.utc).isoformat()
 (ROOT/'result.json').write_text(json.dumps(out,indent=2)+'\n');state('FINISHED',restore=out.get('restore'),result=out['result'])
