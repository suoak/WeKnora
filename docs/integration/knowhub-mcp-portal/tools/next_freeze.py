"""Freeze a verified joint worktree without committing, touching V1, or deploying."""
from pathlib import Path
import hashlib, importlib.util, json, re, shlex, subprocess, tarfile

root = Path(__file__).resolve().parents[4]
assert root == Path('E:/code/WeKnora-worktrees/knowhub-v1-wiki-dynamic-20261022')
out = root/'docs/integration/knowhub-mcp-portal/evidence/v1-wiki-dynamic'
current = json.loads((out/'development-current.json').read_text(encoding='utf8'))
quality_dir = out/('development-'+current['source_sha256'][:16])
if current.get('results_dir','results')!='results':
    quality_dir=quality_dir/current['results_dir']
quality = quality_dir/'done.json'
gate = json.loads(quality.read_text(encoding='utf8'))
assert gate['all_passed'] and gate['source_sha256']==current['source_sha256'], 'Current development gate incomplete'
rows=json.loads((quality.parent/'results.json').read_text())
required={'focused','new-real-PG','new-real-PG-race','formal-migrations','formal-tagged-migration-race','database','race-new','vet','full-go','build','seed-compile'}
assert required.issubset({r['name']for r in rows if r['exit_code']==0}), 'Missing current quality gates'
canonical={'all_passed':True,'source_sha256':gate['source_sha256'],'commands':rows}
extra_dir=out/('extra-'+current['source_sha256'][:16])
if current.get('extra_results_dir','extra-quality')!='extra-quality':
    extra_dir=extra_dir/current['extra_results_dir']
extra=json.loads((extra_dir/'done.json').read_text())
assert extra['all_passed'] and extra['source_sha256']==current['source_sha256'], 'Complete race regression missing or failed'
canonical['complete_race']=extra
names = set(subprocess.check_output(['git','ls-files','--modified','--cached','--others','--exclude-standard','-z'],cwd=root).decode().split('\0'))
files = {n:hashlib.sha256((root/n).read_bytes()).hexdigest() for n in sorted(names)
         if (root/n).is_file() and not n.startswith('docs/integration/knowhub-mcp-portal/evidence/')}
runtime = {n:h for n,h in files.items() if not n.startswith('docs/integration/knowhub-mcp-portal/')}
runtime_sha = hashlib.sha256(''.join(n+'\0'+h+'\n' for n,h in runtime.items()).encode()).hexdigest()
assert runtime_sha == gate['source_sha256'], 'Worktree changed after quality candidate was captured'
rules = {'private_key':rb'-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----',
         'github_token':rb'gh[pousr]_[A-Za-z0-9]{36,}', 'aws_key':rb'AKIA[A-Z0-9]{16}',
         'openai_project_secret':rb'sk-proj-[A-Za-z0-9_-]{40,}'}
reviewed = {'internal/types/memory_test.go':'b7167eeb6ecb45af99d2c0c3362580e79b2b3716645feed725bd33f5ecc25028'}
findings = []
for name,h in files.items():
    for rule,pattern in rules.items():
        if re.search(pattern,(root/name).read_bytes()) and reviewed.get(name) != h:
            findings.append({'path':name,'rule':rule})
assert not findings, 'Potential credential; inspect privately before upload: '+str(findings)
sha = hashlib.sha256(json.dumps(files,sort_keys=True).encode()).hexdigest()
archive = Path('E:/AIData/Codex')/('knowhub-v1-wiki-dynamic-'+sha+'.tar.gz')
assert not archive.exists(), 'Frozen artifacts are immutable'
with tarfile.open(archive,'w:gz',compresslevel=1) as tf:
    for name in files: tf.add(root/name,arcname=name,recursive=False)
with tarfile.open(archive) as tf:
    assert {x.name:hashlib.sha256(tf.extractfile(x).read()).hexdigest() for x in tf if x.isfile()} == files
remote = '/home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-20261022/'+sha[:16]
manifest = dict(candidate_sha256=sha,runtime_source_sha256=runtime_sha,
                base_sha=subprocess.check_output(['git','rev-parse','HEAD'],cwd=root).decode().strip(),
                files=files,archive=str(archive),archive_sha256=hashlib.sha256(archive.read_bytes()).hexdigest(),remote=remote,
                additional_quality=canonical,dependencies={n:h for n,h in files.items() if n.endswith(('go.mod','go.sum','package-lock.json','package.json','Cargo.lock','uv.lock','requirements.txt','pyproject.toml'))},
                migrations={n:h for n,h in files.items() if n.startswith('migrations/')},
                secret_scan={'rules':list(rules),'findings':findings,'reviewed_fixtures':reviewed,'limitation':'Pattern scan is not proof of absence'},
                prior_v1_sha256='5468da42c518cc83539040506c85b7964bdf2ae20826e641dbd030253316e231')
hotout=Path('E:/code/WeKnora-worktrees/feishu-safe-incremental-20261021/docs/integration/knowhub-mcp-portal/evidence/feishu-incremental')
hotcurrent=json.loads((hotout/'hotfix-frozen-current.json').read_text(encoding='utf8'))
hotimage=json.loads((hotout/('hotfix-build-result-'+hotcurrent['candidate_sha256']+'.json')).read_text(encoding='utf8'))
assert hotimage['exit_code']==0 and hotimage['runtime_source_sha256']==hotcurrent['runtime_source_sha256'] and not hotimage['includes_personal_Go_MCP']
manifest['independent_hotfix_reference']=hotimage
out.mkdir(parents=True,exist_ok=True)
(out/(sha+'.json')).write_text(json.dumps(manifest,indent=2),encoding='utf8')
spec = importlib.util.spec_from_file_location('host',root/'docs/integration/knowhub-mcp-portal/tools/v1_host.py')
h = importlib.util.module_from_spec(spec);spec.loader.exec_module(h)
with h.connect() as ssh:
    _,stdout,stderr = ssh.exec_command('mkdir -p '+shlex.quote(str(Path(remote).parent).replace('\\','/'))+' && mkdir '+shlex.quote(remote)+' && mkdir '+shlex.quote(remote+'/source')+' '+shlex.quote(remote+'/results')+' '+shlex.quote(remote+'/artifacts'),timeout=30)
    assert stdout.channel.recv_exit_status()==0,stderr.read().decode()
    with ssh.open_sftp() as ftp:
        ftp.put(str(archive),remote+'/source.tar.gz')
        ftp.put(str(out/(sha+'.json')),remote+'/manifest.json')
        ftp.put(str(quality),remote+'/quality-gate.json')
        ftp.put(str(root/'docs/integration/knowhub-mcp-portal/tools/next_test.compose.yml'),remote+'/deployment-compose.yml')
    code="import hashlib,json,tarfile;from pathlib import Path;b=Path("+repr(remote)+");m=json.loads((b/'manifest.json').read_text());assert hashlib.sha256((b/'source.tar.gz').read_bytes()).hexdigest()==m['archive_sha256'];t=tarfile.open(b/'source.tar.gz');assert all(not x.name.startswith('/') and '..' not in Path(x.name).parts for x in t.getmembers());t.extractall(b/'source');assert all(hashlib.sha256((b/'source'/n).read_bytes()).hexdigest()==h for n,h in m['files'].items());print('SOURCE_READBACK_OK')"
    _,stdout,stderr=ssh.exec_command('python3 -c '+shlex.quote(code),timeout=90)
    answer=stdout.read().decode();assert stdout.channel.recv_exit_status()==0,stderr.read().decode()
(out/'current.json').write_text(json.dumps({'candidate_sha256':sha,'runtime_source_sha256':runtime_sha,'remote':remote}),encoding='utf8')
print(json.dumps({'candidate_sha256':sha,'runtime_source_sha256':runtime_sha,'archive_sha256':manifest['archive_sha256'],'remote':remote,'verification':answer.strip()}))
