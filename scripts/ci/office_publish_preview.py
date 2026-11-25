"""Attach ONLY the task frontend to a checked, separate host-preview network.

The database, queue, scanner, model fixture and app stay on the internal bridge.
Docker does not publish host ports for an internal-only network.
"""
from office_test_remote import connect,execute,REMOTE_ROOT
CODE='ROOT='+repr(REMOTE_ROOT)+'\n'+r'''
import ipaddress,json,os,pathlib,socket,subprocess,urllib.request
assert socket.gethostname()=='aqyfzx10' and os.geteuid()==1000
site=pathlib.Path(ROOT)/'site';project='knowhub-office-guard-test'
command=['docker','compose','-p',project,'-f',str(site/'compose.json')]
plan=json.loads((site/'compose.json').read_text())
if 'preview' not in plan['networks']:
 subnet=ipaddress.ip_network('10.254.222.16/28')
 nets=json.loads(subprocess.check_output(['docker','network','inspect',*subprocess.check_output(['docker','network','ls','-q'],text=True).split()],text=True))
 ranges=[ipaddress.ip_network(c['Subnet']) for n in nets for c in n['IPAM'].get('Config',[]) if c.get('Subnet')]
 ranges.extend(ipaddress.ip_network(r['dst']) for r in json.loads(subprocess.check_output(['ip','-j','route','show','table','all'],text=True)) if r.get('dst') and r['dst']!='default')
 assert all(not subnet.overlaps(r) for r in ranges if r.version==4)
 plan['networks']['preview']={'name':project+'-preview','ipam':{'config':[{'subnet':str(subnet)}]}}
 plan['services']['frontend']['networks']=['private','preview']
 pending=site/'compose-preview.json'
 fd=os.open(pending,os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
 with os.fdopen(fd,'w') as f:json.dump(plan,f,indent=2)
 pending.replace(site/'compose.json')
 print(json.dumps({'preview_network':str(subnet),'private_services_network_unchanged':True,'only_frontend_dual_network':True}))
p=subprocess.run(command+['up','-d','--no-build','--pull','never','frontend'],capture_output=True,text=True,timeout=60)
print(p.stdout+p.stderr);assert p.returncode==0
for path in ['/','/health']:
 try:
  with urllib.request.urlopen('http://127.0.0.1:39781'+path,timeout=10) as r:print(json.dumps({'url':'127.0.0.1:39781'+path,'status':r.status,'content_type':r.headers.get('Content-Type')}))
 except Exception as e:print(json.dumps({'url':path,'error':type(e).__name__}))
'''
if __name__=='__main__':
 with connect() as client:raise SystemExit(execute(client,CODE,100))
