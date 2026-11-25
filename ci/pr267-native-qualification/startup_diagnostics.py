"""Export bounded startup classifications, never complete container logs or Env."""
import hashlib,json,os,pathlib,re,subprocess
root=pathlib.Path(os.environ['QUALIFICATION_ROOT']);project='knowhub-pr267-'+os.environ['GITHUB_RUN_ID'];out=pathlib.Path(os.environ['GITHUB_WORKSPACE'])/'evidence';out.mkdir(exist_ok=True)
report={'source_sha':os.environ['SOURCE_SHA'],'scope':'disposable_own_project','raw_logs_exported':False}
try:
 if not (root/'compose.json').exists():raise FileNotFoundError('stack_not_created')
 c=['docker','compose','-p',project,'-f',str(root/'compose.json')]
 ids=subprocess.check_output(c+['ps','-aq'],text=True).split();rows=json.loads(subprocess.check_output(['docker','inspect',*ids],text=True)) if ids else []
 assert all(x['Config']['Labels']['com.docker.compose.project']==project for x in rows)
 report['runtime']=[{'service':x['Config']['Labels']['com.docker.compose.service'],'running':x['State']['Running'],'health':x['State'].get('Health',{}).get('Status'),'exit_code':x['State']['ExitCode'],'oom':x['State']['OOMKilled'],'restarts':x['RestartCount']} for x in rows]
 for name in ['approval.pub','config.yaml']:
  if (root/name).exists():
   stat=(root/name).stat();report[name+'_permissions']={'uid':stat.st_uid,'mode':oct(stat.st_mode&0o777)}
 app=next((x for x in rows if x['Config']['Labels']['com.docker.compose.service']=='app'),None)
 if app:
  logs=subprocess.run(['docker','logs','--tail','300',app['Id']],capture_output=True,timeout=15);text=(logs.stdout+logs.stderr).decode(errors='replace')[-1000000:]
  classes=[]
  for label,terms in {'recovery_manager_configuration_rejected':['recovery approval configuration invalid'],'invalid_feishu_safety_configuration':['invalid_feishu_safety_configuration'],'approved_lane_startup_refused':['approved sync lane','panic'],'filesystem_permission_denied':['permission denied'],'database_authentication_failed':['password authentication failed'],'native_dictionary_asset_error':['jieba','no such file'],'memory_allocation_failure':['out of memory']}.items():
   if all(v.lower() in text.lower() for v in terms):classes.append(label)
  report['startup_classes']=classes
  report['panic_present']='panic:' in text
  report['stack_source_frames']=re.findall(r'github.com/Tencent/WeKnora/[A-Za-z0-9_./*(){}-]+',text)[-12:]
  panic=next((line.split('panic:',1)[1].strip() for line in text.splitlines() if 'panic:' in line),'')
  if panic:report['panic_sha256']=hashlib.sha256(panic.encode()).hexdigest()
  # Only source/runtime error classes are exported; no free-form error body.
except Exception as e:report['collector_class']=type(e).__name__
(out/'startup-diagnostics.json').write_text(json.dumps(report,indent=2));print(json.dumps({'startup_classification_saved':True,'raw_logs_exported':False}))
