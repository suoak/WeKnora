b=pathlib.Path('/home/aqyfzx/ai/knowhub-enterprise-candidate-09e6d86db')
out={}
for name in ('build-status.json','upgrade-result.json','validation-result.json','rollback-result.json','final-result.json'):
 if (b/name).exists():out[name]=json.loads((b/name).read_text())
print(json.dumps(out))
