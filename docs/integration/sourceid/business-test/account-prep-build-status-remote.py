b=pathlib.Path('/home/aqyfzx/ai/knowhub-accountprep-20261016')
s=json.loads((b/'status.json').read_text())
t=(b/'artifacts/tests.log').read_text(errors='replace') if (b/'artifacts/tests.log').exists() else ''
s['test_lines']=[x for x in t.splitlines() if x.startswith(('=== RUN','--- PASS','--- FAIL','PASS','FAIL','ok ','ok\t','#','go version')) or 'undefined:' in x or 'cannot ' in x or 'Error:' in x or 'disabled' in x or 'missing' in x or 'cannot find' in x or 'not found' in x][-70:]
s['worker_error_classes']=re.findall(r'^([A-Za-z]+Error)(?=:|$)',(b/'worker.log').read_text(errors='replace'),re.M)
print(json.dumps(s))
