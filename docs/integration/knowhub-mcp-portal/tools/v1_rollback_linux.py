"""Bounded rollback drill on the exact V1 Compose project; no schema down."""
from pathlib import Path
import json,subprocess,sys,urllib.request,urllib.error

base=Path(sys.argv[1]).resolve()
assert base.parent==Path('/home/aqyfzx/ai/WeKnora-release-build/knowhub-mcp-v1-20261021')
m=json.loads((base/'manifest.json').read_text());assert base.name==m['candidate_sha256'][:16]
instance=json.loads((base/'results/deploy/instance.json').read_text());project=instance['project'];origin=instance['url']
out=base/'results/rollback';out.mkdir(exist_ok=False);rows=[];checks=[]
compose=['docker','compose','--project-name',project,'--project-directory',str(base),'-f',str(base/'compose.yml')]
config=(base/'runtime-config.yaml').read_text();env=(base/'.env').read_text()
def run(name,args):
    with (out/(name+'.log')).open('wb')as f:code=subprocess.run(args,stdout=f,stderr=subprocess.STDOUT,timeout=240).returncode
    rows.append({'name':name,'argv':args,'exit_code':code});(out/'results.json').write_text(json.dumps(rows,indent=2));assert code==0,name
def status(path,authenticated=False):
    headers={}
    if authenticated:headers['Authorization']='Bearer '+json.loads((base/'private/client.json').read_text())['login']['token']
    request=urllib.request.Request(origin+path,headers=headers)
    try:
        with urllib.request.urlopen(request,timeout=15)as r:return r.status
    except urllib.error.HTTPError as e:return e.code
try:
    (base/'runtime-config.yaml').write_text(config.replace('personal_mcp:\n  enabled: true','personal_mcp:\n  enabled: false',1))
    run('switch-off',compose+['up','-d','--force-recreate','--wait','--wait-timeout','180','app','frontend'])
    assert status('/health')==200 and status('/api/v1/mcp/personal')==403
    # This old image is only used for our new synthetic DB, never shared RC3.
    oldapp='knowhub-rc3/app:0.8.0-oa-feishu-rc3-1e2ded5b6';oldfront='knowhub-rc3/frontend:0.8.0-oa-feishu-rc3-1e2ded5b6'
    run('old-image-material',['docker','image','inspect',oldapp,oldfront])
    override=base/'rollback.compose.json'
    override.write_text(json.dumps({'services':{'app':{'image':oldapp,'environment':{'AUTO_MIGRATE':'false'}},'frontend':{'image':oldfront}}}))
    run('restore-old-app-frontend',compose+['-f',str(override),'up','-d','--force-recreate','--wait','--wait-timeout','180','app','frontend'])
    health=status('/health');anonymous=status('/api/v1/mcp/personal');authenticated=status('/api/v1/mcp/personal',True)
    checks.append({'stage':'old-version','health':health,'anonymous_mcp':anonymous,'authenticated_mcp':authenticated})
    (out/'http-checks.json').write_text(json.dumps(checks,indent=2))
    assert health==200 and anonymous in (401,403,404) and authenticated in (403,404), 'old-version route must not expose MCP; anonymous global Auth can return401'
    run('schema-retained',compose+['exec','-T','postgres','psql','-U','postgres','-d','knowhub_v1','-Atc','SELECT version,dirty FROM schema_migrations; SELECT count(*) FROM tenant_api_keys;'])
finally:
    (base/'runtime-config.yaml').write_text(config)
    (base/'.env').write_text(env)
    run('restore-v1-candidate',compose+['up','-d','--force-recreate','--wait','--wait-timeout','180','app','frontend'])
assert status('/health')==200 and status('/api/v1/mcp/personal')==401
(out/'done.json').write_text(json.dumps({'all_passed':True,'schema_down_executed':False,'production_changes':False,'restored_current_candidate':True,'project':project}))
