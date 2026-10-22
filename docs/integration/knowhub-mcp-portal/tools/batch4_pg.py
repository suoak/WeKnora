"""3035 and live PostgreSQL CAS/epoch checks on a disposable tmpfs DB."""
import json
from pathlib import Path
import subprocess
import sys
import time
import shutil
import hashlib

BASE=Path(sys.argv[1]).resolve()
assert BASE.parent==Path('/home/aqyfzx/ai') and BASE.name.startswith('knowhub-mcp-m4-')
SOURCE=BASE/'source';LOGS=BASE/'results';name=BASE.name+'-pg'
socket=BASE/('pgsocket-'+str(time.time_ns()));socket.mkdir();socket.chmod(0o777)
checks=[]
def command(args,data=None,timeout=120):
    p=subprocess.run(args,input=data,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,timeout=timeout)
    if p.returncode:raise RuntimeError(p.stdout.decode(errors='replace'))
    return p.stdout.decode()
def sql(query,db='knowhub_m2_test'):
    return command(['docker','exec','-i',name,'psql','-X','-qAt','-v','ON_ERROR_STOP=1','-U','postgres','-d',db],query.encode())
assert subprocess.run(['docker','inspect',name],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode!=0
try:
    command(['docker','run','-d','--name',name,'--network','none','--tmpfs','/var/lib/postgresql/data','-v',str(socket)+':/var/run/postgresql','-e','POSTGRES_HOST_AUTH_METHOD=trust','-e','POSTGRES_DB=knowhub_m2_test','paradedb/paradedb:v0.22.2-pg17'])
    for _ in range(80):
        if subprocess.run(['docker','exec',name,'pg_isready','-h','127.0.0.1','-U','postgres'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode==0:break
        time.sleep(.5)
    else:raise RuntimeError('isolated PostgreSQL startup timeout')
    # Both databases start empty. The Go tests establish 3033 using the
    # candidate's official migrate source and Migrate(3033), never Force.
    sql('CREATE DATABASE knowhub_m3_chain','postgres')
    sql('CREATE DATABASE knowhub_m3_fresh','postgres')
    sql('CREATE DATABASE knowhub_m3_empty','postgres')
    sql('CREATE DATABASE knowhub_m3_failure','postgres')
    args=['docker','run','--rm','--network','none','-v',str(SOURCE)+':'+str(SOURCE),
        '-v',str(socket)+':'+str(socket),'-v','/home/aqyfzx/go/pkg/mod:/home/aqyfzx/go/pkg/mod:ro',
        '-v','/home/aqyfzx/ai/knowhub-mcp-m2-20261020-f44954/go-cache:/cache',
        '-v',str(BASE/'sqlite-headers')+':/knowhub-m3-sqlite-3.46.1:ro','-e','CGO_CFLAGS=-O2 -g -I/knowhub-m3-sqlite-3.46.1',
        '-e','GOPATH=/home/aqyfzx/go','-e','GOCACHE=/cache','-e','GOTOOLCHAIN=local','-e','GOSUMDB=sum.golang.org',
        '-e','KNOWHUB_M2_TEST_POSTGRES_DSN=host='+str(socket)+' user=postgres dbname=knowhub_m2_test sslmode=disable',
        '-w',str(SOURCE),'knowhub-build/golang:1.26-bookworm-ustc','go','test','-tags','sqlite_fts5,knowhub_m3_postgres','-race','-count=1','-v','./internal/application/repository','-run','^TestEnterpriseM2Postgres|^TestDocumentM3Postgres.*MigrationRunner$']
    with (LOGS/'pg-live-cas.log').open('wb')as f:code=subprocess.run(args,stdout=f,stderr=subprocess.STDOUT,timeout=900).returncode
    checks.append({'phase':'live-pg-cas-row-truncate-incarnation','command':args,'exit_code':code,'status':'PASS'if code==0 else 'FAIL'})
except Exception as exc:
    checks.append({'phase':'postgres','status':'FAIL','error':str(exc)})
finally:
    subprocess.run(['docker','rm','-f',name],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
    (LOGS/'pg-results.json').write_text(json.dumps(checks,indent=2)+'\n')
print(json.dumps(checks))
sys.exit(0 if checks and all(x['status']=='PASS'for x in checks)else 1)
