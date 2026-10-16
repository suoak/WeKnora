"""Read only fixed whitelist receipts, never worker logs."""
b=pathlib.Path('/home/aqyfzx/ai/knowhub-company-entry-20261017')
out={}
for name in ('prepare-status','prepare-result','publish-result','validation-result','unapproved-probes'):
 f=b/(name+'.json')
 if f.exists():out[name]=json.loads(f.read_text())
print(json.dumps(out))
