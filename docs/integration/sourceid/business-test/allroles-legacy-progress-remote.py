out={}
for name,path in [('build','/home/aqyfzx/ai/knowhub-allroles-035f35e1d/status.json'),('migration','/home/aqyfzx/ai/knowhub-legacy79-rehearsal-20261016/migration-result.json')]:
    p=pathlib.Path(path);out[name]=json.loads(p.read_text()) if p.exists() else {'result':'RUNNING_OR_NOT_RECORDED'}
if out['build'].get('result')=='FAIL':
    # Only build-time logs from committed sources and synthetic tests.
    log=pathlib.Path('/home/aqyfzx/ai/knowhub-allroles-035f35e1d/artifacts/build.log').read_text(errors='replace')
    out['synthetic_build_error_tail']=log.splitlines()[-22:]
print(json.dumps(out))
