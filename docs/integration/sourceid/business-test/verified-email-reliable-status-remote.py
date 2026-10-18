B=pathlib.Path('/home/aqyfzx/ai/knowhub-verified-email-candidate-5f988be51')
print(json.dumps({n:json.loads((B/n).read_text()) for n in ('reliable-backup-result.json','reliable-deploy-result.json') if (B/n).exists()}))
