"""Safe synthetic test result only."""
p=pathlib.Path('/home/aqyfzx/ai/knowhub-verified-email-implementation-20261017/test-result.json')
v=json.loads(p.read_text()) if p.exists() else {'result':'NOT_STARTED'}
if v['result']=='RUNNING':
 z=subprocess.run(['docker','top','knowhub-email-policy-test-builder','-eo','comm'],stdout=subprocess.PIPE,stderr=subprocess.DEVNULL,timeout=15)
 v['synthetic_runner_process_classes']=z.stdout.decode().splitlines()[1:] if z.returncode==0 else []
 z=subprocess.run(['docker','ps','-a','--filter','name=knowhub-email-policy-','--format','{{.Names}} {{.Status}}'],stdout=subprocess.PIPE,stderr=subprocess.DEVNULL,timeout=15)
 v['synthetic_runner_containers']=z.stdout.decode().splitlines()
 z=subprocess.run(['pgrep','-f','^python3 /home/aqyfzx/ai/knowhub-verified-email-implementation-20261017/worker.py$'],stdout=subprocess.PIPE,stderr=subprocess.DEVNULL,timeout=5)
 v['controller_alive']=z.returncode==0
print(json.dumps(v))
