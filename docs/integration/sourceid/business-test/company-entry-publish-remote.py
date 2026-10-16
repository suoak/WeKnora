"""Publish approved green 80/8080 gateway, rollback only old entry containers."""
import os,time,ipaddress,traceback
os.umask(0o077)
B=pathlib.Path('/home/aqyfzx/ai/knowhub-company-entry-20261017');E=B/'entry';P='knowhub-company-entry';GREEN='knowhub-company-test'
FRONT='sha256:ab7845c89fadeb17ad2b68bd924e892f948b893781a50ab79e48385c319ddbe1'
out={'result':'FAIL','real_sid_login':False,'production_accessed':False,'rollback_triggered':False};stage='gate';cut=False
cmd=['docker','compose','-p',P,'--project-directory',str(E),'-f',str(E/'compose.json')]
class NoRedirect(urllib.request.HTTPRedirectHandler):
 def redirect_request(self,*args,**kwargs):return None
op=urllib.request.build_opener(urllib.request.ProxyHandler({}),NoRedirect())
def req(port,path):
 r=urllib.request.Request('http://127.0.0.1:'+str(port)+path,headers={'Host':'knowhub.ruijie.com.cn'})
 try:r=op.open(r,timeout=15)
 except urllib.error.HTTPError as error:r=error
 data=r.read(2000000);result={'status':r.code,'environment':r.headers.get('X-KnowHub-Environment')}
 if path.endswith('/callback'):
  loc=urllib.parse.urlsplit(r.headers.get('Location',''));assert loc.hostname in (None,'knowhub.ruijie.com.cn');result['redirect_path']=loc.path;result['invalid_state']='invalid_state' in urllib.parse.parse_qs(loc.fragment).get('oidc_error',[])
 if path.endswith('/config'):
  value=json.loads(data);result['oidc_enabled']=value.get('enabled');result['basic_encoding']=value.get('sourceid_basic_encoding','NOT_CAPTURED')
 return result,data
def checks(port):
 found={}
 for path in ('/health','/login','/api/v1/auth/oidc/config','/api/v1/auth/oidc/callback'):
  found[path],data=req(port,path)
  assert found[path]['environment']=='company-test-f40-db3029'
  assert found[path]['status']==(302 if path.endswith('/callback') else 200)
  if path.endswith('/config'):assert found[path]['oidc_enabled'] is False
  if path.endswith('/callback'):assert found[path]['invalid_state']
  if path=='/login' and port!=8080:
   assets=re.findall(r'(?:src|href)="(/assets/[^"?]+\.(?:js|css))"',data.decode('utf-8','replace'))
   if assets:
    info,_=req(port,assets[0]);assert info['status']==200;found['static_asset']=info
 if port!=8080:assert 'static_asset' in found
 return found
def rollback():
 subprocess.run(['docker','stop',P+'-entry-1'],stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=45)
 run(['docker','start','knowhub-sourceid-business-app-1'])
 for _ in range(75):
  if inspect('knowhub-sourceid-business-app-1','.State.Health.Status')=='healthy':break
  time.sleep(1)
 else:raise RuntimeError('old_app_recovery_failed')
 run(['docker','start','knowhub-sourceid-business-frontend-1'])
 run(['docker','exec','knowhub-sourceid-business-frontend-1','nginx','-s','reload'])
try:
 prep=json.loads((B/'prepare-result.json').read_text());assert prep['result']=='PASS'
 assert inspect(GREEN+'-app-1','.State.Health.Status')=='healthy'
 prior=json.loads((B/'private/pre-switch/containers.json').read_text())
 for project,items in prior.items():
  for s,m in items.items():assert inspect(project+'-'+s+'-1','.Id')==m['id'] and inspect(m['id'],'.State.Running')
 source=ipaddress.ip_address(os.environ['SSH_CONNECTION'].split()[0]);assert source.is_private
 allowed=[str(source)]
 for n in (GREEN+'_edge',GREEN+'_isolated'):
  ipam=json.loads(run(['docker','network','inspect','--format','{{json .IPAM.Config}}',n]))
  allowed.extend(x['Gateway'] for x in ipam if x.get('Gateway'))
 E.mkdir(mode=0o700,exist_ok=True)
 allow='\n'.join('allow '+x+';' for x in allowed+['127.0.0.1'])+'\ndeny all;'
 def server(port,upstream):
  return 'server { listen '+str(port)+'; '+allow+'\nadd_header X-KnowHub-Environment company-test-f40-db3029 always; location / { set $upstream '+upstream+'; proxy_pass http://$upstream; proxy_http_version 1.1; proxy_set_header Host $http_host; proxy_set_header X-Forwarded-Host $http_host; proxy_set_header X-Forwarded-Proto $scheme; proxy_set_header X-Forwarded-For $remote_addr; proxy_set_header Upgrade $http_upgrade; proxy_set_header Connection $connection_upgrade; proxy_buffering off; proxy_request_buffering off; proxy_read_timeout 3600s; } }'
 conf='worker_processes auto; pid /tmp/nginx.pid; error_log /dev/null; events { worker_connections 1024; } http { access_log off; resolver 127.0.0.11 ipv6=off valid=5s; map $http_upgrade $connection_upgrade { default upgrade; "" close; } client_max_body_size 1024m; proxy_temp_path /tmp/proxy; '+server(80,'frontend:80')+server(8080,'app:8080')+' }'
 (E/'nginx.conf').write_text(conf)
 compose={'services':{'entry':{'image':FRONT,'entrypoint':['nginx'],'command':['-g','daemon off;'],'restart':'unless-stopped','read_only':True,'tmpfs':['/tmp','/var/cache/nginx','/var/run'],'logging':{'driver':'none'},'volumes':[str(E/'nginx.conf')+':/etc/nginx/nginx.conf:ro'],'networks':['edge','isolated'],'ports':['127.0.0.1:18092:80','127.0.0.1:18093:8080']}},'networks':{'edge':{'external':True,'name':GREEN+'_edge'},'isolated':{'external':True,'name':GREEN+'_isolated'}}}
 (E/'compose.json').write_text(json.dumps(compose,indent=2))
 stage='staged_gateway';run(cmd+['up','-d','--no-build','--pull','never'],timeout=75)
 for _ in range(30):
  try:
   info,_=req(18092,'/health')
   if info['status']==200:break
  except (OSError,urllib.error.URLError):pass
  time.sleep(.5)
 else:raise RuntimeError('staged_gateway_health_timeout')
 out['staged_frontend']=checks(18092)
 # Direct APP has no SPA /login route. Verify APP API independently.
 direct={}
 for path in ('/health','/api/v1/auth/oidc/config','/api/v1/auth/oidc/callback'):
  direct[path],_=req(18093,path);assert direct[path]['status']==(302 if path.endswith('/callback') else 200)
  assert direct[path]['environment']=='company-test-f40-db3029'
 out['staged_app']=direct
 (B/'private/rollback-entry.sh').write_text('#!/bin/sh\nset -eu\ndocker stop '+P+'-entry-1\ndocker start knowhub-sourceid-business-app-1\nwhile [ "$(docker inspect --format \'{{.State.Health.Status}}\' knowhub-sourceid-business-app-1)" != healthy ]; do sleep 1; done\ndocker start knowhub-sourceid-business-frontend-1\ndocker exec knowhub-sourceid-business-frontend-1 nginx -s reload\n')
 (B/'private/rollback-entry.sh').chmod(0o700)
 stage='switch_80_8080';cut=True
 run(['docker','stop','--time','30','knowhub-sourceid-business-frontend-1','knowhub-sourceid-business-app-1'],timeout=90)
 compose['services']['entry']['ports']=['0.0.0.0:80:80','0.0.0.0:8080:8080']
 (E/'compose.json').write_text(json.dumps(compose,indent=2));run(cmd+['up','-d','--no-deps','--no-build','--pull','never','--force-recreate','entry'],timeout=75)
 stage='published_health'
 for _ in range(30):
  try:
   info,_=req(80,'/health')
   if info['status']==200:break
  except (OSError,urllib.error.URLError):pass
  time.sleep(.5)
 else:raise RuntimeError('published_gateway_health_timeout')
 out['frontend_80']=checks(80);out['app_8080']={}
 for path in ('/health','/api/v1/auth/oidc/config','/api/v1/auth/oidc/callback'):
  info,_=req(8080,path);assert info['status']==(302 if path.endswith('/callback') else 200);assert info['environment']=='company-test-f40-db3029';out['app_8080'][path]=info
 for project,items in prior.items():
  for s,m in items.items():
   assert inspect(m['id'],'.Id')==m['id']
   if project!='knowhub-sourceid-business' or s not in ('app','frontend'):assert inspect(m['id'],'.State.Running')
 assert not inspect('knowhub-sourceid-business-app-1','.State.Running') and not inspect('knowhub-sourceid-business-frontend-1','.State.Running')
 out.update(result='PASS',stage='published',green_project=GREEN,entry_project=P,app_revision='f40b1602f69ce5d88e1e87dc3cefae36e9bde356',frontend_revision='832b6652adf541892545a7eac8b68733f846ab95',db_version=3029,rollback_ready=True,allowed_sources='current SSH client address + explicit Docker host gateways + loopback; IPv4 listeners only',old_app_frontend_preserved_stopped=True,other_original_and_pilot_containers_unchanged=True)
except Exception as e:
 out.update(failed_stage=stage,failure_class=type(e).__name__,failure_lines=[f.lineno for f in traceback.extract_tb(e.__traceback__) if f.filename==__file__])
 if cut:
  out['rollback_triggered']=True
  try:rollback();out['old_entry_restored']=True
  except Exception:out['old_entry_restored']=False
out['utc']=datetime.datetime.now(datetime.timezone.utc).isoformat();(B/'publish-result.json').write_text(json.dumps(out,indent=2));print(json.dumps(out))
