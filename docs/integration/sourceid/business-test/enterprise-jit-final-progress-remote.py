b=pathlib.Path('/home/aqyfzx/ai/knowhub-enterprise-jit-f40b1602f')
out=json.loads((b/'validation-result.json').read_text())
if out['result']=='RUNNING':
 log=(b/'private/test.log').read_text(errors='replace')
 out['passed_tests']=re.findall(r'^--- PASS: ([A-Za-z0-9_/.-]+)',log,re.M)
 out['failed_tests']=re.findall(r'^--- FAIL: ([A-Za-z0-9_/.-]+)',log,re.M)
print(json.dumps(out))
