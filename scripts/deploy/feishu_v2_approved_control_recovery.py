"""Execute the specifically approved isolated two-source recovery from report 19.

No production mode, scope editing, cursor reset, approval write or budget reset.
Each mutation is a separate explicit operation with persisted phase checks.
"""
import argparse
import sys

import feishu_v2_control_audit as audit
import feishu_v2_docreader_service as transport

RESET = r'''
local t=redis.call('TIME');local now=t[1]*1000+math.floor(t[2]/1000)
if redis.call('GET',KEYS[1])~='automatic_permit_consumer_lost' then return redis.error_reply('STOP_CHANGED') end
if redis.call('GET',KEYS[3])~='feishu_safety_stop' then return redis.error_reply('QUEUE_STATE_CHANGED') end
if redis.call('EXISTS',KEYS[2])~=0 or redis.call('HLEN',KEYS[4])~=0 or redis.call('HLEN',KEYS[6])~=0 then
 return redis.error_reply('CONTROLLER_NOT_QUIESCENT')
end
if redis.call('ZCOUNT',KEYS[5],now,'+inf')~=0 or redis.call('ZCARD',KEYS[7])~=0 then
 return redis.error_reply('LEASE_NOT_QUIESCENT')
end
if (redis.call('GET',KEYS[8]) or '0')~=ARGV[2] then return redis.error_reply('MEMBERSHIP_CHANGED') end
if redis.call('LLEN',KEYS[9])~=0 or redis.call('LLEN',KEYS[10])~=0 or
 redis.call('ZCARD',KEYS[11])~=0 or redis.call('ZCARD',KEYS[12])~=0 then return redis.error_reply('QUEUE_NOT_QUIESCENT') end
if redis.call('EXISTS',KEYS[13])~=0 then return redis.error_reply('RECOVERY_ALREADY_RESET') end
local kind=redis.call('TYPE',KEYS[14]).ok
if kind~='none' and kind~='stream' then return redis.error_reply('AUDIT_UNAVAILABLE') end
redis.call('XADD',KEYS[14],'*','event','feishu_safety_recovery_reset','plan_id',ARGV[1],
 'approval_ref',ARGV[3],'source_sha',ARGV[4],'scope','two_existing_test001_sources')
redis.call('SET',KEYS[13],ARGV[1]);redis.call('DEL',KEYS[1]);return 1
'''

RESUME = r'''
local t=redis.call('TIME');local now=t[1]*1000+math.floor(t[2]/1000)
if redis.call('EXISTS',KEYS[1])~=0 then return redis.error_reply('STOP_REAPPEARED') end
if redis.call('GET',KEYS[2])~=ARGV[3] or redis.call('PTTL',KEYS[2])<100 then return redis.error_reply('NO_LIVE_PERMIT') end
if redis.call('GET',KEYS[3])~='feishu_safety_stop' or redis.call('GET',KEYS[13])~=ARGV[1] then
 return redis.error_reply('RECOVERY_STATE_CHANGED')
end
if redis.call('HLEN',KEYS[4])~=2 or redis.call('HLEN',KEYS[6])~=2 or
 (redis.call('GET',KEYS[8]) or '0')~=ARGV[2] then return redis.error_reply('MEMBERSHIP_CHANGED') end
for i=4,6,2 do
 if redis.call('HGET',KEYS[4],ARGV[i])~=ARGV[i+1] or redis.call('HGET',KEYS[6],ARGV[i])~=ARGV[i+1] then
  return redis.error_reply('INCARNATION_CHANGED')
 end
 local lease=redis.call('ZSCORE',KEYS[5],ARGV[i])
 if not lease or tonumber(lease)<now+200 then return redis.error_reply('NO_LIVE_LEASE') end
end
if redis.call('LLEN',KEYS[9])~=0 or redis.call('ZCARD',KEYS[11])~=0 or redis.call('ZCARD',KEYS[12])~=0 then
 return redis.error_reply('QUEUE_STATE_CHANGED')
end
local pending=redis.call('LRANGE',KEYS[10],0,-1)
if #pending~=#ARGV-8 then return redis.error_reply('PENDING_CHANGED') end
for i,id in ipairs(pending) do if id~=ARGV[8+i] then return redis.error_reply('PENDING_CHANGED') end end
local kind=redis.call('TYPE',KEYS[14]).ok
if kind~='none' and kind~='stream' then return redis.error_reply('AUDIT_UNAVAILABLE') end
redis.call('XADD',KEYS[14],'*','event','feishu_safety_recovery_resumed','plan_id',ARGV[1],
 'approval_ref',ARGV[8],'scope','two_existing_test001_sources')
redis.call('DEL',KEYS[3]);return 1
'''

BOOT = audit.REMOTE[:audit.REMOTE.index("clock=redis('TIME')")]
BOOT = BOOT.replace("['ps','--quiet',name]", "['ps','--all','--quiet',name]")
BOOT = BOOT.replace("'ZCARD']", "'ZCARD','HLEN','LRANGE','HGET','EVAL']")
PROTO = audit.REMOTE[audit.REMOTE.index('def fields(data):'):audit.REMOTE.index('servers=[]')]

REMOTE = BOOT + PROTO + r'''
import time,uuid
FIX='2a7f651e6ed078fcf5113905a9358a82084d7d17'
APP='sha256:315bc552506ec0fead2777f811221b2b6d355968210e23e5b1662fe82db14bfe'
APPROVAL='user:call_085aca217ce54805be1d8372feec744b:approved_two_sources'
EXPECTED={'6d1698a6e0d9','1dcf458346ba'}
prefix='feishu:safety:{legacy-feishu}:'
queue='asynq:{sync}:'
if SHA!=FIX or project!='knowhub-feishu-v2-d6e99d894d3a':raise ValueError('approved_project_source_mismatch')
directory=site/'approved-control-recovery-20261026'
if directory.is_symlink():raise ValueError('unexpected_recovery_directory')
directory.mkdir(mode=0o700,exist_ok=True)
path=directory/'state.json'
state=json.loads(path.read_text()) if path.exists() else None

def save():
 temporary=directory/'state.tmp'
 temporary.write_text(json.dumps(state,indent=2)+'\n');temporary.chmod(0o600);temporary.replace(path)

def pairs(key):
 values=redis('HGETALL',key);return dict(zip(values[::2],values[1::2]))

def zmap(key):
 values=redis('ZRANGE',key,0,-1,'WITHSCORES');return dict(zip(values[::2],values[1::2]))

def clock():
 value=redis('TIME');return int(value[0])*1000+int(value[1])//1000

def text_value(key):
 value=redis('GET',key);return value.decode() if value is not None else None

def sql(query):
 return json.loads(run(compose+['exec','-T','postgres','psql','-qX','-v','ON_ERROR_STOP=1','-U','postgres',
  '-d','knowhub_feishu_v2_test','-At'],"BEGIN READ ONLY; SET LOCAL statement_timeout='10s'; "+query+"; COMMIT;").strip())

def sources():
 values=sql("SELECT coalesce(json_agg(q),'[]') FROM (SELECT ds.id,ds.status,ds.legacy_access_state,ds.sync_mode,ds.sync_schedule,"+
  "md5(ds.config::text) AS config_hash,md5(coalesce(ds.last_sync_cursor::text,'')) AS cursor_hash,ds.last_sync_at,"+
  "m.knowledge_id,m.auth_mode FROM data_sources ds JOIN feishu_personal_imports m ON m.id=ds.id "+
  "JOIN users u ON u.id=m.user_id JOIN knowledge_bases kb ON kb.id=m.kb_id "+
  "WHERE u.username='test' AND kb.name='test001' AND m.resource_type IN ('sheet','bitable') AND kb.deleted_at IS NULL) q")
 selected=[v for v in values if mask(v['id']) in EXPECTED]
 if len(selected)!=2 or {mask(v['id']) for v in selected}!=EXPECTED:raise ValueError('approved_sources_missing')
 for row in selected:
  if row['status']!='active' or row['legacy_access_state'] in ['paused','hold','revoked'] or row['auth_mode']!='user_oauth':
   raise ValueError('source_is_not_eligible_for_control_recovery')
 return sorted(selected,key=lambda row:row['id'])

def indexes(rows):
 kids=[v['knowledge_id'] for v in rows]
 if any(not re.fullmatch('[a-zA-Z0-9-]{1,64}',k or '') for k in kids):raise ValueError('published_knowledge_missing')
 selected=','.join("'"+k+"'" for k in kids)
 return sql("SELECT coalesce(json_agg(q),'[]') FROM (SELECT k.id,k.parse_status,k.updated_at,"+
  "(SELECT count(*) FROM chunks c WHERE c.knowledge_id=k.id AND c.deleted_at IS NULL) AS chunks,"+
  "(SELECT count(*) FROM embeddings e WHERE e.knowledge_id=k.id) AS vectors,"+
  "(SELECT md5(string_agg(e.id::text,',' ORDER BY e.id::text)) FROM embeddings e WHERE e.knowledge_id=k.id) AS vector_ids_hash "+
  "FROM knowledges k WHERE k.id IN ("+selected+") ORDER BY k.id) q")

def configuration():
 records={name:inspect(name) for name in ['app','worker2']}
 result={}
 for name,record in records.items():
  env=dict(v.split('=',1) for v in record['Config']['Env'] if '=' in v)
  scope=env.get('FEISHU_LEGACY_SYNC_ALLOWED_SOURCE_IDS','').split(',')
  if record['Image']!=APP or record['Config']['Labels'].get('knowhub.source-sha')!=FIX or \
   {mask(v) for v in scope}!=EXPECTED or env.get('FEISHU_SYNC_SAFETY_MODE')!='automatic' or \
   env.get('FEISHU_SYNC_SAFETY_ENABLED','').lower()!='true':raise ValueError('approved_configuration_drift')
  result[name]={'container_id':record['Id'],'environment_hash':mask('\n'.join(sorted(record['Config']['Env']))),
   'image':record['Image'],'running':record['State']['Running'],'health':record['State'].get('Health',{}).get('Status')}
 return result

def consumers(require_active=True):
 result=[];now=clock()
 for key,until in zmap('asynq:servers').items():
  if float(until)*1000<=now:continue
  raw=redis('GET',key.decode())
  if not raw:continue
  data=fields(raw);queues={}
  for entry in data.get(5,[]):
   entry=fields(entry);queues[entry[1][0].decode()]=entry.get(2,[0])[0]
  if queues.get('sync',0)<=0:continue
  if data.get(7,[b''])[0]!=b'active':
   if require_active:raise ValueError('inactive_sync_consumer_still_registered')
   continue
  result.append(data[1][0].decode()+':'+str(data[2][0]))
 return sorted(result)

def topology():
 members=pairs(prefix+'workers');leases=zmap(prefix+'leases');ids=consumers();now=clock()
 if len(members)!=2 or set(ids)!={k.decode() for k in members}:raise ValueError('two_process_topology_not_ready')
 if len({v.decode().split(':')[0] for v in members.values()})!=1:raise ValueError('controller_policy_mismatch')
 if any(float(leases.get(k,b'0'))<now+200 for k in members):raise ValueError('controller_lease_not_live')
 return members

def queue_counts():
 return {name:redis('LLEN' if name in ['active','pending'] else 'ZCARD',queue+name)
  for name in ['active','pending','scheduled','retry']}

def keys():
 return [prefix+'trip',prefix+'permit',queue+'paused',prefix+'workers',prefix+'leases',prefix+'admitted',
  prefix+'joining',prefix+'membership_revision',queue+'active',queue+'pending',queue+'scheduled',queue+'retry',
  prefix+'recovery:'+state['plan_id'],prefix+'control_audit']

def invariant():
 if hashlib.sha256((site/'compose.json').read_bytes()).hexdigest()!=state['compose_hash'] or \
  hashlib.sha256((site/'app.env').read_bytes()).hexdigest()!=state['environment_hash']:
  raise ValueError('deployment_files_changed_after_snapshot')
 current=configuration()
 if {k:{a:v[a] for a in ['container_id','environment_hash','image']} for k,v in current.items()}!=state['configuration']:
  raise ValueError('configuration_changed_after_approval_snapshot')
 return current

def quiet():
 if pairs(prefix+'workers') or pairs(prefix+'admitted') or consumers(False):return False
 if redis('ZRANGEBYSCORE',prefix+'leases',clock(),'+inf') or redis('ZCARD',prefix+'joining'):return False
 return not any(queue_counts().values())

def pending_in_scope():
 counts=queue_counts()
 if any(counts[k] for k in ['active','scheduled','retry']):raise ValueError('queue_not_ready_for_approved_resume')
 ids=redis('LRANGE',queue+'pending',0,-1)
 if len(ids)>10:raise ValueError('unexpected_pending_volume')
 allowed={v['id'] for v in state['sources']}
 for task_id in ids:
  raw=redis('HGET',queue+'t:'+task_id.decode(),'msg')
  if not raw:raise ValueError('pending_task_receipt_missing')
  msg=fields(raw)
  payload=json.loads(msg.get(2,[b'{}'])[0])
  if msg.get(1,[b''])[0]!=b'datasource:sync' or payload.get('data_source_id') not in allowed or payload.get('force_full'):
   raise ValueError('pending_task_outside_approved_scope')
 return [v.decode() for v in ids]

if OPERATION=='self-test':
 name='knowhub-feishu-recovery-selftest-'+uuid.uuid4().hex[:12]
 image=redis_info['Image'];checks=[]
 started=run(['docker','run','-d','--name',name,'--label','knowhub.phase=control-recovery-selftest',
  '--network','none','--read-only','--cap-drop','ALL','--security-opt','no-new-privileges:true',
  '--memory','128m','--cpus','0.25','--pids-limit','64','--user','redis','--tmpfs','/data:rw,nosuid,nodev,size=16m',
  image,'redis-server','--save','','--appendonly','no']).strip()
 def test_redis(*args):return run(['docker','exec',name,'redis-cli','--raw',*map(str,args)]).strip()
 try:
  deadline=time.monotonic()+5
  while True:
   try:
    if test_redis('PING')=='PONG':break
   except ValueError:pass
   if time.monotonic()>deadline:raise ValueError('disposable_redis_not_ready')
   time.sleep(0.2)
  kk=['test:'+str(i) for i in range(1,15)]
  test_redis('SET',kk[0],'other-stop');test_redis('SET',kk[2],'feishu_safety_stop');test_redis('SET',kk[7],'3')
  test_redis('SET','untouched-budget','127')
  def check(script,args,expected):
   value=test_redis('EVAL',script,14,*kk,*args)
   if expected not in value:raise ValueError('recovery_lua_selftest_failed')
   checks.append(expected)
  aa=['plan','3',APPROVAL,FIX]
  check(RESET,aa,'STOP_CHANGED')
  test_redis('SET',kk[0],'automatic_permit_consumer_lost');test_redis('HSET',kk[3],'ghost','fingerprint:old')
  check(RESET,aa,'CONTROLLER_NOT_QUIESCENT');test_redis('HDEL',kk[3],'ghost')
  until=int(time.time()*1000)+60000;test_redis('ZADD',kk[4],until,'ghost')
  check(RESET,aa,'LEASE_NOT_QUIESCENT');test_redis('ZREM',kk[4],'ghost')
  check(RESET,aa,'1')
  if test_redis('GET',kk[0]) or test_redis('GET',kk[2])!='feishu_safety_stop':raise ValueError('reset_did_not_keep_queue_closed')
  for member in ['a','b']:
   test_redis('HSET',kk[3],member,'fingerprint:'+member);test_redis('HSET',kk[5],member,'fingerprint:'+member)
   test_redis('ZADD',kk[4],until,member)
  test_redis('SET',kk[1],'permit','PX',30000)
  bb=['plan','3','wrong','a','fingerprint:a','b','fingerprint:b',APPROVAL]
  check(RESUME,bb,'NO_LIVE_PERMIT');bb[2]='permit'
  test_redis('SET',kk[0],'new-stop');check(RESUME,bb,'STOP_REAPPEARED');test_redis('DEL',kk[0])
  test_redis('ZADD',kk[4],0,'a');check(RESUME,bb,'NO_LIVE_LEASE');test_redis('ZADD',kk[4],until,'a')
  test_redis('HSET',kk[5],'a','wrong-incarnation');check(RESUME,bb,'INCARNATION_CHANGED')
  test_redis('HSET',kk[5],'a','fingerprint:a');check(RESUME,bb,'1')
  if test_redis('GET',kk[2]) or test_redis('GET','untouched-budget')!='127':raise ValueError('resume_damaged_budget_or_pause')
 finally:
  owner=json.loads(run(['docker','inspect',name]))[0]
  if owner['Id']!=started or owner['Config']['Labels'].get('knowhub.phase')!='control-recovery-selftest':
   raise ValueError('disposable_container_ownership_mismatch')
  run(['docker','rm','--force',name])
 print(json.dumps({'status':'SELF_TEST_PASS','checks':checks,'disposable_redis_only':True,
  'network':'none','existing_redis_mutations':False}));raise SystemExit(0)

if OPERATION=='prepare':
 if state:raise ValueError('recovery_already_prepared')
 cfg=configuration();topology();rows=sources()
 if text_value(prefix+'trip')!='automatic_permit_consumer_lost' or text_value(queue+'paused')!='feishu_safety_stop' or text_value(prefix+'permit') is not None:
  raise ValueError('original_closed_state_changed')
 if any(queue_counts().values()):raise ValueError('queue_not_quiescent')
 state={'plan_id':uuid.uuid4().hex,'approval_ref':APPROVAL,'phase':'PREPARED','prepared_at':utc(clock()),
  'configuration':{k:{a:v[a] for a in ['container_id','environment_hash','image']} for k,v in cfg.items()},
  'sources':rows,'indexes':indexes(rows),'compose_hash':hashlib.sha256((site/'compose.json').read_bytes()).hexdigest(),
  'environment_hash':hashlib.sha256((site/'app.env').read_bytes()).hexdigest()}
 save()
elif OPERATION!='status' and not state:raise ValueError('approved_snapshot_required')

if OPERATION=='quiesce':
 if state['phase']!='PREPARED':raise ValueError('prepared_recovery_required')
 invariant();state['phase']='QUIESCING';save()
 p=subprocess.run(compose+['stop','--timeout','45','app','worker2'],stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True,timeout=120)
 if p.returncode:raise ValueError('graceful_stop_failed')
 deadline=time.monotonic()+20
 while not quiet():
  if time.monotonic()>deadline:raise ValueError('graceful_departure_not_proven')
  time.sleep(1)
 if any(v['running'] for v in configuration().values()):raise ValueError('process_still_running')
 state['phase']='QUIESCED';state['quiesced_at']=utc(clock());save()
elif OPERATION=='reset':
 if state['phase']!='QUIESCED' or not quiet():raise ValueError('proven_quiescence_required')
 invariant();rows=sources()
 if rows!=state['sources'] or indexes(rows)!=state['indexes']:raise ValueError('source_or_index_changed_while_stopped')
 revision=text_value(prefix+'membership_revision') or '0'
 redis('EVAL',RESET,len(keys()),*keys(),state['plan_id'],revision,APPROVAL,FIX)
 state['phase']='RESET';state['reset_at']=utc(clock());save()
elif OPERATION=='restart':
 if state['phase']!='RESET':raise ValueError('reset_receipt_required')
 invariant()
 if text_value(prefix+'trip') is not None or text_value(queue+'paused')!='feishu_safety_stop':raise ValueError('reset_state_drift')
 p=subprocess.run(compose+['start','app','worker2'],stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True,timeout=120)
 if p.returncode:raise ValueError('approved_process_restart_failed')
 state['phase']='STARTING';state['restart_at']=utc(clock());save()
elif OPERATION=='resume':
 if state['phase']!='STARTING':raise ValueError('fresh_process_restart_required')
 cfg=invariant()
 if any(v['health']!='healthy' for v in cfg.values()):raise ValueError('fresh_process_health_not_ready')
 members=topology();permit=text_value(prefix+'permit')
 if not re.fullmatch('[a-f0-9]{64}',permit or '') or redis('PTTL',prefix+'permit')<100:raise ValueError('live_permit_not_ready')
 time.sleep(0.4)
 if topology()!=members or text_value(prefix+'permit')!=permit:raise ValueError('topology_not_stable')
 ids=pending_in_scope();revision=text_value(prefix+'membership_revision') or '0';args=[]
 for key,value in sorted(members.items()):args.extend([key.decode(),value.decode()])
 redis('EVAL',RESUME,len(keys()),*keys(),state['plan_id'],revision,permit,*args,APPROVAL,*ids)
 state['phase']='RESUMED';state['resumed_at']=utc(clock());state['pending_allowed_count']=len(ids);save()

rows=sources();current=configuration()
result={'status':state['phase'] if state else 'NOT_PREPARED','at':utc(clock()),'operation':OPERATION,
 'project':project,'source_sha':FIX,'approval_ref':APPROVAL,'plan_id':state['plan_id'] if state else None,
 'trip':text_value(prefix+'trip'),'sync_paused':text_value(queue+'paused') is not None,
 'permit_ttl_ms':redis('PTTL',prefix+'permit'),'queues':queue_counts(),
 'processes':{name:{'running':v['running'],'health':v['health'],'container_hash':mask(v['container_id'])} for name,v in current.items()},
 'sources':[{'source_hash':mask(v['id']),'knowledge_hash':mask(v['knowledge_id']),'last_sync_at':v['last_sync_at'],
  'config_hash':v['config_hash'],'cursor_hash':v['cursor_hash']} for v in rows],
 'indexes':[dict(v,id=mask(v['id'])) for v in indexes(rows)],
 'scope_edited':False,'cursor_reset':False,'budgets_reset':False,'publication_approval_written':False}
connection.close();print(json.dumps(result))
'''


def main():
    parser = argparse.ArgumentParser(description=__doc__, add_help=False)
    parser.add_argument('--operation', choices=['status', 'self-test', 'prepare', 'quiesce', 'reset', 'restart', 'resume'], default='status')
    options, remaining = parser.parse_known_args()
    if '--mode' in remaining:
        raise SystemExit('use explicit recovery --operation, not --mode')
    transport.REMOTE = ('OPERATION=' + repr(options.operation) + '\nRESET=' + repr(RESET) +
                        '\nRESUME=' + repr(RESUME) + '\n' + REMOTE)
    sys.argv = [sys.argv[0], *remaining, '--mode', 'status']
    return transport.main()


if __name__ == '__main__':
    raise SystemExit(main())
