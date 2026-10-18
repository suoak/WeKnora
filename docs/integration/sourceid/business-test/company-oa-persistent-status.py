"""Sanitized progress only; never reads credentials into tool output."""
APP='knowhub-company-test-app-1'
base=pathlib.Path('/home/aqyfzx/ai/knowhub-company-entry-20261017/private')
ds=sorted(base.glob('oa-persistent-*'));d=ds[-1] if ds else None
e=environment(APP);s=inspect(APP,'.State')
print(json.dumps({'directory':str(d),'files':[p.name for p in d.iterdir()] if d else [],'app_state':s.get('Status'),'health':s.get('Health',{}).get('Status'),'jit':e.get('SOURCEID_JIT_ENABLE'),'result':json.loads((d/'result.json').read_text()) if d and (d/'result.json').exists() else None,'failure':json.loads((d/'failure-safe.json').read_text()) if d and (d/'failure-safe.json').exists() else None}))
