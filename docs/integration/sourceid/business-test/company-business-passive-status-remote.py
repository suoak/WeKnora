B=pathlib.Path('/home/aqyfzx/ai/knowhub-verified-email-candidate-5f988be51')
s=json.loads((B/'passive-business-operation.json').read_text());Q=pathlib.Path(s['directory'])
v=json.loads((Q/'status.json').read_text()) if (Q/'status.json').exists() else {'phase':'PREPARING'}
print(json.dumps({k:v[k] for k in ('phase','run_id','result','reason','stages','business_probes','business_gates_remain_open','automatic_config_changes','class','real_requests_by_agent') if k in v}))
