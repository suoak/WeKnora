"""Check staged evidence cannot include locally retained session credentials."""
import json,subprocess
from office_test_remote import ROOT
private=ROOT/'docs/qa/office-test-deployment/private'
secret_values=[]
for path in (private/'browser-session.json',):
    if path.exists():
        data=json.loads(path.read_text())
        secret_values.extend(v.encode() for k,v in data.items() if k in ('token','refresh_token','password') and isinstance(v,str) and len(v)>16)
paths=subprocess.check_output(['git','diff','--cached','--name-only','-z'],cwd=ROOT).decode('utf-8').split('\0')
paths=[p for p in paths if p]
assert paths
for path in paths:
    assert '/private/' not in path.replace('\\','/'),'private evidence staged'
    assert not path.endswith(('.dpapi','.dump','.tar.gz')),'private runtime material staged'
    data=subprocess.check_output(['git','show',':'+path],cwd=ROOT)
    assert not any(secret in data for secret in secret_values),'session credential found in staged publication'
print(json.dumps({'staged_files_checked':len(paths),'retained_session_tokens_found':False,'private_archives_staged':False}))
