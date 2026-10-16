"""One extra disposable JIT check: directed disable revokes another device."""
b=pathlib.Path('/home/aqyfzx/ai/knowhub-enterprise-jit-f40b1602f')
s=(b/'mock-jit-worker.py').read_text()
needle="print(json.dumps(out))\n'''.replace"
assert needle in s
extra='''import os,subprocess,datetime
env=dict(os.environ);env['SOURCEID_OWNERSHIP_DSN']='postgres://clean_owner:'+urllib.parse.quote(env['DB_PASSWORD'],safe='')+'@127.0.0.1:5432/'+env['DB_NAME']+'?sslmode=disable'
row={'issuer':'https://127.0.0.1:8443','employee_no':'SYNTHETIC-JIT-NEW','email':'new-jit@example.test','revision':3,'status':'disabled','policy_ref':'synthetic-enterprise-only','ownership_ref':'synthetic-jit-only','source_ref':'approved-jit:synthetic-jit-only','verified_at':datetime.datetime.now(datetime.timezone.utc).isoformat(),'valid_until':(datetime.datetime.now(datetime.timezone.utc)+datetime.timedelta(hours=1)).isoformat()}
z=subprocess.run(['/app/sourceid-ownership','--actor','synthetic-jit-revoker','--apply'],input=(json.dumps(row)+'\\n').encode(),stdout=subprocess.PIPE,stderr=subprocess.PIPE,env=env);assert z.returncode==0
assert req('/api/v1/auth/me',token=tokens[0])[0]==401
out.pop('switch_does_not_extend_expiry',None);out.pop('switch_tenant_access_only',None)
out['unauthorized_switch_denied']=True;out['disabled_jit_ownership_revokes_remaining_device']=True
'''
s=s.replace(needle,extra+needle).replace("(BASE/'mock-result.json')","(BASE/'jit-revoke-result.json')")
s=s.replace("role='original system_admin+owner; other real roles NOT_REAL_DATA_VERIFIED'","role='new tenantless ordinary JIT; original admin memberships unchanged'")
# Capacity owns the PG namespace listeners now. Validate after it finishes.
worker="""import pathlib,subprocess,time,json,os
b=pathlib.Path('/home/aqyfzx/ai/knowhub-enterprise-jit-f40b1602f')
for _ in range(1200):
 if (b/'capacity-result.json').exists():break
 time.sleep(1)
else:raise SystemExit(2)
assert json.loads((b/'capacity-result.json').read_text())['result']=='PASS'
with (b/'private/jit-revoke-worker.log').open('wb') as log:
 z=subprocess.run(['python3',str(b/'jit-revoke-worker.py')],stdout=log,stderr=subprocess.STDOUT,timeout=240)
"""
(b/'jit-revoke-worker.py').write_text(s);(b/'jit-revoke-delayed.py').write_text(worker)
with (b/'private/jit-revoke-delayed.log').open('wb') as f:z=subprocess.Popen(['python3',str(b/'jit-revoke-delayed.py')],stdout=f,stderr=subprocess.STDOUT,start_new_session=True)
print(json.dumps({'started':True,'pid':z.pid,'waits_for_capacity_cleanup':True,'real_sid_called':False}))
