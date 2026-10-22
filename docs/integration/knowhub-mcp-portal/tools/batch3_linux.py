"""Run only in a fresh disposable test-host task directory; no production DSN."""
import datetime
import json
from pathlib import Path
import subprocess
import hashlib
import shutil
import sys

BASE=Path(__file__).resolve().parent
assert BASE.parent==Path('/home/aqyfzx/ai') and BASE.name.startswith('knowhub-mcp-m3-')
SOURCE=BASE/'source'
LOGS=BASE/'results'
LOGS.mkdir(exist_ok=True)
results=[]
phase=sys.argv[1] if len(sys.argv)>1 else 'all'
assert phase in ('all','python-integration','postgres')
headers=BASE/'sqlite-headers';headers.mkdir(exist_ok=True)
sqlite_module=Path('/home/aqyfzx/go/pkg/mod/github.com/mattn/go-sqlite3@v1.14.24')
shutil.copyfile(sqlite_module/'sqlite3-binding.h',headers/'sqlite3.h')
shutil.copyfile(sqlite_module/'sqlite3ext.h',headers/'sqlite3ext.h')
(LOGS/'sqlite-header-manifest.json').write_text(json.dumps({p.name:hashlib.sha256(p.read_bytes()).hexdigest()for p in headers.iterdir()},indent=2))
def run(name,args,timeout=1800):
    if phase=='postgres' and name!='postgres':return 125
    if phase=='python-integration' and name not in ('python-integration-build','python-http-sse-go-acl'):
        return 125  # Not selected; no PASS result is recorded.
    record={'name':name,'command':args,'started_utc':datetime.datetime.now(datetime.timezone.utc).isoformat()}
    with (LOGS/(name+'.log')).open('wb') as f:
        try:record['exit_code']=subprocess.run(args,cwd=SOURCE,stdout=f,stderr=subprocess.STDOUT,timeout=timeout).returncode
        except subprocess.TimeoutExpired:record['exit_code']=124
    record['finished_utc']=datetime.datetime.now(datetime.timezone.utc).isoformat()
    results.append(record)
    (LOGS/'results.json').write_text(json.dumps(results,indent=2)+'\n')
    return record['exit_code']

go=['docker','run','--rm','--network','none','-v',str(SOURCE)+':'+str(SOURCE),
    '-v','/home/aqyfzx/go/pkg/mod:/home/aqyfzx/go/pkg/mod:ro',
    '-v','/home/aqyfzx/ai/knowhub-mcp-m2-20261020-f44954/go-cache:/cache',
    '-v',str(headers)+':/knowhub-m3-sqlite-3.46.1:ro',
    '-e','CGO_CFLAGS=-O2 -g -I/knowhub-m3-sqlite-3.46.1',
    '-v',str(LOGS)+':'+str(LOGS),'-e','GOPATH=/home/aqyfzx/go','-e','GOCACHE=/cache',
    '-e','GOTOOLCHAIN=local','-e','GOSUMDB=sum.golang.org','-w',str(SOURCE),
    'knowhub-build/golang:1.26-bookworm-ustc']
run('core',go+['go','test','-tags','sqlite_fts5','-count=1','-v','./internal/application/access','./internal/application/repository','-run','TestDocumentM3|TestEnterpriseM2'])
old_go=go[:-1]+['-e','CGO_CFLAGS=-O2 -g','-v','/usr/include/sqlite3.h:/usr/local/include/sqlite3.h:ro',
    '-v','/usr/include/sqlite3ext.h:/usr/local/include/sqlite3ext.h:ro',go[-1]]
run('old-sqlite-capability-hold',old_go+['go','test','-tags','sqlite_fts5,knowhub_m3_old_sqlite','-count=1','-v','./internal/application/repository','-run','^TestDocumentM3OldSQLiteBuildHOLD$'])
run('go-full',go+['go','test','-tags','sqlite_fts5','-p','2','-count=1','./internal/application/access','./internal/application/repository','./internal/application/repository/retriever/...','./internal/application/service/retriever','./internal/types','./internal/database','./internal/middleware','./internal/router','./internal/mcpserver'])
run('go-race',go+['go','test','-tags','sqlite_fts5','-race','-p','2','-count=1','./internal/application/access/...','./internal/application/repository','./internal/application/repository/retriever/sqlite','./internal/application/service/retriever','./internal/middleware','./internal/router'])
run('go-vet',go+['go','vet','-tags','sqlite_fts5','./internal/application/access/...','./internal/application/repository','./internal/application/repository/retriever/sqlite','./internal/application/service/retriever','./internal/types','./internal/database','./internal/middleware','./internal/router'])
python_image='weknora-custom/mcp-candidate:v0.8.0-knowhub.24-rc1-854f3b95'
py=['docker','run','--rm','--network','none','--entrypoint','python','-v',str(SOURCE)+':/source:ro','-e','WEKNORA_API_KEY=','-e','WEKNORA_BASE_URL=http://127.0.0.1:9/api/v1','-e','PYTHONDONTWRITEBYTECODE=1','-w','/source',python_image]
run('python-environment',py+['-c','import sys,tomllib,packaging,json,importlib.metadata as m;print(sys.version);print(json.dumps({k:m.version(k) for k in ["mcp","requests","packaging","uvicorn"]}))'])
run('python-full',py+['-m','unittest','discover','-s','mcp-server','-p','test_*.py','-v'],600)
for package,name in [('internal/application/service','service'),('internal/agent/tools','agent')]:
    binary=LOGS/(name+'.test')
    code=run(name+'-build',go+['go','test','-tags','sqlite_fts5','-c','-o',str(binary),'./'+package])
    if code==0:
        # Compile with Bookworm's C++ toolchain; execute on a newer Linux image
        # with the controlled Python 3.11.16 runtime, tomllib and ensurepip.
        run(name+'-python311',['docker','run','--rm','--network','none','--entrypoint',str(binary),
            '--add-host','example.com:8.8.8.8','--add-host','api.e2b.app:8.8.8.8',
            '-v',str(SOURCE)+':'+str(SOURCE),'-v',str(LOGS)+':'+str(LOGS),
            '-v','/home/aqyfzx/go/pkg/mod:/home/aqyfzx/go/pkg/mod:ro',
            '-e','WEKNORA_API_KEY=','-w',str(SOURCE/package),python_image,'-test.v','-test.count=1'])
binary=LOGS/'python-integration.test'
code=run('python-integration-build',go+['go','test','-tags','sqlite_fts5,knowhub_m3_integration','-c','-o',str(binary),'./internal/application/repository'])
if code==0:
    run('python-http-sse-go-acl',['docker','run','--rm','--network','none','--entrypoint',str(binary),
        '-v',str(SOURCE)+':'+str(SOURCE),'-v',str(LOGS)+':'+str(LOGS),
        '-v','/home/aqyfzx/go/pkg/mod:/home/aqyfzx/go/pkg/mod:ro',
        '-e','WEKNORA_API_KEY=','-w',str(SOURCE/'internal/application/repository'),python_image,
        '-test.run','^TestDocumentM3PythonHTTPCompatibility$','-test.v','-test.count=1'],400)
run('postgres', ['python3',str(SOURCE/'docs/integration/knowhub-mcp-portal/tools/batch3_pg.py'),str(BASE)])
uploaded=json.loads((BASE/'manifest.json').read_text())
mismatches=[p for p,digest in uploaded['files'].items()if hashlib.sha256((SOURCE/p).read_bytes()).hexdigest()!=digest]
(LOGS/'candidate-integrity.json').write_text(json.dumps({'candidate_sha256':uploaded['candidate_sha256'],'mismatches':mismatches,'status':'PASS'if not mismatches else 'FAIL'},indent=2)+'\n')
(LOGS/'test-binaries-sha256.json').write_text(json.dumps({p.name:hashlib.sha256(p.read_bytes()).hexdigest()for p in LOGS.glob('*.test')},indent=2)+'\n')
images={}
for image in ('knowhub-build/golang:1.26-bookworm-ustc',python_image,'paradedb/paradedb:v0.22.2-pg17'):
    p=subprocess.run(['docker','image','inspect',image,'--format','{{.Id}}'],stdout=subprocess.PIPE,stderr=subprocess.STDOUT)
    images[image]={'exit_code':p.returncode,'id':p.stdout.decode().strip()}
(LOGS/'images.json').write_text(json.dumps(images,indent=2)+'\n')
(LOGS/'done.json').write_text(json.dumps({'completed':True,'phase':phase,'results':results,'production_changes':False},indent=2)+'\n')
