"""Candidate status only; no raw compiler/app/environment logs."""
p=pathlib.Path('/home/aqyfzx/ai/knowhub-verified-email-candidate-5f988be51/build-result.json')
print(p.read_text() if p.exists() else json.dumps({'result':'NOT_STARTED'}))
