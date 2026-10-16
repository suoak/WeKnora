"""Read current containers after restore; paths/stage enums only."""
out={'result':'PASS','since_utc':'2026-10-16T03:46:00Z','utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'containers':{},'new_requests_sent':False,'raw_logs_saved':False}
allowed_paths={'/api/v1/auth/oidc/url','/api/v1/auth/oidc/callback','/api/v1/auth/oidc/result','/api/v1/auth/oidc/config'}
for name in ('knowhub-sourceid-business-app-1','knowhub-sourceid-business-frontend-1'):
 p=subprocess.run(['docker','logs','--since',out['since_utc'],'--until',out['utc'],'--tail','3000',name],stdout=subprocess.PIPE,stderr=subprocess.STDOUT,timeout=20);assert p.returncode==0 and len(p.stdout)<=2097152
 rows={};stages=[]
 for line in p.stdout.decode('utf-8','replace').splitlines():
  m=re.search(r'"(GET|POST) ([^\s"]+) HTTP/[0-9.]+"\s+(\d{3})',line)
  if m:
   path=urllib.parse.urlsplit(m.group(2)).path
   if path in allowed_paths:
    key=m.group(1)+' '+path+' '+m.group(3);rows[key]=rows.get(key,0)+1
  k=line.find('{"event"')
  if k<0:continue
  try:e=json.JSONDecoder().raw_decode(line[k:])[0]
  except (ValueError,TypeError):continue
  if isinstance(e,dict) and e.get('event')=='sourceid_stage' and e.get('stage') in {'authorize','callback_state','token','id_token','userinfo','identity_mapping','local_session','callback_result'} and e.get('category') in {'none','rejected','token_http_401','nonce_mismatch','enrollment_required','identity_conflict','account_disabled','identity_fields_missing','subject_mismatch','id_token_verification'} and re.fullmatch(r'[a-f0-9-]{36}',e.get('trace','')) and type(e.get('pass')) is bool and type(e.get('duration_ms')) is int:
   stages.append({k:e[k] for k in ('trace','stage','pass','duration_ms','category')})
 out['containers'][name]={'safe_path_status_counts':rows,'safe_stages':stages,'bytes_scanned':len(p.stdout),'tail_limit':3000}
print(json.dumps(out))
