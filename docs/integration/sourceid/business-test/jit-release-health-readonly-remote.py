"""Credential-free default/pilot health and host resource snapshot."""
import os
op=urllib.request.build_opener(urllib.request.ProxyHandler({}));out={'utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'health':{}}
for port in (80,8080,18090):
 for path in ('/health','/api/v1/auth/oidc/config'):
  try:
   with op.open('http://127.0.0.1:'+str(port)+path,timeout=8) as r:out['health'][str(port)+path]=r.status
  except Exception:out['health'][str(port)+path]='FAILED'
out['host_logical_cpus']=os.cpu_count()
out['load_averages']=list(os.getloadavg())
out['capacity_container_limits']={}
for name in ('knowhub-jit-capacity-app','knowhub-jit-capacity-provider'):
 z=subprocess.run(['docker','inspect',name],stdout=subprocess.PIPE,stderr=subprocess.PIPE)
 if z.returncode==0:out['capacity_container_limits'][name]={'nano_cpus':inspect(name,'.HostConfig.NanoCpus'),'memory_bytes':inspect(name,'.HostConfig.Memory')}
print(json.dumps(out))
