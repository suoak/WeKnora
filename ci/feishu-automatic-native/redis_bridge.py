"""Bounded RESP bridge for this disposable APP stack only; no production use."""
import base64,json,os,re,socket,sys,time
assert os.getuid()==1000
settings=json.loads(sys.stdin.readline())
assert re.fullmatch(r'feishu:safety:\{automatic-native-[0-9]+\}:',settings['prefix'])
assert settings['worker']==socket.gethostname()+':1'
sock=socket.create_connection(('redis',6379),timeout=3);stream=sock.makefile('rb');gap=None

def response():
 line=stream.readline();kind=line[:1];raw=line[1:-2]
 if kind==b'+':return raw.decode()
 if kind==b'-':raise RuntimeError('isolated_redis_command_error')
 if kind==b':':return int(raw)
 if kind==b'$':
  n=int(raw)
  if n<0:return None
  assert n<262144
  value=stream.read(n);assert len(value)==n and stream.read(2)==b'\r\n';return value
 if kind==b'*':
  n=int(raw);assert -1<=n<1024;return None if n==-1 else [response() for _ in range(n)]
 raise RuntimeError('invalid_redis_response')

def command(values):
 data=[x if isinstance(x,bytes) else str(x).encode() for x in values]
 sock.sendall(b'*'+str(len(data)).encode()+b'\r\n'+b''.join(b'$'+str(len(x)).encode()+b'\r\n'+x+b'\r\n' for x in data));return response()

def encode(value):
 if isinstance(value,bytes):return {'base64_bytes':base64.b64encode(value).decode()}
 if isinstance(value,list):return [encode(x) for x in value]
 return value

def request(values):
 global gap
 name=values[0]
 if name in ['GET','HGETALL','ZRANGE','PTTL','TIME','EXISTS','XLEN','ZSCORE','XRANGE']:
  if name!='TIME':assert values[1].startswith(settings['prefix']) or values[1]=='asynq:servers' or re.fullmatch(r'asynq:servers:\{'+re.escape(settings['worker'])+r':[0-9a-f-]{36}\}',values[1])
  if name=='XRANGE':assert values[-2:] in [['COUNT',32],['COUNT',64]]
  return command(values)
 if name=='BEGIN_OWN_METADATA_GAP':
  assert gap is None and len(values)==2
  key=values[1];assert re.fullmatch(r'asynq:servers:\{'+re.escape(settings['worker'])+r':[0-9a-f-]{36}\}',key)
  start=time.monotonic()
  raw=command(['GET',key]);ttl=command(['PTTL',key]);score=command(['ZSCORE','asynq:servers',key])
  assert isinstance(raw,bytes) and 0<len(raw)<8192 and 1000<ttl<=15000 and score
  removed=command(['EVAL',"if redis.call('GET',KEYS[1])~=ARGV[1] then return 0 end;return redis.call('DEL',KEYS[1])",1,key,raw])
  assert removed==1
  gap={'key':key,'raw':raw,'ttl':ttl,'score':score,'start':start}
  return {'deleted_own_metadata':True,'original_ttl_ms':ttl}
 if name=='END_OWN_METADATA_GAP':
  assert gap is not None and len(values)==1
  elapsed=int((time.monotonic()-gap['start'])*1000);remaining=gap['ttl']-elapsed
  assert 0<=elapsed<2000 and remaining>0
  # Never overwrite a genuine newer heartbeat or extend the captured TTL.
  result=command(['EVAL',"local raw=redis.call('GET',KEYS[1]);if raw then return 2 end;local score=redis.call('ZSCORE',KEYS[2],KEYS[1]);if score and tonumber(score)~=tonumber(ARGV[3]) then return redis.error_reply('REGISTRY_CHANGED') end;redis.call('SET',KEYS[1],ARGV[1],'PX',ARGV[2]);if not score then redis.call('ZADD',KEYS[2],ARGV[3],KEYS[1]) end;return 1",2,gap['key'],'asynq:servers',gap['raw'],remaining,gap['score']])
  assert result in (1,2)
  gap=None
  return {'elapsed_ms':elapsed,'restore_class':'captured_real_bytes_remaining_ttl' if result==1 else 'newer_real_heartbeat_preserved'}
 raise RuntimeError('isolated_operation_not_allowed')

command(['AUTH',settings['password']]);print(json.dumps({'ready':True}),flush=True)
try:
 for line in sys.stdin:print(json.dumps(encode(request(json.loads(line)))),flush=True)
finally:
 # A failed controller assertion must not leave our own metadata gap behind.
 if gap is not None:
  try:request(['END_OWN_METADATA_GAP'])
  except Exception:pass
 stream.close();sock.close()
