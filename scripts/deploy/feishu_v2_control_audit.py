"""Read-only isolated controller, Asynq, lifecycle and OAuth evidence."""
import sys

import feishu_v2_docreader_service as transport

REMOTE = r'''
import json,pathlib,subprocess,hashlib,re,datetime,socket
root=pathlib.Path(ROOT);site=root/'isolated-site';project='knowhub-feishu-v2-'+BASE_SHA[:12]
if root.is_symlink() or json.loads((site/'preparation.json').read_text())['project']!=project:raise ValueError('isolated_project_mismatch')
compose=['docker','compose','--project-name',project,'--file',str(site/'compose.json')]
def run(args,input=None):
 p=subprocess.run(args,input=input,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True,timeout=60)
 if p.returncode:raise ValueError('readonly_operation_failed')
 return p.stdout
def mask(value):return hashlib.sha256(str(value).encode()).hexdigest()[:12]
def inspect(name):return json.loads(run(['docker','inspect',run(compose+['ps','--quiet',name]).strip()]))[0]
def utc(ms):return datetime.datetime.fromtimestamp(float(ms)/1000,datetime.timezone.utc).isoformat()
redis_info=inspect('redis');ip=next(iter(redis_info['NetworkSettings']['Networks'].values()))['IPAddress']
secret=re.search(r'^requirepass (.+)$',(site/'redis.conf').read_text(),re.M).group(1)
connection=socket.create_connection((ip,6379),timeout=10);stream=connection.makefile('rb')
def redis(*args):
 if args[0] not in ['AUTH','TIME','GET','HGETALL','ZRANGE','ZRANGEBYSCORE','PTTL','TYPE','SCAN','XRANGE','XREVRANGE','LLEN','ZCARD','INFO','LATENCY','SLOWLOG']:raise ValueError('non_readonly_command_refused')
 parts=[str(a).encode() for a in args];connection.sendall(b'*'+str(len(parts)).encode()+b'\r\n'+b''.join(b'$'+str(len(p)).encode()+b'\r\n'+p+b'\r\n' for p in parts))
 def read():
  line=stream.readline();kind=line[:1];value=line[1:-2]
  if kind==b'+':return value
  if kind==b'-':raise ValueError('readonly_redis_failed')
  if kind==b':':return int(value)
  if kind==b'$':
   size=int(value)
   if size<0:return None
   data=stream.read(size);stream.read(2);return data
  if kind==b'*':return [read() for _ in range(int(value))]
  raise ValueError('invalid_redis_response')
 return read()
redis('AUTH',secret);secret=None
clock=redis('TIME');now=int(clock[0])*1000+int(clock[1])//1000
prefix='feishu:safety:{legacy-feishu}:'
def zmap(key):
 values=redis('ZRANGE',key,0,-1,'WITHSCORES');return dict(zip(values[::2],values[1::2]))
values=redis('HGETALL',prefix+'workers');workers=dict(zip(values[::2],values[1::2]));leases=zmap(prefix+'leases');drains=zmap(prefix+'draining')
registry=[]
for worker,value in workers.items():
 policy,_,incarnation=value.decode().partition(':');until=float(leases.get(worker,b'0'))
 registry.append({'worker_hash':mask(worker.decode()),'policy_fingerprint':policy,'incarnation_hash':mask(incarnation),'lease_until':utc(until),'lease_remaining_ms':int(until-now)})
def fields(data):
 result={};offset=0
 def varint():
  nonlocal offset
  value=0;shift=0
  while offset<len(data):
   byte=data[offset];offset+=1;value|=(byte&127)<<shift
   if byte<128:return value
   shift+=7
   if shift>70:raise ValueError('invalid_protobuf')
  raise ValueError('short_protobuf')
 while offset<len(data):
  tag=varint();field=tag>>3;wire=tag&7
  if wire==0:value=varint()
  elif wire==2:
   length=varint();value=data[offset:offset+length];offset+=length
  elif wire in [1,5]:offset+=8 if wire==1 else 4;continue
  else:raise ValueError('unsupported_protobuf')
  result.setdefault(field,[]).append(value)
 return result
servers=[]
for key,until in zmap('asynq:servers').items():
 raw=redis('GET',key.decode())
 if not raw:continue
 data=fields(raw);host=data.get(1,[b''])[0].decode();pid=data.get(2,[0])[0];queues={}
 for value in data.get(5,[]):
  queue=fields(value);queues[queue[1][0].decode()]=queue.get(2,[0])[0]
 started=fields(data[8][0]) if data.get(8) else {}
 servers.append({'worker_hash':mask(host+':'+str(pid)),'server_hash':mask(data.get(3,[b''])[0].decode()),'pid':pid,'status':data.get(7,[b''])[0].decode(),'concurrency':data.get(4,[0])[0],'queues':queues,'registry_expires_at':utc(float(until)*1000),'registry_remaining_ms':int(float(until)*1000-now),'started_at':utc(started.get(1,[0])[0]*1000)})
def safe_events(key):
 result=[]
 for record in redis('XRANGE',key,'-','+','COUNT',1000):
  values=dict(zip(record[1][::2],record[1][1::2]));row={'at':utc(int(record[0].split(b'-')[0]))}
  for name in ['kind','event','reason','fenced','queue_paused']:
   value=values.get(name.encode(),b'').decode()
   if value and re.fullmatch('[a-zA-Z0-9_:.-]{1,100}',value):row[name]=value
  if values.get(b'worker_id'):row['worker_hash']=mask(values[b'worker_id'].decode())
  result.append(row)
 return result
containers=[];log_events=[]
for name in ['app','worker2']:
 info=inspect(name);environment=dict(v.split('=',1) for v in info['Config']['Env'] if '=' in v)
 scope=environment.get('FEISHU_LEGACY_SYNC_ALLOWED_SOURCE_IDS','')
 if name=='app':
  callback_expiry=environment.get('FEISHU_TEST_HTTP_CALLBACK_EXPIRES_AT')
  callback_scope_enabled=environment.get('FEISHU_PERSONAL_PERMISSION_AUTH_SCOPE_ENABLED')=='true'
 containers.append({'service':name,'container_hash':mask(info['Id']),'hostname_hash':mask(info['Config']['Hostname']),'started_at':info['State']['StartedAt'],'health':info['State'].get('Health',{}).get('Status'),'source_sha':info['Config']['Labels'].get('knowhub.source-sha'),'configured_worker_hash':mask(environment['FEISHU_SYNC_WORKER_ID']) if environment.get('FEISHU_SYNC_WORKER_ID') else None,'scope_hashes':[mask(v) for v in scope.split(',') if re.fullmatch('[a-f0-9-]{36}',v)],'scope_unrestricted':scope in ['','all'],'safety_mode':environment.get('FEISHU_SYNC_SAFETY_MODE'),'maintenance_concurrency':environment.get('WEKNORA_ASYNQ_MAINTENANCE_CONCURRENCY')})
 text=run(compose+['logs','--timestamps','--no-color','--since','2026-10-26T00:00:00Z',name])
 for line in text.splitlines():
  timestamp=re.search(r'\d{4}-\d{2}-\d{2}T[0-9:.]+Z',line)
  event=re.search(r'\[FeishuSafety\] event=([a-z_]+) reason=([a-z_]+) worker=([^\s]+)',line)
  if event:log_events.append({'service':name,'at':timestamp.group() if timestamp else None,'event':event[1],'reason':event[2],'worker_hash':mask(event[3])})
  elif any(v in line for v in ['[Scheduler] started','Starting processing','Starting Server','Stopping processor','Waiting for all workers','All workers have finished','Shutdown complete','Graceful shutdown']):
   log_events.append({'service':name,'at':timestamp.group() if timestamp else None,'phase':next(v for v in ['[Scheduler] started','Starting processing','Starting Server','Stopping processor','Waiting for all workers','All workers have finished','Shutdown complete','Graceful shutdown'] if v in line)})
 query="BEGIN READ ONLY; SELECT json_build_object('access_valid',c.expires_at>clock_timestamp(),'refresh_valid',c.refresh_expires_at>clock_timestamp(),'access_expires_at',c.expires_at,'refresh_expires_at',c.refresh_expires_at) FROM feishu_connections c JOIN users u ON u.id=c.user_id WHERE u.username='test' AND u.deleted_at IS NULL; COMMIT;"
oauth=json.loads(run(compose+['exec','-T','postgres','psql','-qX','-U','postgres','-d','knowhub_feishu_v2_test','-At'],query).strip() or 'null')
def latch(key):
 value=redis('GET',key)
 return {'present':value is not None,'value':value.decode() if value and re.fullmatch(b'[a-zA-Z0-9_:-]{1,100}',value) else None,'ttl_ms':redis('PTTL',key)}
result={'status':'READ_ONLY','at':utc(now),'namespace':'legacy-feishu','containers':containers,'controllers':registry,'asynq_servers':servers,'draining':[{'worker_hash':mask(k.decode()),'until':utc(v),'remaining_ms':int(float(v)-now)} for k,v in drains.items()],'trip':latch(prefix+'trip'),'stop_latch_key_is_trip':True,'permit':latch(prefix+'permit'),'sync_queue_pause':latch('asynq:{sync}:paused'),'control_audit':safe_events(prefix+'control_audit'),'guard_audit':safe_events(prefix+'audit'),'recent_audit':safe_events(prefix+'events')[-30:],'lifecycle_logs':sorted(log_events,key=lambda r:r.get('at') or ''),'oauth':oauth,'redis_mutations':False}
builders=[]
for cid in run(['docker','ps','-a','--filter','label=knowhub.phase=compile-only','--format','{{.ID}}']).splitlines():
 record=json.loads(run(['docker','inspect',cid]))[0]
 if record['Config']['Labels'].get('knowhub.source-sha')!=SHA:continue
 caches=[]
 for mount in record['Mounts']:
  if mount['Destination']=='/cache' and mount['Type']=='volume':
   volume=json.loads(run(['docker','volume','inspect',mount['Name']]))[0]
   caches.append({'volume_name':mount['Name'],'owner_source_sha':volume.get('Labels',{}).get('knowhub.source-sha')})
 builders.append({'name':record['Name'],'state':record['State']['Status'],'exit_code':record['State'].get('ExitCode'),'caches':caches})
result['reference_build_caches']=builders
telemetry={}
for section in ['persistence','stats','clients']:
 raw=redis('INFO',section).decode();values={}
 for line in raw.splitlines():
  if ':' not in line:continue
  key,value=line.split(':',1)
  allowed=['latest_fork_usec','total_forks','aof_delayed_fsync','aof_pending_bio_fsync','aof_last_write_status',
   'aof_last_bgrewrite_status','aof_enabled','rdb_last_bgsave_status','rdb_last_bgsave_time_sec',
   'rdb_last_cow_size','rdb_current_bgsave_time_sec','blocked_clients','connected_clients','rejected_connections']
  if key in allowed:values[key]=value
 telemetry[section]=values
telemetry['latency_events']=redis('LATENCY','LATEST')
telemetry['latency_events']=[[x.decode() if isinstance(x,bytes) else x for x in row] for row in telemetry['latency_events']]
telemetry['slow_commands']=[{'at':row[1],'duration_us':row[2],
 'command':row[3][0].decode() if row[3] else 'unknown'} for row in redis('SLOWLOG','GET','12')]
telemetry['host_load']=pathlib.Path('/proc/loadavg').read_text().split()[:3]
telemetry['cpu_pressure']=pathlib.Path('/proc/pressure/cpu').read_text() if pathlib.Path('/proc/pressure/cpu').is_file() else None
telemetry['io_pressure']=pathlib.Path('/proc/pressure/io').read_text() if pathlib.Path('/proc/pressure/io').is_file() else None
result['read_only_dependency_telemetry']=telemetry
result['personal_authorization_configuration']={'http_callback_expires_at':callback_expiry,
 'permission_auth_scope_requested':callback_scope_enabled}
def readonly_sql(statement):
 payload="BEGIN READ ONLY; SET LOCAL statement_timeout='10s'; "+statement+'; COMMIT;'
 return json.loads(run(compose+['exec','-T','postgres','psql','-qX','-v','ON_ERROR_STOP=1','-U','postgres','-d','knowhub_feishu_v2_test','-At'],payload).strip() or 'null')
sources=readonly_sql("SELECT coalesce(json_agg(q),'[]') FROM (SELECT ds.id,ds.status,ds.legacy_access_state,ds.updated_at,ds.last_sync_at,md5(ds.config::text) AS config_digest,md5(ds.last_sync_cursor::text) AS cursor_digest,m.resource_type,m.knowledge_id FROM data_sources ds JOIN feishu_personal_imports m ON m.id=ds.id JOIN users u ON u.id=m.user_id JOIN knowledge_bases kb ON kb.id=m.kb_id WHERE u.username='test' AND kb.name='test001' AND m.resource_type IN ('sheet','bitable') AND ds.deleted_at IS NULL) q")
if any(not re.fullmatch('[a-f0-9-]{36}',row['id']) for row in sources):raise ValueError('source_identifier_invalid')
ids=','.join("'"+row['id']+"'" for row in sources) or "''"
events=readonly_sql("SELECT coalesce(json_agg(q),'[]') FROM (SELECT action,outcome,created_at,target_id,actor_user_id,details->'changed_fields' AS changed_fields FROM audit_logs WHERE target_id IN ("+ids+") ORDER BY created_at DESC LIMIT 60) q")
for row in events:
 row['source_hash']=mask(row.pop('target_id'));row['actor_hash']=mask(row.pop('actor_user_id'))
 if row['changed_fields'] is not None and (not isinstance(row['changed_fields'],list) or any(not re.fullmatch('[a-z_]{1,64}',str(v)) for v in row['changed_fields'])):row['changed_fields']=['unrecognized_field_names_omitted']
for row in sources:
 row['source_hash']=mask(row.pop('id'));row['knowledge_hash']=mask(row.pop('knowledge_id'))
result['two_source_readonly_forensics']={'sources':sources,'audit_events':events}
history=readonly_sql("SELECT coalesce(json_agg(q),'[]') FROM (SELECT id,data_source_id,status,created_at,finished_at,error_message,result FROM sync_logs WHERE data_source_id IN ("+ids+") ORDER BY created_at DESC LIMIT 12) q")
for row in history:
 row['sync_hash']=mask(row.pop('id'));row['source_hash']=mask(row.pop('data_source_id'))
 detail=json.dumps(row.pop('result'),ensure_ascii=False);message=row.pop('error_message') or ''
 row['fixed_error_matches']=[v for v in ['sharing_approval_required','feishu_sharing_approval_required','source item fetch failed','safety gate','Feishu safety','context deadline','stopped','feishu controller','permission','reconnect','refresh','staging','capacity','version','fence'] if v.lower() in (detail+' '+message).lower()]
 row['provider_codes']=sorted(set(re.findall(r'\b(?:code[=: ]+|feishu_error_code[^0-9]{1,8})([0-9]{4,9})\b',detail+' '+message)))
 try:
  parsed=json.loads(detail);errors=(parsed or {}).get('errors',[])
  row['error_codes']=[v.get('code') for v in errors if isinstance(v,dict) and re.fullmatch('[a-z_]{1,80}',v.get('code',''))]
  row['provider_codes']=sorted(set(row['provider_codes']+[str(v.get('params',{}).get('code')) for v in errors if isinstance(v,dict) and re.fullmatch('[0-9]{1,12}',str(v.get('params',{}).get('code','')))]))
 except (ValueError,AttributeError,TypeError):row['error_codes']=[]
result['two_source_readonly_forensics']['sync_failure_categories']=history
samples=[]
for _ in range(80):
 start=__import__('time').perf_counter();redis('GET',prefix+'membership_revision')
 samples.append((__import__('time').perf_counter()-start)*1000)
 __import__('time').sleep(0.05)
samples.sort()
result['membership_get_latency_sample']={'count':len(samples),'p50_ms':round(samples[len(samples)//2],3),'p95_ms':round(samples[int(len(samples)*0.95)-1],3),'max_ms':round(max(samples),3),'scope':'current_sample_only_not_historical_root_cause'}
connection.close();print(json.dumps(result))
'''


if __name__ == '__main__':
    if '--mode' not in sys.argv or sys.argv[sys.argv.index('--mode') + 1] != 'status':
        raise SystemExit('read-only audit supports status only')
    transport.REMOTE = REMOTE
    raise SystemExit(transport.main())
