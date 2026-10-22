"""Package immutable source archives with separately hashed reports/test evidence.

Never modifies a frozen source archive or includes private runtime credentials.
"""
from pathlib import Path
import datetime,hashlib,json,re,subprocess,tarfile
root=Path(__file__).resolve().parents[4]
assert root==Path('E:/code/WeKnora-worktrees/knowhub-v1-feishu-integration-20261021')
docs=root/'docs/integration/knowhub-mcp-portal';out=docs/'evidence/feishu-joint'
current=json.loads((out/'current.json').read_text(encoding='utf8'))
sha=current['candidate_sha256'];assert sha.startswith('b47025118adfc4cf')
hot=Path('E:/code/WeKnora-worktrees/feishu-safe-incremental-20261021')
hotdocs=hot/'docs/integration/knowhub-mcp-portal'
h=json.loads((hotdocs/'evidence/feishu-incremental/hotfix-frozen-current.json').read_text(encoding='utf8'))
def digest(path):
 d=hashlib.sha256()
 with path.open('rb')as f:
  for b in iter(lambda:f.read(1024*1024),b''):d.update(b)
 return d.hexdigest()
names=set(subprocess.check_output(['git','ls-files','--modified','--cached','--others','--exclude-standard','-z'],cwd=root).decode().split('\0'))
runtime={n:digest(root/n)for n in sorted(names)if(root/n).is_file()and not n.startswith('docs/integration/knowhub-mcp-portal/')}
runtime_sha=hashlib.sha256(''.join(n+'\0'+v+'\n'for n,v in runtime.items()).encode()).hexdigest()
assert runtime_sha==current['runtime_source_sha256']
required=['feishu-app-post-a4b','worker-fault','independent-hotfix-offline']
for stage in required:
 assert json.loads((out/(stage+'-'+sha[:16])/'result.json').read_text())['exit_code']==0
assert json.loads((out/('rollback-'+sha[:16])/'done.json').read_text())['all_passed']
assert json.loads((out/('old-image-offline-probe-'+sha[:16])/'result.json').read_text())['drop_in_rollback']=='UNSAFE'
stamp=datetime.datetime.now(datetime.timezone.utc).strftime('%Y%m%dT%H%M%SZ')
dest=Path('E:/AIData/Codex')/('knowhub-feishu-joint-handoff-'+sha[:16]+'-'+stamp)
dest.mkdir(exist_ok=False)
artifacts={}
for role,candidate,prefix in [('joint',sha,'knowhub-v1-feishu-'),('independent_rc3_hotfix',h['candidate_sha256'],'knowhub-feishu-rc3-hotfix-')]:
 path=Path('E:/AIData/Codex')/(prefix+candidate+'.tar.gz');assert path.is_file()
 artifacts[role]={'path':str(path),'sha256':digest(path),'bytes':path.stat().st_size}
files={}
for role,parent in [('joint-docs',docs),('hotfix-docs',hotdocs)]:
 for p in sorted(parent.rglob('*')):
  if not p.is_file()or '__pycache__'in p.parts:continue
  assert not any(part.lower()in('private','.env','app.env','client.json')for part in p.relative_to(parent).parts)
  files[role+'/'+p.relative_to(parent).as_posix()]={'path':p,'sha256':digest(p),'bytes':p.stat().st_size}
rules=[rb'-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----',rb'gh[pousr]_[A-Za-z0-9]{36,}',rb'AKIA[A-Z0-9]{16}',rb'sk-proj-[A-Za-z0-9_-]{40,}',rb'Bearer\s+(?:mcp_|sk-)[A-Za-z0-9_-]{20,}']
findings=[]
for n,v in files.items():
 if v['path'].suffix.lower()in('.json','.log','.txt','.md','.py','.yaml','.yml'):
  raw=v['path'].read_bytes()
  if any(re.search(rule,raw)for rule in rules):findings.append(n)
assert not findings,'Potential secrets in supplement; review before packaging: '+str(findings)
manifest={'created_utc':stamp,'git_head':subprocess.check_output(['git','rev-parse','HEAD'],cwd=root).decode().strip(),
 'candidate_sha256':sha,'runtime_source_sha256':runtime_sha,'source_archives':artifacts,
 'runtime_files':runtime,'supplement_files':{n:{k:v for k,v in row.items()if k!='path'}for n,row in files.items()},
 'secret_pattern_scan':'PASS; synthetic exact-secret scan separately completed; not a production credential scan',
 'production_changes':False,'git_commit_push_merge':False,'approval':'isolated evidence only, no production sync/release approval',
 'overall':'CONDITIONAL_ISOLATED_ACCEPTANCE; formal environment and rollback constraints remain',
 'historical_failures_preserved':True,'old_unprotected_APP_rollback':'UNSAFE_verified_old_HMAC_200',
 'source_archives_immutable':True,'supplement_not_part_of_built_image':True}
(dest/'MANIFEST.json').write_text(json.dumps(manifest,indent=2),encoding='utf8')
supplement=dest/'reports-tests-evidence.tar.gz'
with tarfile.open(supplement,'w:gz')as tf:
 for n,v in files.items():tf.add(v['path'],arcname=n,recursive=False)
 tf.add(dest/'MANIFEST.json',arcname='MANIFEST.json')
bundle=dest.with_suffix('.tar.gz')
with tarfile.open(bundle,'w:gz',compresslevel=3)as tf:
 for role,v in artifacts.items():tf.add(v['path'],arcname='frozen-source/'+Path(v['path']).name,recursive=False)
 tf.add(supplement,arcname=supplement.name);tf.add(dest/'MANIFEST.json',arcname='MANIFEST.json')
result={'bundle':str(bundle),'bundle_sha256':digest(bundle),'bundle_bytes':bundle.stat().st_size,
 'manifest':str(dest/'MANIFEST.json'),'manifest_sha256':digest(dest/'MANIFEST.json'),
 'supplement_sha256':digest(supplement),'candidate_sha256':sha,'runtime_source_sha256':runtime_sha}
(dest/'CHECKSUMS.json').write_text(json.dumps(result,indent=2),encoding='utf8')
(out/'final-delivery-package.json').write_text(json.dumps(result,indent=2),encoding='utf8')
print(json.dumps(result,indent=2))
