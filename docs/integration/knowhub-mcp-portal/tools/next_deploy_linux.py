"""Deploy only this candidate's independent, loopback-only synthetic instance."""
from pathlib import Path
import datetime,hashlib,ipaddress,json,os,secrets,shutil,socket,subprocess,sys,time,urllib.error,urllib.request

base=Path(sys.argv[1]).resolve()
assert base.parent==Path('/home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022')
m=json.loads((base/'manifest.json').read_text());sha=m['candidate_sha256'];assert base.name==sha[:16]
quality=json.loads((base/'quality-gate.json').read_text()); assert quality['all_passed'] and quality['source_sha256']==m['runtime_source_sha256']
build=json.loads((base/'results/build/done.json').read_text());assert build['all_passed']
out=base/'results/deploy';out.mkdir(exist_ok=True)
results=json.loads((out/'results.json').read_text())if(out/'results.json').exists()else[]
project='knowhub-v1-wiki-dynamic-'+sha[:12]
if (base/'.env').exists():
    port=int(next(x.split('=',1)[1]for x in(base/'.env').read_text().splitlines()if x.startswith('V1_WEB_PORT=')))
else:
    with socket.socket()as s:s.bind(('127.0.0.1',0));port=s.getsockname()[1]
private=base/'private';private.mkdir(mode=0o700,exist_ok=True)
if not(private/'app.env').exists():
    (private/'app.env').write_text('JWT_SECRET='+secrets.token_hex(32)+'\nSYSTEM_AES_KEY='+secrets.token_hex(16)+'\nSYSTEM_SIGNING_KEY='+secrets.token_hex(32)+'\nKNOWHUB_V1_TEST_PASSWORD='+secrets.token_urlsafe(32)+'\n')
os.chmod(private/'app.env',0o600)
if '\nDB_PASSWORD='not in(private/'app.env').read_text():
    with(private/'app.env').open('a')as f:f.write('DB_PASSWORD='+secrets.token_hex(24)+'\n')
routes=json.loads(subprocess.check_output(['ip','-j','-4','route','show','table','all']))
# Every immutable candidate must retain its own networks, including earlier
# intermediate candidates. Select a free pair rather than reusing their IPAM.
if (base/'.env').exists():
    settings=dict(x.split('=',1)for x in(base/'.env').read_text().splitlines())
    subnet=ipaddress.ip_network(settings['V1_SUBNET']);edge=ipaddress.ip_network(settings['V1_EDGE_SUBNET'])
else:
    occupied=[ipaddress.ip_network(r['dst'],strict=False)for r in routes if r.get('dst','default')!='default']
    network_ids=subprocess.check_output(['docker','network','ls','-q']).decode().split()
    networks=json.loads(subprocess.check_output(['docker','network','inspect']+network_ids))if network_ids else[]
    occupied.extend(ipaddress.ip_network(c['Subnet'],strict=False)for net in networks for c in net.get('IPAM',{}).get('Config',[])if c.get('Subnet')and ':'not in c['Subnet'])
    pair=next(((ipaddress.ip_network('10.253.'+str(n)+'.0/28'),ipaddress.ip_network('10.253.'+str(n+1)+'.0/28'))for n in range(44,250,2)if not any(ipaddress.ip_network('10.253.'+str(v)+'.0/28').overlaps(o)for v in (n,n+1)for o in occupied)),None)
    assert pair is not None,'No free test-owned IPAM pair; do not modify shared networking'
    subnet,edge=pair
for checked,suffix in [(subnet,'isolated'),(edge,'edge')]:
    for route in routes:
        if route.get('dst','default')!='default'and checked.overlaps(ipaddress.ip_network(route['dst'],strict=False)):
            own=json.loads(subprocess.check_output(['docker','network','inspect',project+'_'+suffix]))
            assert any(ipaddress.ip_network(c['Subnet'])==checked for c in own[0]['IPAM']['Config'])
(out/'ipam.json').write_text(json.dumps({'subnet':str(subnet),'routes_checked':True,'global_daemon_or_firewall_config_changed':False}))
(base/'.env').write_text('V1_APP_IMAGE='+build['app']+'\nV1_FRONTEND_IMAGE='+build['frontend']+'\nV1_WEB_PORT='+str(port)+'\nV1_SUBNET='+str(subnet)+'\nV1_EDGE_SUBNET='+str(edge)+'\n')
shutil.copy2(base/'deployment-compose.yml',base/'compose.yml')
config=(base/'source/config/config.yaml').read_text()
assert 'personal_mcp:\n  enabled: false' in config
(base/'runtime-config.yaml').write_text(config)
compose=['docker','compose','--project-name',project,'--project-directory',str(base),'-f',str(base/'compose.yml')]
def run(name,args,timeout=600):
    previous=[r for r in results if r['name']==name or r['name'].startswith(name+'-retry-')]
    if previous and previous[-1]['exit_code']==0:return
    if previous:name=name+'-retry-'+str(len(previous))
    row={'name':name,'argv':args,'started_utc':datetime.datetime.now(datetime.timezone.utc).isoformat()}
    with (out/(name+'.log')).open('wb')as f:
        try:row['exit_code']=subprocess.run(args,stdout=f,stderr=subprocess.STDOUT,timeout=timeout).returncode
        except subprocess.TimeoutExpired:row['exit_code']=124
    row['finished_utc']=datetime.datetime.now(datetime.timezone.utc).isoformat();results.append(row)
    (out/'results.json').write_text(json.dumps(results,indent=2));(out/(name+'.exit.txt')).write_text(str(row['exit_code']))
    assert row['exit_code']==0,name
def status(path):
    try:
        with urllib.request.urlopen('http://127.0.0.1:'+str(port)+path,timeout=15)as r:return r.status
    except urllib.error.HTTPError as e:return e.code
def wait_http_health(phase):
    deadline=time.monotonic()+90;attempts=[]
    evidence=out/('http-readiness-'+phase+'-'+str(time.time_ns())+'.json')
    while time.monotonic()<deadline:
        try:
            code=status('/health');attempts.append({'http_status':code})
            if code==200:
                evidence.write_text(json.dumps({'ready':True,'bounded_seconds':90,'attempts':attempts},indent=2));return
        except (OSError,TimeoutError) as error:attempts.append({'error_class':type(error).__name__})
        time.sleep(.5)
    evidence.write_text(json.dumps({'ready':False,'bounded_seconds':90,'attempts':attempts},indent=2))
    raise RuntimeError('candidate Nginx HTTP health never ready within 90 seconds')
try:
    run('compose-validate',compose+['config','--quiet'])
    run('startup-disabled-edge',compose+['up','-d','--wait','--wait-timeout','180'],240)
    wait_http_health('disabled')
    assert status('/health')==200
    disabled=status('/api/v1/mcp/personal');assert disabled==403
    (out/'disabled.json').write_text(json.dumps({'http_status':disabled,'health':200,'candidate':sha}))
    run('nginx-check',compose+['exec','-T','frontend','nginx','-t'])
    run('schema-disabled',compose+['exec','-T','postgres','psql','-U','postgres','-d','knowhub_v1','-Atc','SELECT version,dirty FROM schema_migrations'])
    run('postgres-engine-version',compose+['exec','-T','postgres','psql','-U','postgres','-d','knowhub_v1','-Atc',"SHOW server_version; SELECT extname,extversion FROM pg_extension WHERE extname IN ('pg_search','vector') ORDER BY extname"])
    network=project+'_isolated'
    run('seed-synthetic',['docker','run','--rm','--network',network,'--env-file',str(private/'app.env'),
        '-e','KNOWHUB_V1_SEED=SYNTHETIC_V1_ONLY','-e','KNOWHUB_V1_ARTIFACTS=/artifacts',
        '-v',str(base/'source')+':/source:ro','-v',str(base/'artifacts')+':/artifacts',
        '-w','/source','--entrypoint','/artifacts/seed.test',build['app'],
        '-test.v','-test.count=1','-test.run','^TestPortalV1SeedDeployed$'])
    run('seed-synthetic-wiki',['docker','run','--rm','--network',network,'--env-file',str(private/'app.env'),
        '-e','KNOWHUB_V1_SEED=SYNTHETIC_V1_ONLY',
        '-v',str(base/'artifacts')+':/artifacts',
        '--entrypoint','/artifacts/seed.test',build['app'],
        '-test.v','-test.count=1','-test.run','^TestV1NextWikiSeedDeployed$'])
    enabled=config.replace('personal_mcp:\n  enabled: false','personal_mcp:\n  enabled: true',1).replace('  policy_reference: ""','  policy_reference: "V1_SYNTHETIC_ISOLATED_PUBLISHED"',1).replace('  exceptions_enabled: false','  exceptions_enabled: true',1)
    assert '  review_enabled: false' in enabled
    (base/'runtime-config.yaml').write_text(enabled)
    with (private/'app.env').open('a') as f: f.write('FEISHU_SAFE_INCREMENTAL_ENABLED=true\n')
    run('startup-enabled',compose+['up','-d','--force-recreate','--wait','--wait-timeout','180','app','frontend'],240)
    wait_http_health('enabled')
    assert status('/api/v1/mcp/personal')==401
    run('containers',compose+['ps','--format','json'])
    (out/'instance.json').write_text(json.dumps({'candidate_sha256':sha,'project':project,'url':'http://127.0.0.1:'+str(port),'mcp_path':'/api/v1/mcp/personal','port':port,'exposure':'build-host loopback only; SSH forwarding required','database':'knowhub_v1','model':'SYNTHETIC 1024, not formal Embedding acceptance','sid':'NOT_CONFIGURED','rollback_stop':compose+['stop'],'cleanup_after_separate_confirmation':compose+['down'],'volumes_retained':True},indent=2))
    (out/'done.json').write_text(json.dumps({'all_passed':True,'candidate_sha256':sha,'production_changes':False}))
finally:
    # Logs only from our project; credentials are never requested via inspect.
    with (out/('app-'+str(len(results))+'-'+str(time.time_ns())+'.log')).open('wb')as f:subprocess.run(compose+['logs','--no-color','app'],stdout=f,stderr=subprocess.STDOUT,timeout=30)
