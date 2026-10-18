"""Start the calibrated single transaction monitor; no authorize requests."""
import os
os.umask(0o077)
B=pathlib.Path('/home/aqyfzx/ai/knowhub-verified-email-candidate-5f988be51')
state=json.loads((B/'real-business-operation.json').read_text());P=pathlib.Path(state['prepared_directory'])
assert json.loads((P/'preparation-result.json').read_text())['result']=='PREPARED_NOT_ENABLED'
assert not (P/'launch-result.json').exists(), 'one_authorized_run_only'
worker=r'''import os,sys,json,pathlib,subprocess,datetime,fcntl,signal
os.umask(0o077)
B=pathlib.Path('/home/aqyfzx/ai/knowhub-verified-email-candidate-5f988be51');P=pathlib.Path(PRIVATE)
sys.path.insert(0,str(B/'monitor/business-test'))
from business_monitor_pilot import DockerPilotPort,protected,RELEASE
from business_monitor_adapter import run
def stop(signum,frame):raise KeyboardInterrupt()
signal.signal(signal.SIGTERM,stop);signal.signal(signal.SIGINT,stop)
out={'result':'STARTING','real_sid_requests_by_agent':0,'jit':False}
(P/'launch-result.json').write_text(json.dumps(out))
try:
 with (RELEASE/'private/business-monitor.lock').open('a') as lock:
  fcntl.flock(lock,fcntl.LOCK_EX|fcntl.LOCK_NB)
  port=DockerPilotPort(protected(P/'approved-inputs.json'),'existing')
  state={'prepared_directory':str(P),'monitor_directory':str(port.dir),'observer_directory':str(port.observer_dir)}
  (B/'real-business-operation.json').write_text(json.dumps(state))
  (P/'monitor-location.json').write_text(json.dumps(state))
  result=run(port,'existing',True,True)
  out={k:result.get(k) for k in ('run_id','case','result','reason','stages','business_probes','recovery','acceptance_scope','cross_space_negative','link_policy_verification','restore_gates','targeted_session_revocation','restore_verification')}
except BaseException as e:out={'result':'FAIL','reason':'CONTROLLED_MONITOR_LAUNCH_FAILURE','class':type(e).__name__,'raw_exception_saved':False}
out['utc']=datetime.datetime.now(datetime.timezone.utc).isoformat();(P/'launch-result.json').write_text(json.dumps(out))
'''.replace('PRIVATE',repr(str(P)))
(P/'business-worker.py').write_text(worker)
process=subprocess.Popen(['python3',str(P/'business-worker.py')],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,start_new_session=True)
(P/'worker-pid.json').write_text(json.dumps({'pid':process.pid}))
print(json.dumps({'result':'STARTED_PREPARING','one_transaction_only':True,'jit':False,'actual_authorize_requested':False}))
