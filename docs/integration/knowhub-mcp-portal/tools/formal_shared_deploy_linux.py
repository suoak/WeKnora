"""Approved in-place P3 test upgrade. Never starts the unsafe old APP."""
from pathlib import Path
import datetime,hashlib,json,os,re,subprocess,sys,tarfile,time,urllib.parse,urllib.request,urllib.error
base=Path(sys.argv[1]).resolve();assert base.parent==Path('/home/aqyfzx/ai/knowhub-formal-c2-20261022')
assert '172.30.232.42'in subprocess.check_output(['hostname','-I']).decode().split()
state=json.loads((base/'private/state.json').read_text());candidate=Path(state['candidate']);backup=Path(state['backup_path']);private=Path(state['private_path'])
assert json.loads((base/'results/backup-a3/result.json').read_text())['exit_code']==0
assert json.loads((base/'results/build-ops/result.json').read_text())['exit_code']==0
attempt=sys.argv[2]if len(sys.argv)>2 else'initial';assert re.fullmatch(r'[a-z0-9-]+',attempt)
out=base/'results'/('deploy'if attempt=='initial'else'deploy-'+attempt);out.mkdir(exist_ok=False);rows=[];os.umask(0o077)
a=json.loads((private/'app-inspect.json').read_text());f=json.loads((private/'front-inspect.json').read_text());env=dict(v.split('=',1)for v in a['Config']['Env']if'='in v)
assert env['DB_HOST']=='WeKnora-postgres'and env['DB_NAME']=='WeKnora'
app='knowhub-mvp-p3-app-1';front='knowhub-mvp-p3-frontend-1';pg='WeKnora-postgres';entry='knowhub-company-entry-entry-1'
manifest=json.loads((candidate/'manifest.json').read_text());sha=manifest['candidate_sha256']
build=json.loads((candidate/'results/build/done.json').read_text());assert build['all_passed']and build['candidate_sha256']==sha
image=build['app'];frontimage=build['frontend']
expected_images=json.loads((candidate/'results/build/images.json').read_text())
assert all(json.loads(subprocess.check_output(['docker','image','inspect',v['image'][0]]))[0]['Id']==v['id']for v in expected_images)
expected_binary=json.loads((candidate/'results/build/artifacts.json').read_text())['WeKnora']
secretvalues=[v.encode()for k,v in env.items()if any(s in k for s in ['PASSWORD','SECRET','KEY'])and len(v)>8]
def run(name,args,stdin=None,timeout=300):
 start=datetime.datetime.now(datetime.timezone.utc).isoformat();p=subprocess.run(args,input=stdin,capture_output=True,timeout=timeout);raw=p.stdout+p.stderr
 for v in secretvalues:raw=raw.replace(v,b'[REDACTED]')
 (out/(name+'.log')).write_bytes(raw);rows.append({'name':name,'argv':args,'exit_code':p.returncode,'started_utc':start});(out/'commands.json').write_text(json.dumps(rows,indent=2));assert p.returncode==0,name
 return p.stdout
def sql(q,db='WeKnora'):
 p=subprocess.run(['docker','exec','-i','-u','postgres',pg,'psql','-X','-w','-U','postgres','-d',db,'-qAt','-v','ON_ERROR_STOP=1'],input=q.encode(),capture_output=True,timeout=60)
 assert p.returncode==0,'test SQL operation failed';return p.stdout.decode().strip()
def operator(mode,db,label):
 dsn='postgres://'+urllib.parse.quote(env.get('DB_USER','postgres'),safe='')+':'+urllib.parse.quote(env.get('DB_PASSWORD',''),safe='')+'@WeKnora-postgres:5432/'+db+'?sslmode=disable'
 path=private/('operator-'+label+'.env');path.write_text('FORMAL_TEST_AUTHORIZATION=P3_TEST_ONLY_C2\nFORMAL_OPERATION='+mode+'\nFORMAL_TEST_DSN='+dsn+'\nSYSTEM_AES_KEY='+env['SYSTEM_AES_KEY']+'\n')
 return run(label,['docker','run','--rm','--network','weknora_WeKnora-network','--env-file',str(path),'-v',str(backup/'operators')+':/ops:ro','--entrypoint','/ops/formal-operator',image])
def snapshot():
 prior=json.loads((base/'results/backup-a3/protected-row-snapshot.json').read_text());result={}
 for t in prior:
  if t=='schema':continue
  q="SELECT json_build_object('count',count(*),'fingerprint',md5(COALESCE(string_agg((to_jsonb(t)-'personal_scope_mode'-'legacy_access_state')::text,E'\\n' ORDER BY (to_jsonb(t)-'personal_scope_mode'-'legacy_access_state')::text),'')))FROM "+t+' t'
  result[t]=json.loads(sql(q))
 return result
def status(url):
 try:
  with urllib.request.urlopen(url,timeout=5)as r:return r.status
 except urllib.error.HTTPError as e:return e.code
def app_http(path):return run('app-http-'+str(len(rows)),['docker','exec',app,'curl','-s','--max-time','5','-o','/dev/null','-w','%{http_code}','http://127.0.0.1:8080'+path]).decode().strip()
def wait_app():
 for _ in range(90):
  p=subprocess.run(['docker','exec',app,'curl','-s','--max-time','2','-o','/dev/null','-w','%{http_code}','http://127.0.0.1:8080/health'],capture_output=True)
  if p.returncode==0 and p.stdout==b'200':return
  time.sleep(1)
 raise RuntimeError('new APP failed bounded readiness; never restart unsafe old APP')

gatepath=private/'entry-current.conf'
maintenance=False;upgraded=False
try:
 entry_inspect=json.loads(subprocess.check_output(['docker','inspect',entry]))[0]
 (private/'entry-inspect.json').write_text(json.dumps(entry_inspect))
 originalpath=private/'entry-original.conf'
 if not originalpath.exists()or originalpath.stat().st_size<100:
  # docker cp remounts deleted host bind paths and fails; exec reads the
  # existing mount namespace without exposing the private config in logs.
  capture=subprocess.run(['docker','exec',entry,'cat','/etc/nginx/nginx.conf'],capture_output=True,timeout=20)
  assert capture.returncode==0 and len(capture.stdout)>100,'live entry configuration unavailable'
  originalpath.write_bytes(capture.stdout)
 gate_original=originalpath.read_bytes();gatepath.write_bytes(gate_original)
 recovered={}
 for i,m in enumerate(a['Mounts']):
  if m['Type']=='bind'and not Path(m['Source']).exists():
   target=private/('recovered-app-bind-'+str(i)+'-'+attempt);assert not target.exists()
   packed=private/('recovered-app-bind-'+str(i)+'-'+attempt+'.tar')
   with packed.open('wb')as stream:
    capture=subprocess.run(['docker','exec',app,'tar','-C',m['Destination'],'-cf','-','.'],stdout=stream,stderr=subprocess.PIPE,timeout=60)
   assert capture.returncode==0,'live bind recovery failed'
   target.mkdir(mode=0o700)
   with tarfile.open(packed)as tf:
    for member in tf:
     resolved=(target/member.name).resolve();assert resolved==target or target in resolved.parents
     assert not member.issym()and not member.islnk(),'unsafe recovered ownership archive link'
    tf.extractall(target)
   recovered[m['Source']]=str(target)
 (out/'recovered-bind-metadata.json').write_text(json.dumps({'missing_host_bind_paths_recovered':list(recovered),'entry_config_recovered_from_running_container':True,'private_contents_exported':False},indent=2))
 assert sql('SELECT version||\':\'||dirty FROM schema_migrations')=='3033:false'
 clone_state=sql('SELECT version||\':\'||dirty FROM schema_migrations',state['clone_database'])
 if clone_state!='3033:false':
  # Preserve the real failed clone, including its dirty record. Restore the
  # verified original backup into a NEW DB; no Force, down or table deletion.
  fresh='knowhub_formal_restore_'+base.name.replace('-','_')+'_'+attempt
  assert re.fullmatch(r'knowhub_formal_restore_[a-z0-9_]+',fresh)
  sql('CREATE DATABASE '+fresh+' TEMPLATE template0','postgres')
  with(private/'test-before-3033.dump').open('rb')as stream:
   p=subprocess.run(['docker','exec','-i','-u','postgres',pg,'pg_restore','-U','postgres','-d',fresh],stdin=stream,capture_output=True,timeout=300)
  assert p.returncode==0,'fresh restoration failed'
  state['failed_clone_database_retained']=state['clone_database'];state['clone_database']=fresh
 operator('verify-keys',state['clone_database'],'restored-AES-verify')
 operator('migrate',state['clone_database'],'clone-formal-3033-3037')
 operator('verify-keys',state['clone_database'],'migrated-clone-AES-verify')
 # Reconstruct only the two authorized services from actual running state.
 spec={'name':'knowhub-mvp-p3','services':{},'networks':{},'volumes':{}}
 def service(x,role,ref):
  s={'image':ref,'container_name':x['Name'].lstrip('/'),'environment':dict(v.split('=',1)for v in x['Config']['Env']if'='in v),'networks':{},'volumes':[],'restart':x['HostConfig']['RestartPolicy'].get('Name','no')or'no'}
  for k,key in [('entrypoint','Entrypoint'),('command','Cmd'),('working_dir','WorkingDir'),('user','User')]:
   if x['Config'].get(key):s[k]=x['Config'][key]
  for name,n in x['NetworkSettings']['Networks'].items():
   key='n'+str(list(spec['networks']).index(name))if name in spec['networks']else'n'+str(len(spec['networks']))
   # Map actual names to simple Compose keys without changing any network.
   existing=next((k for k,v in spec['networks'].items()if v['name']==name),None);key=existing or'n'+str(len(spec['networks']))
   extra=['p3app']if role=='app'else(['p3frontend']if role=='frontend'else[])
   spec['networks'][key]={'external':True,'name':name};s['networks'][key]={'aliases':list(dict.fromkeys((n.get('Aliases')or[])+extra))}
  for m in x['Mounts']:
   if m['Type']=='volume':
    key='v'+str(len(spec['volumes']));spec['volumes'][key]={'external':True,'name':m['Name']};src=key
   elif m['Type']=='bind':src=recovered.get(m['Source'],m['Source'])
   else:raise RuntimeError('unknown mount kind')
   s['volumes'].append(src+':'+m['Destination']+(''if m['RW']else':ro'))
  return s
 spec['services']['app']=service(a,'app',image);spec['services']['frontend']=service(f,'frontend',frontimage)
 entry_service=service(entry_inspect,'entry',entry_inspect['Image'])
 assert len(entry_inspect['Mounts'])==1 and entry_inspect['Mounts'][0]['Destination']=='/etc/nginx/nginx.conf'
 entry_service['volumes']=[str(gatepath)+':/etc/nginx/nginx.conf:ro']
 entry_service['ports']=[v.get('HostIp','0.0.0.0')+':'+v['HostPort']+':'+port.split('/')[0]for port,bindings in entry_inspect['HostConfig']['PortBindings'].items()for v in(bindings or[])]
 entry_spec={'name':'knowhub-company-entry','services':{'entry':entry_service},'networks':spec['networks'],'volumes':spec['volumes']}
 entry_composepath=private/'entry-upgrade.json';entry_composepath.write_text(json.dumps(entry_spec,indent=2))
 entry_compose=['docker','compose','--project-name','knowhub-company-entry','--project-directory',str(private),'-f',str(entry_composepath)]
 config=(candidate/'source/config/config.yaml').read_text();assert 'personal_mcp:\n  enabled: false'in config
 configpath=private/'runtime-config.yaml';configpath.write_text(config)
 ae=spec['services']['app']['environment'];ae.update({'AUTO_MIGRATE':'false','AUTO_RECOVER_DIRTY':'false','MCP_PUBLIC_URL':'http://172.30.232.42','FEISHU_LEGACY_SYNC_SUSPENDED':'true','FEISHU_SAFE_INCREMENTAL_ENABLED':'true','DUCKDB_SKIP_EXTENSION_LOAD':'true'})
 spec['services']['app']['volumes'].append(str(configpath)+':/app/config/config.yaml:ro')
 spec['services']['frontend']['environment'].update({'APP_HOST':'p3app','APP_PORT':'8080','APP_SCHEME':'http'})
 composepath=private/'compose-upgrade.json';composepath.write_text(json.dumps(spec,indent=2))
 compose=['docker','compose','--project-name','knowhub-mvp-p3','--project-directory',str(private),'-f',str(composepath)]
 run('compose-validate',compose+['config','--quiet'])
 modified=gate_original.replace(b'proxy_pass http://$upstream;',b'return 503;')
 assert modified!=gate_original,'unknown entry routing; do not guess'
 gatepath.write_bytes(modified)
 run('entry-compose-validate',entry_compose+['config','--quiet'])
 run('maintenance-enable',entry_compose+['up','-d','--no-deps','--force-recreate','entry'],timeout=120);maintenance=True
 run('maintenance-nginx-check',['docker','exec',entry,'nginx','-t'])
 time.sleep(1);assert status('http://127.0.0.1/api/v1/mcp/personal')==503
 run('stop-old-APP-and-workers',['docker','stop','--time','60',app],timeout=90)
 for _ in range(30):
  n=sql("SELECT count(*)FROM pg_stat_activity WHERE datname='WeKnora'AND pid<>pg_backend_pid()AND backend_type='client backend'")
  if n=='0':break
  time.sleep(1)
 else:raise RuntimeError('unexpected test database clients remain; migration held')
 # A second quiescent snapshot closes the gap between online backup and stop.
 dump=private/'quiescent-test-3033.dump'
 with dump.open('wb')as stream:p=subprocess.run(['docker','exec','-u','postgres',pg,'pg_dump','-U','postgres','-Fc','WeKnora'],stdout=stream,stderr=subprocess.PIPE,timeout=300)
 assert p.returncode==0;before=snapshot()
 operator('migrate','WeKnora','actual-formal-3033-3037');upgraded=True
 assert sql('SELECT version||\':\'||dirty FROM schema_migrations')=='3037:false'
 assert before==snapshot(),'migration changed protected original rows'
 operator('verify-keys','WeKnora','actual-AES-verify')
 run('start-candidate-disabled',compose+['up','-d','--no-deps','app','frontend'],timeout=240);wait_app()
 assert app_http('/api/v1/mcp/personal')=='403'
 binary=run('actual-candidate-binary',['docker','exec',app,'sha256sum','/app/WeKnora']).decode().split()[0];assert binary==expected_binary
 enabled=config.replace('personal_mcp:\n  enabled: false','personal_mcp:\n  enabled: true',1).replace('  policy_reference: ""','  policy_reference: "P3_TEST_C2_USER_069a9f12"',1).replace('  exceptions_enabled: false','  exceptions_enabled: true',1)
 assert '  review_enabled: false'in enabled;configpath.write_text(enabled)
 run('enable-personal-MCP',compose+['up','-d','--no-deps','--force-recreate','app','frontend'],timeout=240);wait_app()
 assert app_http('/api/v1/mcp/personal')=='401'
 run('frontend-nginx-check',['docker','exec',front,'nginx','-t'])
 gatepath.write_bytes(gate_original);run('restore-entry-nginx-check',['docker','exec',entry,'nginx','-t']);run('maintenance-end',['docker','exec',entry,'nginx','-s','reload']);maintenance=False
 for _ in range(40):
  try:
   if status('http://127.0.0.1/health')==200 and status('http://127.0.0.1/api/v1/mcp/personal')==401:break
  except OSError:pass
  time.sleep(1)
 else:raise RuntimeError('shared entry not ready')
 assert before==snapshot(),'startup modified original protected rows'
 py=json.loads(subprocess.check_output(['docker','inspect','WeKnora-mcp']))[0]
 current=json.loads(subprocess.check_output(['docker','inspect',app,front]))
 result={'exit_code':0,'candidate_sha256':sha,'schema':3037,'dirty':False,'url':'http://172.30.232.42','ports':[80,8080],'new_MCP_port':False,'actual_binary_sha256':binary,'image_ids':[x['Image']for x in current],'original_rows_unchanged':True,'python_container_id':py['Id'],'python_image_id':py['Image'],'python_running':py['State']['Running'],'backup_path':str(backup),'maintenance':False,'production_accessed':False,'formal_model':'BLOCKED_CREDENTIAL_APPROVAL','real_SID':'NOT_REPEATED_EXISTING_EVIDENCE','real_Feishu':'BLOCKED_TEST_APPLICATION_APPROVAL'}
 state['upgrade_compose_argv']=compose;state['entry_compose_argv']=entry_compose;state['runtime_config']=str(configpath);state['shared_origin']='http://172.30.232.42';(base/'private/state.json').write_text(json.dumps(state,indent=2))
 (out/'protected-row-after.json').write_text(json.dumps(before,indent=2))
except Exception as e:
 result={'exit_code':1,'error_class':type(e).__name__,'message':str(e),'schema_upgraded':upgraded,'maintenance':maintenance,'unsafe_old_APP_not_restarted':True,'production_accessed':False}
 # On failure remain closed rather than rolling back to unsafe old Reader.
 if maintenance:(out/'operator-action-required.txt').write_text('P3 remains in maintenance; never start unsafe RC3. Use proven hardened fallback and retained current schema/denials.\n')
(out/'result.json').write_text(json.dumps(result,indent=2));print(json.dumps(result));sys.exit(result['exit_code'])
