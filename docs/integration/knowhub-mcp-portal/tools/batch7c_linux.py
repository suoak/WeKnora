"""Isolated volume build gate. No deletion or migration of existing host data."""
import datetime,json,os,subprocess,sys,threading,time
from pathlib import Path

BASE=Path(__file__).resolve().parent
CONFIG=json.loads((BASE/'run-config.json').read_text())
assert BASE.parent==Path('/home/aqyfzx/ai') and BASE.name.startswith('knowhub-mcp-b7-')
VOLUME=CONFIG['volume'];assert VOLUME==BASE.name+'-data'
SOURCE='/work/source/'+CONFIG['candidate_sha256'][:16]
LOGS=BASE/('results-'+CONFIG['candidate_sha256'][:16]+'-'+CONFIG['phase']);LOGS.mkdir(exist_ok=False)
IMAGE='knowhub-build/golang:1.26-bookworm-ustc'
results=[];stop=threading.Event();blocked=threading.Event();active={'name':None}
def capacity():
    rows={}
    for path,minimum in [('/',8*1024**3),('/mnt/sata0',40*1024**3)]:
        stat=os.statvfs(path);available=stat.f_bavail*stat.f_frsize
        rows[path]={'available_bytes':available,'minimum_bytes':minimum,'available_inodes':stat.f_favail}
        if available<minimum or stat.f_favail<100000:blocked.set()
    return rows
def monitor():
    count=0
    with (LOGS/'capacity.jsonl').open('a')as log:
        while not stop.is_set():
            row={'utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'filesystems':capacity()}
            name=active['name']
            if name and count%15==0:
                try:
                    p=subprocess.run(['docker','exec',name,'du','-sk','/work'],capture_output=True,text=True,timeout=30)
                    if p.returncode==0:
                        row['observed_task_bytes']=int(p.stdout.split()[0])*1024
                        if row['observed_task_bytes']>30*1024**3:blocked.set()
                    else:row['usage_sample_status']='container-ended-or-unavailable'
                except (subprocess.TimeoutExpired,ValueError,OSError) as error:
                    row['monitor_error']=type(error).__name__;blocked.set()
            log.write(json.dumps(row)+'\n');log.flush()
            if blocked.is_set() and name:
                try:subprocess.run(['docker','stop','--time','3',name],capture_output=True,timeout=15)
                except (subprocess.TimeoutExpired,OSError) as error:
                    log.write(json.dumps({'stop_error':type(error).__name__})+'\n');log.flush()
            count+=1;stop.wait(1)
def run(name,args,timeout=1800):
    capacity()
    record={'name':name,'command':args,'started_utc':datetime.datetime.now(datetime.timezone.utc).isoformat()}
    if blocked.is_set():record.update(exit_code=125,status='BLOCKED_CAPACITY')
    else:
        container=BASE.name+'-'+CONFIG['candidate_sha256'][:16]+'-'+CONFIG['phase']+'-'+name
        if args[:2]==['docker','run']:
            args=args[:2]+['--name',container,'--label','knowhub.batch=7']+args[2:];record['command']=args;active['name']=container
        with (LOGS/(name+'.log')).open('wb')as f:
            try:record['exit_code']=subprocess.run(args,stdout=f,stderr=subprocess.STDOUT,timeout=timeout).returncode
            except subprocess.TimeoutExpired:
                record['exit_code']=124
                if active['name']:
                    subprocess.run(['docker','stop','--time','3',active['name']],capture_output=True,timeout=15)
        active['name']=None
    record['finished_utc']=datetime.datetime.now(datetime.timezone.utc).isoformat()
    results.append(record)
    p=LOGS/'results.tmp';p.write_text(json.dumps(results,indent=2));p.replace(LOGS/'results.json')
    (LOGS/(name+'.exit.txt')).write_text(str(record['exit_code']))
    return record['exit_code']
go=['docker','run','--rm','--network','none','-v',VOLUME+':/work',
    '--add-host','example.com:8.8.8.8','--add-host','api.e2b.app:8.8.8.8',
    '-v','/home/aqyfzx/go/pkg/mod:/home/aqyfzx/go/pkg/mod:ro',
    '-e','GOPATH=/home/aqyfzx/go','-e','GOCACHE=/work/go-cache','-e','GOTMPDIR=/work/tmp',
    '-e','GOTOOLCHAIN=local','-e','GOSUMDB=sum.golang.org','-e','CGO_CFLAGS=-O2 -g -I/work/sqlite-headers',
    '-w',SOURCE,IMAGE]
pyimage='weknora-custom/mcp-candidate:v0.8.0-knowhub.24-rc1-854f3b95'
try:
    assert not blocked.is_set()
    monitor_thread=threading.Thread(target=monitor,daemon=True);monitor_thread.start()
    init=['docker','run','--rm','--network','none','-v',VOLUME+':/work','-v','/home/aqyfzx/go/pkg/mod:/home/aqyfzx/go/pkg/mod:ro','--entrypoint','sh',IMAGE,'-c',
          'mkdir -p /work/tmp /work/sqlite-headers /work/results; cp /home/aqyfzx/go/pkg/mod/github.com/mattn/go-sqlite3@v1.14.24/sqlite3-binding.h /work/sqlite-headers/sqlite3.h; cp /home/aqyfzx/go/pkg/mod/github.com/mattn/go-sqlite3@v1.14.24/sqlite3ext.h /work/sqlite-headers/']
    assert run('environment-init',init)==0
    run('go-environment',go+['go','version'])
    if CONFIG['phase']=='focused':
        run('focused',go+['go','test','-tags','sqlite_fts5','-p','2','-count=1','-v','./internal/application/service','./internal/application/access','./internal/application/repository','./internal/router','./internal/mcpserver','-run','TestPortalM6|TestPortalM7|TestSourceIDM7|TestEnterpriseSnapshotM7'])
    if CONFIG['phase'] in ('all','integration','clients','service'):
        pgname=BASE.name+'-'+CONFIG['candidate_sha256'][:16]+'-'+CONFIG['phase']+'-pg-start'
        pgdata='/work/postgres/'+CONFIG['candidate_sha256'][:16]+'-'+CONFIG['phase']
        pgstart=['docker','run','-d','--network','none','-v',VOLUME+':/work','-e','POSTGRES_HOST_AUTH_METHOD=trust','-e','POSTGRES_DB=knowhub_m6','-e','PGHOST=/work/pgsocket','-e','PGDATA='+pgdata,'paradedb/paradedb:v0.22.2-pg17','-c','unix_socket_directories=/var/run/postgresql,/work/pgsocket','-c','listen_addresses=']
        if CONFIG['phase']=='clients':
            socket_volume=VOLUME+'-clients-socket'
            assert run('client-socket-init',['docker','run','--rm','--network','none','-v',socket_volume+':/socket','--entrypoint','chmod',IMAGE,'1777','/socket'])==0
            pgstart[pgstart.index('paradedb/paradedb:v0.22.2-pg17'):pgstart.index('paradedb/paradedb:v0.22.2-pg17')]=['-v',socket_volume+':/work/pgsocket']
        assert run('pg-start',pgstart)==0
        ready=False
        for _ in range(60):
            capacity()
            if blocked.is_set():break
            if subprocess.run(['docker','exec',pgname,'pg_isready','-h','/work/pgsocket','-U','postgres'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode==0:
                ready=True;break
            time.sleep(.5)
        assert ready,'isolated PostgreSQL readiness failure'
        pggo=go[:]
        pggo.insert(-1,'KNOWHUB_M6_POSTGRES_DSN=host=/work/pgsocket user=postgres dbname=knowhub_m6 sslmode=disable');pggo.insert(-2,'-e')
        if CONFIG['phase']=='clients':
            clientdir=BASE/('client-'+CONFIG['candidate_sha256'][:16]);clientdir.mkdir(mode=0o700,exist_ok=False)
            pggo=[('host' if value=='none' else value) for value in pggo]
            image_position=pggo.index(IMAGE);pggo[image_position:image_position]=['-v',socket_volume+':/work/pgsocket','-v',str(clientdir)+':/client','-e','KNOWHUB_CLIENT_DIRECTORY=/client']
        pggo.insert(-1,'KNOWHUB_M7_PLAN_DIR=/work/results/'+CONFIG['candidate_sha256'][:16]+'-plans');pggo.insert(-2,'-e')
        pgtest=['go','test','-tags','sqlite_fts5,knowhub_m6_postgres','-p','2','-count=1','-v','./internal/router','-timeout','18m','-run','TestPortalM6RealPostgresPersonal|TestPortalM7RealPostgresScale|TestPortalM7PublishedDynamicRealPostgres']
        if CONFIG['phase']=='clients':pgtest=['go','test','-tags','sqlite_fts5,knowhub_m7_clients','-p','2','-count=1','-v','./internal/router','-timeout','35m','-run','TestPortalM7ClientFixture']
        if CONFIG['phase']=='all':pgtest.insert(2,'-race')
        if CONFIG['phase']!='service':run('postgres-personal',pggo+pgtest,2100 if CONFIG['phase']=='clients'else 1200)
        if CONFIG['phase'] not in ('clients','service'):
            run('postgres-feishu-ingress',pggo+['go','test','-tags','sqlite_fts5,knowhub_m6_postgres','-p','2','-count=1','-v','./internal/application/service','-run','TestPortalM7PublishedFeishuIngressPostgres'],600)
            run('postgres-explain-plans',go+['sh','-c','for f in /work/results/'+CONFIG['candidate_sha256'][:16]+'-plans/*.json; do echo PLAN_FILE:$f; cat "$f"; done'],60)
        run('postgres-server-log',['docker','logs',pgname],60)
        if CONFIG['phase'] not in ('all','service'):run('postgres-stop',['docker','stop','--time','5',pgname],30)
    if CONFIG['phase']=='all':
        run('go-core',go+['go','test','-tags','sqlite_fts5','-p','2','-count=1','./internal/application/access/...','./internal/application/repository','./internal/application/repository/retriever/...','./internal/database','./internal/types','./internal/middleware','./internal/router','./internal/handler','./internal/container','./internal/mcpserver','./internal/config'])
        run('race',go+['go','test','-tags','sqlite_fts5','-race','-p','2','-count=1','./internal/application/access/...','./internal/application/repository','./internal/application/service','./internal/router','./internal/handler','-run','TestPortalM6|TestPortalM7|TestSourceIDM7|TestEnterpriseSnapshotM7|TestDocumentM5|TestDocumentM3|TestEnterpriseM2'])
        run('vet',go+['go','vet','-tags','sqlite_fts5','./internal/application/access/...','./internal/application/repository','./internal/application/service','./internal/router','./internal/handler','./internal/container','./internal/config'])
    if CONFIG['phase'] in ('all','python'):
        run('python-full',['docker','run','--rm','--network','none','-v',VOLUME+':/work','-w',SOURCE,'-e','WEKNORA_API_KEY=','-e','PYTHONDONTWRITEBYTECODE=1','-e','TMPDIR=/work/tmp','--entrypoint','python',pyimage,'-m','unittest','discover','-s','mcp-server','-p','test_*.py','-v'],600)
    if CONFIG['phase'] in ('all','service'):
        testnet=BASE.name+'-'+CONFIG['candidate_sha256'][:16]+'-service-net'
        assert run('service-network',['docker','network','create','--internal',testnet])==0
        redisname=BASE.name+'-'+CONFIG['candidate_sha256'][:16]+'-service-redis'
        assert run('service-redis',['docker','run','-d','--name',redisname,'--restart','always','--network',testnet,'--network-alias','knowhub-p1-dispatch-redis-20261018','redis:7.0-alpine','redis-server','--save','','--appendonly','no'])==0
        binary='/work/results/'+CONFIG['candidate_sha256'][:16]+'-router-python.test'
        if CONFIG['phase']=='all' and run('router-python-build',go+['go','test','-tags','sqlite_fts5,knowhub_m5_integration','-c','-o',binary,'./internal/router'])==0:
            run('router-python-sdk',['docker','run','--rm','--network','none','-v',VOLUME+':/work','-v','/home/aqyfzx/go/pkg/mod:/home/aqyfzx/go/pkg/mod:ro','-w',SOURCE+'/internal/router','-e','TMPDIR=/work/tmp','-e','WEKNORA_API_KEY=','-e','SYNC_EXECUTION_POSTGRES_DSN=postgresql://postgres@/knowhub_m6?host=%2Fwork%2Fpgsocket&sslmode=disable','-e','REQUIRE_SYNC_EXECUTION_POSTGRES=1','--entrypoint',binary,pyimage,'-test.v','-test.count=1','-test.run','TestDocumentM5PythonActualREST'],600)
        for package,name in [('internal/application/service','service'),('internal/agent/tools','agent')]:
            binary='/work/results/'+CONFIG['candidate_sha256'][:16]+'-'+name+'.test'
            if run(name+'-build',go+['go','test','-tags','sqlite_fts5','-c','-o',binary,'./'+package])==0:
                run(name+'-full',['docker','run','--rm','--network',testnet,'-v',VOLUME+':/work','-v','/home/aqyfzx/go/pkg/mod:/home/aqyfzx/go/pkg/mod:ro','--add-host','example.com:8.8.8.8','--add-host','api.e2b.app:8.8.8.8','-w',SOURCE+'/'+package,'-e','TMPDIR=/work/tmp','-e','WEKNORA_API_KEY=','-e','SYNC_EXECUTION_POSTGRES_DSN=postgresql://postgres@/knowhub_m6?host=%2Fwork%2Fpgsocket&sslmode=disable','-e','REQUIRE_SYNC_EXECUTION_POSTGRES=1','-e','FEISHU_DISPATCH_REDIS_ADDR=knowhub-p1-dispatch-redis-20261018:6379','--entrypoint',binary,pyimage,'-test.v','-test.count=1'],1200)
except BaseException as error:
    results.append({'name':'orchestration','exit_code':1,'error_class':type(error).__name__})
    (LOGS/'results.json').write_text(json.dumps(results,indent=2))
    raise
finally:
    if 'redisname' in globals():subprocess.run(['docker','stop','--time','3',redisname],capture_output=True,timeout=20)
    if 'pgname' in globals():
        with (LOGS/'postgres-final.log').open('wb') as pglog:
            subprocess.run(['docker','logs',pgname],stdout=pglog,stderr=subprocess.STDOUT,timeout=20)
        subprocess.run(['docker','stop','--time','5',pgname],capture_output=True,timeout=20)
    stop.set()
    if 'monitor_thread' in globals():monitor_thread.join(timeout=45)
    (LOGS/'capacity-final.json').write_text(json.dumps(capacity()))
    (LOGS/'done.json').write_text(json.dumps({'completed':True,'blocked_capacity':blocked.is_set(),'all_commands_passed':all(r['exit_code']==0 for r in results),'candidate_sha256':CONFIG['candidate_sha256'],'volume_retained':VOLUME,'no_existing_files_deleted':True}))
