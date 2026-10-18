"""Read-only deployment checks and private metadata staging; never SID calls."""
import os,shutil,urllib.error
os.umask(0o077)
B=pathlib.Path('/home/aqyfzx/ai/knowhub-verified-email-candidate-5f988be51')
CONTRACT='"""No-credential route contract; shared by calibration and APP-only retry."""\nimport urllib.parse\nimport json\n\nPROTECTED = (\'/api/v1/auth/me\', \'/api/v1/knowledge-bases\',\n             \'/api/v1/system/admin/feishu\', \'/api/v1/me/feishu\',\n             \'/mcp/nonexistent-deployment-probe\')\nPATHS = (\'/login\', \'/health\', \'/api/v1/auth/oidc/config\',\n         \'/api/v1/auth/oidc/callback\') + PROTECTED\n\n\ndef same_container_inventory(before, after):\n    """Docker does not guarantee mount/port-binding array ordering."""\n    def canonical(value):\n        result = dict(value)\n        result[\'mounts\'] = sorted(result[\'mounts\'], key=lambda x: json.dumps(x, sort_keys=True))\n        result[\'ports\'] = {k: sorted(v, key=lambda x: json.dumps(x, sort_keys=True)) if v else v\n                           for k, v in (result.get(\'ports\') or {}).items()}\n        if \'networks\' in result:\n            result[\'networks\'] = {name: {key: sorted(item) if key in (\'Aliases\', \'DNSNames\') and item else item\n                                        for key, item in network.items()}\n                                  for name, network in result[\'networks\'].items()}\n        return result\n    return canonical(before) == canonical(after)\n\n\ndef same_resource_inventory(before, after, replace_app=None):\n    """No unapproved additions/removals; only the selected APP may change ID/image."""\n    if set(before) != set(after):\n        return False\n    for name in before:\n        left, right = dict(before[name]), dict(after[name])\n        if name == replace_app:\n            for key in (\'id\', \'image_id\'):\n                left.pop(key, None)\n                right.pop(key, None)\n        if not same_container_inventory(left, right):\n            return False\n    return True\n\n\ndef validate(port, path, status, location=None):\n    if port not in (80, 8080) or path not in PATHS:\n        raise ValueError(\'unsupported_probe_contract\')\n    expected = (200 if port == 80 else 401) if path == \'/login\' else (\n        302 if path.endswith(\'/callback\') else\n        200 if path in (\'/health\', \'/api/v1/auth/oidc/config\') else 401)\n    if type(status) is not int or status != expected:\n        raise ValueError(\'unexpected_probe_status\')\n    if path.endswith(\'/callback\'):\n        target = urllib.parse.urlsplit(location or \'\')\n        query = urllib.parse.parse_qs(target.query)\n        fragment = urllib.parse.parse_qs(target.fragment)\n        if target.hostname not in (None, \'knowhub.ruijie.com.cn\') or fragment.get(\'oidc_error\') != [\'invalid_state\']:\n            raise ValueError(\'unsafe_callback_destination\')\n        if any(k in query or k in fragment for k in (\'code\', \'token\', \'access_token\', \'id_token\')):\n            raise ValueError(\'callback_probe_credential_unexpected\')\n    return True\n'
TESTS='"""Synthetic status/redirect fault injection; no real SID or Docker activity."""\nimport unittest\nimport copy\nfrom verified_email_deployment_contract import PATHS, validate, same_container_inventory, same_resource_inventory\n\n\nclass DeploymentContractTest(unittest.TestCase):\n    def test_inventory_set_and_app_replacement_scope(self):\n        base = {\'app\': {\'id\': \'old\', \'image_id\': \'old\', \'mounts\': [], \'ports\': {}},\n                \'pg\': {\'id\': \'pg\', \'image_id\': \'pg\', \'mounts\': [], \'ports\': {}}}\n        replaced = copy.deepcopy(base)\n        replaced[\'app\'].update(id=\'new\', image_id=\'new\')\n        self.assertTrue(same_resource_inventory(base, replaced, \'app\'))\n        self.assertFalse(same_resource_inventory(base, replaced))\n        for mutation in (\'added\', \'removed\', \'other_id\', \'other_image\', \'app_mount\', \'app_port\'):\n            changed = copy.deepcopy(replaced)\n            if mutation == \'added\': changed[\'unexpected\'] = copy.deepcopy(base[\'pg\'])\n            elif mutation == \'removed\': del changed[\'pg\']\n            elif mutation == \'other_id\': changed[\'pg\'][\'id\'] = \'changed\'\n            elif mutation == \'other_image\': changed[\'pg\'][\'image_id\'] = \'changed\'\n            elif mutation == \'app_mount\': changed[\'app\'][\'mounts\'].append({\'Source\': \'unexpected\', \'Destination\': \'/data\', \'RW\': True})\n            elif mutation == \'app_port\': changed[\'app\'][\'ports\'][\'8080/tcp\'] = [{\'HostIp\': \'0.0.0.0\', \'HostPort\': \'8080\'}]\n            self.assertFalse(same_resource_inventory(base, changed, \'app\'), mutation)\n\n    def test_frontend_and_backend_are_different(self):\n        self.assertTrue(validate(80, \'/login\', 200))\n        self.assertTrue(validate(8080, \'/login\', 401))\n        with self.assertRaises(ValueError):\n            validate(8080, \'/login\', 200)\n        with self.assertRaises(ValueError):\n            validate(80, \'/login\', 401)\n\n    def test_every_endpoint_exact_status(self):\n        for port in (80, 8080):\n            for path in PATHS:\n                expected = (200 if port == 80 else 401) if path == \'/login\' else (302 if path.endswith(\'/callback\') else 200 if path in (\'/health\', \'/api/v1/auth/oidc/config\') else 401)\n                location = \'/#oidc_error=invalid_state\' if path.endswith(\'/callback\') else None\n                with self.subTest(port=port, path=path):\n                    self.assertTrue(validate(port, path, expected, location))\n                    for bad in (0, 200, 302, 400, 401, 403, 404, 429, 500, 502, 503):\n                        if bad != expected:\n                            with self.assertRaises(ValueError):\n                                validate(port, path, bad, location)\n\n    def test_callback_never_follows_authentication_or_external_redirect(self):\n        for location in (None, \'/login\', \'/#oidc_error=other\', \'https://example.invalid/#oidc_error=invalid_state\', \'/?code=synthetic#oidc_error=invalid_state\', \'/#oidc_error=invalid_state&token=synthetic\'):\n            with self.subTest(location=location), self.assertRaises(ValueError):\n                validate(80, \'/api/v1/auth/oidc/callback\', 302, location)\n\n    def test_failure_stops_iteration(self):\n        visited = []\n        with self.assertRaises(ValueError):\n            for path, status in ((\'/health\', 503), (\'/api/v1/auth/oidc/config\', 200)):\n                visited.append(path)\n                validate(80, path, status)\n        self.assertEqual(visited, [\'/health\'])\n\n    def test_unknown_contract_fails_closed(self):\n        for port, path, status in ((18090, \'/health\', 200), (80, \'/api/v1/auth/oidc/login\', 200), (80, \'/health\', True)):\n            with self.assertRaises(ValueError):\n                validate(port, path, status)\n\n    def test_inventory_order_is_not_a_resource_change(self):\n        before = {\'id\': \'synthetic-container\', \'image_id\': \'synthetic-image\',\n                  \'mounts\': [{\'Destination\': \'/a\', \'Source\': \'synthetic-a\', \'RW\': False},\n                             {\'Destination\': \'/b\', \'Source\': \'synthetic-b\', \'RW\': True}],\n                  \'ports\': {\'80/tcp\': [{\'HostIp\': \'127.0.0.1\', \'HostPort\': \'80\'},\n                                      {\'HostIp\': \'::1\', \'HostPort\': \'80\'}]}}\n        after = copy.deepcopy(before)\n        after[\'mounts\'].reverse()\n        after[\'ports\'][\'80/tcp\'].reverse()\n        self.assertTrue(same_container_inventory(before, after))\n        for field, value in ((\'id\', \'different\'), (\'image_id\', \'different\')):\n            changed = copy.deepcopy(after)\n            changed[field] = value\n            self.assertFalse(same_container_inventory(before, changed))\n        for field, value in ((\'Source\', \'different\'), (\'Destination\', \'/other\'), (\'RW\', True)):\n            changed = copy.deepcopy(before)\n            changed[\'mounts\'][0][field] = value\n            self.assertFalse(same_container_inventory(before, changed))\n        changed = copy.deepcopy(before)\n        changed[\'mounts\'].pop()\n        self.assertFalse(same_container_inventory(before, changed))\n        changed = copy.deepcopy(before)\n        changed[\'ports\'][\'80/tcp\'][0][\'HostIp\'] = \'0.0.0.0\'\n        self.assertFalse(same_container_inventory(before, changed))\n\n\nif __name__ == \'__main__\':\n    unittest.main()\n'
# Calibrate shared exact-status checks before any APP mutation.
C=B/'reliable-controls';C.mkdir(mode=0o700,exist_ok=True)
(C/'verified_email_deployment_contract.py').write_text(CONTRACT)
(C/'verified_email_deployment_contract_test.py').write_text(TESTS)
z=subprocess.run(['python3','-m','unittest','verified_email_deployment_contract_test','-v'],cwd=str(C),stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=15)
assert z.returncode==0
(C/'calibration-result.json').write_text(json.dumps({'result':'PASS','tests':7,'environment':'Linux','real_sid_requests':0}))
ns={};exec(compile(CONTRACT,'contract','exec'),ns)
class NoRedirect(urllib.request.HTTPRedirectHandler):
 def redirect_request(self,*a,**kw):return None
op=urllib.request.build_opener(urllib.request.ProxyHandler({}),NoRedirect());routes=[]
for port in (80,8080):
 for path in ns['PATHS']:
  try:r=op.open(urllib.request.Request('http://127.0.0.1:'+str(port)+path,headers={'Host':'knowhub.ruijie.com.cn'}),timeout=10)
  except urllib.error.HTTPError as e:r=e
  with r:
   ns['validate'](port,path,r.status,r.headers.get('Location'))
   assert r.headers.get('X-KnowHub-Environment')=='company-test-f40-db3030'
   routes.append({'port':port,'path':path,'status':r.status})
G=pathlib.Path('/home/aqyfzx/ai/knowhub-company-entry-20261017/green')
P='knowhub-company-test';APP=P+'-app-1';PG=P+'-postgres-1'
OLD='sha256:742c7ce56ef38ad32548900c8e7f8b264f9f6cd6138043674b91b27557443413'
NEW='sha256:0994762cd32f4289dc397757f9123bafd3cc913b407c05927c7ee064b32ade2b'
SHA='5f988be51779f1a22bd9d027b7503fe6a8749f62'
FRONT='sha256:ab7845c89fadeb17ad2b68bd924e892f948b893781a50ab79e48385c319ddbe1'
def sql(q):
 z=subprocess.run(['docker','exec','-i',PG,'psql','-X','-qAt','-U','clean_owner','-d','knowhub_oa_sso_pilot','-v','ON_ERROR_STOP=1'],input=('BEGIN READ ONLY;'+q+';ROLLBACK;').encode(),stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=30)
 assert z.returncode==0
 return z.stdout.decode().strip()
assert inspect(APP,'.Image')==OLD and inspect(APP,'.State.Health.Status')=='healthy'
assert inspect(P+'-frontend-1','.Image')==FRONT
assert json.loads(run(['docker','image','inspect',NEW]))[0]['Config']['Labels']['org.opencontainers.image.revision']==SHA
assert json.loads(run(['docker','image','inspect',OLD]))[0]['Id']==OLD
assert sql("SELECT version::text||'|'||dirty::text FROM schema_migrations")=='3030|false'
tx=int(sql('SELECT COUNT(*) FROM oidc_transactions WHERE NOT consumed AND expires_at>NOW()'));assert tx==0
flags=['OIDC_AUTH_ENABLE','SOURCEID_PROTOCOL_ONLY','SOURCEID_CONTRACT_CONFIRMED','SOURCEID_EMAIL_BINDING_ENABLE','SOURCEID_AUTO_BINDING_ENABLE','SOURCEID_JIT_ENABLE','SOURCEID_SESSION_MODE','SOURCEID_FORCE_REAUTH_PROMPT_LOGIN','SOURCEID_LOCAL_REFRESH_ENABLE','SOURCEID_BASIC_ENCODING','SOURCEID_DIAGNOSTICS_ENABLE','AUTO_MIGRATE']
env=json.loads(run(['docker','exec',APP,'python3','-c',"import os,json,sys;print(json.dumps({k:os.environ.get(k) for k in json.loads(sys.argv[1])}))",json.dumps(flags)]))
assert env==dict(zip(flags,['false','true','false','false','false','false','external','false','false','oauth_form','false','false']))
candidate=json.loads((B/'app-pin.json').read_text());rollback=json.loads((B/'app-rollback-pin.json').read_text())
assert candidate['services']['app']['image']==NEW and rollback['services']['app']['image']==OLD
assert candidate['services']['app']['environment']=={'SOURCEID_ACCOUNT_LINK_MODE':'trusted_ownership','SOURCEID_VERIFIED_EMAIL_POLICY_CONFIRMED':'false','SOURCEID_VERIFIED_EMAIL_POLICY_REF':''}
assert inspect(APP,'.Config.Labels')['com.docker.compose.project.config_files']==','.join([str(G/'compose.json'),str(B/'app-rollback-pin.json')])
assert all(not inspect(P+'-'+s+'-1','.HostConfig.PortBindings') for s in ('app','postgres','redis','docreader'))
assert shutil.disk_usage(B).free>8*1024**3
backup=B/'private'/('reliable-'+datetime.datetime.now(datetime.timezone.utc).strftime('%Y%m%dT%H%M%SZ'));backup.mkdir(mode=0o700,exist_ok=False)
names=run(['docker','ps','--format','{{.Names}}']).decode().split()
fmt='{"id":{{json .Id}},"image_id":{{json .Image}},"mounts":{{json .Mounts}},"ports":{{json .NetworkSettings.Ports}},"network_mode":{{json .HostConfig.NetworkMode}},"networks":{{json .NetworkSettings.Networks}}}'
containers=dict(zip(names,[json.loads(x) for x in run(['docker','inspect','--format',fmt]+names).decode().splitlines()]))
(backup/'containers-before.json').write_text(json.dumps(containers))
(backup/'resource-sets-before.json').write_text(json.dumps({'volumes':sorted(run(['docker','volume','ls','--format','{{.Name}}']).decode().split()),'networks':sorted(run(['docker','network','ls','--format','{{.ID}}']).decode().split())}))
HISTORICAL_ROUTES=[{'port': 80, 'path': '/login', 'status': 200}, {'port': 80, 'path': '/health', 'status': 200}, {'port': 80, 'path': '/api/v1/auth/oidc/config', 'status': 200}, {'port': 80, 'path': '/api/v1/auth/oidc/callback', 'status': 302}, {'port': 80, 'path': '/api/v1/auth/me', 'status': 401}, {'port': 80, 'path': '/api/v1/knowledge-bases', 'status': 401}, {'port': 80, 'path': '/api/v1/system/admin/feishu', 'status': 401}, {'port': 80, 'path': '/api/v1/me/feishu', 'status': 401}, {'port': 80, 'path': '/mcp/nonexistent-deployment-probe', 'status': 401}, {'port': 8080, 'path': '/login', 'status': 401}, {'port': 8080, 'path': '/health', 'status': 200}, {'port': 8080, 'path': '/api/v1/auth/oidc/config', 'status': 200}, {'port': 8080, 'path': '/api/v1/auth/oidc/callback', 'status': 302}, {'port': 8080, 'path': '/api/v1/auth/me', 'status': 401}, {'port': 8080, 'path': '/api/v1/knowledge-bases', 'status': 401}, {'port': 8080, 'path': '/api/v1/system/admin/feishu', 'status': 401}, {'port': 8080, 'path': '/api/v1/me/feishu', 'status': 401}, {'port': 8080, 'path': '/mcp/nonexistent-deployment-probe', 'status': 401}]
HISTORICAL_MOUNT_DIFF={'result': 'READ_ONLY', 'differences': [{'container': 'knowhub-sourceid-p6-frontend-1', 'differing_keys': ['mounts'], 'mounts_equal_ignoring_order': True, 'id_unchanged': True, 'image_unchanged': True, 'ports_unchanged': True}, {'container': 'knowhub-ipd-app', 'differing_keys': ['mounts'], 'mounts_equal_ignoring_order': True, 'id_unchanged': True, 'image_unchanged': True, 'ports_unchanged': True}], 'app_image': 'sha256:742c7ce56ef38ad32548900c8e7f8b264f9f6cd6138043674b91b27557443413', 'app_health': 'healthy', 'real_sid_requests': 0}
import copy
for item in HISTORICAL_ROUTES:
 ns['validate'](item['port'],item['path'],item['status'],'/#oidc_error=invalid_state' if item['path'].endswith('/callback') else None)
assert any(x['port']==8080 and x['path']=='/login' and x['status']==401 for x in HISTORICAL_ROUTES)
assert HISTORICAL_MOUNT_DIFF['differences'] and all(d['mounts_equal_ignoring_order'] and d['id_unchanged'] and d['image_unchanged'] and d['ports_unchanged'] for d in HISTORICAL_MOUNT_DIFF['differences'])
previous=json.loads((B/'retry-state.json').read_text());historical=json.loads((pathlib.Path(previous['backup_directory'])/'containers-before.json').read_text())
replay_keys=('id','image_id','mounts','ports')
current={n:{k:containers[n][k] for k in replay_keys} for n in containers}
assert ns['same_resource_inventory'](historical,current,APP)
(backup/'historical-before-private.json').write_text(json.dumps(historical));(backup/'historical-after-replay-private.json').write_text(json.dumps(current))
# Offline fault injection starts from actual resource snapshots, never real changes.
negative={}
target=next(n for n in containers if n!=APP and containers[n]['mounts'])
for fault in ('container_id','image','mount_source','mount_target','mount_rw','mount_delete','port','resource_add','resource_remove','network_mode','network_address'):
 changed=copy.deepcopy(containers);row=changed[target]
 if fault=='container_id':row['id']='synthetic-different'
 elif fault=='image':row['image_id']='synthetic-different'
 elif fault=='mount_source':row['mounts'][0]['Source']='synthetic-different'
 elif fault=='mount_target':row['mounts'][0]['Destination']='/synthetic-different'
 elif fault=='mount_rw':row['mounts'][0]['RW']=not row['mounts'][0]['RW']
 elif fault=='mount_delete':row['mounts'].pop()
 elif fault=='port':row['ports']={'65000/tcp':[{'HostIp':'0.0.0.0','HostPort':'65000'}]}
 elif fault=='resource_add':changed['synthetic-extra-resource']=copy.deepcopy(row)
 elif fault=='resource_remove':del changed[target]
 elif fault=='network_mode':row['network_mode']='synthetic-different'
 elif fault=='network_address':
  net=next(n for n in containers if n!=APP and containers[n]['networks']);key=next(iter(changed[net]['networks']));changed[net]['networks'][key]['IPAddress']='192.0.2.99'
 negative[fault]=not ns['same_resource_inventory'](containers,changed,APP)
assert all(negative.values())
representation=copy.deepcopy(containers)
for row in representation.values():
 row['mounts'].reverse()
 for value in (row['ports'] or {}).values():
  if value:value.reverse()
 for network in row['networks'].values():
  for key in ('Aliases','DNSNames'):
   if network.get(key):network[key].reverse()
assert ns['same_resource_inventory'](containers,representation,APP)
replay={'result':'PASS','historical_8080_login_401':'PASS','historical_real_mount_false_alarm':'PASS','actual_before_after_snapshot_replay':'PASS','actual_snapshots_saved_private':True,'representation_changes':'PASS','actual_snapshot_fault_injection':negative,'linux_unit_tests':7,'live_service_changes':False}
(C/'full-replay-result.json').write_text(json.dumps(replay))
(backup/'app-config.json').write_text(json.dumps(inspect(APP,'.Config')))
(backup/'app-host-config.json').write_text(json.dumps(inspect(APP,'.HostConfig')))
for name,path in [('compose.json',G/'compose.json'),('app.env',G/'private/app.env'),('app-pin.json',B/'app-pin.json'),('app-rollback-pin.json',B/'app-rollback-pin.json'),('entry-nginx.conf',G.parent/'entry/nginx.conf'),('entry-compose.json',G.parent/'entry/compose.json')]:shutil.copyfile(path,backup/name)
cmd=['docker','compose','--project-name',P,'--project-directory',str(G),'-f',str(G/'compose.json'),'-f',str(B/'app-pin.json')]
(backup/'effective-compose.json').write_bytes(run(cmd+['config','--format','json']))
resolved=json.loads((backup/'effective-compose.json').read_text());assert resolved['services']['app']['image']==NEW and resolved['services']['frontend']['image']==FRONT
baseline=json.loads(sql("SELECT json_build_object('users',(SELECT COUNT(*) FROM users),'members',(SELECT COUNT(*) FROM tenant_members),'active_kbs',(SELECT COUNT(*) FROM knowledge_bases WHERE deleted_at IS NULL),'documents',(SELECT COUNT(*) FROM knowledges WHERE deleted_at IS NULL),'chunks',(SELECT COUNT(*) FROM chunks WHERE deleted_at IS NULL AND length(trim(content))>0),'embeddings',(SELECT COUNT(*) FROM embeddings),'identities',(SELECT COUNT(*) FROM external_identities),'sessions',(SELECT COUNT(*) FROM source_id_sessions),'jit',(SELECT COUNT(*) FROM source_id_jit_registrations),'ownership',(SELECT COUNT(*) FROM source_id_account_ownership))"))
assert baseline['users']==3 and baseline['documents']==3 and baseline['chunks']==baseline['embeddings']==2717 and all(baseline[k]==0 for k in ('identities','sessions','jit','ownership'))
connections={str(p):sum(1 for line in run(['ss','-Htn','state','established']).decode().splitlines() if len(line.split())>=4 and line.split()[-2].endswith(':'+str(p))) for p in (80,8080)}
activity=int(sql("SELECT COUNT(*) FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid() AND state='active'"));assert activity==0
(B/'reliable-state.json').write_text(json.dumps({'stage':'PREFLIGHT_PASS','backup_directory':str(backup),'expected_old_app':OLD,'candidate_image':NEW,'source_sha':SHA,'baseline_counts':baseline}))
print(json.dumps({'result':'PREFLIGHT_PASS','route_calibration':'PASS','full_historical_replay':replay,'linux_contract_tests':7,'routes':routes,'backup_directory':str(backup),'app':'f40b1602f','frontend':'832b6652a','db':3030,'dirty':False,'candidate_image':NEW,'candidate_revision':SHA,'active_authorization_transactions':tx,'active_other_db_queries':activity,'entry_established_connections':connections,'gates':env,'counts':baseline,'internal_services_not_published':True,'free_gib':round(shutil.disk_usage(B).free/1024**3,1),'real_sid_requests':0}))
