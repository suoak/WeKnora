b=pathlib.Path('/home/aqyfzx/ai/knowhub-enterprise-jit-f40b1602f');out={'state':json.loads((b/'build-status.json').read_text())}
raw=(b/'artifacts/build-app.log').read_text(errors='replace')
out['go_packages_passed']=re.findall(r'^ok\s+(github.com/Tencent/WeKnora/[A-Za-z0-9_/.-]+)\s+([0-9.]+s)',raw,re.M)
out['failure_test_names']=re.findall(r'^--- FAIL: ([A-Za-z0-9_/.-]+)',raw,re.M)
out['build_failure_categories']=re.findall(r'(no space left on device|signal: killed|network is unreachable|module lookup disabled|build constraints exclude all Go files)',raw)
out['log_bytes']=(b/'artifacts/build-app.log').stat().st_size
print(json.dumps(out))
