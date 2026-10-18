import shutil,sys
R=pathlib.Path('/home/aqyfzx/ai/knowhub-company-entry-20261017');B=pathlib.Path('/home/aqyfzx/ai/knowhub-verified-email-candidate-5f988be51')
c=json.loads((R/'green/compose.json').read_text())
sys.path.insert(0,str(B/'monitor/business-test'))
from business_monitor_pilot import DockerPilotPort
p=DockerPilotPort.__new__(DockerPilotPort)
out={'result':'READ_ONLY','free_disk_bytes':shutil.disk_usage(R).free,'services':{},'source_schema':p.query("SELECT version::text||'|'||dirty FROM schema_migrations"),'active_authorizations':int(p.query("SELECT COUNT(*) FROM oidc_transactions WHERE nonce<>'' AND NOT consumed AND expires_at>NOW()")),'active_sessions':int(p.query("SELECT COUNT(*) FROM source_id_sessions WHERE NOT revoked AND expires_at>NOW()"))}
for name,s in c['services'].items():
 out['services'][name]={'image':s.get('image'),'env_keys':sorted(s.get('environment',{})) if isinstance(s.get('environment',{}),dict) else 'env_file','volumes':s.get('volumes',[]),'ports':s.get('ports',[]),'networks':s.get('networks'),'network_mode':s.get('network_mode'),'command_type':type(s.get('command')).__name__}
out['networks']=c.get('networks');out['volumes']=c.get('volumes')
out['app_config_files']=inspect('knowhub-company-test-app-1','.Config.Labels')['com.docker.compose.project.config_files']
out['gateway_config_path']=str(R/'entry/nginx.conf')
out['migration_directory_exists']=(pathlib.Path('/home/aqyfzx/ai/knowhub-verified-email-implementation-20261017/source/migrations/versioned')).exists()
out['app_mounts']=inspect('knowhub-company-test-app-1','.Mounts')
print(json.dumps(out))
