"""Company-test read-only inventory + synthetic calibration; no enable call."""
import sys,hashlib
B=pathlib.Path('/home/aqyfzx/ai/knowhub-company-entry-20261017');folder=B/'monitor-company-first/business-test'
sys.path.insert(0,str(folder))
from business_monitor_pilot import DockerPilotPort,CLOSED,IMAGE,REVISION,SCHEMA,PROJECT,RELEASE
v=DockerPilotPort.__new__(DockerPilotPort);v.override=RELEASE/'private/not-created-readonly.json';v.tx_cache={}
i=v.inventory();before=v.snapshot();transactions=v.transactions()
assert i['project']=='knowhub-company-test' and i['image']==IMAGE and i['revision']==REVISION and i['schema']==3029 and not i['dirty'] and i['healthy']
assert all(i['flags'].get(k)==x for k,x in CLOSED.items())
z=subprocess.run(['python3','-m','unittest','discover','-s',str(folder),'-p','business_monitor_test.py','-v'],stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=90)
raw=(z.stdout+z.stderr).decode('utf-8','replace');passed=re.findall(r'^(test_[A-Za-z0-9_]+) .* \.\.\. ok$',raw,re.M);count=re.search(r'Ran ([0-9]+) tests?',raw)
assert z.returncode==0 and count and int(count.group(1))==len(passed)
assert before==v.snapshot()
out={'result':'PASS','project':PROJECT,'revision':REVISION,'image':IMAGE,'schema':SCHEMA,'dirty':False,'tests':len(passed),'passed_tests':passed,'public_80_8080_and_empty_callback_verified':True,'new_project_snapshot_unchanged':True,'active_unconsumed_transactions':sum(not t.consumed and t.expires>datetime.datetime.now(datetime.timezone.utc).timestamp() for t in transactions),'counts':{k:len(before[k]) for k in ('users','members','org_members','kbs','acl','sessions','jit')},'gate_values':{k:i['flags'][k] for k in CLOSED},'app_recreated':False,'real_authentication':False,'real_ownership_apply':False,'code_sha256':{n:hashlib.sha256((folder/n).read_bytes()).hexdigest() for n in ('business_monitor.py','business_monitor_adapter.py','business_monitor_pilot.py','business_monitor_test.py','company-first-ownership-dryrun.py','company_business_observer.py','company_gateway_observer.py')},'utc':datetime.datetime.now(datetime.timezone.utc).isoformat()}
print(json.dumps(out))
