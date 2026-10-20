import traceback
b=pathlib.Path('/home/aqyfzx/ai/knowhub-oa-sso-pilot-832b6652a');app='knowhub-oa-sso-pilot-app-1';gateway='knowhub-oa-sso-pilot-sid-network-1';out={'result':'FAIL','real_authentication':False}
try:
 assert json.loads((b/'sid-egress-apply-result.json').read_text())['result']=='PASS'
 code=r'''
import socket,ssl,json,urllib.request,urllib.parse,time
out={};host='sid.ruijie.com.cn'
out['dns']=sorted({x[4][0] for x in socket.getaddrinfo(host,443,type=socket.SOCK_STREAM)})
start=time.monotonic()
with socket.create_connection((host,443),5) as sock:
 with ssl.create_default_context().wrap_socket(sock,server_hostname=host) as tls:
  out['tls']={'certificate_validation':True,'hostname_validation':True,'version':tls.version(),'remote_address':list(tls.getpeername()),'milliseconds':round((time.monotonic()-start)*1000)}
class NR(urllib.request.HTTPRedirectHandler):
 def redirect_request(self,*a,**k):return None
o=urllib.request.build_opener(urllib.request.ProxyHandler({}),NR())
with o.open('https://sid.ruijie.com.cn/oidc/.well-known/openid-configuration',timeout=10) as r:
 d=json.loads(r.read(131072));out['discovery_status']=r.status
assert d['issuer']=='https://sid.ruijie.com.cn'
for k in ('authorization_endpoint','token_endpoint','userinfo_endpoint','jwks_uri'):
 u=urllib.parse.urlsplit(d[k]);assert u.scheme=='https' and u.hostname==host
with o.open(d['jwks_uri'],timeout=10) as r:
 keys=json.loads(r.read(262144));out['jwks']={'status':r.status,'keys':len(keys['keys']),'rsa_present':any(x.get('kty')=='RSA' for x in keys['keys'])}
out['auth_endpoints']={d[k].split(host)[1]:'TLS_SHARED_HOST_VERIFIED_NOT_CALLED' for k in ('authorization_endpoint','token_endpoint','userinfo_endpoint')}
out['denied']=[]
for ip,port in [('1.1.1.1',443),('39.107.195.76',80),('172.30.232.42',80),('172.30.232.42',8080)]:
 s=socket.socket();s.settimeout(1)
 try:s.connect((ip,port));denied=False
 except OSError:denied=True
 finally:s.close()
 out['denied'].append({'target':ip,'port':port,'denied':denied})
assert all(x['denied'] for x in out['denied'])
out['ipv6_interfaces']=open('/proc/net/if_inet6').read().strip();assert not out['ipv6_interfaces']
print(json.dumps(out))
'''
 out['app_network']=json.loads(run(['docker','exec','-i',app,'python3','-c',code],timeout=40))
 op=urllib.request.build_opener(urllib.request.ProxyHandler({}))
 out['frontend_api']={}
 for path in ('/login','/health','/api/v1/auth/oidc/config'):
  with op.open('http://127.0.0.1:18090'+path,timeout=5) as r:
   body=r.read(131072);out['frontend_api'][path]=r.status
   if path.endswith('/config'):assert json.loads(body)['enabled'] is False
 out['app']=metadata(app);out['gateway']=metadata(gateway)
 assert not inspect(app,'.HostConfig.PortBindings') and not inspect(gateway,'.HostConfig.PortBindings')
 assert inspect(app,'.HostConfig.NetworkMode')=='container:'+inspect(gateway,'.Id')
 ips=inspect(gateway,'.NetworkSettings.Networks');probe=[(v['IPAddress'],8080) for v in ips.values()]
 probe+=[(inspect('knowhub-oa-sso-pilot-postgres-1','.NetworkSettings.Networks')['knowhub-oa-sso-pilot_isolated']['IPAddress'],5432)]
 script="import socket,json;out=[]\nfor ip,port in "+repr(probe)+":\n s=socket.socket();s.settimeout(.5)\n try:s.connect((ip,port));denied=False\n except OSError:denied=True\n finally:s.close()\n out.append({'target':ip,'port':port,'denied':denied})\nprint(json.dumps(out))"
 out['unapproved_bridge_source']=json.loads(run(['docker','run','--rm','--network','bridge','--log-driver','none','--entrypoint','python3',out['app']['image_id'],'-c',script],timeout=15));assert all(x['denied'] for x in out['unapproved_bridge_source'])
 out['result']='PASS'
except Exception as e:out.update(failure_class=type(e).__name__,failure_lines=[f.lineno for f in traceback.extract_tb(e.__traceback__) if f.filename=='<stdin>'])
(pathlib.Path('/home/aqyfzx/ai/knowhub-enterprise-jit-f40b1602f')/'network-result.json').write_text(json.dumps(out,indent=2));print(json.dumps(out))
