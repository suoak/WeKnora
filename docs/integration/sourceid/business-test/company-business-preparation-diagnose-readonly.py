import sys
B=pathlib.Path('/home/aqyfzx/ai/knowhub-verified-email-candidate-5f988be51');state=json.loads((B/'real-business-operation.json').read_text());P=pathlib.Path(state['prepared_directory'])
sys.path.insert(0,str(B/'monitor/business-test'))
from business_monitor_pilot import DockerPilotPort,protected
out={'result':'READ_ONLY','stage':'construct','business_gate_changed':False}
try:
 port=DockerPilotPort(protected(P/'approved-inputs.json'),'existing')
 out['stage']='inventory';v=port.inventory();out['target_correct']=v['schema']==3030 and v['healthy'];out['closed_gate_mismatches']=[k for k,x in port.closed_baseline.items() if v['flags'].get(k)!=x]
 out['stage']='snapshot';snap=port.snapshot();out['identity_session_counts']={k:len(snap[k]) for k in ('sessions','jit','ownership')}
 out['stage']='transactions';rows=port.transactions();now=datetime.datetime.now(datetime.timezone.utc).timestamp();out['active_transactions']=sum(not r.consumed and r.expires>now for r in rows);out['unconsumed_total']=sum(not r.consumed for r in rows);out['stage']='complete'
except Exception as e:out['failure_class']=type(e).__name__
print(json.dumps(out))
