pg='oa-legacy79-pg-20261016'
q="SELECT json_agg(json_build_object('table',table_name,'column',column_name)) FROM information_schema.columns WHERE table_schema='public' AND table_name IN ('tenant_api_keys','data_sources','system_settings','knowledges','knowledge_bases')"
p=subprocess.run(['docker','exec','-i',pg,'psql','-X','-U','clone','-d','clone','-qAt','-v','ON_ERROR_STOP=1'],input=('BEGIN READ ONLY;'+q+';ROLLBACK;').encode(),stdout=subprocess.PIPE,stderr=subprocess.PIPE)
assert p.returncode==0
print(json.dumps({'columns':json.loads(p.stdout)}))
