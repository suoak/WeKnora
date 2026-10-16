b=pathlib.Path('/home/aqyfzx/ai/knowhub-enterprise-candidate-09e6d86db');out={}
for name in ('validation-progress.json','preparation-status.json','mock-progress.json','validation-result.json','acceptance-result.json','mock-result.json'):
 if (b/name).exists():out[name]=json.loads((b/name).read_text())
for name in ('validation-worker.log','business-validation-worker.py.log','old-app-business-validation-worker.py.log','mock-pilot-worker.py.log','mock-enterprise_all-worker.py.log'):
 p=b/'private'/name
 if p.exists():
  raw=p.read_text(errors='replace');out[name]={'bytes':p.stat().st_size,'exception_classes':re.findall(r'^([A-Za-z_]+Error)(?=:|$)',raw,re.M),'line_numbers':re.findall(r'line ([0-9]+)',raw)}
print(json.dumps(out))
