"""Start bounded observation only; never toggle business configuration."""
import os,sys
os.umask(0o077)
B=pathlib.Path('/home/aqyfzx/ai/knowhub-verified-email-candidate-5f988be51')
active=json.loads((B/'active-business-config.json').read_text());P=pathlib.Path(active['prepared_directory'])
Q=pathlib.Path(active['directory'])/'passive';Q.mkdir(mode=0o700)
worker=r'''import os,sys,json,pathlib,time,fcntl
os.umask(0o077)
B=pathlib.Path('/home/aqyfzx/ai/knowhub-verified-email-candidate-5f988be51');Q=pathlib.Path(PRIVATE)
sys.path.insert(0,str(B/'monitor/business-test'))
from business_monitor_pilot import DockerPilotPort,protected,atomic,RELEASE
from business_monitor import BusinessMonitor
from business_monitor_adapter import PilotBusinessAdapter
port=None;m=None
try:
 with (RELEASE/'private/business-monitor.lock').open('a') as lock:
  fcntl.flock(lock,fcntl.LOCK_EX|fcntl.LOCK_NB)
  active=protected(B/'active-business-config.json');P=pathlib.Path(active['prepared_directory']);inputs=protected(P/'approved-inputs.json')
  observer=Q/'observer';observer.mkdir(mode=0o700);inputs['protected_observer_directory']=str(observer)
  port=DockerPilotPort(inputs,'existing')
  assert all(port.flags().get(k)==v for k,v in port.env.items())
  baseline=port.transactions();assert not any(not t.consumed and t.expires>time.time() for t in baseline)
  adapter=PilotBusinessAdapter(port,'existing',True,True);adapter.before=port.snapshot()
  port.gateway_observation.enable();port.start_stream()
  m=BusinessMonitor('existing',time.time(),[t.handle for t in baseline]);m.acceptance_scope='A_positive'
  adapter.monitor=m
  atomic(Q/'location.json',{'directory':str(port.dir),'observer':str(observer),'run_id':m.run_id})
  atomic(Q/'status.json',{'phase':'READY','run_id':m.run_id,'business_gates_remain_open':True,'real_requests_by_agent':0})
  while not m.finished:
   adapter.poll(m,time.time());time.sleep(.25)
  adapter.save_evidence(m.receipt())
  atomic(Q/'status.json',dict(m.receipt(),business_gates_remain_open=True,automatic_config_changes=False))
except BaseException as e:
 if m is not None and port is not None:
  m.end('OBSERVABILITY_INCOMPLETE');port.save_safe('passive-final',m.receipt())
 atomic(Q/'status.json',{'phase':'STOPPED','result':'INCOMPLETE','class':type(e).__name__,'business_gates_remain_open':True,'raw_exception_saved':False})
finally:
 if port is not None:
  port.close_stream()
  if port.gateway_observation is not None:port.gateway_observation.restore()
'''.replace('PRIVATE',repr(str(Q)))
(Q/'worker.py').write_text(worker)
proc=subprocess.Popen(['python3',str(Q/'worker.py')],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,start_new_session=True)
(B/'passive-business-operation.json').write_text(json.dumps({'directory':str(Q),'pid':proc.pid}))
print(json.dumps({'result':'PASSIVE_OBSERVER_STARTING','automatic_business_gate_restore':False,'real_sid_requests':0}))
