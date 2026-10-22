"""Development checks only: no APP deployment or business acceptance."""
from pathlib import Path
import datetime,json,subprocess,sys

base=Path(sys.argv[1]).resolve()
assert base.parent==Path('/home/aqyfzx/ai/WeKnora-release-build/knowhub-mcp-v1-20261021')
m=json.loads((base/'manifest.json').read_text());sha=m['candidate_sha256'];assert base.name==sha[:16]
out=base/'results'/'development';out.mkdir(exist_ok=False)
volume='knowhub-mcp-v1-dev-'+sha[:16]
results=[]
def run(name,args,timeout=1800):
    row={'name':name,'argv':args,'started_utc':datetime.datetime.now(datetime.timezone.utc).isoformat()}
    with (out/(name+'.log')).open('wb')as f:
        try:row['exit_code']=subprocess.run(args,stdout=f,stderr=subprocess.STDOUT,timeout=timeout).returncode
        except subprocess.TimeoutExpired:row['exit_code']=124
    row['finished_utc']=datetime.datetime.now(datetime.timezone.utc).isoformat();results.append(row)
    (out/'results.json').write_text(json.dumps(results,indent=2));(out/(name+'.exit.txt')).write_text(str(row['exit_code']))
    return row['exit_code']
assert run('volume',['docker','volume','create',volume])==0
image='knowhub-build/golang:1.26-bookworm-ustc'
common=['docker','run','--rm','--label','knowhub.v1=development','--network','none',
        '-v',str(base/'source')+':/source:ro','-v',str(base/'artifacts')+':/artifacts',
        '-v',volume+':/v1','-v','knowhub-mcp-b7-20261021-102200-data:/prior',
        '-v','/home/aqyfzx/go/pkg/mod:/home/aqyfzx/go/pkg/mod:ro',
        '--add-host','example.com:8.8.8.8','--add-host','api.e2b.app:8.8.8.8',
        '-e','GOPATH=/home/aqyfzx/go','-e','GOCACHE=/prior/go-cache','-e','GOTMPDIR=/v1/tmp',
        '-e','GOTOOLCHAIN=local','-e','CGO_CFLAGS=-O2 -g -I/v1/sqlite-headers',
        '-w','/source',image]
assert run('init',common+['sh','-c','mkdir -p /v1/tmp /v1/sqlite-headers; cp /home/aqyfzx/go/pkg/mod/github.com/mattn/go-sqlite3@v1.14.24/sqlite3-binding.h /v1/sqlite-headers/sqlite3.h; cp /home/aqyfzx/go/pkg/mod/github.com/mattn/go-sqlite3@v1.14.24/sqlite3ext.h /v1/sqlite-headers/'])==0
run('go-version',common+['go','version'])
packages=['./internal/application/access/...','./internal/application/repository','./internal/application/repository/retriever/...','./internal/database','./internal/types','./internal/middleware','./internal/router','./internal/handler','./internal/container','./internal/mcpserver','./internal/config']
run('core',common+['go','test','-tags','sqlite_fts5','-p','2','-count=1']+packages)
run('service-security',common+['go','test','-tags','sqlite_fts5','-p','2','-count=1','-v','./internal/application/service','-run','TestPortalV1|TestPortalM7|TestPortalM6|TestMCPEndpoint.*Revalidation'])
run('race',common+['go','test','-race','-tags','sqlite_fts5','-p','2','-count=1','./internal/application/access/...','./internal/application/service','./internal/mcpserver','./internal/router','-run','TestPortalV1|TestPortalM7|TestPortalM6|TestMCPEndpoint.*Revalidation|TestDocumentM5|TestEnterpriseM2'])
run('vet',common+['go','vet','-tags','sqlite_fts5']+packages+['./internal/application/service'])
run('app-build',common+['go','build','-tags','sqlite_fts5','-o','/artifacts/WeKnora','./cmd/server'])
run('postgres-test-build',common+['go','test','-tags','sqlite_fts5,knowhub_m6_postgres','-c','-o','/artifacts/postgres.test','./internal/router'])
(out/'done.json').write_text(json.dumps({'candidate_sha256':sha,'all_passed':all(r['exit_code']==0 for r in results),'results':[{k:r[k]for k in ['name','exit_code']}for r in results]}))
