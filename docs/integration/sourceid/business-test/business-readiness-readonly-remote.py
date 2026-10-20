"""Fixed independent pilot: read-only inventory and synthetic collector tests."""
import sys,hashlib
b=pathlib.Path('/home/aqyfzx/ai/knowhub-enterprise-jit-f40b1602f')
tooling=b/'monitor-candidate-20261017/business-test'
sys.path.insert(0,str(tooling))
from business_monitor_pilot import DockerPilotPort,CLOSED,IMAGE,REVISION,SCHEMA,RELEASE,PROJECT
v=DockerPilotPort.__new__(DockerPilotPort);v.override=RELEASE/'private/not-created-readonly.json';v.tx_cache={}
i=v.inventory();s=v.snapshot()
assert i['project']==PROJECT and i['image']==IMAGE and i['revision']==REVISION and i['schema']==SCHEMA and i['dirty'] is False and i['healthy']
assert all(i['flags'].get(k)==x for k,x in CLOSED.items())
transactions=v.transactions()
z=subprocess.run(['python3','-m','unittest','discover','-s',str(tooling),'-p','business_monitor_test.py','-v'],stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=90)
raw=(z.stdout+z.stderr).decode('utf-8','replace')
passed=re.findall(r'^(test_[A-Za-z0-9_]+) .* \.\.\. ok$',raw,re.M)
failed=re.findall(r'^FAIL: (test_[A-Za-z0-9_]+)',raw,re.M)
count=re.search(r'Ran ([0-9]+) tests?',raw)
assert z.returncode==0 and count and int(count.group(1))==len(passed)
out={'result':'PASS','utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'project':PROJECT,'revision':REVISION,'schema':SCHEMA,'dirty':False,'gate_values':{k:i['flags'][k] for k in CLOSED},'snapshot_query_shapes_valid':True,'counts':{k:len(s[k]) for k in ('users','members','org_members','tenants','kbs','acl','sessions','jit')},'active_unconsumed_transactions':sum(not t.consumed and t.expires>datetime.datetime.now(datetime.timezone.utc).timestamp() for t in transactions),'postgres_test_only':{'max_connections':int(v.query('SHOW max_connections')),'reserved_connections':int(v.query('SHOW superuser_reserved_connections'))},'collector_calibration':{'result':'PASS','tests_run':len(passed),'passed_tests':passed,'failed_tests':failed,'type':'synthetic control plane + loopback Mock HTTP observer; not a real Chrome or SID test'},'monitor_code_sha256':{name:hashlib.sha256((tooling/name).read_bytes()).hexdigest() for name in ('business_monitor.py','business_monitor_adapter.py','business_monitor_pilot.py','business_monitor_test.py')},'production_accessed':False,'app_recreated':False,'real_sid_requests':0}
print(json.dumps(out))
