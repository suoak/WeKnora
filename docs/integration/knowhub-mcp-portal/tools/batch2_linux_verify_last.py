"""Verify the added PostgreSQL JSONB/read fixture and explicit key capability."""
import datetime
import json
import os
from pathlib import Path
import subprocess

BASE=Path(__file__).resolve().parent
SOURCE=BASE/'source'
LOGS=BASE/'results'
assert BASE.name.startswith('knowhub-mcp-m2-') and BASE.parent==Path('/home/aqyfzx/ai')
results=[]


def run(name,args,env=None):
    started=datetime.datetime.now(datetime.timezone.utc).isoformat()
    with (LOGS/(name+'.log')).open('wb') as f:
        try:code=subprocess.run(args,cwd=SOURCE,env=env,stdout=f,stderr=subprocess.STDOUT,timeout=900).returncode
        except subprocess.TimeoutExpired:code=124
    results.append({'name':name,'command':args,'exit_code':code,'started_utc':started,'finished_utc':datetime.datetime.now(datetime.timezone.utc).isoformat()})
    (LOGS/'last-results.json').write_text(json.dumps(results,indent=2)+'\n')


env=dict(os.environ,GOTOOLCHAIN='local',GOSUMDB='sum.golang.org',CGO_ENABLED='1',GOCACHE=str(BASE/'go-cache'))
old=LOGS/'postgres-go-cas.log'
if old.exists():(LOGS/'postgres-go-cas-before-read-extension.log').write_bytes(old.read_bytes())
run('postgres-jsonb-read-final',['python3',str(SOURCE/'docs/integration/knowhub-mcp-portal/tools/batch2_pg_migrations.py'),str(BASE)],env)
run('enterprise-capability-final',['docker','run','--rm','--network','none','-v',str(SOURCE)+':'+str(SOURCE),
    '-v','/home/aqyfzx/go/pkg/mod:/home/aqyfzx/go/pkg/mod:ro','-v',str(BASE/'go-cache')+':'+str(BASE/'go-cache'),
    '-e','GOPATH=/home/aqyfzx/go','-e','GOCACHE='+str(BASE/'go-cache'),'-e','GOTOOLCHAIN=local','-e','GOSUMDB=sum.golang.org',
    '-w',str(SOURCE),'knowhub-build/golang:1.26-bookworm-ustc','go','test','-count=1','-v','./internal/application/service','-run','^TestEnterpriseCapabilityIsExplicitAndRequiresRetrieve$'])
(LOGS/'last-done.json').write_text(json.dumps({'completed':True,'production_changes':False,'manifest':json.loads((BASE/'manifest.json').read_text()),'results':results},indent=2)+'\n')
