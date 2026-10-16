"""Re-use accepted API/Mock fixtures, only in disposable green DB copies."""
import os,shutil,time,traceback
os.umask(0o077)
B=pathlib.Path('/home/aqyfzx/ai/knowhub-company-entry-20261017');F=pathlib.Path('/home/aqyfzx/ai/knowhub-enterprise-jit-f40b1602f');G=B/'green';V=B/'validation';PG='knowhub-company-test-postgres-1'
out={'result':'RUNNING','real_sid_login':False,'live_business_gates_changed':False,'live_identity_writes':False};stage='fixture_prepare'
def status():
 out['stage']=stage;(B/'validation-result.json').write_text(json.dumps(out,indent=2))
try:
 status();assert json.loads((B/'prepare-result.json').read_text())['result']=='PASS'
 V.mkdir(mode=0o700);(V/'private').mkdir(mode=0o700)
 support=(F/'pilot_acceptance_support.py').read_text().replace(str(F),str(V)).replace("PG='knowhub-oa-sso-pilot-postgres-1'","PG='"+PG+"'")
 assert PG in support
 (V/'pilot_acceptance_support.py').write_text(support)
 for source,target in [(G/'private/app.env',V/'private/candidate.env'),(G/'private/postgres.env',V/'private/postgres.env'),(F/'private/selected-data-baseline.json',V/'private/selected-data-baseline.json'),(F/'mock_sid.py',V/'mock_sid.py')]:shutil.copyfile(source,target)
 for name in ('preparation-result.json','build-status.json','upgrade-result.json'):shutil.copyfile(F/name,V/name)
 with (V/'private/sanitized-3029-clean.dump').open('wb') as f:
  z=subprocess.run(['docker','exec',PG,'pg_dump','-U','clean_owner','-d','knowhub_oa_sso_pilot','-Fc','--no-owner','--no-acl'],stdout=f,stderr=subprocess.PIPE,timeout=120);assert z.returncode==0
 ns={};exec(compile(support,'private_support','exec'),ns)
 for name,result in [('business-validation-worker.py','acceptance-result.json'),('mock-enterprise_all-worker.py','mock-result.json'),('mock-jit-worker.py','mock-result.json')]:
  stage=name;status()
  source=(F/name).read_text().replace(str(F),str(V)).replace('knowhub-oa-sso-pilot-postgres-1',PG)
  (V/name).write_text(source)
  if name=='business-validation-worker.py':
   ns['sql']('CREATE DATABASE enterprise_business_validation',db='postgres');ns['restore'](V/'private/sanitized-3029-clean.dump','enterprise_business_validation')
  with (V/'private'/(name+'.log')).open('wb') as f:z=subprocess.run(['python3',str(V/name)],stdout=f,stderr=subprocess.STDOUT,timeout=600)
  assert z.returncode==0
  r=json.loads((V/result).read_text());assert r['result']=='PASS'
  # Results are already fixed fixture summaries; retain them only private,
  # export strict booleans and known test names, not nested response objects.
  out[name]={'result':'PASS','fixture_result_saved_private':True}
  if name=='business-validation-worker.py':ns['sql']('DROP DATABASE enterprise_business_validation',db='postgres')
 stage='live_data_unchanged';status()
 nsg={};green_support=support.replace("DB='enterprise_business_validation';VALIDATION='enterprise_acl_validation'","DB='knowhub_oa_sso_pilot';VALIDATION='enterprise_acl_validation'");exec(compile(green_support,'private_green_support','exec'),nsg)
 baseline=json.loads((V/'private/selected-data-baseline.json').read_text());assert nsg['signatures'](baseline['columns'])==baseline['signatures']
 q="SELECT json_build_object('identities',(SELECT COUNT(*) FROM external_identities),'ownership',(SELECT COUNT(*) FROM source_id_account_ownership),'jit',(SELECT COUNT(*) FROM source_id_jit_registrations),'sessions',(SELECT COUNT(*) FROM source_id_sessions),'active_tokens',(SELECT COUNT(*) FROM auth_tokens WHERE NOT is_revoked))"
 values=nsg['scalar'](q);assert all(v==0 for v in values.values())
 out.update(result='PASS',stage='complete',live_identity_counts=values,live_permission_signatures_unchanged=True)
except Exception as e:out.update(result='FAIL',stage=stage,failure_class=type(e).__name__,failure_lines=[f.lineno for f in traceback.extract_tb(e.__traceback__) if f.filename==__file__])
out['utc']=datetime.datetime.now(datetime.timezone.utc).isoformat();status();print(json.dumps(out))
