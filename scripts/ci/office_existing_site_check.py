"""Read-only check of verified existing site entrypoints; never changes its stack."""
from office_test_remote import connect,execute
CODE=r'''
import json,os,socket,subprocess,urllib.request,urllib.error
assert socket.gethostname()=='aqyfzx10' and os.geteuid()==1000
out={'http':[]}
opener=urllib.request.build_opener(urllib.request.ProxyHandler({}))
for address in ['http://172.30.232.42/','http://172.30.232.42:8080/health','http://127.0.0.1:28480/health','http://127.0.0.1:28481/']:
 try:
  with opener.open(address,timeout=10) as r:out['http'].append({'url':address,'status':r.status,'content_type':r.headers.get('Content-Type')})
 except urllib.error.HTTPError as e:out['http'].append({'url':address,'status':e.code})
 except Exception as e:out['http'].append({'url':address,'error':str(e)})
args=['docker','ps','-a','--filter','label=com.docker.compose.project=knowhub-feishu-v2-d6e99d894d3a','--format','{{json .}}']
p=subprocess.run(args,text=True,capture_output=True,timeout=20)
out['existing_containers']=[{k:r[k] for k in ('ID','Names','Image','State','Status','Ports')} for line in p.stdout.splitlines() if line.strip() for r in [json.loads(line)]]
print(json.dumps(out))
'''
if __name__=='__main__':
 with connect() as client:raise SystemExit(execute(client,CODE,70))
