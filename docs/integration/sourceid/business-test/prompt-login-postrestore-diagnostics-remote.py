"""Reuse reviewed white-list collector for independent post-restore traces."""
import ast
root=pathlib.Path('/home/aqyfzx/ai/knowhub-prompt-login-real-20261016')
tree=ast.parse((root/'worker.py').read_text())
selected=[n for n in tree.body if isinstance(n,(ast.Import,ast.ImportFrom)) or isinstance(n,ast.FunctionDef) and n.name in ('load','collect')]
scope=dict(globals(),ROOT=root,START='2026-10-16T03:46:00Z')
exec(compile(ast.Module(body=selected,type_ignores=[]),'safe_collection_only','exec'),scope)
out=scope['collect']()
out.update(scope='POST_RESTORE_INDEPENDENT_TRANSACTIONS_NOT_PROMPT_TEST',utc=datetime.datetime.now(datetime.timezone.utc).isoformat(),new_requests_sent=False,config_changed=False)
print(json.dumps(out))
