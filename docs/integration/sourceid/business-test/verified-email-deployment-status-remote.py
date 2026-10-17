B=pathlib.Path('/home/aqyfzx/ai/knowhub-verified-email-candidate-5f988be51')
out={}
for name in ('deployment-backup-result.json','deployment-result.json','deployment-acceptance-result.json'):
 if (B/name).exists():out[name]=json.loads((B/name).read_text())
print(json.dumps(out))
