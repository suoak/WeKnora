"""Packaging candidate status only; never inspects credentials or runtime logs."""
b=pathlib.Path('/home/aqyfzx/ai/knowhub-prompt-login-479416c90')
p=b/'status.json';out=json.loads(p.read_text()) if p.exists() else {'result':'NOT_STARTED'}
if out.get('result')=='FAIL':
    log=(b/'artifacts/build.log').read_text(errors='replace')
    out['failed_synthetic_tests']=re.findall(r'^--- FAIL: ([A-Za-z0-9_/]+)',log,re.M)
    out['synthetic_test_locations']=re.findall(r'([A-Za-z_]+_test\.go:[0-9]+)',log)[-20:]
    out['build_error_classes']=sorted(set(re.findall(r'\b(undefined: [A-Za-z0-9_]+|syntax error|build failed)\b',log)))
print(json.dumps(out))
