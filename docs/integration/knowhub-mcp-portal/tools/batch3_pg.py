"""3035 and live PostgreSQL CAS/epoch checks on a disposable tmpfs DB."""
import json
from pathlib import Path
import subprocess
import sys
import time
import shutil
import hashlib

BASE=Path(sys.argv[1]).resolve()
assert BASE.parent==Path('/home/aqyfzx/ai') and BASE.name.startswith('knowhub-mcp-m3-')
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
    # Replay the immutable production SOURCE chain into an empty TEST DB.
    frozen=Path('/home/aqyfzx/ai/knowhub-mcp-m2-20261020-f44954/production-migrations/migrations/versioned')
    assert frozen.is_dir()
    sql('CREATE DATABASE knowhub_m3_chain','postgres')
    for p in sorted(frozen.glob('*.up.sql')):
        if int(p.name.split('_')[0])>3033:continue
        try:(LOGS/('pg-chain-'+p.name+'.log')).write_text(sql(p.read_text(),'knowhub_m3_chain'))
        except Exception as exc:
            (LOGS/('pg-chain-'+p.name+'.log')).write_text(str(exc));raise
    signature="SELECT md5(string_agg(table_name||':'||column_name||':'||data_type,',' ORDER BY table_name,column_name)) FROM information_schema.columns WHERE table_schema='public' AND table_name NOT IN ('enterprise_space_policies','kb_publication_policies','knowledge_access_policies','document_subject_grants','document_access_epochs')"
    old=sql(signature,'knowhub_m3_chain')
    ups=[(SOURCE/('migrations/versioned/'+n+'.up.sql')).read_text()for n in ['003034_enterprise_publication','003035_document_access']]
    downs=[(SOURCE/('migrations/versioned/'+n+'.down.sql')).read_text()for n in ['003035_document_access','003034_enterprise_publication']]
    for phase in ('empty-up','down-up'):
        for migration in ups:sql(migration,'knowhub_m3_chain')
        assert sql(signature,'knowhub_m3_chain')==old
        assert sql('SELECT count(*) FROM document_subject_grants','knowhub_m3_chain').strip()=='0'
        for migration in downs:sql(migration,'knowhub_m3_chain')
        assert sql(signature,'knowhub_m3_chain')==old
        checks.append({'phase':phase,'status':'PASS','3033_columns_unchanged':True})
    sql('CREATE DATABASE knowhub_m3_composed TEMPLATE knowhub_m3_chain','postgres')
    composed=BASE/'composed-migrations';composed.mkdir()
    for p in frozen.glob('*.sql'):
        if int(p.name.split('_')[0])<=3033:shutil.copyfile(p,composed/p.name)
    for stem in ('003034_enterprise_publication','003035_document_access'):
        for direction in ('up','down'):
            p=SOURCE/('migrations/versioned/'+stem+'.'+direction+'.sql');shutil.copyfile(p,composed/p.name)
    (LOGS/'composed-source-sha256.json').write_text(json.dumps({'baseline_commit':'e62b17e2d305358467821aeff1cce63ce1d61254','test_only':True,'files':{p.name:hashlib.sha256(p.read_bytes()).hexdigest()for p in sorted(composed.iterdir())}},indent=2))
    args=['docker','run','--rm','--network','none','-v',str(SOURCE)+':'+str(SOURCE),
        '-v',str(composed)+':'+str(composed)+':ro',
        '-v',str(socket)+':'+str(socket),'-v','/home/aqyfzx/go/pkg/mod:/home/aqyfzx/go/pkg/mod:ro',
        '-v','/home/aqyfzx/ai/knowhub-mcp-m2-20261020-f44954/go-cache:/cache',
        '-v',str(BASE/'sqlite-headers')+':/knowhub-m3-sqlite-3.46.1:ro','-e','CGO_CFLAGS=-O2 -g -I/knowhub-m3-sqlite-3.46.1',
        '-e','GOPATH=/home/aqyfzx/go','-e','GOCACHE=/cache','-e','GOTOOLCHAIN=local','-e','GOSUMDB=sum.golang.org',
        '-e','KNOWHUB_M2_TEST_POSTGRES_DSN=host='+str(socket)+' user=postgres dbname=knowhub_m2_test sslmode=disable',
        '-e','KNOWHUB_M3_COMPOSED_MIGRATIONS='+str(composed),
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
