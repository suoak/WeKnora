B=pathlib.Path('/home/aqyfzx/ai/knowhub-verified-email-candidate-5f988be51')
print(json.dumps({n:json.loads((B/n).read_text()) for n in ('retry-backup-result.json','retry-deploy-result.json') if (B/n).exists()}))
