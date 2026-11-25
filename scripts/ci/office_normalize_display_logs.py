"""Normalize whitespace in checked-in display logs; preserve immutable originals."""
import hashlib,json,subprocess
from pathlib import Path
ROOT=Path(__file__).resolve().parents[2]
prior=subprocess.check_output(['git','rev-parse','HEAD'],cwd=ROOT,text=True).strip()
check=subprocess.run(['git','diff','--check','origin/main...HEAD'],cwd=ROOT,capture_output=True,text=True)
names=sorted({line.split(':')[0] for line in check.stdout.splitlines() if line.startswith('docs/')})
records=[]
for name in names:
    assert name.startswith('docs/qa/office-encryption-evidence/') and name.endswith('.txt')
    path=ROOT/name
    assert not path.is_symlink() and path.resolve().is_relative_to(ROOT.resolve())
    subprocess.run(['git','diff','--exit-code','HEAD','--',name],cwd=ROOT,check=True,capture_output=True)
    raw=subprocess.check_output(['git','show',prior+':'+name],cwd=ROOT)
    normalized=('\n'.join(line.rstrip() for line in raw.decode('utf-8').splitlines()).rstrip()+'\n').encode('utf-8')
    path.write_bytes(normalized)
    records.append({'path':name,'original_commit':prior,'original_sha256':hashlib.sha256(raw).hexdigest(),
        'display_sha256':hashlib.sha256(normalized).hexdigest(),'change':'trailing whitespace / final blank lines only'})
out=ROOT/'docs/qa/office-encryption-evidence/display-normalization.json'
out.write_text(json.dumps({'originals':'git show <original_commit>:<path>; original CI zip archives remain unchanged','files':records},indent=2)+'\n',encoding='utf-8')
print(json.dumps({'display_copies_normalized':len(records),'raw_archives_changed':False,'check_rules_relaxed':False}))
