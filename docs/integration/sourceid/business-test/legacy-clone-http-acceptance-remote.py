pg='oa-legacy79-pg-20261016';app='oa-legacy79-app-20261016'
assert inspect(pg,'.HostConfig.NetworkMode')=='none'
p=subprocess.run(['docker','exec',app,'python3','/fixture/legacy-clone-http-acceptance.py'],stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=100)
assert p.returncode==0
out=json.loads(p.stdout)
pathlib.Path('/home/aqyfzx/ai/knowhub-legacy79-rehearsal-20261016/http-acceptance-result.json').write_text(json.dumps(out,indent=2))
print(json.dumps(out))
