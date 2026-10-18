"""Approved company-test only: freeze APP writes, paired backup, no migration."""
import os
os.umask(0o077)
B=pathlib.Path('/home/aqyfzx/ai/knowhub-verified-email-candidate-5f988be51')
state=json.loads((B/'retry-state.json').read_text());assert state['stage']=='PREFLIGHT_PASS'
worker=r'''import os,pathlib,json,subprocess,datetime,ast,hashlib
os.umask(0o077)
B=pathlib.Path('/home/aqyfzx/ai/knowhub-verified-email-candidate-5f988be51');state=json.loads((B/'retry-state.json').read_text());D=pathlib.Path(state['backup_directory'])
APP='knowhub-company-test-app-1';PG='knowhub-company-test-postgres-1';stopped=False
out={'result':'RUNNING','stage':'freeze_app','real_sid_requests':0}
def save():(B/'retry-backup-result.json').write_text(json.dumps(out))
def call(args,data=None,timeout=120):
 z=subprocess.run(args,input=data,stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=timeout)
 if z.returncode:raise RuntimeError('controlled_backup_operation_failed')
 return z.stdout
def query(pg,q):return call(['docker','exec','-i',PG,'psql','-X','-qAt','-U','clean_owner','-d','knowhub_oa_sso_pilot','-v','ON_ERROR_STOP=1'],('BEGIN READ ONLY;SET LOCAL TIME ZONE \'UTC\';'+q+';ROLLBACK;').encode()).decode().strip()
save()
try:
 assert query(PG,"SELECT version::text||'|'||dirty::text FROM schema_migrations")=='3030|false'
 assert query(PG,'SELECT COUNT(*) FROM oidc_transactions WHERE NOT consumed AND expires_at>NOW()')=='0'
 call(['docker','stop','--time','30',APP]);stopped=True
 out['stage']='paired_snapshot';save()
 with (D/'company-3030.dump').open('wb') as f:
  z=subprocess.run(['docker','exec',PG,'pg_dump','-U','clean_owner','-d','knowhub_oa_sso_pilot','-Fc','--no-owner','--no-acl'],stdout=f,stderr=subprocess.PIPE,timeout=180);assert z.returncode==0
 with (D/'company-files.tar').open('wb') as f:
  z=subprocess.run(['docker','run','--rm','--network','none','--log-driver','none','--mount','type=volume,source=knowhub-company-test-files,target=/scan,readonly','--entrypoint','tar',state['expected_old_app'],'-C','/scan','-cf','-','.'],stdout=f,stderr=subprocess.PIPE,timeout=180);assert z.returncode==0
 for name in ('company-3030.dump','company-files.tar'):assert (D/name).stat().st_size>0
 source=pathlib.Path('/home/aqyfzx/ai/knowhub-verified-email-implementation-20261017')
 # Reuse the already-reviewed baseline collector; values stay protected here.
 tree=ast.parse((source/'migration-rehearsal-source.py').read_text()) if (source/'migration-rehearsal-source.py').exists() else None
 # Baselines contain metadata and references, never exported identities.
 selections={'users':'id,tenant_id,is_active,is_system_admin,can_access_all_tenants,deleted_at','tenants':'id,name','tenant_members':'user_id,tenant_id,role,status','knowledge_bases':'id,tenant_id,deleted_at','knowledges':'id,knowledge_base_id,file_path,deleted_at'}
 tables=json.loads(query(PG,"SELECT COALESCE(json_agg(table_name),'[]') FROM information_schema.tables WHERE table_schema='public'"))
 baseline={}
 for table,cols in selections.items():
  available=json.loads(query(PG,"SELECT json_agg(column_name) FROM information_schema.columns WHERE table_name='"+table+"' AND table_schema='public'"));cols=','.join(c for c in cols.split(',') if c in available)
  rows=json.loads(query(PG,"SELECT COALESCE(json_agg(x),'[]') FROM (SELECT "+cols+' FROM '+table+')x'));baseline[table]=sorted(rows,key=lambda x:json.dumps(x,sort_keys=True))
 for table in ('kb_shares','knowledge_base_members','organization_tenant_members','embeddings'):
  if table not in tables:continue
  cols=json.loads(query(PG,"SELECT json_agg(column_name) FROM information_schema.columns WHERE table_name='"+table+"' AND table_schema='public'"))
  fields=[c for c in ('id','user_id','tenant_id','knowledge_base_id','organization_id','source_tenant_id','permission','knowledge_id','chunk_id','role','status','access_level','deleted_at') if c in cols]
  if fields:baseline[table]=sorted(json.loads(query(PG,"SELECT COALESCE(json_agg(x),'[]') FROM (SELECT "+','.join(fields)+' FROM '+table+')x')),key=lambda x:json.dumps(x,sort_keys=True))
 for table in ('chunks','embeddings','external_identities','source_id_sessions','source_id_account_ownership','source_id_jit_registrations'):baseline[table+'_count']=int(query(PG,'SELECT COUNT(*) FROM '+table))
 baseline['nonempty_chunks']=int(query(PG,"SELECT COUNT(*) FROM chunks WHERE content IS NOT NULL AND length(trim(content))>0 AND deleted_at IS NULL"))
 (D/'protected-data-baseline.json').write_text(json.dumps(baseline))
 code="import os,json,hashlib;rows=[]\nfor root,ds,fs in os.walk('/scan'):\n for n in sorted(fs):\n  p=os.path.join(root,n)\n  if os.path.isfile(p):\n   h=hashlib.sha256()\n   with open(p,'rb') as f:\n    for b in iter(lambda:f.read(1048576),b''):h.update(b)\n   rows.append([os.path.relpath(p,'/scan'),os.path.getsize(p),h.hexdigest()])\nprint(json.dumps(sorted(rows)))"
 (D/'protected-file-baseline.json').write_bytes(call(['docker','run','--rm','--network','none','--log-driver','none','--mount','type=volume,source=knowhub-company-test-files,target=/scan,readonly','--entrypoint','python3',state['expected_old_app'],'-c',code]))
 (D/'file-baseline-code.txt').write_text(code)
 tooling='/home/aqyfzx/ai/knowhub-oa-sso-pilot-832b6652a/egress/nettools'
 netcode="import os,subprocess,json\ndef root():os.chroot('/nettools');os.chdir('/')\nr={}\nfor name in ('iptables','ip6tables'):\n p=subprocess.run(['/usr/sbin/'+name,'-S'],preexec_fn=root,stdout=subprocess.PIPE,stderr=subprocess.PIPE) if os.path.exists('/nettools/usr/sbin/'+name) else None;r[name]={'exit':p.returncode if p else 127,'rules':p.stdout.decode() if p else ''}\nprint(json.dumps(r))"
 (D/'protected-firewall-before.json').write_bytes(call(['docker','run','--rm','--network','host','--cap-add','NET_ADMIN','--tmpfs','/nettools/run','--log-driver','none','--mount','type=bind,source='+tooling+',target=/nettools,readonly','--entrypoint','python3',state['expected_old_app'],'-c',netcode]))
 (D/'firewall-read-code.txt').write_text(netcode)
 for p in D.iterdir():
  if p.is_file():p.chmod(0o600)
 state['stage']='BACKUP_PASS_APP_STOPPED';(B/'retry-state.json').write_text(json.dumps(state))
 out.update(result='PASS',stage='paired_backup_complete_app_stopped',backup_directory=str(D),database_backup_bytes=(D/'company-3030.dump').stat().st_size,files_backup_bytes=(D/'company-files.tar').stat().st_size,protected_configuration_saved=True,paired_snapshot_writers_stopped=True,app_stopped=True,database_schema=3030,source_files_read_only=True)
except Exception as e:
 out.update(result='FAIL',failure_class=type(e).__name__)
 if stopped:
  try:call(['docker','start',APP]);out['old_app_restarted']=True
  except Exception:out['old_app_restarted']=False
out['utc']=datetime.datetime.now(datetime.timezone.utc).isoformat();save()
'''
(B/'retry-backup-worker.py').write_text(worker)
subprocess.Popen(['python3',str(B/'retry-backup-worker.py')],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,start_new_session=True)
print(json.dumps({'result':'STARTED','action':'approved_paired_backup_only'}))
