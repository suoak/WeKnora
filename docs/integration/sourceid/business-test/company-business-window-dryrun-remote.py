import sys
B=pathlib.Path('/home/aqyfzx/ai/knowhub-verified-email-candidate-5f988be51');state=json.loads((B/'real-business-operation.json').read_text());P=pathlib.Path(state['prepared_directory'])
sys.path.insert(0,str(B/'monitor/business-test'))
from business_monitor_pilot import DockerPilotPort,protected,atomic,ENTRY,ENV_ALLOWED
from company_gateway_observer import configuration,CONFIG
port=DockerPilotPort(protected(P/'approved-inputs.json'),'existing');out={'result':'DRY_RUN','stage':'compose','business_gate_changed':False}
try:
 atomic(port.override,{'services':{'app':{'environment':port.env}}})
 cfg=json.loads(run(port.compose(True)+['config','--format','json']))
 assert all(cfg['services']['app']['environment'].get(k)==v for k,v in port.env.items())
 out['compose_policy_overlay']='PASS';out['stage']='gateway_config'
 g=configuration(CONFIG.read_text(),port.inputs['approved_client_address'],port.server_observer.allowed,port.server_observer.documents,None);path=port.dir/'gateway-proposed.conf';path.write_text(g)
 image=inspect(ENTRY,'.Image')
 z=subprocess.run(['docker','run','--rm','--network','container:'+ENTRY,'--log-driver','none','--mount','type=bind,source='+str(path)+',target=/etc/nginx/nginx.conf,readonly','--entrypoint','nginx',image,'-t'],stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=20)
 out['nginx_syntax']='PASS' if z.returncode==0 else 'FAIL'
 if z.returncode:
  out['nginx_safe_error']=re.sub(r'"[^"]*"','<quoted>',z.stderr.decode('utf-8','replace'))[-600:]
  out['nginx_error_classes']=[k for k in ('unknown directive','invalid number','duplicate','not allowed','permission denied','cannot load','host not found','emerg') if k in z.stderr.decode('utf-8','replace').lower()]
 out['gateway_log_driver']=inspect(ENTRY,'.HostConfig.LogConfig').get('Type');out['result']='PASS' if z.returncode==0 else 'FAIL';out['stage']='complete'
except Exception as e:out['failure_class']=type(e).__name__
print(json.dumps(out))
