"""Freeze the reviewed UI delta; operate only on the pinned packaging test host."""
from pathlib import Path
import hashlib, json, shlex, subprocess, tarfile, sys
from v1_host import connect

ROOT = Path(__file__).resolve().parents[4]
OUT = ROOT / 'docs/integration/knowhub-mcp-portal/evidence/private-scope'
OUT.mkdir(exist_ok=True)

def remote(code):
    with connect() as ssh:
        _, stdout, stderr = ssh.exec_command('python3 -c ' + shlex.quote(code), timeout=60)
        answer, error = stdout.read().decode(), stderr.read().decode()
        status = stdout.channel.recv_exit_status()
        stdout.close(); stderr.close()
    assert status == 0, error[-1000:]
    return answer

if len(sys.argv)>1 and sys.argv[1] == 'freeze':
    names = subprocess.check_output(['git', '-c', 'core.fsmonitor=false', 'ls-files', '-z'], cwd=ROOT).decode().split('\0')
    files = {n: hashlib.sha256((ROOT/n).read_bytes()).hexdigest() for n in sorted(names) if n and (ROOT/n).is_file()}
    runtime = {n:h for n,h in files.items() if not n.startswith('docs/')}
    sha = hashlib.sha256(json.dumps(runtime, sort_keys=True).encode()).hexdigest()
    dest = '/home/aqyfzx/ai/WeKnora-release-build/knowhub-mcp-private-scope/' + sha[:16]
    archive = Path('E:/AIData/Codex') / ('knowhub-mcp-private-scope-' + sha + '.tar.gz')
    if not archive.exists():
        with tarfile.open(archive, 'w:gz', compresslevel=1) as t:
            for n in files: t.add(ROOT/n, arcname=n, recursive=False)
    with tarfile.open(archive) as t:
        assert {x.name:hashlib.sha256(t.extractfile(x).read()).hexdigest() for x in t if x.isfile()} == files, 'Never replace frozen archives'
    m = {'runtime_source_sha256':sha, 'base_sha':subprocess.check_output(['git','-c','core.fsmonitor=false','rev-parse','HEAD'],cwd=ROOT).decode().strip(), 'files':files, 'archive_sha256':hashlib.sha256(archive.read_bytes()).hexdigest(), 'remote':dest, 'archive':str(archive)}
    mp = OUT/'candidate.json'; mp.write_text(json.dumps(m, indent=2), encoding='utf8')
    remote('from pathlib import Path\np=Path('+repr(dest)+');p.parent.mkdir(exist_ok=True);p.mkdir(exist_ok=False);(p/"source").mkdir();(p/"results").mkdir()')
    with connect() as ssh:
        with ssh.open_sftp() as ftp:
            ftp.put(str(archive), dest+'/source.tar.gz'); ftp.put(str(mp), dest+'/manifest.json')
    print(remote('''from pathlib import Path
import json,hashlib,tarfile
p=Path('''+repr(dest)+''');m=json.loads((p/'manifest.json').read_text())
assert hashlib.sha256((p/'source.tar.gz').read_bytes()).hexdigest()==m['archive_sha256']
with tarfile.open(p/'source.tar.gz')as t:
 assert all(not x.name.startswith('/')and '..'not in Path(x.name).parts for x in t)
 t.extractall(p/'source')
assert all(hashlib.sha256((p/'source'/n).read_bytes()).hexdigest()==h for n,h in m['files'].items())
print(json.dumps({'sha256':m['runtime_source_sha256'],'archive_verified':True,'remote':str(p)}))
'''))
elif len(sys.argv)>1 and sys.argv[1] == 'run':
    m=json.loads((OUT/'candidate.json').read_text()); script=Path(sys.argv[2]); dest=m['remote']
    with connect() as ssh:
        with ssh.open_sftp() as ftp: ftp.put(str(script),dest+'/'+script.name)
        command='nohup python3 '+shlex.quote(dest+'/'+script.name)+' '+shlex.quote(dest)+' > '+shlex.quote(dest+'/'+script.stem+'.launch.log')+' 2>&1 < /dev/null &'
        _,o,e=ssh.exec_command(command); assert o.channel.recv_exit_status()==0; o.close();e.close()
    print('Started isolated candidate job: '+script.name)
elif len(sys.argv)>1 and sys.argv[1] == 'collect':
    m=json.loads((OUT/'candidate.json').read_text()); dest=m['remote']
    with connect() as ssh:
        with ssh.open_sftp() as ftp:
            for attr in ftp.listdir_attr(dest+'/results'):
                if attr.filename.endswith(('.json','.log')): ftp.get(dest+'/results/'+attr.filename,str(OUT/attr.filename))
    for p in OUT.glob('*result.json'): print(p.name+': '+p.read_text())
