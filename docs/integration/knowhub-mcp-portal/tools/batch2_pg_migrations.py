"""Synthetic PostgreSQL migration/CAS tests in a task-owned networkless DB.

Keeps actual production-3033 migration-chain checks separate from narrow
compatibility fixtures. Never connects to any existing DB/container.
"""
import json
import os
from pathlib import Path
import subprocess
import sys
import time

BASE = Path(sys.argv[1]).resolve()
assert BASE.name.startswith('knowhub-mcp-m2-') and BASE.parent == Path('/home/aqyfzx/ai')
SOURCE = BASE / 'source'
LOGS = BASE / 'results'
name = BASE.name + '-pg'
attempt = str(time.time_ns())
socket = BASE / ('pgsocket-' + attempt)
socket.mkdir()
socket.chmod(0o777)
checks = []


def command(args, data=None, timeout=120):
    p = subprocess.run(args, input=data, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=timeout)
    if p.returncode:
        raise RuntimeError(p.stdout.decode(errors='replace'))
    return p.stdout.decode()


def sql(query, db='knowhub_m2_test'):
    return command(['docker', 'exec', '-i', name, 'psql', '-X', '-qAt', '-v', 'ON_ERROR_STOP=1',
                    '-U', 'postgres', '-d', db], query.encode())


assert subprocess.run(['docker', 'inspect', name], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode != 0
try:
    command(['docker', 'run', '-d', '--name', name, '--network', 'none',
             '--tmpfs', '/var/lib/postgresql/data', '-v', str(socket)+':/var/run/postgresql',
             '-e', 'POSTGRES_HOST_AUTH_METHOD=trust', '-e', 'POSTGRES_DB=knowhub_m2_test',
             'paradedb/paradedb:v0.22.2-pg17'])
    for _ in range(60):
        # The image starts a temporary Unix-only initialization server. A TCP
        # readiness probe prevents tests racing its subsequent shutdown.
        if subprocess.run(['docker','exec',name,'pg_isready','-h','127.0.0.1','-U','postgres'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode == 0:
            break
        time.sleep(0.5)
    else:
        raise RuntimeError('isolated PostgreSQL startup timeout')
    # A narrow 3033 compatibility fixture with synthetic legacy data. It is
    # explicitly not a substitute for the full real migration-chain check.
    sql('''CREATE DATABASE knowhub_m2_fixture;''', 'postgres')
    sql('''CREATE TABLE schema_migrations(version BIGINT PRIMARY KEY,dirty BOOLEAN NOT NULL);
INSERT INTO schema_migrations VALUES(3033,false);
CREATE TABLE users(id VARCHAR(36) PRIMARY KEY); INSERT INTO users VALUES('owner'),('reviewer');
CREATE TABLE tenants(id INTEGER PRIMARY KEY); INSERT INTO tenants VALUES(99);
CREATE TABLE knowledge_bases(id VARCHAR(36) PRIMARY KEY); INSERT INTO knowledge_bases VALUES('historical');
CREATE TABLE knowledges(id VARCHAR(36) PRIMARY KEY); INSERT INTO knowledges VALUES('historical-doc');
CREATE TABLE tenant_api_keys(id BIGINT PRIMARY KEY,scope_type TEXT,capabilities JSONB);
INSERT INTO tenant_api_keys VALUES(1,'user_mcp','["retrieve"]'),(2,'tenant','["retrieve"]');
CREATE TABLE tenant_portal_configs(tenant_id INTEGER PRIMARY KEY,status TEXT); INSERT INTO tenant_portal_configs VALUES(99,'published');
''', 'knowhub_m2_fixture')
    up = (SOURCE/'migrations/versioned/003034_enterprise_publication.up.sql').read_text()
    down = (SOURCE/'migrations/versioned/003034_enterprise_publication.down.sql').read_text()
    before = sql("SELECT md5(string_agg(row_to_json(k)::text,'' ORDER BY id)) FROM tenant_api_keys k", 'knowhub_m2_fixture')
    for phase in ('old-up', 'down-up'):
        sql(up, 'knowhub_m2_fixture')
        assert sql('SELECT count(*) FROM enterprise_space_policies','knowhub_m2_fixture').strip() == '0'
        assert sql("SELECT md5(string_agg(row_to_json(k)::text,'' ORDER BY id)) FROM tenant_api_keys k",'knowhub_m2_fixture') == before
        assert sql('SELECT version FROM schema_migrations','knowhub_m2_fixture').strip() == '3033'
        sql("INSERT INTO kb_publication_policies(knowledge_base_id,updated_by) VALUES('historical','owner')",'knowhub_m2_fixture')
        assert sql("SELECT state || ':' || policy || ':' || inherit_new_ordinary::text FROM kb_publication_policies",'knowhub_m2_fixture').strip() == 'pending_review:pending_review:false'
        try:
            sql("UPDATE kb_publication_policies SET state='approved'",'knowhub_m2_fixture')
        except RuntimeError:
            pass
        else:
            raise AssertionError('incomplete approval accepted')
        checks.append({'phase':phase,'status':'PASS','legacy_keys_unchanged':True,'historical_public':False})
        sql(down,'knowhub_m2_fixture')
    # A genuinely empty database upgraded through the frozen production source
    # migrations. Preserve every migration's raw output and failure separately.
    sql('CREATE DATABASE knowhub_m2_chain', 'postgres')
    chain_ok = True
    for migration in sorted((BASE/'production-migrations/migrations/versioned').glob('*.up.sql')):
        if int(migration.name.split('_',1)[0]) > 3033:
            continue
        try:
            output = sql(migration.read_text(), 'knowhub_m2_chain')
            (LOGS/('chain-'+migration.name+'.log')).write_text(output)
        except RuntimeError as exc:
            (LOGS/('chain-'+migration.name+'.log')).write_text(str(exc))
            checks.append({'phase':'production-3033-chain','status':'BLOCKED','migration':migration.name})
            chain_ok = False
            break
    if chain_ok:
        # All pre-existing schema columns must remain byte-for-byte unchanged.
        signature_query = "SELECT md5(string_agg(table_name||':'||column_name||':'||data_type,',' ORDER BY table_name,column_name)) FROM information_schema.columns WHERE table_schema='public' AND table_name NOT IN ('enterprise_space_policies','kb_publication_policies','knowledge_access_policies')"
        previous = sql(signature_query,'knowhub_m2_chain')
        sql(up,'knowhub_m2_chain'); assert sql(signature_query,'knowhub_m2_chain') == previous
        sql(down,'knowhub_m2_chain'); assert sql(signature_query,'knowhub_m2_chain') == previous
        sql(up,'knowhub_m2_chain'); assert sql(signature_query,'knowhub_m2_chain') == previous
        checks.append({'phase':'production-3033-chain-and-empty-up-down-up','status':'PASS'})
    env = dict(os.environ, KNOWHUB_M2_TEST_POSTGRES_DSN='host='+str(socket)+' user=postgres dbname=knowhub_m2_test sslmode=disable')
    with (LOGS/'postgres-go-cas.log').open('wb') as output:
        p = subprocess.run(['go','test','-race','-count=1','-v','./internal/application/repository','-run','^TestEnterpriseM2Postgres'],cwd=SOURCE,env=env,stdout=output,stderr=subprocess.STDOUT,timeout=900)
    checks.append({'phase':'postgres-go-cas','status':'PASS' if p.returncode==0 else 'FAIL','exit_code':p.returncode})
finally:
    # Delete only the exact container created above, preserving all task logs.
    subprocess.run(['docker','rm','-f',name],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
    report = json.dumps(checks,indent=2)+'\n'
    (LOGS/('postgres-results-'+attempt+'.json')).write_text(report)
    (LOGS/'postgres-results.json').write_text(report)
print(json.dumps(checks))
sys.exit(0 if checks and all(x['status']=='PASS' for x in checks) else 1)
