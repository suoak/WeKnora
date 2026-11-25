"""Authenticated READ-ONLY RESP bridge in own APP container UID1000."""
import base64,json,os,socket,sys
assert os.getuid()==1000
settings=json.loads(sys.stdin.readline());sock=socket.create_connection(('redis',6379),timeout=3);stream=sock.makefile('rb')
def response():
 line=stream.readline();kind=line[:1];raw=line[1:-2]
 if kind==b'+':return raw.decode()
 if kind==b'-':raise RuntimeError('isolated_redis_read_error')
 if kind==b':':return int(raw)
 if kind==b'$':
  n=int(raw)
  if n<0:return None
  assert n<65536
  value=stream.read(n);assert stream.read(2)==b'\r\n';return value
 if kind==b'*':
  n=int(raw);assert n<256;return [response() for _ in range(n)]
 raise RuntimeError('invalid_redis_read_response')
def command(values):
 assert values[0] in ['AUTH','GET','HGETALL','ZRANGE','PTTL','TIME','EXISTS','XLEN']
 data=[str(x).encode() for x in values];sock.sendall(b'*'+str(len(data)).encode()+b'\r\n'+b''.join(b'$'+str(len(x)).encode()+b'\r\n'+x+b'\r\n' for x in data));return response()
def encode(value):
 if isinstance(value,bytes):return {'base64_bytes':base64.b64encode(value).decode()}
 if isinstance(value,list):return [encode(x) for x in value]
 return value
command(['AUTH',settings['password']]);print(json.dumps({'ready':True}),flush=True)
try:
 for line in sys.stdin:
  print(json.dumps(encode(command(json.loads(line)))),flush=True)
finally:stream.close();sock.close()
