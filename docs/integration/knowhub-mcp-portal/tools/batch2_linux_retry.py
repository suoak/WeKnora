"""Retry documented environment failures; retain first-run logs unchanged."""
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


def run(name,args,env=None,timeout=1800):
    started=datetime.datetime.now(datetime.timezone.utc).isoformat()
    with (LOGS/(name+'.log')).open('wb') as f:
        try:code=subprocess.run(args,cwd=SOURCE,env=env,stdout=f,stderr=subprocess.STDOUT,timeout=timeout).returncode
        except subprocess.TimeoutExpired:code=124
    results.append({'name':name,'command':args,'exit_code':code,'started_utc':started,'finished_utc':datetime.datetime.now(datetime.timezone.utc).isoformat()})
    (LOGS/'retry-results.json').write_text(json.dumps(results,indent=2)+'\n')


env=dict(os.environ,GOTOOLCHAIN='local',GOSUMDB='sum.golang.org',CGO_ENABLED='1',GOCACHE=str(BASE/'go-cache'))
go_container=['docker','run','--rm','--network','none','-v',str(SOURCE)+':'+str(SOURCE),
    '-v','/home/aqyfzx/go/pkg/mod:/home/aqyfzx/go/pkg/mod:ro','-v',str(BASE/'go-cache')+':'+str(BASE/'go-cache'),
    '-e','GOPATH=/home/aqyfzx/go','-e','GOCACHE='+str(BASE/'go-cache'),'-e','GOTOOLCHAIN=local','-e','GOSUMDB=sum.golang.org',
    '-w',str(SOURCE),'knowhub-build/golang:1.26-bookworm-ustc']
run('go-full-bookworm',go_container+['go','test','-p','2','-count=1',
    './internal/application/access','./internal/types','./internal/application/repository','./internal/middleware',
    './internal/mcpserver','./internal/router','./internal/application/service','./internal/agent/tools'])
run('go-race-final',['go','test','-race','-count=1','./internal/application/access','./internal/application/repository'],env)
run('postgres-migrations-retry',['python3',str(SOURCE/'docs/integration/knowhub-mcp-portal/tools/batch2_pg_migrations.py'),str(BASE)],env)
image='weknora-custom/mcp-candidate:v0.8.0-knowhub.24-rc1-854f3b95'
python_container=['docker','run','--rm','--network','none','--entrypoint','python',
    '-v',str(SOURCE)+':/source:ro','-e','PYTHONDONTWRITEBYTECODE=1','-e','WEKNORA_API_KEY=',
    '-e','WEKNORA_BASE_URL=http://127.0.0.1:9/api/v1','-w','/source',image]
run('python-image-deps',python_container+['-c',"import importlib.metadata as m,json;print(json.dumps({k:m.version(k) for k in ['mcp','requests','starlette','uvicorn']}))"])
run('python-full-offline',python_container+['-m','unittest','discover','-s','mcp-server','-p','test_*.py','-v'],timeout=600)
(LOGS/'retry-done.json').write_text(json.dumps({'completed':True,'production_changes':False,'manifest':json.loads((BASE/'manifest.json').read_text()),'results':results},indent=2)+'\n')
