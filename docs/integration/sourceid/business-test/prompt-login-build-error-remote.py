"""Read only the final Docker build failure from an isolated no-secret build."""
b=pathlib.Path('/home/aqyfzx/ai/knowhub-prompt-login-c24c1cf21')
assert json.loads((b/'status.json').read_text())=={'utc':json.loads((b/'status.json').read_text())['utc'],'result':'FAIL','stage':'image','exit_code':1}
lines=(b/'artifacts/build.log').read_text(errors='replace').splitlines()[-16:]
assert not any(re.search(r'Bearer |Basic |eyJ[A-Za-z0-9_-]+\.',s) for s in lines)
print(json.dumps({'isolated_docker_build_error':lines}))
