import os
os.umask(0o077)
b=pathlib.Path('/home/aqyfzx/ai/knowhub-enterprise-jit-f40b1602f')
assert json.loads((b/'upgrade-result.json').read_text())['result']=='PASS'
with (b/'private/validation-worker.log').open('wb') as f:p=subprocess.Popen(['python3',str(b/'jit-release-validation-worker.py')],stdout=f,stderr=subprocess.STDOUT,start_new_session=True)
print(json.dumps({'started':True,'pid':p.pid,'disposable_databases_only':True,'real_authentication':False}))
