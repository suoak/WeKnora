"""Safe bounded receipt read; no raw log access."""
r=pathlib.Path('/home/aqyfzx/ai/knowhub-prompt-login-real-20261016')
p=r/'result.json'
if p.exists():print(p.read_text())
else:
 p=r/'status.json'
 print(p.read_text() if p.exists() else json.dumps({'phase':'STARTING'}))
