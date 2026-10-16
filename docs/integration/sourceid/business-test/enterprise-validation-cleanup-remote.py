b=pathlib.Path('/home/aqyfzx/ai/knowhub-enterprise-candidate-09e6d86db');pg='knowhub-oa-sso-pilot-postgres-1'
assert json.loads((b/'validation-result.json').read_text())['result']=='FAIL'
for name in ('knowhub-enterprise-validation-app','knowhub-enterprise-validation-front','knowhub-enterprise-validation-redis','knowhub-enterprise-mock-app','knowhub-enterprise-mock-front','knowhub-enterprise-mock-provider','knowhub-enterprise-mock-redis'):
 z=subprocess.run(['docker','inspect',name],stdout=subprocess.PIPE,stderr=subprocess.PIPE)
 if z.returncode==0:
  assert inspect(name,'.HostConfig.NetworkMode') in ('container:'+pg,'container:'+inspect(pg,'.Id'));run(['docker','rm','-f',name])
for db in ('enterprise_business_validation','enterprise_acl_validation','enterprise_mock_pilot','enterprise_mock_all','enterprise_scale_validation'):
 q="SELECT COUNT(*) FROM pg_database WHERE datname='"+db+"'"
 exists=run(['docker','exec',pg,'psql','-X','-U','clean_owner','-d','postgres','-qAt','-c',q]).decode().strip()
 if exists=='1':run(['docker','exec',pg,'dropdb','-U','clean_owner',db])
import shutil,datetime
archive=b/'private'/('failed-validation-'+datetime.datetime.now().strftime('%H%M%S'));archive.mkdir(mode=0o700)
for name in ('validation-result.json','acceptance-result.json','mock-result.json'):
 if (b/name).exists():shutil.move(str(b/name),str(archive/name))
print(json.dumps({'result':'PASS','removed_only_enterprise_disposable_databases':True,'clean_pilot_changed':False}))
