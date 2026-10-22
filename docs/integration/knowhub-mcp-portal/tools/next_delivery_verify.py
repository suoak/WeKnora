"""Read-only evidence gates; never deploys, changes flags or blesses failures."""
from pathlib import Path
import hashlib,json,subprocess

root=Path(__file__).resolve().parents[4]
out=root/'docs/integration/knowhub-mcp-portal/evidence/v1-wiki-dynamic'
c=json.loads((out/'current.json').read_text());short=c['candidate_sha256'][:16]
frozen=json.loads((out/(c['candidate_sha256']+'.json')).read_text())
checks={};missing=[]

def read(path):
    p=out/path
    if not p.exists():missing.append(str(path));return {}
    return json.loads(p.read_text(encoding='utf8'))

dev=json.loads((out/'development-current.json').read_text())
quality=read('development-'+dev['source_sha256'][:16]+'/done.json')
checks['development']=quality.get('all_passed')is True and quality.get('source_sha256')==c['runtime_source_sha256']
extra=read('extra-'+dev['source_sha256'][:16]+'/done.json')
checks['complete_relevant_race']=extra.get('exit_code')==0 and extra.get('source_sha256')==c['runtime_source_sha256']
checks['build']=read('build-'+short+'/done.json').get('all_passed')is True
checks['deploy']=read('deploy-'+short+'/done.json').get('all_passed')is True
a=read('acceptance-'+short+'/done.json')
checks['concentrated_commands']=a.get('commands_passed')is True and a.get('http_business')==0
checks['http_business']=read('acceptance-'+short+'/http-business.json').get('exit_code')==0
checks['backup_restore']=read('acceptance-'+short+'/backup-restore.json').get('restore_exit_code')==0
checks['image_binary_binding']=read('binary-image-binding-'+short+'.json').get('built_artifact_equals_image_binary')is True
for name in ('wiki-negatives','portal-dynamic'):
    v=read(name+'-'+short+'/result.json')
    checks[name]=v.get('exit_code')==0 and bool(v.get('checks'))and all(x=='PASS'for x in v['checks'].values())
browser=list((out/('browser-'+short)).glob('*/result.json'))
checks['browser']=any(json.loads(x.read_text()).get('exit_code')==0 for x in browser)
codex=list((out/('codex-'+short)).glob('*/semantic-review.json'))
checks['actual_codex_eight_semantics']=any(json.loads(x.read_text()).get('exit_code')==0 for x in codex)
probe=list((out/('codex-'+short)).glob('*/feishu-semantic-review.json'))
checks['actual_codex_both_feishu_sources']=any(json.loads(x.read_text()).get('exit_code')==0 for x in probe)
source=read('feishu-source-current-'+short+'.json')
fs=read(source.get('results_dir','__missing__')+'-'+short+'/result.json')
checks['real_APP_worker_synthetic_feishu']=fs.get('exit_code')==0 and fs.get('candidate_sha256')==c['candidate_sha256']
rb=read('rollback-compatibility-'+short+'/proof.json')
checks['safe_rollback']=rb.get('exit_code')==0 and rb.get('candidate_sha256')==c['candidate_sha256'] and bool(rb.get('checks'))and all(v=='PASS'for v in rb['checks'].values())
approval=read('rollback-approval-'+short+'.json')
checks['rollback_admission']=approval.get('admitted')is True and approval.get('proof_sha256')==hashlib.sha256((out/('rollback-compatibility-'+short)/'proof.json').read_bytes()).hexdigest() if rb else False
names=set(subprocess.check_output(['git','ls-files','--modified','--cached','--others','--exclude-standard','-z'],cwd=root).decode().split('\0'))
runtime={n:hashlib.sha256((root/n).read_bytes()).hexdigest()for n in sorted(names)if(root/n).is_file()and not n.startswith('docs/integration/knowhub-mcp-portal/')}
sha=hashlib.sha256(''.join(n+'\0'+h+'\n'for n,h in runtime.items()).encode()).hexdigest()
checks['runtime_matches_developed_built_candidate']=sha==c['runtime_source_sha256']
aux=[dict(path=n,frozen_sha256=h,current_sha256=hashlib.sha256((root/n).read_bytes()).hexdigest())for n,h in frozen['files'].items()if(root/n).is_file()and hashlib.sha256((root/n).read_bytes()).hexdigest()!=h]
checks['post_freeze_changes_are_documentation_or_harness_only']=all(v['path'].startswith('docs/integration/knowhub-mcp-portal/')for v in aux)
result=dict(candidate_sha256=c['candidate_sha256'],runtime_source_sha256=sha,checks=checks,missing=missing,
            isolated_synthetic_gate_pass=all(checks.values()),production_approved=False,
            post_freeze_documentation_or_harness=aux,
            real_SID='NOT_TESTED',formal_model='NOT_TESTED',real_Feishu_cloud='NOT_TESTED',production_backlog='NOT_TESTED')
(out/('delivery-gates-'+short+'.json')).write_text(json.dumps(result,indent=2),encoding='utf8')
print(json.dumps({k:result[k]for k in ('candidate_sha256','checks','missing','isolated_synthetic_gate_pass','production_approved')}))
raise SystemExit(0 if result['isolated_synthetic_gate_pass']else 1)
