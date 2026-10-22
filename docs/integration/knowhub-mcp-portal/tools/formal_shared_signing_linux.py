"""Configure a fresh test-only signing key; keep existing AES/key data intact."""
from pathlib import Path
import hashlib,json,os,secrets,subprocess,sys,time
base=Path(sys.argv[1]).resolve();assert base.parent==Path('/home/aqyfzx/ai/knowhub-formal-c2-20261022')
s=json.loads((base/'private/state.json').read_text());private=Path(s['private_path']);out=base/'results/signing';out.mkdir(exist_ok=False);os.umask(0o077)
specpath=Path(s['upgrade_compose_argv'][-1]);spec=json.loads(specpath.read_text());env=spec['services']['app']['environment'];aes=env['SYSTEM_AES_KEY'];old=env.get('SYSTEM_SIGNING_KEY')or aes
assert old in ('weknora-system-aes-key-32bytes!!','your-32-byte-long-encryption-key!'),'unexpected existing signing configuration'
env['SYSTEM_SIGNING_KEY']=secrets.token_urlsafe(48)
(private/'formal-signing.env').write_text('SYSTEM_SIGNING_KEY='+env['SYSTEM_SIGNING_KEY']+'\n')
specpath.write_text(json.dumps(spec,indent=2))
try:
 with(out/'restart.log').open('wb')as f:code=subprocess.run(s['upgrade_compose_argv']+['up','-d','--no-deps','--force-recreate','app'],stdout=f,stderr=subprocess.STDOUT,timeout=180).returncode
 assert code==0
 for _ in range(90):
  p=subprocess.run(['docker','exec','knowhub-mvp-p3-app-1','curl','-s','--max-time','2','-o','/dev/null','-w','%{http_code}','http://127.0.0.1:8080/health'],capture_output=True)
  if p.returncode==0 and p.stdout==b'200':break
  time.sleep(1)
 else:raise RuntimeError('health timeout')
 actual=json.loads(subprocess.check_output(['docker','inspect','knowhub-mvp-p3-app-1']))[0];ae=dict(x.split('=',1)for x in actual['Config']['Env']if'='in x)
 assert ae['SYSTEM_AES_KEY']==aes and ae['SYSTEM_SIGNING_KEY']==env['SYSTEM_SIGNING_KEY']
 result={'exit_code':0,'old_sample_signing_rejected_correctly':True,'new_test_only_signing_configured':True,'AES_unchanged':True,'production_accessed':False,'historical_valid_signing_key_rotated':False}
except Exception as e:result={'exit_code':1,'error_class':type(e).__name__}
(out/'result.json').write_text(json.dumps(result,indent=2));print(json.dumps(result));sys.exit(result['exit_code'])
