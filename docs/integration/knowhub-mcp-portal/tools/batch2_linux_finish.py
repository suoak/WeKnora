"""Final isolated migration checks and retries of environment-dependent suites."""
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


def run(name,args,env=None,timeout=1200):
    started=datetime.datetime.now(datetime.timezone.utc).isoformat()
    with (LOGS/(name+'.log')).open('wb') as f:
        try:code=subprocess.run(args,cwd=SOURCE,env=env,stdout=f,stderr=subprocess.STDOUT,timeout=timeout).returncode
        except subprocess.TimeoutExpired:code=124
    results.append({'name':name,'command':args,'exit_code':code,'started_utc':started,'finished_utc':datetime.datetime.now(datetime.timezone.utc).isoformat()})
    (LOGS/'finish-results.json').write_text(json.dumps(results,indent=2)+'\n')
    return code


env=dict(os.environ,GOTOOLCHAIN='local',GOSUMDB='sum.golang.org',CGO_ENABLED='1',GOCACHE=str(BASE/'go-cache'))
run('postgres-migrations-final',['python3',str(SOURCE/'docs/integration/knowhub-mcp-portal/tools/batch2_pg_migrations.py'),str(BASE)],env)
go_container=['docker','run','--rm','--network','none','-v',str(SOURCE)+':'+str(SOURCE),
    '-v','/home/aqyfzx/go/pkg/mod:/home/aqyfzx/go/pkg/mod:ro','-v',str(BASE/'go-cache')+':'+str(BASE/'go-cache'),
    '-e','GOPATH=/home/aqyfzx/go','-e','GOCACHE='+str(BASE/'go-cache'),'-e','GOTOOLCHAIN=local','-e','GOSUMDB=sum.golang.org',
    '-w',str(SOURCE)]
image='knowhub-build/golang:1.26-bookworm-ustc'
run('go-database-full',go_container+[image,'go','test','-count=1','./internal/database'])
# Two unrelated suites need a usable ensurepip runtime and public-host DNS
# for their existing fake providers. Network remains NONE; these host mappings
# are deterministic DNS fixtures, not outbound access or SSRF bypass switches.
runtime=BASE/'python-runtime'
runtime.mkdir(exist_ok=True)
container=BASE.name+'-python-tools'
try:
    subprocess.run(['docker','create','--name',container,'--network','none','--entrypoint','true','python:3.10.18-bookworm'],check=True,stdout=subprocess.DEVNULL)
    subprocess.run(['docker','cp',container+':/usr/local/.',str(runtime)],check=True,stdout=subprocess.DEVNULL)
finally:
    subprocess.run(['docker','rm',container],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
fixtures=['--add-host','example.com:8.8.8.8','--add-host','api.e2b.app:8.8.8.8',
    '-v',str(runtime)+':'+str(runtime)+':ro','-e','PATH='+str(runtime/'bin')+':/usr/local/go/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin',
    '-e','LD_LIBRARY_PATH='+str(runtime/'lib')]
run('go-service-agent-full-fixtures',go_container+fixtures+[image,'go','test','-p','2','-count=1','./internal/application/service','./internal/agent/tools'])
run('go-final-vet',go_container+[image,'go','vet','./internal/application/access','./internal/application/repository','./internal/database','./internal/application/service','./internal/types','./internal/middleware','./internal/mcpserver'])
(LOGS/'finish-done.json').write_text(json.dumps({'completed':True,'production_changes':False,'manifest':json.loads((BASE/'manifest.json').read_text()),'results':results},indent=2)+'\n')
