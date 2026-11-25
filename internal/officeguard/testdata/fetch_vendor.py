"""Fetch public MIT-licensed compatibility fixtures, never execute their content."""
import hashlib
import json
import time
import urllib.request
from pathlib import Path

root = Path(__file__).parent / 'vendor'
root.mkdir(exist_ok=True)


def get(url):
    for attempt in range(4):
        try:
            with urllib.request.urlopen(url, timeout=20) as response:
                return response.read()
        except Exception:
            if attempt == 3:
                raise
            time.sleep(1)


provenance = root / 'provenance.json'
if provenance.exists():
    commit = json.loads(provenance.read_text())['commit']
else:
    commit = json.loads(get('https://api.github.com/repos/nolze/msoffcrypto-tool/commits/master'))['sha']
    provenance.write_text(json.dumps(dict(commit=commit)))
base = 'https://raw.githubusercontent.com/nolze/msoffcrypto-tool/' + commit + '/'
(root / 'LICENSE.txt').write_bytes(get(base + 'LICENSE.txt'))
names = ['ecma376agile_sha1_password.xlsx', 'ecma376standard_password.docx',
         'example_password.docx', 'example_password.xlsx', 'plain.doc', 'plain.ppt',
         'plain.xls', 'rc4cryptoapi_password.doc', 'rc4cryptoapi_password.ppt',
         'rc4cryptoapi_password.xls', 'xor_password_123456789012345.xls']
entries = []
for name in names:
    data = get(base + 'tests/inputs/' + name)
    (root / name).write_bytes(data)
    state = 'readable' if name.startswith('plain') else 'encrypted'
    entries.append(dict(name='vendor/' + name, state=state, frontend=state,
                        sha256=hashlib.sha256(data).hexdigest()))
provenance.write_text(json.dumps(dict(repository='https://github.com/nolze/msoffcrypto-tool',
                                      commit=commit, fixtures=entries), indent=2) + '\n')
print('Fetched', len(entries), 'fixtures at', commit)
