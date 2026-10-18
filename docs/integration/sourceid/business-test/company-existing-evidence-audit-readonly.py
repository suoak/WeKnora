"""Review saved sanitized receipts only, plus non-sensitive DB metadata."""
import sys
B=pathlib.Path('/home/aqyfzx/ai/knowhub-verified-email-candidate-5f988be51')
a=json.loads((B/'active-business-config.json').read_text());inputs=json.loads((pathlib.Path(a['prepared_directory'])/'approved-inputs.json').read_text())
s=json.loads((B/'passive-business-operation.json').read_text());Q=pathlib.Path(s['directory']);location=json.loads((Q/'location.json').read_text());M=pathlib.Path(location['directory'])
v=json.loads((Q/'status.json').read_text());saved=json.loads((M/'before-restore.json').read_text())
sys.path.insert(0,str(B/'monitor/business-test'))
from business_monitor_pilot import DockerPilotPort
p=DockerPilotPort.__new__(DockerPilotPort)
meta=json.loads(p.query("SELECT json_build_object('active_sessions',(SELECT COUNT(*) FROM source_id_sessions WHERE NOT revoked AND expires_at>NOW()),'active_enterprise_access',(SELECT COUNT(*) FROM auth_tokens WHERE session_id IN (SELECT id FROM source_id_sessions) AND NOT is_revoked AND expires_at>NOW()),'active_knowledge_bases',(SELECT COUNT(*) FROM knowledge_bases WHERE deleted_at IS NULL),'active_unconsumed_authorizations',(SELECT COUNT(*) FROM oidc_transactions WHERE nonce<>'' AND NOT consumed AND expires_at>NOW()))"))
print(json.dumps({'result':'READ_ONLY','saved_observer_result':v.get('result'),'saved_reason':v.get('reason'),'transaction_bound':saved.get('transaction_bound'),'saved_stage_events':len(saved.get('events',[])),'saved_http_events':len(saved.get('http_events',[])),'authorize_trace_present':saved.get('authorize_trace') is not None,'callback_trace_present':saved.get('callback_trace') is not None,'previous_stage_receipt_cannot_reconstruct_missing_trace':True,'current_metadata':meta,'business_config_matches':all(p.flags().get(k)==val for k,val in inputs['approved_environment'].items()),'raw_identity_values_read':False}))
