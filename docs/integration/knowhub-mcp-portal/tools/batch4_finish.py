"""Resume on immutable 675615 source; retain failed attempts and bound disk use."""
import datetime
import hashlib
import json
from pathlib import Path
import subprocess
import sys

BASE=Path(sys.argv[1]).resolve()
assert BASE==Path('/home/aqyfzx/ai/knowhub-mcp-m4-20261021-675615')
SOURCE=BASE/'source';LOGS=BASE/'results-final';LOGS.mkdir(exist_ok=False)
manifest=json.loads((BASE/'manifest.json').read_text())
bad=[p for p,digest in manifest['files'].items() if hashlib.sha256((SOURCE/p).read_bytes()).hexdigest()!=digest]
assert not bad,bad
results=[];binaries={}
def save():
    path=LOGS/'results.tmp'
    path.write_text(json.dumps(results,indent=2))
    path.replace(LOGS/'results.json')
def run(name,args,timeout=1800):
    record={'name':name,'command':args,'started_utc':datetime.datetime.now(datetime.timezone.utc).isoformat()}
    with (LOGS/(name+'.log')).open('wb')as f:
        try:record['exit_code']=subprocess.run(args,stdout=f,stderr=subprocess.STDOUT,timeout=timeout).returncode
        except subprocess.TimeoutExpired:record['exit_code']=124
    record['finished_utc']=datetime.datetime.now(datetime.timezone.utc).isoformat()
    (LOGS/(name+'.exit.txt')).write_text(str(record['exit_code'])+'\n')
    results.append(record);save()
    print(json.dumps({'name':name,'exit_code':record['exit_code']}),flush=True)
    return record['exit_code']
go=['docker','run','--rm','--network','none','--tmpfs','/tmp:rw,exec,size=3g',
    '-v',str(SOURCE)+':'+str(SOURCE),'-v',str(LOGS)+':'+str(LOGS),
    '-v','/home/aqyfzx/go/pkg/mod:/home/aqyfzx/go/pkg/mod:ro',
    '-v','/home/aqyfzx/ai/knowhub-mcp-m2-20261020-f44954/go-cache:/cache',
    '-v',str(BASE/'sqlite-headers')+':/knowhub-m3-sqlite-3.46.1:ro',
    '-e','CGO_CFLAGS=-O2 -g -I/knowhub-m3-sqlite-3.46.1',
    '-e','GOPATH=/home/aqyfzx/go','-e','GOCACHE=/cache','-e','GOTOOLCHAIN=local','-e','GOSUMDB=sum.golang.org',
    '-w',str(SOURCE),'knowhub-build/golang:1.26-bookworm-ustc']
run('go-environment',go+['go','version'])
run('go-full',go+['go','test','-tags','sqlite_fts5','-p','2','-count=1',
    './internal/application/access/...','./internal/application/repository','./internal/application/repository/retriever/...',
    './internal/application/service/retriever','./internal/types','./internal/database','./internal/middleware','./internal/router','./internal/mcpserver'])
run('go-race',go+['go','test','-tags','sqlite_fts5','-race','-p','2','-count=1',
    './internal/application/access/...','./internal/application/repository','./internal/application/repository/retriever/sqlite',
    './internal/application/service/retriever','./internal/middleware','./internal/router'])
image='weknora-custom/mcp-candidate:v0.8.0-knowhub.24-rc1-854f3b95'
py=['docker','run','--rm','--network','none','--entrypoint','python','-v',str(SOURCE)+':/source:ro',
    '-e','WEKNORA_API_KEY=','-e','WEKNORA_BASE_URL=http://127.0.0.1:9/api/v1',
    '-e','PYTHONDONTWRITEBYTECODE=1','-w','/source',image]
for package,name in [('internal/application/service','service'),('internal/agent/tools','agent'),('internal/application/repository','python-integration')]:
    binary=LOGS/(name+'.test')
    tags='sqlite_fts5,knowhub_m3_integration' if name=='python-integration' else 'sqlite_fts5'
    code=run(name+'-build',go+['go','test','-tags',tags,'-c','-o',str(binary),'./'+package])
    if code==0:
        binaries[binary.name]=hashlib.sha256(binary.read_bytes()).hexdigest()
        args=['docker','run','--rm','--network','none','--entrypoint',str(binary),
            '--tmpfs','/tmp:rw,exec,size=1g','--add-host','example.com:8.8.8.8','--add-host','api.e2b.app:8.8.8.8',
            '-v',str(SOURCE)+':'+str(SOURCE),'-v',str(LOGS)+':'+str(LOGS),
            '-v','/home/aqyfzx/go/pkg/mod:/home/aqyfzx/go/pkg/mod:ro','-e','WEKNORA_API_KEY=',
            '-w',str(SOURCE/package),image,'-test.v','-test.count=1']
        if name=='python-integration':args+=['-test.run','^TestDocumentM3PythonHTTPCompatibility$']
        run(name+'-runtime',args,900)
        (LOGS/'test-binaries-sha256.json').write_text(json.dumps(binaries,indent=2))
        binary.unlink() # Disposable output copy; source/digest/logs retained.
run('postgres',['python3',str(SOURCE/'docs/integration/knowhub-mcp-portal/tools/batch4_pg.py'),str(BASE)])
bad=[p for p,digest in manifest['files'].items() if hashlib.sha256((SOURCE/p).read_bytes()).hexdigest()!=digest]
(LOGS/'candidate-integrity.json').write_text(json.dumps({'candidate_sha256':manifest['candidate_sha256'],'mismatches':bad}))
(LOGS/'done.json').write_text(json.dumps({'completed':True,'all_commands_passed':all(r['exit_code']==0 for r in results),'source_mismatches':bad}))
