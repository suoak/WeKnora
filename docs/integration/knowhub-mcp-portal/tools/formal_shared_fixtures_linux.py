"""Add synthetic fixtures to approved P3, without resetting existing users."""
from pathlib import Path
import hashlib,json,os,secrets,subprocess,sys,time,urllib.parse
base=Path(sys.argv[1]).resolve();assert base.parent==Path('/home/aqyfzx/ai/knowhub-formal-c2-20261022')
state=json.loads((base/'private/state.json').read_text());private=Path(state['private_path']);candidate=Path(state['candidate']);backup=Path(state['backup_path'])
assert state.get('upgrade_compose_argv'),'successful upgrade required'
out=base/'results/fixtures';out.mkdir(exist_ok=False);rows=[];os.umask(0o077)
env=dict(v.split('=',1)for v in(private/'app.env').read_text().splitlines()if'='in v)
image=json.loads((candidate/'results/build/done.json').read_text())['app']
password=secrets.token_urlsafe(32)
dsn='postgres://'+urllib.parse.quote(env.get('DB_USER','postgres'),safe='')+':'+urllib.parse.quote(env.get('DB_PASSWORD',''),safe='')+'@WeKnora-postgres:5432/WeKnora?sslmode=disable'
seedenv=private/'formal-fixture.env';seedenv.write_text('FORMAL_TEST_AUTHORIZATION=P3_TEST_ONLY_C2\nFORMAL_SEED_MODE=initial\nFORMAL_TEST_DSN='+dsn+'\nFORMAL_TEST_PASSWORD='+password+'\nSYSTEM_AES_KEY='+env['SYSTEM_AES_KEY']+'\n')
def run(name,args,timeout=240):
 p=subprocess.run(args,capture_output=True,timeout=timeout);raw=p.stdout+p.stderr
 for key in [password,env.get('DB_PASSWORD',''),env.get('SYSTEM_AES_KEY','')]:
  if key:raw=raw.replace(key.encode(),b'[REDACTED]')
 (out/(name+'.log')).write_bytes(raw);rows.append({'name':name,'argv':args,'exit_code':p.returncode});(out/'commands.json').write_text(json.dumps(rows,indent=2));assert p.returncode==0,name
 return p.stdout
def sql(q):
 p=subprocess.run(['docker','exec','-u','postgres','WeKnora-postgres','psql','-X','-U','postgres','-d','WeKnora','-qAtc',q],capture_output=True,timeout=40)
 assert p.returncode==0,'fixture metadata query failed'
 return p.stdout.decode().strip()
try:
 assert sql("SELECT count(*)FROM users WHERE id LIKE 'fm-%'")=='0'
 assert sql("SELECT count(*)FROM data_sources WHERE deleted_at IS NULL AND type IN('feishu','feishu_drive')")=='0','do not unsuspend any pre-existing source'
 fixtures=[]
 for kind,filename in [('model','v1_model_fixture.py'),('feishu','joint_feishu_fixture.py')]:
  name='kh-formal-'+kind+'-'+base.name
  run('start-synthetic-'+kind,['docker','run','-d','--name',name,'--label','knowhub.formal=synthetic-test-dependency','--network','weknora_WeKnora-network','--network-alias','formal-'+kind,'-v',str(candidate/'source/docs/integration/knowhub-mcp-portal/tools'/filename)+':/fixture.py:ro','--entrypoint','python','weknora-custom/mcp-candidate:v0.8.0-knowhub.24-rc1-854f3b95','/fixture.py'])
  fixtures.append(name)
  inspect=json.loads(subprocess.check_output(['docker','inspect',name]))[0];assert not inspect['HostConfig']['PortBindings']
 specpath=Path(state['upgrade_compose_argv'][-1]);spec=json.loads(specpath.read_text());ae=spec['services']['app']['environment']
 ae['SSRF_WHITELIST']=','.join(v for v in [ae.get('SSRF_WHITELIST',''),'formal-model','formal-feishu']if v)
 ae.update({'FEISHU_LEGACY_SYNC_SUSPENDED':'false','FEISHU_SAFE_INCREMENTAL_ENABLED':'true','FEISHU_DOCX_PARSE_MODE':'blocks','FEISHU_API_RETRY_MAX':'0'})
 specpath.write_text(json.dumps(spec,indent=2))
 run('test-only-helper-configuration',state['upgrade_compose_argv']+['up','-d','--no-deps','--force-recreate','app','frontend'])
 for _ in range(90):
  p=subprocess.run(['docker','exec','knowhub-mvp-p3-app-1','curl','-s','--max-time','2','-o','/dev/null','-w','%{http_code}','http://127.0.0.1:8080/health'],capture_output=True)
  if p.returncode==0 and p.stdout==b'200':break
  time.sleep(1)
 else:raise RuntimeError('APP did not restart')
 run('seed-only-fm-namespace',['docker','run','--rm','--network','weknora_WeKnora-network','--env-file',str(seedenv),'-v',str(backup/'operators')+':/ops:ro','--entrypoint','/ops/formal-seed',image])
 state['synthetic_helper_containers']=fixtures;state['fixture_env']=str(seedenv);(base/'private/state.json').write_text(json.dumps(state,indent=2))
 result={'exit_code':0,'synthetic_only':True,'APP_image_unchanged':True,'new_APP_or_port':False,'new_user_prefix':'fm-','existing_users_passwords_not_reset':True,'existing_private_scopes_not_expanded':True,'fixture_tenants':[820001,820002,820007,820099,820198],'formal_model':'BLOCKED_CREDENTIAL_APPROVAL','real_Feishu':'BLOCKED_TEST_APP_APPROVAL','production_accessed':False}
except Exception as e:result={'exit_code':1,'error_class':type(e).__name__,'message':str(e),'production_accessed':False}
(out/'result.json').write_text(json.dumps(result,indent=2));print(json.dumps(result));sys.exit(result['exit_code'])
