"""Read-only diagnostics scoped to the Office project; never prints env secrets."""
from office_test_remote import connect,execute,REMOTE_ROOT
CODE='ROOT='+repr(REMOTE_ROOT)+'\n'+r'''
import json,os,pathlib,socket,subprocess,urllib.request
assert socket.gethostname()=='aqyfzx10' and os.geteuid()==1000
root=pathlib.Path(ROOT);site=root/'site'
secrets=[]
for file in ['app.env','postgres.env']:
 for line in (site/file).read_text().splitlines():
  k,_,v=line.partition('=')
  if any(word in k for word in ['PASSWORD','SECRET','KEY']):secrets.append(v)
compose=['docker','compose','-p','knowhub-office-guard-test','-f',str(site/'compose.json')]
def redact(s):
 for secret in secrets:
  if secret:s=s.replace(secret,'[redacted]')
 return s
for args in [['ps','-a','--format','json'],['logs','--no-color','--tail','55','app','docreader','scanner']]:
 p=subprocess.run(compose+args,capture_output=True,text=True,timeout=30)
 print(redact(p.stdout+p.stderr))
ids=subprocess.check_output(compose+['ps','-aq'],text=True).split()
if ids:
 rows=json.loads(subprocess.check_output(['docker','inspect',*ids],text=True))
 print(json.dumps({'resources':[{'name':x['Name'],'image':x['Image'],'state':x['State'],'network':x['NetworkSettings']['Networks'],
  'mounts':[{'type':m['Type'],'source':m['Source'],'destination':m['Destination']} for m in x['Mounts']],
  'memory_limit':x['HostConfig']['Memory'],'privileged':x['HostConfig']['Privileged'],'ports':x['HostConfig']['PortBindings']} for x in rows]}))
'''
if __name__=='__main__':
 with connect() as client:raise SystemExit(execute(client,CODE))
