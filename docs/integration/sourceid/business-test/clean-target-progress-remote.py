base=pathlib.Path('/home/aqyfzx/ai/knowhub-legacy-clean-target-20261016')
p=base/'preparation-result.json';s=base/'preparation-status.json'
out=json.loads(p.read_text()) if p.exists() else json.loads(s.read_text()) if s.exists() else {'result':'NOT_RECORDED'}
print(json.dumps(out))
