"""Runtime qualification and one bounded fault in our own disposable APP only."""
import datetime,json,os,pathlib,socket,subprocess,time,uuid,zipfile,hashlib,base64
ROOT=pathlib.Path(os.environ['QUALIFICATION_ROOT']);SHA=os.environ['SOURCE_SHA'];assert len(SHA)==40 and all(c in '0123456789abcdef' for c in SHA)
PROJECT='knowhub-auto-native-'+os.environ['GITHUB_RUN_ID'];assert os.environ.get('GITHUB_ACTIONS')=='true' and os.environ['SOURCE_SHA']==SHA
PHASE='runtime'
PREFIX='feishu:safety:{automatic-native-'+os.environ['GITHUB_RUN_ID']+'}:'
C=['docker','compose','-p',PROJECT,'-f',str(ROOT/'compose.json')]
OUT=pathlib.Path(os.environ['GITHUB_WORKSPACE'])/'evidence';OUT.mkdir(exist_ok=True)
class Redis:
 def __init__(self):
  env=dict(x.split('=',1) for x in (ROOT/'app.env').read_text().splitlines() if '=' in x)
  code=pathlib.Path(__file__).with_name('redis_bridge.py').read_text()
  self.process=subprocess.Popen(C+['exec','-T','--user','1000:1000','app','python3','-u','-c',code],stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
  app=json.loads(subprocess.check_output(['docker','inspect',subprocess.check_output(C+['ps','-q','app'],text=True).strip()],text=True))[0]
  assert app['Config']['Labels']['com.docker.compose.project']==PROJECT
  self.process.stdin.write((json.dumps({'password':env['REDIS_PASSWORD'],'prefix':PREFIX,'worker':app['Config']['Hostname']+':1'})+'\n').encode());self.process.stdin.flush()
  assert json.loads(self.process.stdout.readline())=={'ready':True}
 def cmd(self,name,*args):
  assert name in ['GET','HGETALL','ZRANGE','PTTL','TIME','EXISTS','XLEN','ZSCORE','XRANGE','BEGIN_OWN_METADATA_GAP','END_OWN_METADATA_GAP']
  self.process.stdin.write((json.dumps([name,*args])+'\n').encode());self.process.stdin.flush()
  def decode(value):
   if isinstance(value,dict) and set(value)=={'base64_bytes'}:return base64.b64decode(value['base64_bytes'])
   if isinstance(value,list):return [decode(x) for x in value]
   return value
  return decode(json.loads(self.process.stdout.readline()))
 def close(self):
  self.process.stdin.close();assert self.process.wait(timeout=5)==0

def fields(raw):
 def vint(i):
  v=0;shift=0
  while True:
   b=raw[i];i+=1;v|=(b&127)<<shift
   if not b&128:return v,i
   shift+=7;assert shift<70
 out={};i=0
 while i<len(raw):
  tag,i=vint(i);n,w=tag>>3,tag&7
  if w==0:v,i=vint(i)
  elif w==2:
   size,i=vint(i);v=raw[i:i+size];i+=size
  elif w==1:v=raw[i:i+8];i+=8
  elif w==5:v=raw[i:i+4];i+=4
  else:raise ValueError('unknown_protobuf_wire_type')
  out.setdefault(n,[]).append(v)
 return out

def runtime():
 ids=subprocess.check_output(C+['ps','-aq'],text=True).split();rows=json.loads(subprocess.check_output(['docker','inspect',*ids],text=True))
 return [{'service':r['Config']['Labels']['com.docker.compose.service'],'image_id':r['Image'],'running':r['State']['Running'],'health':r['State'].get('Health',{}).get('Status'),'restarts':r['RestartCount'],'oom':r['State']['OOMKilled']} for r in rows]

def snapshot():
 r=Redis()
 try:
  workers=r.cmd('HGETALL',PREFIX+'workers');workers={workers[i].decode():workers[i+1].decode() for i in range(0,len(workers),2)}
  leases=r.cmd('ZRANGE',PREFIX+'leases',0,-1,'WITHSCORES');leases={leases[i].decode():float(leases[i+1]) for i in range(0,len(leases),2)}
  servers=r.cmd('ZRANGE','asynq:servers',0,-1,'WITHSCORES');summaries=[]
  for i in range(0,len(servers),2):
   key=servers[i].decode()
   # Other non-sync APP lanes may have separate servers; inspect only our worker.
   if not any(key.startswith('asynq:servers:{'+worker+':') for worker in workers):continue
   raw=r.cmd('GET',key)
   if raw is None:continue
   assert len(raw)<8192
   wire=fields(raw);strings=[];queues={}
   for vals in wire.values():
    for v in vals:
     if not isinstance(v,bytes):continue
     try:strings.append(v.decode())
     except UnicodeError:pass
     try:
      entry=fields(v);q=entry.get(1,[b''])[0];weight=entry.get(2,[0])[0]
      if isinstance(q,bytes) and isinstance(weight,int) and weight>0 and q.decode() in ['core','postprocess','enrichment','shared','low','sync','approved_sync','wiki']:queues[q.decode()]=weight
     except (ValueError,IndexError,UnicodeError,AssertionError):pass
   identity=key.removeprefix('asynq:servers:{').removesuffix('}').rsplit(':',2);assert len(identity)==3
   summaries.append({'server_id':identity[2],'worker_id':identity[0]+':'+identity[1],'identity_tokens_match':identity[0] in strings and identity[2] in strings,'ttl_ms':r.cmd('PTTL',key),'registry_score':float(servers[i+1]),'queues':queues})
  current=r.cmd('TIME');now=int(current[0])*1000+int(current[1])//1000
  raw_state=r.cmd('GET',PREFIX+'system_service');state=json.loads(raw_state) if raw_state else None
  return {'workers':workers,'leases':leases,'membership_revision':(r.cmd('GET',PREFIX+'membership_revision') or b'').decode(),'servers':summaries,'trip_present':r.cmd('EXISTS',PREFIX+'trip')==1,'permit_present':r.cmd('EXISTS',PREFIX+'permit')==1,'observation_present':r.cmd('EXISTS',PREFIX+'observation_required')==1,'now_ms':now,'state':state}
 finally:r.close()

def sql(query):
 p=subprocess.run(C+['exec','-T','postgres','psql','-XqAt','-U','postgres','-d','consumer_native_test','-v','ON_ERROR_STOP=1'],input=('BEGIN READ ONLY;'+query+';ROLLBACK;').encode(),capture_output=True,timeout=20)
 assert p.returncode==0
 return json.loads(p.stdout)

def journal(r):
 rows=r.cmd('XRANGE',PREFIX+'system_journal','-','+','COUNT',64)
 assert len(rows)<64,'Diagnostic journal bound exceeded; do not export/truncate unreviewed evidence'
 return [(row[0].decode(),{row[1][i].decode():row[1][i+1].decode() for i in range(0,len(row[1]),2)}) for row in rows]

def diagnostic_summary(value,server_id):
 diag=json.loads(value)
 allowed={'expected_queues','class','elapsed_ms','membership_revision','post_failure_membership_revision','expected_incarnation','observed_incarnation','admitted_incarnation','incarnation_matches','read_class','probe_class','registry_count','registry_class','truncated','consumer_server_ids','previous_consumer_server_ids','missing_previous_server_ids','servers'}
 assert set(diag)<=allowed and not diag.get('truncated')
 assert set(diag['expected_queues'])=={'sync','approved_sync'}
 allowed_server={'server_id','metadata_class','post_failure_ttl_ms','post_failure_registry_score','score_present','post_failure_read_class','expiry_evidence','queue_subscribed','key_identity_matches_metadata'}
 for row in diag.get('servers',[]):assert set(row)<=allowed_server
 missing=next(x for x in diag['servers'] if x['server_id']==server_id and x['metadata_class']=='metadata_get_missing')
 assert -3<=missing['post_failure_ttl_ms']<=15000
 assert missing['score_present'], 'Missing GET evidence must retain the registry observation'
 text=json.dumps(diag)
 private=dict(x.split('=',1) for x in (ROOT/'app.env').read_text().splitlines() if '=' in x)
 for key in ['DB_PASSWORD','REDIS_PASSWORD','JWT_SECRET','SYSTEM_AES_KEY','SYSTEM_SIGNING_KEY']:assert private[key] not in text
 return {'class':diag['class'],'read_class':diag['read_class'],'probe_class':diag['probe_class'],'registry_count':diag['registry_count'],'expected_queues':diag['expected_queues'],'metadata_class':missing['metadata_class'],'ttl_ms':missing['post_failure_ttl_ms'],'registry_score':missing['post_failure_registry_score'],'membership_present':bool(diag['membership_revision']),'incarnation_present':bool(diag['expected_incarnation']),'raw_metadata_exported':False,'sensitive_value_check':'PASS'}

def fault_gap(baseline):
 # Start immediately after a genuine heartbeat, so the scheduled next beat is
 # unlikely to erase the controlled gap before the controller's 200ms sample.
 old=next(x for x in baseline['servers'] if 'sync' in x['queues'] and 'approved_sync' in x['queues'])
 deadline=time.monotonic()+15
 while True:
  fresh=snapshot();assert not fresh['trip_present'] and fresh['workers']==baseline['workers']
  pulse=next(x for x in fresh['servers'] if x['server_id']==old['server_id'])
  if pulse['registry_score']>old['registry_score']:
   baseline=fresh;break
  assert time.monotonic()<deadline, 'No real pre-fault heartbeat observed'
  time.sleep(0.15)
 r=Redis();a=next(x for x in baseline['servers'] if 'sync' in x['queues'] and 'approved_sync' in x['queues'])
 key='asynq:servers:{'+a['worker_id']+':'+a['server_id']+'}'
 before=journal(r);last=before[-1][0] if before else '0-0'
 try:
  begin=r.cmd('BEGIN_OWN_METADATA_GAP',key);started=time.monotonic();withdrawn=False
  try:
   while time.monotonic()-started<0.6:
    assert r.cmd('EXISTS',PREFIX+'trip')==0
    withdrawn=withdrawn or r.cmd('EXISTS',PREFIX+'permit')==0
    time.sleep(min(0.025,max(0,0.6-(time.monotonic()-started))))
  finally:restored=r.cmd('END_OWN_METADATA_GAP')
  assert 590<=restored['elapsed_ms']<=900,'Gap exceeded the controlled 0.6 second envelope'
  assert withdrawn,'Driver did not withdraw its own permit during metadata absence'
  events=journal(r)
  fresh=[(i,e) for i,e in events if tuple(map(int,i.split('-')))>tuple(map(int,last.split('-')))]
  wait=next(e for _,e in fresh if e.get('event')=='automatic_service_wait' and e.get('reason')=='consumer_unavailable')
  summary=diagnostic_summary(wait['consumer_diagnostic'],a['server_id'])
  deadline=time.monotonic()+20;after=None
  while time.monotonic()<deadline:
   after=snapshot();assert not after['trip_present']
   b=next((x for x in after['servers'] if x['server_id']==a['server_id']),None)
   if b and b['registry_score']>a['registry_score'] and after['permit_present']:
    assert after['workers']==baseline['workers'] and after['membership_revision']==baseline['membership_revision']
    assert after['state']['epoch']==baseline['state']['epoch'] and after['state']['phase']=='canary' and not after['state'].get('canary_source_id')
    return {'result':'PASS','controlled_gap_ms':restored['elapsed_ms'],'restore_class':restored['restore_class'],'permit_withdrawn':True,'trip_created':False,'real_next_heartbeat_advanced':True,'stable_requalification':True,'journal_diagnostic':summary,'registry_score_samples':[a['registry_score'],b['registry_score']]}
   time.sleep(0.25)
  raise AssertionError('Real consumer heartbeat and stable permit did not recover in bounded observation')
 finally:r.close()

def main():
 global PHASE
 state=runtime();assert len(state)==6 and all(x['running'] and not x['oom'] and x['restarts']==0 and x['health'] not in ['unhealthy','starting'] for x in state)
 PHASE='database_schema_and_rows'
 database=sql("SELECT json_build_object('schema',(SELECT version FROM schema_migrations LIMIT 1),'dirty',(SELECT dirty FROM schema_migrations LIMIT 1),'sources',(SELECT count(*) FROM data_sources),'sync_logs',(SELECT count(*) FROM sync_logs),'active_chunks',(SELECT count(*) FROM chunks WHERE deleted_at IS NULL),'embeddings',(SELECT count(*) FROM embeddings),'receipt_phase',(SELECT value::jsonb->>'phase' FROM system_settings WHERE key='integration.feishu.automatic_service_v1'),'receipt_epoch',(SELECT value::jsonb->>'epoch' FROM system_settings WHERE key='integration.feishu.automatic_service_v1'))")
 assert database['schema']==3051 and not database['dirty'] and database['sources']==database['sync_logs']==0 and database['active_chunks']>0 and database['embeddings']>0 and database['receipt_phase']=='active'
 PHASE='consumer_heartbeat_lease_and_identity'
 cfg=ROOT/'config.yaml';assert cfg.stat().st_uid==1000 and cfg.stat().st_mode&0o777==0o600
 assert not (ROOT/'approval.pub').exists()
 proc_uid=subprocess.check_output(C+['exec','-T','app','python3','-c',"print(next(x for x in open('/proc/1/status') if x.startswith('Uid:')).split()[1])"],text=True).strip();assert proc_uid=='1000'
 first=snapshot();time.sleep(12);second=snapshot()
 assert len(first['workers'])==len(second['workers'])==1 and first['workers']==second['workers']
 worker=next(iter(first['workers']));incarnation=first['workers'][worker].rsplit(':',1)[-1];assert str(uuid.UUID(incarnation))==incarnation
 assert first['membership_revision']==second['membership_revision']
 assert first['leases'][worker]>first['now_ms'] and second['leases'][worker]>second['now_ms'] and second['leases'][worker]>first['leases'][worker]
 a=next(x for x in first['servers'] if 'sync' in x['queues'] and 'approved_sync' in x['queues']);b=next(x for x in second['servers'] if x['server_id']==a['server_id'])
 assert a['worker_id']==worker and b['worker_id']==worker and a['identity_tokens_match'] and b['identity_tokens_match'] and a['ttl_ms']>0 and b['ttl_ms']>0 and b['registry_score']>a['registry_score']
 for sample in [first,second]:
  st=sample['state'];assert st and st['version']==1 and st['namespace']==PREFIX and st['runtime_sha']==SHA and st['phase']=='canary' and not st.get('canary_source_id') and not st.get('canary_run_id')
  assert st['worker_id']==worker and st['worker_value']==sample['workers'][worker] and st['consumer_server_id']==a['server_id'] and st['membership_revision']==sample['membership_revision']
  assert st['epoch']==database['receipt_epoch'] and not sample['trip_present'] and sample['permit_present']
 PHASE='own_metadata_gap_fault'
 fault=fault_gap(second)
 PHASE='acceptance_cases_and_whitelist'
 acceptance=json.loads((ROOT/'acceptance-final.json').read_text());assert len(acceptance['results'])==16 and all(x['result']=='PASS' for x in acceptance['results'])
 after=runtime();assert all(x['running'] and not x['oom'] and x['restarts']==0 for x in after)
 report={'source_sha':SHA,'project':PROJECT,'utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'cases':acceptance['results'],'runtime':after,'transport':json.loads((ROOT/'transport.json').read_text()),'redis_transport':'bounded_RESP_from_own_APP_UID1000','database':database,'worker_probe':{'result':'PASS','worker_id':worker,'incarnation':incarnation,'membership_revision':first['membership_revision'],'app_uid':int(proc_uid),'config_mode':'0600_uid1000','lease_score_samples':[first['leases'][worker],second['leases'][worker]],'consumer_server_id':a['server_id'],'sync_and_approved_sync':True,'lease_renewed':True,'metadata_heartbeat_advanced':True,'trip_absent':True,'driver_issued_permit':True,'sql_receipt_active':True,'phase':'canary_unselected','platform_approval_credentials_created':False},'metadata_gap_fault':fault,'remote_feishu_publication':{'result':'NOT_RUN','reason':'No production OAuth or source was copied; this stack has zero sources and no fabricated publication'},'model_semantic_quality':{'result':'NOT_RUN','reason':'Deterministic fixture; no real LLM throughput/quality claim'},'production_changes':False}
 (OUT/'dictionary-assets.json').write_text((ROOT/'dictionary-assets.json').read_text());(OUT/'full-app-report.json').write_text(json.dumps(report,indent=2))
 deps=[]
 for row in after:
  x=json.loads(subprocess.check_output(['docker','image','inspect',row['image_id']],text=True))[0];deps.append({'service':row['service'],'image_id':row['image_id'],'repo_digests':x['RepoDigests'],'source_sha':(x['Config'].get('Labels') or {}).get('org.opencontainers.image.revision')})
 (OUT/'dependency-images.json').write_text(json.dumps(deps,indent=2));print(json.dumps({'source_sha':SHA,'app_cases_passed':16,'worker_probe':'PASS','metadata_gap_fault':'PASS','remote_feishu_publication':'NOT_RUN','production_changes':False}))
if __name__=='__main__':
 try:main()
 except Exception as e:
  cases=json.loads((ROOT/'acceptance-final.json').read_text()).get('results',[]) if (ROOT/'acceptance-final.json').exists() else []
  (OUT/'full-app-failure.json').write_text(json.dumps({'source_sha':SHA,'error_class':type(e).__name__,'phase':PHASE,'cases':cases}));print(json.dumps({'phase':PHASE,'error_class':type(e).__name__}));raise SystemExit(1)
