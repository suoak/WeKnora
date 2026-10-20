"""Disposable database/API/Mock acceptance. Never changes clean pilot data."""
import pathlib,subprocess,json,datetime,time,os,traceback,shutil,re,urllib.parse
os.umask(0o077)
B=pathlib.Path('/home/aqyfzx/ai/knowhub-enterprise-candidate-09e6d86db');P=pathlib.Path('/home/aqyfzx/ai/knowhub-oa-sso-pilot-832b6652a');PG='knowhub-oa-sso-pilot-postgres-1'
def execute(args,input=None,timeout=180):
 z=subprocess.run(args,input=input,stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=timeout)
 if z.returncode:raise RuntimeError('controlled_validation_command_failed')
 return z.stdout
def sql(q,db='postgres'):
 assert db in ('postgres','enterprise_business_validation','enterprise_acl_validation','enterprise_mock_pilot','enterprise_mock_all','enterprise_scale_validation')
 return execute(['docker','exec','-i',PG,'psql','-X','-U','clean_owner','-d',db,'-qAt','-v','ON_ERROR_STOP=1'],input=q.encode()).decode().strip()
def progress(stage,out):
 (B/'validation-progress.json').write_text(json.dumps({'stage':stage,'utc':datetime.datetime.now(datetime.timezone.utc).isoformat()}));(B/'validation-result.json').write_text(json.dumps(out,indent=2))
def run_script(name,source):
 (B/name).write_text(source)
 with (B/'private'/(name+'.log')).open('wb') as f:
  z=subprocess.run(['python3',str(B/name)],stdout=f,stderr=subprocess.STDOUT,timeout=900);assert z.returncode==0
def main():
 out={'result':'RUNNING','real_sid_called':False,'clean_pilot_written':False};stage='prepare_disposable_databases'
 try:
  assert json.loads((B/'upgrade-result.json').read_text())['result']=='PASS'
  image=json.loads((B/'build-status.json').read_text())['image_id'];front='sha256:ab7845c89fadeb17ad2b68bd924e892f948b893781a50ab79e48385c319ddbe1'
  for name in ('candidate.env','postgres.env','selected-data-baseline.json'):shutil.copyfile(P/'private'/name,B/'private'/name)
  (B/'preparation-result.json').write_text('{"result":"PASS"}')
  support=(P/'pilot-support.py').read_text().replace(str(P),str(B)).replace("PG='knowhub-oa-sso-pilot-prepare-pg'","PG='knowhub-oa-sso-pilot-postgres-1'").replace("DB='knowhub_oa_sso_pilot';VALIDATION='knowhub_oa_sso_validation'","DB='enterprise_business_validation';VALIDATION='enterprise_acl_validation'").replace("IMAGE='sha256:2d91336c3f5ba1ba330820801c7bf0573b1c9c45b9280994dd0862d016435286'","IMAGE="+repr(image))
  (B/'pilot_acceptance_support.py').write_text(support)
  # Copy only the clean, upgraded pilot into a fresh disposable DB.
  with (B/'private/sanitized-3028-clean.dump').open('wb') as f:
   z=subprocess.run(['docker','exec',PG,'pg_dump','-U','clean_owner','-d','knowhub_oa_sso_pilot','-Fc','--no-owner','--no-acl'],stdout=f,stderr=subprocess.PIPE,timeout=120);assert z.returncode==0
  ns={};exec(compile(support,'isolated_support','exec'),ns)
  ns['sql']('CREATE DATABASE enterprise_business_validation',db='postgres');ns['restore'](B/'private/sanitized-3028-clean.dump','enterprise_business_validation')
  source=(P/'pilot-acceptance-worker.py').read_text().replace("FRONTIMAGE=json.loads((BASE/'build-status.json').read_text())['images']['frontend']['image_id']","FRONTIMAGE="+repr(front)).replace('knowhub-oa-sso-pilot-validation-','knowhub-enterprise-validation-').replace('sanitized-3027-clean.dump','sanitized-3028-clean.dump').replace('ready-3027-clean.dump','ready-3028-clean.dump').replace('3027|false','3028|false')
  stage='original_data_business_api';progress(stage,out);run_script('business-validation-worker.py',source)
  result=json.loads((B/'acceptance-result.json').read_text());assert result['result']=='PASS';out['original_business_api']=result
  ns['sql']('DROP DATABASE enterprise_business_validation',db='postgres')
  stage='old_app_3028_business_compatibility';progress(stage,out)
  ns['sql']('CREATE DATABASE enterprise_business_validation',db='postgres');ns['restore'](B/'private/sanitized-3028-clean.dump','enterprise_business_validation')
  old='sha256:c6e631c631e4488b32f7964e700dae8dfd9105ca4a75f2c0443272f44484c1c5'
  (B/'pilot_acceptance_support.py').write_text(support.replace('IMAGE='+repr(image),'IMAGE='+repr(old)))
  run_script('old-app-business-validation-worker.py',source)
  result=json.loads((B/'acceptance-result.json').read_text());assert result['result']=='PASS';out['old_app_3028_business_compatibility']=result
  ns['sql']('DROP DATABASE enterprise_business_validation',db='postgres')
  (B/'pilot_acceptance_support.py').write_text(support)
  shutil.copyfile(P/'mock_sid.py',B/'mock_sid.py')
  for enterprise in (False,True):
   mode='enterprise_all' if enterprise else 'pilot';stage='mock_'+mode;progress(stage,out)
   source=(P/'pilot-mock-worker.py').read_text().replace("MDB='pilot_mock_validation'","MDB='enterprise_mock_"+('all' if enterprise else 'pilot')+"'").replace('knowhub-oa-sso-pilot-mock-','knowhub-enterprise-mock-').replace("fixture=private/'mock-fixture'","fixture=private/'mock-fixture-"+mode+"'").replace('sanitized-3027-clean.dump','sanitized-3028-clean.dump').replace("front=json.loads((BASE/'build-status.json').read_text())['images']['frontend']['image_id']","front="+repr(front))
   marker="  (private/'mock-runtime.env').write_text"
   assert marker in source
   addition="  env.update(SOURCEID_ACCOUNT_SCOPE="+repr(mode)+",SOURCEID_ENTERPRISE_POLICY_CONFIRMED='true',SOURCEID_ENTERPRISE_POLICY_REF='synthetic-enterprise-only',SOURCEID_ENTERPRISE_MAX_AGE='60m')\n"
   source=source.replace(marker,addition+marker)
   if enterprise:
    marker="  for name,image,extra in [(REDIS,"
    assert marker in source
    sync='''  row={'issuer':'https://127.0.0.1:8443','employee_no':'SYNTHETIC-PILOT-ADMIN','email':admin['email'],'revision':1,'status':'active','policy_ref':'synthetic-enterprise-only','ownership_ref':'synthetic-disposable-history-only','source_ref':'synthetic-directory-only','verified_at':datetime.datetime.now(datetime.timezone.utc).isoformat(),'valid_until':(datetime.datetime.now(datetime.timezone.utc)+datetime.timedelta(hours=1)).isoformat()}
  (fixture/'proof.json').write_text(json.dumps(row))
  cli="import os,sys,subprocess,urllib.parse;env=dict(os.environ);env['SOURCEID_OWNERSHIP_DSN']='postgres://clean_owner:'+urllib.parse.quote(env['DB_PASSWORD'],safe='')+'@127.0.0.1:5432/'+env['DB_NAME']+'?sslmode=disable';z=subprocess.run(['/app/sourceid-ownership','--actor','synthetic-operator']+sys.argv[1:],input=sys.stdin.buffer.read(),stdout=subprocess.PIPE,stderr=subprocess.PIPE,env=env);sys.stdout.buffer.write(z.stdout);raise SystemExit(z.returncode)"
  summaries=[]
  for flags in ([],['--apply'],['--apply']):
   data=execute(['docker','run','--rm','-i','--network','container:'+PG,'--log-driver','none','--env-file',str(private/'mock-runtime.env'),'--entrypoint','python3',IMAGE,'-c',cli]+flags,input=(json.dumps(row)+'\\n').encode())
   summaries.append(json.loads(data))
  assert summaries[0]=={'rows':1,'changed':1,'unchanged':0,'rejected':0,'applied':False}
  assert summaries[1]=={'rows':1,'changed':1,'unchanged':0,'rejected':0,'applied':True}
  assert summaries[2]=={'rows':1,'changed':0,'unchanged':1,'rejected':0,'applied':True}
  out['protected_cli_dry_apply_replay']=summaries
'''
    source=source.replace(marker,sync+marker)
    marker="print(json.dumps(out))\n'''.replace"
    assert marker in source
    extra='''import os,subprocess
row=json.load(open('/fixture/proof.json'));row['status']='disabled';row['revision']=2
env=dict(os.environ);env['SOURCEID_OWNERSHIP_DSN']='postgres://clean_owner:'+urllib.parse.quote(env['DB_PASSWORD'],safe='')+'@127.0.0.1:5432/'+env['DB_NAME']+'?sslmode=disable'
z=subprocess.run(['/app/sourceid-ownership','--actor','synthetic-operator','--apply'],input=(json.dumps(row)+'\\n').encode(),stdout=subprocess.PIPE,stderr=subprocess.PIPE,env=env);assert z.returncode==0
assert req('/api/v1/auth/me',token=tokens[0])[0]==401
out['disabled_ownership_revokes_remaining_device']=True
'''
    source=source.replace(marker,extra+marker)
   run_script('mock-'+mode+'-worker.py',source)
   result=json.loads((B/'mock-result.json').read_text());assert result['result']=='PASS';out['mock_'+mode]=result
  stage='postgres_scale_and_concurrency';progress(stage,out)
  sql('CREATE DATABASE enterprise_scale_validation')
  pw=dict(x.split('=',1) for x in (P/'private/postgres.env').read_text().splitlines())['POSTGRES_PASSWORD']
  (B/'private/pg-scale.env').write_text('SOURCEID_TEST_POSTGRES_DSN=postgres://clean_owner:'+urllib.parse.quote(pw,safe='')+'@127.0.0.1:5432/enterprise_scale_validation?sslmode=disable\n');pw=None
  shell="set -eu\ncd /work/source\nexport PATH=/work/tools/rust/bin:$PATH GOPATH=/work/cache/go GOCACHE=/work/cache/go-build GOTOOLCHAIN=local GOMAXPROCS=2 GOPROXY=off GOSUMDB=off\ngo test -p 2 -tags anydoc -race -v -count=1 ./internal/application/repository -run 'SourceIDPostgresEnterprise'\n"
  (B/'pg-scale.sh').write_text(shell)
  cache='/home/aqyfzx/ai/knowhub-feishu-personal-20261014'
  with (B/'private/pg-scale.log').open('wb') as f:
   z=subprocess.run(['docker','run','--rm','--name','enterprise-pg-scale','--network','container:'+PG,'--cpus','2','--memory','5g','--log-driver','none','--env-file',str(B/'private/pg-scale.env'),'-v',str(B)+':/work','-v',cache+'/tools:/work/tools:ro','-v',cache+'/toolchain:/work/toolchain:ro','-v',cache+'/cache:/work/cache','--entrypoint','bash','sha256:2b63ff41b02b21d7b302c2d3ba6b4ab43990181fc7d2221585d2cfa5161553fb','/work/pg-scale.sh'],stdout=f,stderr=subprocess.STDOUT,timeout=900);assert z.returncode==0
  log=(B/'private/pg-scale.log').read_text(errors='replace');assert '--- FAIL:' not in log
  out['postgres_scale']=[{'population':int(n),'import_ms':int(t),'indexed_1000_checks_ms':int(q)} for n,t,q in re.findall(r'synthetic_population=(\d+) import_ms=(\d+) indexed_1000_checks_ms=(\d+)',log)]
  assert len(out['postgres_scale'])==2
  out['postgres_enterprise_tests_passed']=re.findall(r'^--- PASS: (TestSourceIDPostgresEnterprise\w+)',log,re.M);assert len(out['postgres_enterprise_tests_passed'])==3
  sql('DROP DATABASE enterprise_scale_validation')
  out.update(result='PASS',candidate_app=image,frontend=front,all_role_http_test_evidence='offline packaging tests + four-job CI; eight synthetic roles',original_real_roles='original system_admin+owner/cross-tenant flags; absent role fixtures NOT_REAL_DATA_VERIFIED')
 except Exception as e:
  out.update(result='FAIL',failed_stage=stage,failure_class=type(e).__name__,failure_lines=[f.lineno for f in traceback.extract_tb(e.__traceback__) if f.filename==__file__])
 finally:
  out['utc']=datetime.datetime.now(datetime.timezone.utc).isoformat();progress(stage,out)
if __name__=='__main__':main()
