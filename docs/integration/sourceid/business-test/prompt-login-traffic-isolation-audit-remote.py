"""Read-only source classes/counts, never raw IP, user agent or query values."""
import ipaddress,os
now=datetime.datetime.now(datetime.timezone.utc)
out={'result':'PASS','utc':now.isoformat(),'windows':{},'known_test_processes':[],'new_login':False,'network_rules_changed':False}
for label,since in [('post_restore','2026-10-16T03:46:00Z'),('last_five_minutes',(now-datetime.timedelta(minutes=5)).isoformat())]:
 p=subprocess.run(['docker','logs','--since',since,'--until',now.isoformat(),'--tail','3000','knowhub-sourceid-business-frontend-1'],stdout=subprocess.PIPE,stderr=subprocess.STDOUT,timeout=15);assert p.returncode==0 and len(p.stdout)<2097152
 rows={};sources=set();classes={};ua={}
 for line in p.stdout.decode('utf-8','replace').splitlines():
  m=re.search(r'^([^\s]+).*?"(GET|POST) ([^\s"]+) HTTP/[0-9.]+"\s+(\d{3})',line)
  if not m:continue
  path=urllib.parse.urlsplit(m.group(3)).path
  if path not in ('/api/v1/auth/oidc/url','/api/v1/auth/oidc/callback','/api/v1/auth/oidc/result'):continue
  key=m.group(2)+' '+path+' '+m.group(4);rows[key]=rows.get(key,0)+1
  try:
   ip=ipaddress.ip_address(m.group(1));sources.add(ip);kind='loopback' if ip.is_loopback else 'private_network' if ip.is_private else 'public_network'
  except ValueError:kind='unparsed'
  classes[kind]=classes.get(kind,0)+1
  kind='headless_chrome' if 'HeadlessChrome/' in line else 'chrome_family' if 'Chrome/' in line else 'curl' if 'curl/' in line else 'other_or_absent'
  ua[kind]=ua.get(kind,0)+1
 out['windows'][label]={'since_utc':since,'safe_path_status_counts':rows,'distinct_source_address_count':len(sources),'source_classes':classes,'user_agent_classes':ua,'tail_limit':3000,'bytes_scanned':len(p.stdout),'raw_saved':False}
known={'worker.py':'previous_once_worker','sourceid-joint-persistent-tunnel.py':'joint_tunnel','sourceid-joint-controller.py':'joint_controller','diagnostic-runner.py':'protocol_probe_runner','probe_report_filter.py':'filter_only'}
for entry in pathlib.Path('/proc').iterdir():
 if not entry.name.isdigit():continue
 try:
  parts=(entry/'cmdline').read_bytes().split(b'\0')
  if len(parts)<2 or pathlib.Path(parts[0].decode(errors='ignore')).name not in ('python3','python'):continue
  script=parts[1].decode(errors='ignore');name=pathlib.Path(script).name
  if name in known and ('sourceid' in script or 'knowhub' in script):out['known_test_processes'].append({'pid':int(entry.name),'class':known[name]})
 except (OSError,ValueError):continue
app='knowhub-sourceid-business-app-1'
out['prompt']=run(['docker','exec',app,'printenv','SOURCEID_FORCE_REAUTH_PROMPT_LOGIN']).decode().strip();assert out['prompt']=='false'
q="BEGIN READ ONLY;SELECT COUNT(*) FROM oidc_transactions WHERE NOT consumed AND expires_at>NOW();ROLLBACK;"
p=subprocess.run(['docker','exec','-i','knowhub-sourceid-business-postgres-1','sh','-c','exec psql -X -U "$POSTGRES_USER" -d "$POSTGRES_DB" -qAt -v ON_ERROR_STOP=1'],input=q.encode(),stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=15);assert p.returncode==0
out['active_unconsumed_transactions']=int(p.stdout)
out['operator_identity']='UNKNOWN_NOT_INFERRED';out['future_exclusive_window']='NOT_PROVEN_BY_SNAPSHOT'
print(json.dumps(out))
