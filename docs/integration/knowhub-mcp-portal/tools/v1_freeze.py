"""Create immutable complete-worktree source archives, scan, verify and upload."""
from pathlib import Path
import datetime, hashlib, json, re, shlex, subprocess, tarfile
from v1_host import connect

root = Path(__file__).resolve().parents[4]
out = root / 'docs/integration/knowhub-mcp-portal/evidence/v1'
names = set(subprocess.check_output(['git','ls-files'],cwd=root).decode().splitlines())
names.update(subprocess.check_output(['git','ls-files','--others','--exclude-standard'],cwd=root).decode().splitlines())
files = {n:hashlib.sha256((root/n).read_bytes()).hexdigest() for n in sorted(names)
         if (root/n).is_file() and not n.startswith('docs/integration/knowhub-mcp-portal/evidence/')}
patterns = {'private_key':rb'-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----',
            'github_token':rb'gh[pousr]_[A-Za-z0-9]{36,}',
            'aws_key':rb'AKIA[A-Z0-9]{16}',
            'openai_project_secret':rb'sk-proj-[A-Za-z0-9_-]{40,}'}
findings=[]
reviewed_fixture={'internal/types/memory_test.go':'b7167eeb6ecb45af99d2c0c3362580e79b2b3716645feed725bd33f5ecc25028'}
fixture_matches=[]
for n in files:
    data=(root/n).read_bytes()
    for label,pattern in patterns.items():
        if re.search(pattern,data):
            row={'file':n,'rule':label}
            if reviewed_fixture.get(n)==files[n]: fixture_matches.append(row)
            else: findings.append(row)
assert not findings, 'Potential credential detected; inspect privately before upload'
digest=hashlib.sha256(json.dumps(files,sort_keys=True).encode()).hexdigest()
archive=Path('E:/AIData/Codex')/('knowhub-v1-'+digest+'.tar.gz')
with tarfile.open(archive,'w:gz',compresslevel=1)as tf:
    for n in files: tf.add(root/n,arcname=n,recursive=False)
with tarfile.open(archive)as tf:
    assert {m.name:hashlib.sha256(tf.extractfile(m).read()).hexdigest()for m in tf if m.isfile()}==files
remote='/home/aqyfzx/ai/WeKnora-release-build/knowhub-mcp-v1-20261021/'+digest[:16]
manifest={'candidate_sha256':digest,'base_sha':subprocess.check_output(['git','rev-parse','HEAD'],cwd=root).decode().strip(),
          'files':files,'archive':str(archive),'archive_sha256':hashlib.sha256(archive.read_bytes()).hexdigest(),
          'remote':remote,'utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),
          'secret_scan':{'patterns':list(patterns),'findings':findings,'reviewed_fixture_matches':fixture_matches,'reviewed_fixture_sha256':reviewed_fixture,'scope':'all source archive files; .env and ignored private credentials excluded; pattern scan is not a proof of absence'},
          'dependencies':{n:h for n,h in files.items()if n in ['go.mod','go.sum','frontend/package.json','frontend/package-lock.json','third_party/anydoc-go/Cargo.lock']},
          'migrations':{n:h for n,h in files.items()if n.startswith('migrations/')},
          'configuration':{n:h for n,h in files.items()if n.startswith(('config/','docker/'))or n in ['frontend/nginx.conf','frontend/nginx-api-proxy.conf']}}
out.mkdir(parents=True,exist_ok=True)
(out/(digest+'.json')).write_text(json.dumps(manifest,indent=2),encoding='utf8')
(out/'current.json').write_text(json.dumps({'candidate_sha256':digest,'remote':remote}),encoding='utf8')
with connect()as ssh:
    command=shlex.join(['mkdir','-p',remote+'/source',remote+'/results',remote+'/artifacts'])
    i,o,e=ssh.exec_command(command);assert o.channel.recv_exit_status()==0;i.close();o.close();e.close()
    with ssh.open_sftp()as ftp:
        ftp.put(str(archive),remote+'/source.tar.gz')
        ftp.put(str(out/(digest+'.json')),remote+'/manifest.json')
    command='cd '+shlex.quote(remote)+' && python3 -c '+shlex.quote("import hashlib,json,tarfile;from pathlib import Path;m=json.loads(Path('manifest.json').read_text());assert hashlib.sha256(Path('source.tar.gz').read_bytes()).hexdigest()==m['archive_sha256'];t=tarfile.open('source.tar.gz');assert all(not x.name.startswith('/') and '..' not in Path(x.name).parts for x in t.getmembers());t.extractall('source');assert all(hashlib.sha256((Path('source')/n).read_bytes()).hexdigest()==h for n,h in m['files'].items());print('SOURCE_READBACK_OK')")
    i,o,e=ssh.exec_command(command,timeout=90);answer=o.read().decode();assert o.channel.recv_exit_status()==0,e.read().decode();i.close();o.close();e.close()
print(json.dumps({'candidate_sha256':digest,'files':len(files),'remote':remote,'verification':answer.strip()}))
