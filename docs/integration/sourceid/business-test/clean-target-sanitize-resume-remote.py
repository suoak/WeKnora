"""Resume only candidate credential transaction; no snapshot or migration repeat."""
import sys,ast,secrets
sys.path.insert(0,'/home/aqyfzx/ai/knowhub-legacy-clean-target-20261016')
from support import *
os.umask(0o077)
private=BASE/'private';out=json.loads((BASE/'preparation-result.json').read_text())
assert out['failed_stage']=='invalidate_candidate_credentials' and out['schema']=={'version':3027,'dirty':False,'full_up_chain':45}
assert inspect(PG,'.HostConfig.NetworkMode')=='none'
cols=columns();before=signatures(cols,source=True);n_before=counts(source=True);f_before=files('weknora_data-files')
assert before==signatures(cols)
database_password=dict(x.split('=',1) for x in (private/'postgres.env').read_text().splitlines())['POSTGRES_PASSWORD']
tree=ast.parse((BASE/'prepare.py').read_text());fn=next(n for n in tree.body if isinstance(n,ast.FunctionDef) and n.name=='main');body=next(n.body for n in fn.body if isinstance(n,ast.Try))
start=next(i for i,n in enumerate(body) if isinstance(n,ast.Assign) and any(isinstance(t,ast.Name) and t.id=='stage' for t in n.targets) and isinstance(n.value,ast.Str) and n.value.s=='invalidate_candidate_credentials')
tail=ast.Module(body=body[start:],type_ignores=[]);exec(compile(tail,'candidate_credential_resume','exec'))
out.pop('failed_stage',None);out.pop('failure_class',None);out.pop('failure_lines',None)
out['credential_resume_without_repeating_migration']=True;out['completed_utc']=datetime.datetime.now(datetime.timezone.utc).isoformat()
(BASE/'preparation-result.json').write_text(json.dumps(out,indent=2)+'\n')
print(json.dumps(out))
