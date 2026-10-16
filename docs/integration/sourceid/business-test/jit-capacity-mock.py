"""Synthetic TLS SID, loopback only; no real identity/provider credentials."""
import base64, http.server, json, pathlib, secrets, ssl, subprocess, threading, time, urllib.parse
R=pathlib.Path('/fixture'); ISSUER='https://127.0.0.1:8443'
CALLBACK='http://knowhub.ruijie.com.cn/api/v1/auth/oidc/callback'
lock=threading.Lock(); codes={}; tokens={}
counts={'authorize':0,'token':0,'profile':0,'reused_code':0,'rate_limited':0,'failed':0,'expired_code':0}
def b64(b):return base64.urlsafe_b64encode(b).decode().rstrip('=')
def jwt(nonce, who):
 now=int(time.time());claims={'iss':ISSUER,'aud':'knowhub','sub':who,'iat':now,'exp':now+300,'nonce':nonce}
 body=b64(b'{"alg":"RS256","kid":"synthetic-key"}')+'.'+b64(json.dumps(claims).encode())
 sig=subprocess.run(['openssl','dgst','-sha256','-sign',str(R/'signing-key.pem')],input=body.encode(),stdout=subprocess.PIPE,stderr=subprocess.DEVNULL,check=True).stdout
 return body+'.'+b64(sig)
class H(http.server.BaseHTTPRequestHandler):
 def log_message(self,*a):pass
 def reply(self,s,v):
  b=json.dumps(v).encode();self.send_response(s);self.send_header('Content-Type','application/json');self.send_header('Content-Length',str(len(b)));self.send_header('Cache-Control','no-store');self.end_headers();self.wfile.write(b)
 def do_GET(self):
  u=urllib.parse.urlsplit(self.path);q=urllib.parse.parse_qs(u.query)
  if u.path=='/jwks':return self.reply(200,json.loads((R/'jwks.json').read_text()))
  if u.path=='/status':
   with lock:v=dict(counts)
   return self.reply(200,v)
  if u.path=='/authorize':
   if q.get('redirect_uri')!=[CALLBACK] or q.get('scope')!=['openid'] or q.get('client_id')!=['knowhub'] or not q.get('state') or not q.get('nonce') or 'prompt' in q:return self.reply(400,{'error':'invalid_request'})
   who=q.get('synthetic_user',[''])[0];case=q.get('synthetic_case',['normal'])[0]
   if not who.startswith('capacity-') or len(who)>80:return self.reply(400,{'error':'invalid_request'})
   c=secrets.token_urlsafe(24)
   with lock:codes[c]=(q['nonce'][0],who,case,time.monotonic());counts['authorize']+=1
   self.send_response(302);self.send_header('Location',CALLBACK+'?'+urllib.parse.urlencode({'code':c,'state':q['state'][0]}));self.end_headers();return
  if u.path=='/profile':
   with lock:item=tokens.get(self.headers.get('Authorization','').removeprefix('Bearer '));counts['profile']+=1
   if not item:return self.reply(401,{'error':'invalid_token'})
   who,case=item
   if case=='delay':time.sleep(.2)
   return self.reply(200,{'sub':who,'attributes':{'RJGH':who,'RJEMAIL':who+'@example.test'}})
  return self.reply(404,{'error':'not_found'})
 def do_POST(self):
  if self.path!='/token':return self.reply(404,{'error':'not_found'})
  form=urllib.parse.parse_qs(self.rfile.read(min(int(self.headers.get('Content-Length','0')),8192)).decode())
  expected='Basic '+base64.b64encode(b'knowhub:synthetic-business-secret').decode()
  if self.headers.get('Authorization')!=expected or form.get('redirect_uri')!=[CALLBACK] or form.get('grant_type')!=['authorization_code'] or form.get('client_id')!=['knowhub'] or 'client_secret' in form or 'code_verifier' in form:return self.reply(401,{'error':'invalid_client'})
  with lock:item=codes.pop(form.get('code',[''])[0],None);counts['token']+=1
  if not item:
   with lock:counts['reused_code']+=1
   return self.reply(400,{'error':'invalid_grant'})
  nonce,who,case,issued=item
  if time.monotonic()-issued>90:
   with lock:counts['expired_code']+=1
   return self.reply(400,{'error':'invalid_grant'})
  if case=='limited':
   with lock:counts['rate_limited']+=1
   return self.reply(429,{'error':'temporarily_unavailable'})
  if case=='failure':
   with lock:counts['failed']+=1
   return self.reply(401,{'error':'invalid_client'})
  if case=='delay':time.sleep(.4)
  t=secrets.token_urlsafe(24)
  with lock:tokens[t]=(who,case)
  return self.reply(200,{'access_token':t,'id_token':jwt(nonce,who),'token_type':'Bearer'})
class S(http.server.ThreadingHTTPServer):request_queue_size=256;daemon_threads=True
s=S(('127.0.0.1',8443),H);ctx=ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER);ctx.load_cert_chain(str(R/'ca.pem'),str(R/'tls-key.pem'));s.socket=ctx.wrap_socket(s.socket,server_side=True);s.serve_forever()
