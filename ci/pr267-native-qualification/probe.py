"""Read-only probes and whitelist evidence for the disposable full APP only."""
import datetime,json,os,pathlib,socket,subprocess,time,uuid,zipfile,hashlib
ROOT=pathlib.Path(os.environ['QUALIFICATION_ROOT']);SHA=os.environ['SOURCE_SHA'];assert len(SHA)==40 and all(c in '0123456789abcdef' for c in SHA)
PROJECT='knowhub-pr267-'+os.environ['GITHUB_RUN_ID'];assert os.environ.get('GITHUB_ACTIONS')=='true' and os.environ['SOURCE_SHA']==SHA
PHASE='runtime'
C=['docker','compose','-p',PROJECT,'-f',str(ROOT/'compose.json')]
OUT=pathlib.Path(os.environ['GITHUB_WORKSPACE'])/'evidence';OUT.mkdir(exist_ok=True)
class Redis:
 def __init__(self):
  env=dict(x.split('=',1) for x in (ROOT/'app.env').read_text().splitlines() if '=' in x)
  self.s=socket.create_connection(('127.0.0.1',16381),timeout=3);self.f=self.s.makefile('rb');self.cmd('AUTH',env['REDIS_PASSWORD'])
 def read(self):
  line=self.f.readline();kind=line[:1];v=line[1:-2]
  if kind==b'+':return v.decode()
  if kind==b'-':raise RuntimeError('isolated_redis_error')
  if kind==b':':return int(v)
  if kind==b'$':
   n=int(v)
   if n<0:return None
   out=self.f.read(n);assert self.f.read(2)==b'\r\n';return out
  if kind==b'*':return [self.read() for _ in range(int(v))]
  raise RuntimeError('invalid_isolated_redis_response')
 def cmd(self,name,*args):
  assert name in ['AUTH','GET','HGETALL','ZRANGE','PTTL','TIME','EXISTS','XLEN']
  a=[str(v).encode() for v in [name,*args]];self.s.sendall(b'*'+str(len(a)).encode()+b'\r\n'+b''.join(b'$'+str(len(v)).encode()+b'\r\n'+v+b'\r\n' for v in a));return self.read()
 def close(self):self.f.close();self.s.close()
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
 r=Redis();prefix='feishu:safety:{native-pr267-'+os.environ['GITHUB_RUN_ID']+'}:'
 try:
  workers=r.cmd('HGETALL',prefix+'workers');workers={workers[i].decode():workers[i+1].decode() for i in range(0,len(workers),2)}
  leases=r.cmd('ZRANGE',prefix+'leases',0,-1,'WITHSCORES');leases={leases[i].decode():float(leases[i+1]) for i in range(0,len(leases),2)}
  servers=r.cmd('ZRANGE','asynq:servers',0,-1,'WITHSCORES');summaries=[]
  for i in range(0,len(servers),2):
   key=servers[i].decode();raw=r.cmd('GET',key);assert raw and len(raw)<8192
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
   identity=key.removeprefix('asynq:servers:{').removesuffix('}').rsplit(':',2)
   assert len(identity)==3
   summaries.append({'server_id':identity[2],'worker_id':identity[0]+':'+identity[1],'identity_tokens_match':identity[0] in strings and identity[2] in strings,'ttl_ms':r.cmd('PTTL',key),'registry_score':float(servers[i+1]),'queues':queues})
  current=r.cmd('TIME');now=int(current[0])*1000+int(current[1])//1000
  return {'workers':workers,'leases':leases,'membership_revision':(r.cmd('GET',prefix+'membership_revision') or b'').decode(),'servers':summaries,'trip_present':r.cmd('EXISTS',prefix+'trip')==1,'permit_present':r.cmd('EXISTS',prefix+'permit')==1,'observation_present':r.cmd('EXISTS',prefix+'observation_required')==1,'now_ms':now}
 finally:r.close()

def main():
 global PHASE
 state=runtime();assert len(state)==6 and all(x['running'] and not x['oom'] and x['restarts']==0 and x['health'] not in ['unhealthy','starting'] for x in state)
 PHASE="database_schema_and_rows"
 query="SELECT json_build_object('schema',(SELECT version FROM schema_migrations LIMIT 1),'dirty',(SELECT dirty FROM schema_migrations LIMIT 1),'sources',(SELECT count(*) FROM data_sources),'sync_logs',(SELECT count(*) FROM sync_logs),'active_chunks',(SELECT count(*) FROM chunks WHERE deleted_at IS NULL),'embeddings',(SELECT count(*) FROM embeddings))"
 proc=subprocess.run(C+['exec','-T','postgres','psql','-XqAt','-U','postgres','-d','consumer_native_test','-v','ON_ERROR_STOP=1'],input=('BEGIN READ ONLY;'+query+';ROLLBACK;').encode(),capture_output=True,timeout=20)
 assert proc.returncode==0
 database=json.loads(proc.stdout);assert database['schema']==3051 and not database['dirty'] and database['sources']==database['sync_logs']==0 and database['active_chunks']>0 and database['embeddings']>0
 PHASE="consumer_heartbeat_lease_and_identity"
 first=snapshot();time.sleep(12);second=snapshot()
 assert len(first['workers'])==len(second['workers'])==1 and first['workers']==second['workers']
 assert first['membership_revision']==second['membership_revision']
 worker=next(iter(first['workers']));assert first['leases'][worker]>first['now_ms'] and second['leases'][worker]>second['now_ms'] and second['leases'][worker]>first['leases'][worker]
 a=next(x for x in first['servers'] if 'sync' in x['queues'] and 'approved_sync' in x['queues']);b=next(x for x in second['servers'] if x['server_id']==a['server_id'])
 assert a['worker_id']==worker and b['worker_id']==worker and a['identity_tokens_match'] and b['identity_tokens_match'] and a['ttl_ms']>0 and b['ttl_ms']>0 and b['registry_score']>a['registry_score']
 PHASE='retained_stop_without_permit'
 assert first['trip_present'] and second['trip_present'] and not first['permit_present'] and not second['permit_present'] and not second['observation_present']
 PHASE='acceptance_cases_and_whitelist'
 acceptance=json.loads((ROOT/'acceptance-final.json').read_text());assert len(acceptance['results'])==16 and all(x['result']=='PASS' for x in acceptance['results'])
 report={'source_sha':SHA,'project':PROJECT,'utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'cases':acceptance['results'],'runtime':state,'database':database,'worker_probe':{'result':'PASS','worker_id':worker,'consumer_server_id':a['server_id'],'sync_and_approved_sync':True,'lease_renewed':True,'metadata_heartbeat_advanced':True,'membership_stable':True,'stopped_without_permit':True},'signature_success_path':{'result':'NOT_RUN','reason':'No legitimate non-production offline signer/remote source; no production credentials copied'},'observation_semantic_success':{'result':'NOT_RUN','reason':'HTTP cases prove non-admin denial; signed observation controller paths are separately SQL/Redis/race qualified'},'model_semantic_quality':{'result':'NOT_RUN','reason':'Deterministic fixture; no real LLM performance claim'},'production_changes':False}
 (OUT/'full-app-report.json').write_text(json.dumps(report,indent=2))
 deps=[]
 for row in state:
  x=json.loads(subprocess.check_output(['docker','image','inspect',row['image_id']],text=True))[0];deps.append({'service':row['service'],'image_id':row['image_id'],'repo_digests':x['RepoDigests'],'source_sha':(x['Config'].get('Labels') or {}).get('org.opencontainers.image.revision')})
 (OUT/'dependency-images.json').write_text(json.dumps(deps,indent=2));print(json.dumps({'source_sha':SHA,'app_cases_passed':16,'worker_probe':'PASS','signed_recovery':'NOT_RUN','production_changes':False}))
if __name__=='__main__':
 try:main()
 except Exception as e:
  cases=json.loads((ROOT/'acceptance-final.json').read_text()).get('results',[]) if (ROOT/'acceptance-final.json').exists() else []
  (OUT/'full-app-failure.json').write_text(json.dumps({'source_sha':SHA,'error_class':type(e).__name__,'phase':PHASE,'cases':cases}));print(json.dumps({'phase':PHASE,'error_class':type(e).__name__}));raise SystemExit(1)
