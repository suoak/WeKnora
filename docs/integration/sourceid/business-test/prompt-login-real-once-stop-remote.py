"""Capture safe in-memory observations, then interrupt exact worker to restore.
Operator ended the single test; no further authorization is attempted.
"""
import ast,os,signal,time
root=pathlib.Path('/home/aqyfzx/ai/knowhub-prompt-login-real-20261016')
assert not (root/'result.json').exists()
tree=ast.parse((root/'worker.py').read_text())
selected=[n for n in tree.body if isinstance(n,(ast.Import,ast.ImportFrom)) or isinstance(n,ast.FunctionDef) and n.name in ('load','collect')]
scope=dict(globals(),ROOT=root,START='2026-10-16T03:40:00+00:00')
exec(compile(ast.Module(body=selected,type_ignores=[]),'safe_collection_only','exec'),scope)
observations=scope['collect']()
(root/'operator-end-diagnostics.json').write_text(json.dumps(observations,indent=2)+'\n')
# Exact previously launched PID and exact script command must both match.
pid=2854406
args=pathlib.Path('/proc/'+str(pid)+'/cmdline').read_bytes().split(b'\0')
assert args[:2]==[b'python3',str(root/'worker.py').encode()]
os.kill(pid,signal.SIGINT)
for _ in range(90):
 if (root/'result.json').exists():break
 time.sleep(1)
else:raise RuntimeError('restore_receipt_timeout')
result=json.loads((root/'result.json').read_text())
result['diagnostics']=observations
result['completion_reason']='OPERATOR_ENDED_TEST_NO_CALLBACK_COMPLETION_OBSERVED'
result['operator_feedback']='PLACEHOLDERS_ONLY_NOT_VERIFIED'
(root/'result.json').write_text(json.dumps(result,indent=2)+'\n')
print(json.dumps(result))
