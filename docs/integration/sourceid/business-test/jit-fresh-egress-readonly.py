APP='knowhub-jit-fresh-app-1'
code=r'''import socket,ssl,urllib.request,urllib.parse,json
host='sid.ruijie.com.cn';dns=bool(socket.getaddrinfo(host,443,type=socket.SOCK_STREAM))
with socket.create_connection((host,443),5) as s:
 with ssl.create_default_context().wrap_socket(s,server_hostname=host) as tls:version=tls.version()
o=urllib.request.build_opener(urllib.request.ProxyHandler({}))
with o.open('https://sid.ruijie.com.cn/oidc/.well-known/openid-configuration',timeout=8) as r:d=json.load(r);ds=r.status
assert d['issuer']=='https://sid.ruijie.com.cn'
u=urllib.parse.urlsplit(d['jwks_uri']);assert u.scheme=='https' and u.hostname==host
with o.open(d['jwks_uri'],timeout=8) as r:k=json.load(r);js=r.status
assert any(x.get('kty')=='RSA' for x in k['keys'])
denied=[]
for ip,p in [('1.1.1.1',443),('172.30.232.42',80),('172.30.232.42',8080)]:
 s=socket.socket();s.settimeout(1)
 try:s.connect((ip,p));v=False
 except OSError:v=True
 finally:s.close()
 denied.append(v)
assert all(denied)
print(json.dumps({'dns':dns,'tls_certificate_and_hostname_verified':True,'tls_version':version,'discovery_status':ds,'jwks_status':js,'unrelated_egress_denied':all(denied),'authorize_token_userinfo_called':False}))
'''
z=subprocess.run(['docker','exec',APP,'python3','-c',code],stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=45)
out=json.loads(z.stdout) if z.returncode==0 else {'result':'FAIL','failure_classes':re.findall(r'^([A-Za-z_]+Error)(?=:|$)',z.stderr.decode('utf-8','replace'),re.M),'raw_error_saved':False}
if z.returncode==0:out['result']='PASS'
print(json.dumps(out))
