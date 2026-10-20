"""Synthetic provider, accessible only inside shared network-none namespace."""
import base64,http.server,json,pathlib,secrets,ssl,subprocess,time,urllib.parse
ROOT=pathlib.Path('/fixture');ISSUER='https://127.0.0.1:18443'
CALLBACK='http://knowhub.ruijie.com.cn/api/v1/auth/oidc/callback'
mode='admin';codes={};counts={'token':0,'userinfo':0,'authorization':0}
def b64(b):return base64.urlsafe_b64encode(b).decode().rstrip('=')
def claims():return {'sub':'synthetic-'+mode,'attributes':{'RJGH':'SYNTHETIC-'+mode.upper(),'RJEMAIL':'isolated-'+mode+'@example.test'}}
def jwt(nonce):
    p=dict(claims(),iss=ISSUER,aud='knowhub',iat=int(time.time()),nbf=int(time.time())-1,exp=int(time.time())+120,nonce=nonce)
    raw=b64(json.dumps({'alg':'RS256','kid':'isolated-key'}).encode())+'.'+b64(json.dumps(p).encode())
    sig=subprocess.run(['openssl','dgst','-sha256','-sign',str(ROOT/'key.pem')],input=raw.encode(),stdout=subprocess.PIPE,stderr=subprocess.DEVNULL,check=True).stdout
    return raw+'.'+b64(sig)
class Handler(http.server.BaseHTTPRequestHandler):
    def log_message(self,*a):pass
    def reply(self,status,data):
        b=json.dumps(data).encode();self.send_response(status);self.send_header('Content-Type','application/json');self.send_header('Content-Length',str(len(b)));self.end_headers();self.wfile.write(b)
    def do_GET(self):
        u=urllib.parse.urlsplit(self.path);q=urllib.parse.parse_qs(u.query)
        if u.path=='/.well-known/openid-configuration':return self.reply(200,{'issuer':ISSUER,'authorization_endpoint':ISSUER+'/authorize','token_endpoint':ISSUER+'/token','userinfo_endpoint':ISSUER+'/userinfo','jwks_uri':ISSUER+'/jwks','id_token_signing_alg_values_supported':['RS256']})
        if u.path=='/jwks':return self.reply(200,json.loads((ROOT/'jwks.json').read_text()))
        if u.path=='/status':return self.reply(200,{'counts':counts})
        if u.path=='/userinfo':
            assert self.headers.get('Authorization')=='Bearer synthetic-access'
            counts['userinfo']+=1;return self.reply(200,claims())
        if u.path=='/authorize':
            counts['authorization']+=1
            assert q['client_id']==['knowhub'] and q['redirect_uri']==[CALLBACK] and q['scope']==['openid']
            assert q.get('nonce') and q.get('state') and 'code_challenge' not in q
            if mode=='deny':
                self.send_response(302);self.send_header('Location',CALLBACK+'?error=access_denied&'+urllib.parse.urlencode({'state':q['state'][0]}));self.end_headers();return
            code=secrets.token_urlsafe(24);codes[code]=q['nonce'][0]
            self.send_response(302);self.send_header('Location',CALLBACK+'?'+urllib.parse.urlencode({'code':code,'state':q['state'][0]}));self.end_headers();return
        return self.reply(404,{'error':'not_found'})
    def do_POST(self):
        global mode
        data=self.rfile.read(min(int(self.headers.get('Content-Length','0')),8192))
        if self.path=='/mode':
            mode=json.loads(data)['mode'];assert mode in ('admin','viewer','deny');return self.reply(200,{'ok':True})
        if self.path!='/token':return self.reply(404,{'error':'not_found'})
        counts['token']+=1;q=urllib.parse.parse_qs(data.decode())
        assert len(self.headers.get_all('Authorization'))==1
        assert self.headers['Authorization']=='Basic '+base64.b64encode(b'knowhub:synthetic-only').decode()
        assert q['client_id']==['knowhub'] and q['redirect_uri']==[CALLBACK] and q['grant_type']==['authorization_code']
        assert 'client_secret' not in q and 'code_verifier' not in q
        nonce=codes.pop(q['code'][0],None)
        if nonce is None:return self.reply(400,{'error':'invalid_grant'})
        return self.reply(200,{'access_token':'synthetic-access','id_token':jwt(nonce),'token_type':'Bearer','expires_in':120})
server=http.server.ThreadingHTTPServer(('127.0.0.1',18443),Handler)
ctx=ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER);ctx.load_cert_chain(str(ROOT/'ca.pem'),str(ROOT/'key.pem'));server.socket=ctx.wrap_socket(server.socket,server_side=True);server.serve_forever()
