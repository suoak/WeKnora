"""Read-only bounded in-memory audit. Never export raw access logs or queries."""
root=pathlib.Path('/home/aqyfzx/ai/knowhub-prompt-login-real-20261016')
receipt=json.loads((root/'result.json').read_text());assert receipt['restore']=='PASS'
app='knowhub-sourceid-business-app-1';front='knowhub-sourceid-business-frontend-1'
start='2026-10-16T03:40:00Z';end='2026-10-16T03:46:00Z'
paths={'/api/v1/auth/oidc/url','/api/v1/auth/oidc/start','/api/v1/auth/oidc/callback','/api/v1/auth/oidc/result','/api/v1/auth/oidc/config','/login','/api/v1/auth/me'}
summary={};scans={}
for name in (app,front):
 p=subprocess.run(['docker','logs','--since',start,'--until',end,'--tail','3000',name],stdout=subprocess.PIPE,stderr=subprocess.STDOUT,timeout=20);assert p.returncode==0
 raw=p.stdout
 assert len(raw)<=2*1024*1024
 rows={};structured={};parsed=0
 for line in raw.decode('utf-8','replace').splitlines():
  # Extract only method, then discard query before selecting fixed path.
  m=re.search(r'"(GET|POST|HEAD|OPTIONS) ([^\s"]+) HTTP/[0-9.]+"\s+(\d{3})',line)
  if m:
   path=urllib.parse.urlsplit(m.group(2)).path
   if path in paths:
    key=m.group(1)+' '+path+' '+m.group(3);rows[key]=rows.get(key,0)+1;parsed+=1
  if '"event":"sourceid_' in line:
   m=re.search(r'"event":"(sourceid_[a-z_]+)"',line)
   if m and m.group(1) in {'sourceid_stage','sourceid_token_request','sourceid_token_send','sourceid_token_http','sourceid_reauthentication','sourceid_diagnostics_limit'}:
    structured[m.group(1)]=structured.get(m.group(1),0)+1
 summary[name]={'fixed_path_status_counts':rows,'structured_event_counts':structured}
 scans[name]={'bytes':len(raw),'line_count':len(raw.splitlines()),'tail_limit':3000,'byte_limit':2097152,'raw_saved':False,'paths_not_query_exported':True}
q="""BEGIN READ ONLY;
SELECT json_build_object('version',version,'dirty',dirty) FROM schema_migrations;
SELECT json_build_object('total',COUNT(*),'consumed',COUNT(*) FILTER (WHERE consumed),'unconsumed_expired',COUNT(*) FILTER (WHERE NOT consumed AND expires_at<=NOW()),'unconsumed_live',COUNT(*) FILTER (WHERE NOT consumed AND expires_at>NOW())) FROM oidc_transactions;
SELECT json_build_object('rows_in_test_creation_window',COUNT(*),'consumed',COUNT(*) FILTER (WHERE consumed),'result_cleared',COUNT(*) FILTER (WHERE COALESCE(result,'')=''),'verifier_cleared',COUNT(*) FILTER (WHERE COALESCE(verifier,'')='')) FROM oidc_transactions WHERE expires_at>='2026-10-16T03:51:05.263374Z'::timestamptz AND expires_at<='2026-10-16T03:53:35.456201Z'::timestamptz;
SELECT json_build_object('users',(SELECT COUNT(*) FROM users),'members',(SELECT COUNT(*) FROM tenant_members),'identities',(SELECT COUNT(*) FROM external_identities),'reviews',(SELECT COUNT(*) FROM source_id_binding_reviews),'sessions',(SELECT COUNT(*) FROM source_id_sessions),'local_sessions',(SELECT COUNT(*) FROM source_id_local_sessions));
ROLLBACK;"""
p=subprocess.run(['docker','exec','-i','knowhub-sourceid-business-postgres-1','sh','-c','exec psql -X -U "$POSTGRES_USER" -d "$POSTGRES_DB" -qAt -v ON_ERROR_STOP=1'],input=q.encode(),stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=15);assert p.returncode==0
db=[json.loads(x) for x in p.stdout.decode().splitlines() if x];assert db[0]=={'version':3027,'dirty':False}
assert db[3]=={'users':2,'members':2,'identities':0,'reviews':0,'sessions':0,'local_sessions':0}
flags={}
for k in receipt['restored_flags']:
 v=run(['docker','exec',app,'sh','-c','printf "%s" "${'+k+'-NOT_SET}"']).decode();assert v in ('true','false','oauth_form','external','NOT_SET');flags[k]=v
assert flags==receipt['restored_flags']
image=inspect(app,'.Image');rev=run(['docker','image','inspect','--format','{{index .Config.Labels "org.opencontainers.image.revision"}}',image]).decode().strip();assert rev=='479416c9086d91de4484dbbd2cc5f912cdcdb697'
started=inspect(app,'.State.StartedAt');started=re.sub(r'(\.\d{6})\d+(?=Z$)',r'\1',started)
age=(datetime.datetime.now(datetime.timezone.utc)-datetime.datetime.fromisoformat(started.replace('Z','+00:00'))).total_seconds()
archived=receipt.get('diagnostics',{})
print(json.dumps({'result':'PASS','utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'app_revision':rev,'app_id':inspect(app,'.Id'),'frontend_id':inspect(front,'.Id'),'flags':flags,'db':db,'log_scan_window_utc':[start,end],'current_container_safe_logs':summary,'scan_scope':scans,'archived_stage_events':archived.get('stages',[]),'archived_token_events':len(archived.get('events',[])),'archived_schemas_present':sorted(archived.get('schemas',{})),'original_enabled_app_removed':inspect(app,'.State.StartedAt')>receipt['started_utc'],'current_app_age_seconds':int(age),'diagnostic_30_minute_budget_may_have_expired':age>1800,'diagnostic_budget_expiry_observed_in_scanned_window':any(v['structured_event_counts'].get('sourceid_diagnostics_limit',0) for v in summary.values()),'previous_enabled_app_raw_logs_available':'NOT_AVAILABLE_IN_CURRENT_CONTAINER; use saved safe receipt','new_login':False,'config_changed':False,'transaction_mutated':False}))
