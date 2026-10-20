"""Safe bounded progress, never raw worker/app logs."""
b=pathlib.Path('/home/aqyfzx/ai/knowhub-enterprise-jit-f40b1602f');out={}
for n in ('validation-progress.json','validation-result.json','mock-progress.json','mock-result.json','capacity-progress.json','capacity-result.json','jit-revoke-result.json'):
 if (b/n).exists():
  v=json.loads((b/n).read_text())
  if n in ('validation-result.json','capacity-result.json'):v={k:v.get(k) for k in ('result','failed_stage','failure_class','failure_lines')}
  out[n]=v
out['utc']=datetime.datetime.now(datetime.timezone.utc).isoformat()
for name in ('capacity-worker.log','validation-worker.log','jit-revoke-worker.log','jit-revoke-delayed.log'):
 f=b/'private'/name
 if f.exists():
  raw=f.read_text(errors='replace');out[name]={'bytes':f.stat().st_size,'exception_classes':re.findall(r'^([A-Za-z_]+Error)(?=:|$)',raw,re.M),'line_numbers':re.findall(r'line ([0-9]+)',raw)}
print(json.dumps(out))
