"""Move accidentally collected non-secret KB IDs into ignored task state only."""
import json,subprocess
from office_test_remote import ROOT
OUT=ROOT/'docs/qa/office-test-deployment'
target=OUT/'private/collected-state';target.mkdir(exist_ok=True)
for path in [*OUT.glob('acceptance-kb*.json')]:
    assert path.parent.resolve()==OUT.resolve() and not path.is_symlink()
    relative=str(path.relative_to(ROOT)).replace('\\','/')
    subprocess.run(['git','restore','--staged','--',relative],cwd=ROOT,check=True)
    destination=target/path.name
    assert not destination.exists()
    path.replace(destination)
p=OUT/'runtime-qualification.json'
raw=p.read_bytes()
if raw.startswith((b'\xff\xfe',b'\xfe\xff')):
    value=json.loads(raw.decode('utf-16'));p.write_text(json.dumps(value,indent=2)+'\n',encoding='utf-8')
print(json.dumps({'collected_kb_state_kept_private':True,'remote_or_business_files_changed':False}))
