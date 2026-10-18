"""Safe route, SID public metadata, data and network checks. No real login."""
import sys,urllib.error
R=pathlib.Path('/home/aqyfzx/ai/knowhub-jit-fresh-5f988be51-20261018')
sys.path.insert(0,str(R/'real-window'))
from runtime import jq,env,health,IMAGE,REV,atomic
APP='knowhub-company-test-app-1'
A=json.loads(pathlib.Path('/home/aqyfzx/ai/knowhub-company-entry-20261017/private/active-persistent-oa.json').read_text());D=pathlib.Path(A['directory'])
assert inspect(APP,'.Image')==IMAGE and inspect(APP,'.State.Health.Status')=='healthy'
e=env(APP);desired=json.loads((D/'persistent-oa.json').read_text())['services']['app']['environment'];assert all(e.get(k)==v for k,v in desired.items())
routes=health('company-test-verified-email-db3030')
op=urllib.request.build_opener(urllib.request.ProxyHandler({}));anonymous={}
for port in (80,8080):
 for path in ('/api/v1/auth/me','/api/v1/knowledge-bases','/api/v1/mcp-endpoints','/api/v1/me/feishu','/api/v1/system/admin/feishu'):
  try:r=op.open('http://127.0.0.1:'+str(port)+path,timeout=8)
  except urllib.error.HTTPError as x:r=x
  with r:anonymous[str(port)+path]=r.status;assert r.status in (401,403)
 assert json.load(op.open('http://127.0.0.1:'+str(port)+'/api/v1/auth/oidc/config',timeout=8))['enabled'] is True
code=r'''import urllib.request,urllib.parse,json,ssl
class NR(urllib.request.HTTPRedirectHandler):
 def redirect_request(self,*a,**k):return None
o=urllib.request.build_opener(urllib.request.ProxyHandler({}),NR())
with o.open('https://sid.ruijie.com.cn/oidc/.well-known/openid-configuration',timeout=15) as r:d=json.load(r);assert r.status==200
assert d['issuer']=='https://sid.ruijie.com.cn'
for k in ('authorization_endpoint','token_endpoint','userinfo_endpoint','jwks_uri'):
 u=urllib.parse.urlsplit(d[k]);assert u.scheme=='https' and u.hostname=='sid.ruijie.com.cn' and not u.username
with o.open(d['jwks_uri'],timeout=15) as r:j=json.load(r);assert r.status==200 and any(k.get('kty')=='RSA' for k in j['keys'])
print(json.dumps({'dns_tls_discovery':'PASS','jwks':'PASS','rsa_key_present':True,'official_https_only':True,'authorization_token_userinfo_called':False}))
'''
sid=json.loads(run(['docker','exec',APP,'python3','-c',code],timeout=40))
scan="import os,json,hashlib;rows=[]\nfor root,ds,fs in os.walk('/scan'):\n for n in fs:\n  p=os.path.join(root,n)\n  if os.path.isfile(p):\n   with open(p,'rb') as f:h=hashlib.sha256(f.read()).hexdigest()\n   rows.append([os.path.relpath(p,'/scan'),os.path.getsize(p),h])\nprint(json.dumps(sorted(rows)))"
manifest=json.loads(run(['docker','run','--rm','--network','none','--log-driver','none','--mount','type=volume,source=knowhub-company-test-files,target=/scan,readonly','--entrypoint','python3',IMAGE,'-c',scan]))
assert manifest==json.loads((R/'private/original-backup/protected-file-manifest.json').read_text())
counts=jq("SELECT json_build_object('users',(SELECT COUNT(*) FROM users),'bindings',(SELECT COUNT(*) FROM external_identities),'jit',(SELECT COUNT(*) FROM source_id_jit_registrations),'documents',(SELECT COUNT(*) FROM knowledges WHERE deleted_at IS NULL),'chunks',(SELECT COUNT(*) FROM chunks WHERE deleted_at IS NULL AND content<>''),'vectors',(SELECT COUNT(*) FROM embeddings))",False)
assert counts=={'users':3,'bindings':1,'jit':0,'documents':3,'chunks':2717,'vectors':2717}
for n in ('app','postgres','redis','docreader','sid-network'):assert not inspect('knowhub-company-test-'+n+'-1','.HostConfig.PortBindings')
targets=[('172.30.232.42',80),('172.30.232.42',8080)]
for service,port in (('postgres',5432),('redis',6379)):
 targets.append((next(iter(inspect('knowhub-company-test-'+service+'-1','.NetworkSettings.Networks').values()))['IPAddress'],port))
probe="import socket,json;out=[]\nfor host,port in "+repr(targets)+":\n s=socket.socket();s.settimeout(2)\n try:s.connect((host,port));denied=False\n except OSError:denied=True\n finally:s.close()\n out.append({'port':port,'denied':denied})\nprint(json.dumps(out))"
# Gateway may accept TCP then reject HTTP. Use HTTP status for its two ports.
probe="import socket,json,urllib.request,urllib.error;out=[]\nfor host,port in "+repr(targets)+":\n if port in (80,8080):\n  try:\n   with urllib.request.build_opener(urllib.request.ProxyHandler({})).open('http://'+host+':'+str(port)+'/health',timeout=2) as r:denied=False\n  except urllib.error.HTTPError as e:denied=e.code==403\n  except OSError:denied=True\n else:\n  s=socket.socket();s.settimeout(2)\n  try:s.connect((host,port));denied=False\n  except OSError:denied=True\n  finally:s.close()\n out.append({'port':port,'denied':denied})\nprint(json.dumps(out))"
denials=json.loads(run(['docker','run','--rm','--network','bridge','--log-driver','none','--entrypoint','python3',IMAGE,'-c',probe],timeout=20));assert all(x['denied'] for x in denials)
feishu=jq("SELECT json_build_object('application_settings',(SELECT COUNT(*) FROM system_settings WHERE key='integration.feishu.personal_app'),'connections',(SELECT COUNT(*) FROM feishu_connections),'imports',(SELECT COUNT(*) FROM feishu_personal_imports))",False)
out={'result':'PASS','revision':REV,'schema':3030,'dirty':False,'routes':routes,'anonymous_apis':anonymous,'sid_public_https':sid,'counts':counts,'file_contents_match':True,'internal_ports_unpublished':True,'unapproved_bridge_denied':denials,'effective_approved_config_matches':True,'continuous_oa_and_jit':True,'refresh':False,'force_prompt':False,'feishu_preexisting_state':feishu,'real_sid_auth_calls':0,'new_real_identity_writes':0}
atomic(D/'verification-safe.json',out);print(json.dumps(out))
