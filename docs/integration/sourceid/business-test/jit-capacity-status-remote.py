"""Read only synthetic aggregate progress; never read a response/token dump."""
b=pathlib.Path('/home/aqyfzx/ai/knowhub-enterprise-jit-f40b1602f');app='knowhub-jit-capacity-app'
out={'utc':datetime.datetime.now(datetime.timezone.utc).isoformat()}
z=subprocess.run(['docker','inspect',app],stdout=subprocess.PIPE,stderr=subprocess.PIPE)
if z.returncode==0:
 out['running']=inspect(app,'.State.Running')
 script="import urllib.request,json,ssl;ctx=ssl.create_default_context(cafile='/fixture/ca.pem');o=urllib.request.build_opener(urllib.request.ProxyHandler({}),urllib.request.HTTPSHandler(context=ctx));print(json.dumps(json.load(o.open('https://127.0.0.1:8443/status',timeout=5))))"
 q=subprocess.run(['docker','exec',app,'python3','-c',script],stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=10)
 if q.returncode==0:out['mock_counts']=json.loads(q.stdout)
 p=subprocess.run(['docker','exec',app,'cat','/tmp/capacity-progress.json'],stdout=subprocess.PIPE,stderr=subprocess.PIPE)
 if p.returncode==0:out['progress']=json.loads(p.stdout)
if (b/'capacity-result.json').exists():out['result']=json.loads((b/'capacity-result.json').read_text())
print(json.dumps(out))
