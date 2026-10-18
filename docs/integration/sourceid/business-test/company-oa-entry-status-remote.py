B=pathlib.Path('/home/aqyfzx/ai/knowhub-verified-email-candidate-5f988be51');P=pathlib.Path(json.loads((B/'oa-entry-operation.json').read_text())['private_directory'])
print((P/'result.json').read_text())
