"""Export only existing safe schemas, never runtime configuration or logs."""
b=pathlib.Path('/home/aqyfzx/ai/knowhub-enterprise-jit-f40b1602f');out={}
for n in ('build-status.json','upgrade-result.json','validation-result.json','rollback-result.json','network-result.json','capacity-result.json','final-result.json','image-receipt.json','capacity-fault-evidence.json','jit-revoke-result.json'):
 if (b/n).exists():out[n]=json.loads((b/n).read_text())
if (b/'capacity-first-attempt/capacity-result.json').exists():
 out['capacity-first-attempt.json']=json.loads((b/'capacity-first-attempt/capacity-result.json').read_text())
 out['capacity-first-fault-evidence.json']=json.loads((b/'capacity-first-attempt/capacity-fault-evidence.json').read_text())
print(json.dumps(out))
