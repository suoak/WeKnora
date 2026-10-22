"""Upgrade existing P3 to the same-Go, theme-token-only revised candidate."""
from pathlib import Path
import hashlib,json,os,subprocess,sys,time,urllib.request
base=Path(sys.argv[1]).resolve();assert base.parent==Path('/home/aqyfzx/ai/knowhub-formal-c2-20261022')
s=json.loads((base/'private/state.json').read_text());private=Path(s['private_path']);c=json.loads((base/'formal-promote-current.json').read_text());new=Path(c['remote']);m=json.loads((new/'manifest.json').read_text());old=Path(s['candidate']);om=json.loads((old/'manifest.json').read_text());out=base/'results/promote';out.mkdir(exist_ok=False);rows=[]
assert om['candidate_sha256']=='0a7374bbbdbd8c4a802733811ad3ecbeb70293ba33b89eefa9bd05c36ecbd371'
assert m['minimal_frontend_delta']=={'frontend/src/views/integrations/MCPAccessKeys.vue':{'before':om['files']['frontend/src/views/integrations/MCPAccessKeys.vue'],'after':'614cdec90ac79da12636a21825b84515727905484404c2f275acb9333c7e9dbc'}}
assert all(om['files'].get(n)==h for n,h in m['files'].items()if not n.startswith('docs/')and n!='frontend/src/views/integrations/MCPAccessKeys.vue')
assert json.loads((new/'results/build/done.json').read_text())['all_passed']
images=json.loads((new/'results/build/images.json').read_text());build=json.loads((new/'results/build/done.json').read_text())
def run(name,args,timeout=240):
 with(out/(name+'.log')).open('wb')as f:code=subprocess.run(args,stdout=f,stderr=subprocess.STDOUT,timeout=timeout).returncode
 rows.append({'name':name,'argv':args,'exit_code':code});(out/'commands.json').write_text(json.dumps(rows,indent=2));assert code==0,name
def sql(q):return subprocess.check_output(['docker','exec','-u','postgres','WeKnora-postgres','psql','-X','-U','postgres','-d','WeKnora','-qAtc',q]).decode().strip()
try:
 assert sql('SELECT version||\':\'||dirty FROM schema_migrations')=='3037:false'
 assert sql("SELECT legacy_access_state FROM data_sources WHERE id=(SELECT id FROM data_sources WHERE tenant_id=820099 AND name='Formal synthetic drive'AND deleted_at IS NOT NULL ORDER BY created_at DESC LIMIT 1)")=='revoked'
 args=s['upgrade_compose_argv'];specpath=Path(args[-1]);spec=json.loads(specpath.read_text());(private/'before-theme-promotion.compose.json').write_text(json.dumps(spec,indent=2))
 assert spec['services']['app']['image'].endswith(':0a7374bbbdbd8c4a')
 run('stop-current-test-APP-Worker',args+['stop','--timeout','20','app'])
 backup=private/'before-theme-promotion-3037.dump'
 with backup.open('wb')as f:code=subprocess.run(['docker','exec','-u','postgres','WeKnora-postgres','pg_dump','-U','postgres','-Fc','WeKnora'],stdout=f,stderr=subprocess.PIPE).returncode
 assert code==0;os.chmod(backup,0o600)
 spec['services']['app']['image']=build['app'];spec['services']['frontend']['image']=build['frontend'];specpath.write_text(json.dumps(spec,indent=2))
 run('promote-same-source-APP-frontend',args+['up','-d','--no-deps','--force-recreate','app','frontend'])
 for _ in range(120):
  p=subprocess.run(['docker','exec','knowhub-mvp-p3-app-1','curl','-s','--max-time','2','-o','/dev/null','-w','%{http_code}','http://127.0.0.1:8080/health'],capture_output=True)
  if p.returncode==0 and p.stdout==b'200':break
  time.sleep(1)
 else:raise AssertionError('APP health timeout')
 actual=json.loads(subprocess.check_output(['docker','inspect','knowhub-mvp-p3-app-1','knowhub-mvp-p3-frontend-1']))
 assert [v['Image']for v in actual]==[v['id']for v in images]
 binary=subprocess.check_output(['docker','exec','knowhub-mvp-p3-app-1','sha256sum','/app/WeKnora']).decode().split()[0];assert binary==hashlib.sha256((new/'artifacts/WeKnora').read_bytes()).hexdigest()
 assert sql('SELECT version||\':\'||dirty FROM schema_migrations')=='3037:false'
 for _ in range(30):
  try:
   with urllib.request.urlopen('http://127.0.0.1/health',timeout=3)as response:
    if response.status==200:break
  except Exception:pass
  time.sleep(1)
 else:raise AssertionError('80 entry readiness failed')
 py=json.loads(subprocess.check_output(['docker','inspect','WeKnora-mcp']))[0];assert py['Id']=='590102ce51aab2d5950e42965202c44dbb407ef3e1e2f6675a10397bd5e224e0'and py['State']['Running']
 s['prior_deployed_candidate']=str(old);s['candidate']=str(new);(base/'private/state.json').write_text(json.dumps(s,indent=2))
 result={'exit_code':0,'candidate_sha256':m['candidate_sha256'],'runtime_source_sha256':m['runtime_source_sha256'],'url':'http://172.30.232.42','schema':3037,'image_ids':[v['Image']for v in actual],'actual_binary_sha256':binary,'Go_and_migrations_unchanged':True,'pre_upgrade_dump_sha256':hashlib.sha256(backup.read_bytes()).hexdigest(),'original_python_preserved':True,'production_accessed':False}
except Exception as e:result={'exit_code':1,'error_class':type(e).__name__,'message':str(e),'production_accessed':False,'unsafe_rollback_executed':False}
(out/'result.json').write_text(json.dumps(result,indent=2));print(json.dumps(result));sys.exit(result['exit_code'])
