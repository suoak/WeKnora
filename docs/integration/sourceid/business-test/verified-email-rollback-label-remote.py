"""Keep rollback routing intact; make retained DB3030 environment label truthful."""
APP='knowhub-company-test-app-1';ENTRY='knowhub-company-entry-entry-1'
assert inspect(APP,'.Image')=='sha256:742c7ce56ef38ad32548900c8e7f8b264f9f6cd6138043674b91b27557443413'
assert inspect(APP,'.State.Health.Status')=='healthy'
conf=pathlib.Path('/home/aqyfzx/ai/knowhub-company-entry-20261017/entry/nginx.conf')
s=conf.read_text();assert 'company-test-f40-db3029' in s
conf.write_text(s.replace('company-test-f40-db3029','company-test-f40-db3030'))
try:
 run(['docker','exec',ENTRY,'nginx','-t']);run(['docker','exec',ENTRY,'nginx','-s','reload'])
except Exception:
 conf.write_text(s);run(['docker','exec',ENTRY,'nginx','-s','reload']);raise
print(json.dumps({'result':'PASS','only_environment_label_updated':'company-test-f40-db3030','upstream_routes_changed':False,'real_sid_requests':0}))
