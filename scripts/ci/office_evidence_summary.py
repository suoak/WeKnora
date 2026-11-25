"""Index archived CI logs and count actual Go test events, including skips."""
import argparse
from collections import Counter
import hashlib
import json
from pathlib import Path

p = argparse.ArgumentParser()
p.add_argument('root', type=Path)
p.add_argument('--out', type=Path, required=True)
a = p.parse_args()
report = {'runs': [], 'go_test_logs': [], 'limits': [
    'Counts include subtests; do not add counts as unique test cases.',
    'CI fixtures are not deployed test-site or production acceptance.',
]}
for path in sorted((a.root / 'ci-logs').glob('*.json')):
    run = json.loads(path.read_text(encoding='utf-8'))
    archive = path.with_suffix('.zip')
    report['runs'].append({
        'id': run['id'], 'sha': run['head_sha'], 'conclusion': run['conclusion'],
        'url': run['html_url'], 'archive': archive.relative_to(a.root).as_posix(),
        'sha256': hashlib.sha256(archive.read_bytes()).hexdigest(),
    })
for path in sorted(a.root.rglob('*.jsonl')):
    events = [json.loads(line) for line in path.read_text(encoding='utf-8').splitlines()
              if line.startswith('{')]
    cases = [event for event in events if event.get('Test')]
    report['go_test_logs'].append({
        'path': path.relative_to(a.root).as_posix(),
        'events': dict(Counter(e['Action'] for e in cases if e['Action'] in ('pass', 'fail', 'skip'))),
        'failed': [e['Test'] for e in cases if e['Action'] == 'fail'],
        'skipped': [e['Test'] for e in cases if e['Action'] == 'skip'],
    })
a.out.write_text(json.dumps(report, indent=2) + '\n', encoding='utf-8')
print(json.dumps({'archived_runs': len(report['runs']), 'go_logs': len(report['go_test_logs'])}))
