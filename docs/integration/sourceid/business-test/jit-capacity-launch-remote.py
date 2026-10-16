"""Generate bounded synthetic-only full HTTP capacity worker; no live data writes."""
import os
os.umask(0o077)
b=pathlib.Path('/home/aqyfzx/ai/knowhub-enterprise-jit-f40b1602f')
p=pathlib.Path('/home/aqyfzx/ai/knowhub-oa-sso-pilot-832b6652a')
assert json.loads((b/'validation-result.json').read_text())['result']=='PASS'
s=(p/'pilot-mock-worker.py').read_text()
s=s.replace("MDB='pilot_mock_validation'","MDB='enterprise_capacity_validation'").replace('knowhub-oa-sso-pilot-mock-','knowhub-jit-capacity-').replace("fixture=private/'mock-fixture'","fixture=private/'capacity-fixture'").replace('sanitized-3027-clean.dump','sanitized-3029-clean.dump')
s=s.replace("front=json.loads((BASE/'build-status.json').read_text())['images']['frontend']['image_id']","front='sha256:ab7845c89fadeb17ad2b68bd924e892f948b893781a50ab79e48385c319ddbe1'")
start=s.index("  code=(BASE/'mock_sid.py')")
end=s.index("  env=dict",start)
s=s[:start]+"  (fixture/'mock_sid.py').write_text((BASE/'jit-capacity-mock.py').read_text())\n  (fixture/'client.py').write_text((BASE/'jit-capacity-client.py').read_text())\n"+s[end:]
marker="  (private/'mock-runtime.env').write_text"
s=s.replace(marker,"  env.update(SOURCEID_ACCOUNT_SCOPE='enterprise_all',SOURCEID_ENTERPRISE_POLICY_CONFIRMED='true',SOURCEID_ENTERPRISE_POLICY_REF='synthetic-enterprise-only',SOURCEID_ENTERPRISE_MAX_AGE='60m',SOURCEID_JIT_ENABLE='true',SOURCEID_JIT_POLICY_CONFIRMED='true',SOURCEID_JIT_POLICY_REF='synthetic-jit-only',SOURCEID_JIT_EMAIL_DOMAINS='example.test')\n"+marker)
marker="  for name,image,extra in [(REDIS,"
seed='''  # Synthetic fixtures only: no historical employee identifiers are emitted.
  import uuid
  rows=[];inserts=[]
  for label,n,kind in [('e300',300,'existing'),('e500',500,'existing'),('m300',300,'mixed'),('m500',500,'mixed'),('delay500',500,'mixed'),('limited500',500,'mixed'),('failure500',500,'mixed')]:
   for i in range(n):
    if kind=='mixed' and i%2:continue
    who='capacity-'+label+'-existing-'+str(i);uid=str(uuid.uuid4());email=who+'@example.test'
    inserts.append("('"+uid+"','"+who+"','"+email+"','!synthetic-external-only',NULL,true,false,false,'{}',NOW(),NOW())")
    rows.append({'issuer':'https://127.0.0.1:8443','employee_no':who,'user_id':uid,'email':email,'revision':1,'status':'active','policy_ref':'synthetic-enterprise-only','ownership_ref':'synthetic-capacity-history','source_ref':'synthetic-directory','verified_at':datetime.datetime.now(datetime.timezone.utc).isoformat(),'valid_until':(datetime.datetime.now(datetime.timezone.utc)+datetime.timedelta(hours=1)).isoformat()})
  ms("INSERT INTO users(id,username,email,password_hash,tenant_id,is_active,is_system_admin,can_access_all_tenants,preferences,created_at,updated_at) VALUES "+",".join(inserts))
  cli="import os,sys,subprocess,urllib.parse;env=dict(os.environ);env['SOURCEID_OWNERSHIP_DSN']='postgres://clean_owner:'+urllib.parse.quote(env['DB_PASSWORD'],safe='')+'@127.0.0.1:5432/'+env['DB_NAME']+'?sslmode=disable';z=subprocess.run(['/app/sourceid-ownership','--actor','synthetic-capacity','--apply'],input=sys.stdin.buffer.read(),stdout=subprocess.PIPE,stderr=subprocess.PIPE,env=env);sys.stdout.buffer.write(z.stdout);raise SystemExit(z.returncode)"
  imported=json.loads(execute(['docker','run','--rm','-i','--network','container:'+PG,'--log-driver','none','--env-file',str(private/'mock-runtime.env'),'--entrypoint','python3',IMAGE,'-c',cli],input=('\\n'.join(json.dumps(r) for r in rows)+'\\n').encode(),timeout=180));assert imported['rejected']==0 and imported['changed']==len(rows)
  out['synthetic_existing_accounts']=len(rows)
'''
s=s.replace(marker,seed+marker)
start=s.index("  stage='oidc_access_only'")
end=s.index(" except Exception as e:",start)
capacity='''  stage='capacity';statex(stage=stage)
  import threading
  stop=threading.Event();samples=[];expiry_applied=[]
  def monitor():
   while not stop.is_set():
    try:
     db=json.loads(ms("SELECT json_build_object('connections',(SELECT COUNT(*) FROM pg_stat_activity WHERE datname=current_database()),'active',(SELECT COUNT(*) FROM pg_stat_activity WHERE datname=current_database() AND state='active'),'lock_waiters',(SELECT COUNT(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock'),'users_lock_waiters',(SELECT COUNT(*) FROM pg_locks WHERE database=(SELECT oid FROM pg_database WHERE datname=current_database()) AND relation='users'::regclass AND NOT granted),'max_lock_wait_ms',COALESCE((SELECT MAX(EXTRACT(EPOCH FROM NOW()-query_start)*1000) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock'),0))"))
     stat=json.loads(execute(['docker','stats','--no-stream','--format','{{json .}}',APP],timeout=8))
     db['cpu_percent']=float(stat['CPUPerc'].rstrip('%'));db['memory_usage']=stat['MemUsage']
     pgstat=json.loads(execute(['docker','stats','--no-stream','--format','{{json .}}',PG],timeout=8));db['pg_cpu_percent']=float(pgstat['CPUPerc'].rstrip('%'));db['pg_memory_usage']=pgstat['MemUsage'];samples.append(db)
     if not expiry_applied and subprocess.run(['docker','exec',APP,'test','-f','/tmp/capacity-expiry-ready'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode==0:
      ms("UPDATE source_id_sessions SET expires_at=NOW()-INTERVAL '1 second' WHERE issuer='https://127.0.0.1:8443'")
      execute(['docker','exec',APP,'touch','/tmp/capacity-expiry-applied']);expiry_applied.append(True)
     z=subprocess.run(['docker','exec',APP,'cat','/tmp/capacity-progress.json'],stdout=subprocess.PIPE,stderr=subprocess.DEVNULL)
     if z.returncode==0:(BASE/'capacity-progress.json').write_bytes(z.stdout)
    except Exception:pass
    stop.wait(2)
  thread=threading.Thread(target=monitor,daemon=True);thread.start()
  try:
   data=execute(['docker','exec',APP,'python3','/fixture/client.py'],timeout=1200)
   out['capacity']=json.loads(data);assert out['capacity']['result']=='PASS'
  finally:stop.set();thread.join(timeout=15)
  out['monitoring']={'samples':len(samples),'max_connections':max((x['connections'] for x in samples),default=0),'max_active':max((x['active'] for x in samples),default=0),'max_lock_waiters':max((x['lock_waiters'] for x in samples),default=0),'max_users_lock_waiters':max((x['users_lock_waiters'] for x in samples),default=0),'max_lock_wait_ms':max((float(x['max_lock_wait_ms']) for x in samples),default=0),'max_cpu_percent':max((x['cpu_percent'] for x in samples),default=0),'max_pg_cpu_percent':max((x['pg_cpu_percent'] for x in samples),default=0),'pg_memory_samples':[x['pg_memory_usage'] for x in samples[::max(1,len(samples)//10)]],'memory_samples':[x['memory_usage'] for x in samples[::max(1,len(samples)//10)]],'application_pool_wait_counters':'NOT_CAPTURED (no exported database/sql Stats endpoint)'}
  assert ms("SELECT COUNT(*) FROM auth_tokens a JOIN users u ON a.user_id=u.id WHERE a.token_type='refresh_token' AND u.email LIKE 'capacity-%@example.test'")=='0'
  assert ms("SELECT COUNT(*) FROM tenant_members m JOIN source_id_jit_registrations j ON j.user_id=m.user_id")=='0'
  assert ms("SELECT COUNT(*) FROM users u JOIN source_id_jit_registrations j ON j.user_id=u.id WHERE u.tenant_id<>0 OR u.is_system_admin OR u.can_access_all_tenants")=='0'
  assert ms("SELECT COUNT(*) FROM external_identities WHERE employee_no ~ '^capacity-(limited500|failure500)-existing-[0-9]+$' AND substring(employee_no from '-([0-9]+)$')::int % 10=0")=='0'
  assert ms("SELECT COUNT(*) FROM source_id_sessions s JOIN users u ON s.user_id=u.id WHERE u.email ~ '^capacity-(limited500|failure500)-existing-[0-9]+@example.test$' AND substring(u.email from '-([0-9]+)@')::int % 10=0")=='0'
  out.update(result='PASS',rejected_identities_and_sessions_zero=True,zero_new_refresh_rows=True,zero_jit_memberships_admin_flags=True,production_capacity_pass=False)
'''
s=s[:start]+capacity+s[end:]
s=s.replace("(BASE/'mock-result.json')","(BASE/'capacity-result.json')")
s=s.replace("stage='prepare'","stage='prepare_synthetic_capacity'")
s=s.replace("'--env-file',str(private/'mock-runtime.env'),'--mount'","'--cpus','2','--memory','4g','--env-file',str(private/'mock-runtime.env'),'--mount'")
compile(s,'capacity_worker','exec')
(b/'capacity-worker.py').write_text(s)
with (b/'private/capacity-worker.log').open('wb') as f:z=subprocess.Popen(['python3',str(b/'capacity-worker.py')],stdout=f,stderr=subprocess.STDOUT,start_new_session=True)
print(json.dumps({'started':True,'pid':z.pid,'real_sid_called':False,'live_database_written':False,'scenarios':10}))
