R=pathlib.Path('/home/aqyfzx/ai/knowhub-jit-fresh-5f988be51-20261018')
c=json.loads((R/'compose.json').read_text());c['services']['redis']['command']=['sh','-c','exec redis-server --requirepass "$$FRESH_REDIS_PASSWORD" --appendonly yes'];(R/'compose.json').write_text(json.dumps(c,indent=2))
run(['docker','compose','-p','knowhub-jit-fresh','--project-directory',str(R),'-f',str(R/'compose.json'),'up','-d','--no-deps','--no-build','--pull','never','redis'],timeout=60)
code=r'''import socket,os,json,time
for _ in range(15):
 try:s=socket.create_connection(('redis',6379),1);break
 except OSError:time.sleep(.5)
else:raise RuntimeError('redis_not_ready')
def send(args):
 data=b'*'+str(len(args)).encode()+b'\r\n'
 for a in args:
  b=a.encode();data+=b'$'+str(len(b)).encode()+b'\r\n'+b+b'\r\n'
 s.sendall(data);return s.recv(512)
# Replace literal escapes in a source-only protocol fixture, not credentials.
'''
# RESP uses CRLF; never output responses or credentials.
code+="auth=send(['AUTH',os.environ['REDIS_PASSWORD']]);pong=send(['PING']);s.close();assert auth==b'+OK\\r\\n' and pong==b'+PONG\\r\\n';print(json.dumps({'result':'PASS','authenticated_redis_ping':True,'credentials_exported':False}))"
print(run(['docker','exec','knowhub-jit-fresh-app-1','python3','-c',code],timeout=30).decode())
