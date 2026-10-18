R=pathlib.Path('/home/aqyfzx/ai/knowhub-jit-fresh-5f988be51-20261018');W=R/'real-window'
out={}
for name in ('status.json','failure.json','recovery.json'):
 if (W/name).exists():out[name]=json.loads((W/name).read_text())
print(json.dumps(out if out else {'phase':'PREPARING'}))
