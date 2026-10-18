out={}
for service in ('app','frontend','postgres','redis','docreader'):
 n='knowhub-jit-fresh-'+service+'-1';s=inspect(n,'.State');out[service]={k:s.get(k) for k in ('Status','Running','ExitCode','Restarting','OOMKilled')};out[service]['health']=s.get('Health',{}).get('Status')
 if service=='app':
  records=s.get('Health',{}).get('Log',[])
  out[service]['health_exit_codes']=[x['ExitCode'] for x in records]
  text=' '.join(x.get('Output','') for x in records).lower();out[service]['health_categories']=[x for x in ('connection refused','not found','timeout','healthy','unhealthy') if x in text]
print(json.dumps(out))
