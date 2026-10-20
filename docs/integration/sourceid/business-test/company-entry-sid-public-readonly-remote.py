"""Public SID metadata only, no authorize/Token/UserInfo calls."""
code=r'''
import socket,ssl,json,urllib.request,urllib.parse
host='sid.ruijie.com.cn';out={'dns_resolved':bool(socket.getaddrinfo(host,443,type=socket.SOCK_STREAM)),'real_authentication':False}
with socket.create_connection((host,443),8) as sock:
 with ssl.create_default_context().wrap_socket(sock,server_hostname=host) as tls:out['tls_verified']=True;out['tls_version']=tls.version()
o=urllib.request.build_opener(urllib.request.ProxyHandler({}))
with o.open('https://sid.ruijie.com.cn/oidc/.well-known/openid-configuration',timeout=15) as r:d=json.loads(r.read(131072));out['discovery']=r.status
assert d['issuer']=='https://sid.ruijie.com.cn'
u=urllib.parse.urlsplit(d['jwks_uri']);assert u.scheme=='https' and u.hostname==host and not u.query
with o.open(d['jwks_uri'],timeout=15) as r:k=json.loads(r.read(262144));out['jwks']=r.status;out['rsa_present']=any(x.get('kty')=='RSA' for x in k['keys'])
assert out['discovery']==out['jwks']==200 and out['rsa_present'];out['result']='PASS';print(json.dumps(out))
'''
out=json.loads(run(['docker','exec','knowhub-company-test-app-1','python3','-c',code],timeout=45));pathlib.Path('/home/aqyfzx/ai/knowhub-company-entry-20261017/sid-public-result.json').write_text(json.dumps(out));print(json.dumps(out))
