"""Bounded startup diagnosis of only stopped, labelled private probe containers."""
from pathlib import Path
import hashlib,json,subprocess,sys,time
base=Path(sys.argv[1]).resolve()
assert base.parent==Path('/home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-feishu-20261022')
i=json.loads((base/'results/deploy/instance.json').read_text());p=i['project']
assert p.startswith('knowhub-v1-feishu-')
out=base/'results/hotfix-startup-diagnostic';out.mkdir(exist_ok=False)
names=[p+'-hotfix-redis',p+'-hotfix-probe']
secrets=[x.split('=',1)[1] for x in(base/'private/app.env').read_text().splitlines() if '='in x]
rows=[]
def run(label,argv):
 r=subprocess.run(argv,capture_output=True,timeout=30)
 raw=r.stdout+r.stderr
 for s in secrets:
  if len(s)>12:raw=raw.replace(s.encode(),b'[REDACTED]')
 (out/(label+'.log')).write_bytes(raw)
 rows.append({'name':label,'argv':argv,'exit_code':r.returncode})
 return r
try:
 for name in names:
  state=json.loads(subprocess.check_output(['docker','inspect',name]))[0]
  assert state['Config']['Labels'].get('knowhub.v1')=='hotfix-probe'
  assert not state['State']['Running'] and not state['HostConfig']['PortBindings']
 for name in names:assert run('start-'+name,['docker','start',name]).returncode==0
 time.sleep(15)
 health=run('health',['docker','exec',names[1],'curl','-s','--max-time','3','-o','/dev/null','-w','%{http_code}','http://127.0.0.1:8080/health'])
 run('stats',['docker','stats','--no-stream','--format','{{.Name}} {{.CPUPerc}} {{.MemUsage}} {{.PIDs}}']+names)
 state=json.loads(subprocess.check_output(['docker','inspect',names[1]]))[0]
 if health.stdout!=b'200' and state['State']['Running']:
  # Go SIGQUIT provides initialization stacks and exits ONLY this probe APP.
  run('stack-dump',['docker','kill','--signal','SIGQUIT',names[1]]);time.sleep(2)
 run('app-log',['docker','logs',names[1]])
 result={'diagnostic_completed':True,'health_status':health.stdout.decode(),'production_changes':False}
finally:
 for name in reversed(names):run('stop-'+name,['docker','stop','--time','5',name])
 (out/'commands.json').write_text(json.dumps(rows,indent=2))
 (out/'result.json').write_text(json.dumps(result,indent=2))
