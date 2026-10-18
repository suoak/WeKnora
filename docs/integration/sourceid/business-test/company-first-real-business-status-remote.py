B=pathlib.Path('/home/aqyfzx/ai/knowhub-verified-email-candidate-5f988be51');state=json.loads((B/'real-business-operation.json').read_text());P=pathlib.Path(state['prepared_directory']);M=pathlib.Path(state['monitor_directory'])
out={'launch':json.loads((P/'launch-result.json').read_text()) if (P/'launch-result.json').exists() else {'result':'NOT_STARTED'}}
for name in ('status.json','before-restore.json','final.json'):
 if (M/name).exists():
  v=json.loads((M/name).read_text());out[name]={k:v[k] for k in ('run_id','phase','result','reason','stages','transaction_bound','acceptance_scope','cross_space_negative','link_policy_verification','restore_gates','targeted_session_revocation','restore_verification','business_probes') if k in v}
print(json.dumps(out))
