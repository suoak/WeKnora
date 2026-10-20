"""Read-only dependency/traffic classification, no endpoint values or identities."""
out={'read_only':True,'dependency_candidates':[],'connections':{},'recent_request_categories':{},'database':{}}
ids=run(['docker','ps','--format','{{.ID}}']).decode().split()
ip_names={}
for cid in ids:
 name=inspect(cid,'.Name').lstrip('/')
 for network in inspect(cid,'.NetworkSettings.Networks').values():
  if network.get('IPAddress'):ip_names[network['IPAddress']]=name
 for k,v in environment(cid).items():
  if re.search(r'(?:172\.30\.232\.42|knowhub\.ruijie\.com\.cn|knowhub-sourceid-business(?:-app-1)?|172\.28\.0\.2)(?::(?:80|8080))?(?:/|$)',v):
   out['dependency_candidates'].append({'container':name,'configuration_key':k})
for line in run(['ss','-Htn','state','established']).decode().splitlines():
 parts=line.split()
 for port in (80,8080):
  if len(parts)>=4 and parts[-2].endswith(':'+str(port)):
   peer=parts[-1].rsplit(':',1)[0].strip('[]');category=ip_names.get(peer,'HOST_LOOPBACK' if peer in ('127.0.0.1','::1') else 'OTHER_SOURCE')
   key=str(port)+':'+category;out['connections'][key]=out['connections'].get(key,0)+1
for cname in ('knowhub-sourceid-business-frontend-1','knowhub-sourceid-business-app-1'):
 z=subprocess.run(['docker','logs','--since','30m','--tail','3000',cname],stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=20)
 counters={}
 for line in (z.stdout+z.stderr).decode('utf-8','replace').splitlines():
  m=re.search(r'(?:GET|POST|PUT|DELETE|OPTIONS)\s+(/[^\s"?]*)',line)
  if not m:continue
  path=m.group(1)
  category='MCP' if 'mcp' in path.lower() else 'AUTH' if '/auth/' in path else 'HEALTH' if path in ('/health','/healthz') else 'KNOWLEDGE' if any(x in path for x in ('knowledge','document','search')) else 'OTHER'
  counters[category]=counters.get(category,0)+1
 out['recent_request_categories'][cname]=counters
for project in ('knowhub-sourceid-business','knowhub-oa-sso-pilot'):
 pg=project+'-postgres-1'
 def sql(q):
  z=subprocess.run(['docker','exec','-i',pg,'sh','-c','exec psql -X -U "$POSTGRES_USER" -d "$POSTGRES_DB" -qAt -v ON_ERROR_STOP=1'],input=('BEGIN READ ONLY;'+q+';ROLLBACK;').encode(),stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=20)
  assert z.returncode==0;return z.stdout.decode().strip()
 tables=json.loads(sql("SELECT COALESCE(json_agg(table_name),'[]') FROM information_schema.tables WHERE table_schema='public' AND (table_name ILIKE '%key%' OR table_name ILIKE '%session%' OR table_name ILIKE '%mcp%')"))
 out['database'][project]={'related_tables':tables,'row_counts':{}}
 for table in tables:
  assert re.fullmatch('[a-z_]+',table)
  out['database'][project]['row_counts'][table]=int(sql('SELECT COUNT(*) FROM '+table))
out['utc']=datetime.datetime.now(datetime.timezone.utc).isoformat();print(json.dumps(out))
