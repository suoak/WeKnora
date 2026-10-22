"""Feature rollback retaining schema and hardened reads; only our candidate.

An old APP without persisted source read guards is unsafe after revocation.
Do not replace this drill with an externally reachable old APP downgrade.
"""
from pathlib import Path
import hashlib, hmac, json, subprocess, sys, time, urllib.parse, urllib.request, urllib.error

base = Path(sys.argv[1]).resolve()
assert base.parent == Path('/home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-feishu-20261022')
instance = json.loads((base/'results/deploy/instance.json').read_text())
assert instance['project'].startswith('knowhub-v1-feishu-')
source_run=json.loads((base/'feishu-source-current.json').read_text())
assert source_run['candidate_sha256']==instance['candidate_sha256']
assert json.loads((base/'results'/source_run['results_dir']/'result.json').read_text())['exit_code'] == 0
out = base/'results/rollback'
out.mkdir(exist_ok=False)
compose = ['docker','compose','--project-name',instance['project'],'--project-directory',str(base),'-f',str(base/'compose.yml')]
config = (base/'runtime-config.yaml').read_text()
private = json.loads((base/'private/client.json').read_text())
env = dict(line.split('=',1) for line in (base/'private/app.env').read_text().splitlines())
rows = []

def run(name,argv):
    with (out/(name+'.log')).open('wb') as f:
        code = subprocess.run(argv,stdout=f,stderr=subprocess.STDOUT,timeout=240).returncode
    rows.append({'name':name,'argv':argv,'exit_code':code})
    (out/'results.json').write_text(json.dumps(rows,indent=2))
    assert code == 0,name

def status(path,owner=False):
    headers = {'Authorization':'Bearer '+private['owner_jwt'],'X-Tenant-ID':'99'} if owner else {}
    try:
        with urllib.request.urlopen(urllib.request.Request(instance['url']+path,headers=headers),timeout=20) as response:
            response.read(); return response.status
    except urllib.error.HTTPError as error:
        error.read(); return error.code

def query(sql):
    return subprocess.check_output(compose+['exec','-T','postgres','psql','-U','postgres','-d','knowhub_v1','-Atc',sql]).decode().strip()

try:
    (base/'runtime-config.yaml').write_text(config.replace('personal_mcp:\n  enabled: true','personal_mcp:\n  enabled: false',1))
    override = base/'rollback-protection.compose.json'
    override.write_text(json.dumps({'services':{'app':{'environment':{'FEISHU_SAFE_INCREMENTAL_ENABLED':'false','FEISHU_LEGACY_SYNC_SUSPENDED':'true'}}}}))
    run('disable-with-hardened-reader',compose+['-f',str(override),'up','-d','--force-recreate','--wait','--wait-timeout','180','app','frontend'])
    assert status('/health') == 200 and status('/api/v1/mcp/personal') == 403
    kid = query("SELECT knowledge_id FROM feishu_legacy_resources WHERE data_source_id IN (SELECT id FROM data_sources WHERE legacy_access_state='revoked') LIMIT 1")
    assert kid
    assert status('/api/v1/knowledge/'+kid,True) in (403,404)
    # Reuse the EXACT link that returned 200 before revocation, not a newly
    # generated signature over the now-redacted empty file_path.
    reference=Path(source_run['revoked_file_reference']).resolve()
    assert reference.parent==base/'private'
    original_url=reference.read_text()
    assert hashlib.sha256(original_url.encode()).hexdigest()==source_run['revoked_file_sha256']
    link=urllib.parse.urlsplit(original_url)
    assert int(urllib.parse.parse_qs(link.query)['expires'][0])>time.time()
    assert status(link.path+'?'+link.query) in (403,404)
    assert query('SELECT version||\':\'||dirty FROM schema_migrations') == '3036:false'
    (out/'checks.json').write_text(json.dumps({'disabled_mcp':403,'hardened_REST_denied':True,'unexpired_anonymous_file_denied':True,'schema':'3036:false','production_changes':False,'old_unprotected_image':'NOT_EXECUTED_UNSAFE_AFTER_REVOCATION','old_image_recovery_requirement':'maintenance isolation or retained hardened reader plus denial-state reconciliation'},indent=2))
finally:
    (base/'runtime-config.yaml').write_text(config)
    run('restore-current-candidate',compose+['up','-d','--force-recreate','--wait','--wait-timeout','180','app','frontend'])
assert status('/health') == 200 and status('/api/v1/mcp/personal') == 401
(out/'done.json').write_text(json.dumps({'all_passed':True,'rollback_scope':'feature-off with hardened reads, not unprotected old image','schema_down_executed':False,'production_changes':False}))
