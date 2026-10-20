"""Read-only bounded path/status counts; never persist raw access logs."""
p=subprocess.run(['docker','logs','--since','2026-10-16T06:36:00Z','--tail','3000','knowhub-sourceid-business-frontend-1'],stdout=subprocess.PIPE,stderr=subprocess.STDOUT,timeout=15)
assert p.returncode==0 and len(p.stdout)<2097152
counts={}
for line in p.stdout.decode('utf-8','replace').splitlines():
 m=re.search(r'"(GET|POST) ([^\s"]+) HTTP/[0-9.]+"\s+(\d{3})',line)
 if not m:continue
 path=urllib.parse.urlsplit(m.group(2)).path
 if path not in ('/api/v1/auth/oidc/url','/api/v1/auth/oidc/callback','/api/v1/auth/oidc/result'):continue
 k=m.group(1)+' '+path+' '+m.group(3);counts[k]=counts.get(k,0)+1
print(json.dumps({'utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'since_utc':'2026-10-16T06:36:00Z','safe_path_status_counts':counts,'raw_saved':False,'tail_limit':3000}))
