"""Preserve all existing files before implementation and inspect build host."""
from pathlib import Path
import datetime
import hashlib
import json
import subprocess
import tarfile
from v1_host import connect

root = Path(__file__).resolve().parents[4]
out = root / 'docs/integration/knowhub-mcp-portal/evidence/v1'
out.mkdir(parents=True, exist_ok=True)
archive = Path('E:/AIData/Codex/knowhub-v1-before-development-20261021.tar.gz')
assert not archive.exists(), 'Preservation archive already exists; do not overwrite'
files = {}
with tarfile.open(archive, 'w:gz') as tf:
    for path in sorted(root.rglob('*')):
        if not path.is_file() or any(p in ('.git', 'node_modules', '__pycache__', 'dist') for p in path.relative_to(root).parts):
            continue
        name = path.relative_to(root).as_posix()
        files[name] = hashlib.sha256(path.read_bytes()).hexdigest()
        tf.add(path, arcname=name, recursive=False)
with tarfile.open(archive) as tf:
    assert {m.name: hashlib.sha256(tf.extractfile(m).read()).hexdigest()
            for m in tf if m.isfile()} == files
result = {'utc': datetime.datetime.now(datetime.timezone.utc).isoformat(), 'files': files,
          'archive': str(archive), 'archive_sha256': hashlib.sha256(archive.read_bytes()).hexdigest(),
          'readback_verified': True,
          'head': subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=root).decode().strip()}
(out / 'preservation.json').write_text(json.dumps(result, indent=2), encoding='utf8')
commands = ['hostname', 'df -h / /home/aqyfzx', 'docker compose version',
            'docker ps --format "{{.Names}} {{.Image}} {{.Ports}}"',
            'ss -lnt', 'ls -ld /home/aqyfzx/ai/WeKnora-release-build/',
            'docker network ls', 'docker images --format "{{.Repository}}:{{.Tag}} {{.ID}}"']
checks = []
with connect() as ssh:
    for command in commands:
        stdin, stdout, stderr = ssh.exec_command(command, timeout=30)
        text, error = stdout.read().decode(), stderr.read().decode()
        checks.append({'command': command, 'stdout': text, 'stderr': error,
                       'exit_code': stdout.channel.recv_exit_status()})
        stdin.close(); stdout.close(); stderr.close()
(out / 'host-preflight.json').write_text(json.dumps(checks, indent=2), encoding='utf8')
print(json.dumps({'preserved_files': len(files), 'archive_sha256': result['archive_sha256'],
                  'host_checks': [{'command': c['command'], 'exit': c['exit_code']} for c in checks]}))
