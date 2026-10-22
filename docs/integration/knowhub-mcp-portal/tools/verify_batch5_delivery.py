"""Offline candidate binding, historical preservation and evidence inventory.

Run after collecting the final Linux test directory. No network, database writes,
git mutation, deletion, credential output or production access.
"""
import hashlib
import json
from pathlib import Path
import subprocess
import sys

ROOT=Path(__file__).resolve().parents[4]
DOCS=ROOT/'docs/integration/knowhub-mcp-portal'
OUT=DOCS/'evidence/batch5'
candidate=sys.argv[1]
frozen=json.loads((OUT/'candidates'/(candidate+'.json')).read_text(encoding='utf8'))

def sha(path):return hashlib.sha256(path.read_bytes()).hexdigest()
def digest(files):return hashlib.sha256(json.dumps(files,sort_keys=True).encode()).hexdigest()

previous=json.loads((DOCS/'evidence/batch4/artifact-sha256.json').read_text(encoding='utf8'))['files']
changed_previous=[name for name,value in previous.items() if not (DOCS/name).is_file() or sha(DOCS/name)!=value]
assert not changed_previous,changed_previous
prior_early=json.loads((DOCS/'evidence/batch4/preserved-prior-evidence.json').read_text(encoding='utf8'))
changed_early=[row['path'] for row in prior_early if not (ROOT/row['path']).is_file() or sha(ROOT/row['path'])!=row['sha256']]
assert not changed_early,changed_early

names=set(subprocess.check_output(['git','ls-files'],cwd=ROOT).decode().splitlines())
names.update(subprocess.check_output(['git','ls-files','--others','--exclude-standard'],cwd=ROOT).decode().splitlines())
files={name:sha(ROOT/name) for name in sorted(names) if (ROOT/name).is_file() and not name.startswith('docs/integration/knowhub-mcp-portal/evidence/')}
# Runtime, tests and actual harness scripts must match. The offline delivery
# checker is not executed by the candidate tests; its own hash stays in files.
checker='docs/integration/knowhub-mcp-portal/tools/verify_batch5_delivery.py'
def tested_input(name):
    return name!=checker and (not name.startswith('docs/') or name.startswith('docs/integration/knowhub-mcp-portal/tools/'))
inputs={name:value for name,value in files.items() if tested_input(name)}
mismatch=[name for name,value in inputs.items() if frozen['files'].get(name)!=value]
mismatch += [name for name in frozen['files'] if tested_input(name) and name not in inputs]
assert not mismatch,mismatch
prior_delivery=json.loads((DOCS/'evidence/batch4/source-manifest.json').read_text(encoding='utf8'))
prior_runtime=json.loads((DOCS/'evidence/batch4/linux/675615/manifest.json').read_text(encoding='utf8'))['files']
assert digest(prior_runtime)==prior_delivery['full_frozen_test_candidate_sha256']
prior_runtime.update(prior_delivery['changed_and_new_files'])
changed={name:value for name,value in inputs.items() if name.startswith(('internal/','mcp-server/','migrations/')) and prior_runtime.get(name)!=value}
inventory={name:{'status':'modified' if name in prior_runtime else 'added','previous_sha256':prior_runtime.get(name),'current_sha256':value} for name,value in files.items() if (name.startswith(('internal/','mcp-server/','migrations/')) or name.startswith('docs/integration/knowhub-mcp-portal/tools/')) and prior_runtime.get(name)!=value}
(OUT/'change-inventory.json').write_text(json.dumps(inventory,indent=2),encoding='utf8')
results_dir=OUT/'linux'/('results-'+candidate[:16]+'-all')
done=json.loads((results_dir/'done.json').read_text())
results=json.loads((results_dir/'results.json').read_text())
assert done['completed'] and done['candidate_sha256']==candidate
observations=[]
for path in sorted((OUT/'linux').glob('results-*/capacity.jsonl')):
    for line in path.read_text().splitlines():
        if line.strip():observations.append(json.loads(line))
usage=[row['observed_task_bytes'] for row in observations if 'observed_task_bytes' in row]
capacity={
    'samples':len(observations),
    'min_available_bytes':{mount:min(row['filesystems'][mount]['available_bytes'] for row in observations if 'filesystems' in row) for mount in ['/','/mnt/sata0']},
    'observed_task_high_water_bytes':max(usage) if usage else None,
    'exact_instantaneous_peak':'NOT_MEASURED',
    'previous_batch_exact_peak':'NOT_MEASURED',
    'monitor_errors':[row for row in observations if 'monitor_error' in row],
}
record={
    'base_sha':subprocess.check_output(['git','rev-parse','HEAD'],cwd=ROOT).decode().strip(),
    'branch':subprocess.check_output(['git','branch','--show-current'],cwd=ROOT).decode().strip(),
    'frozen_test_candidate_sha256':candidate,'current_delivery_source_sha256':digest(files),
    'verified_test_input_sha256':digest(inputs),'all_current_test_inputs_match':True,
    'previous_batch_artifacts_verified':len(previous),'all_prior_artifacts_unchanged':True,
    'batches1_to3_artifacts_verified':len(prior_early),
    'source_files':files,'test_inputs':inputs,'changed_runtime_or_test_files_since_batch4':changed,
    'capacity':capacity,'capacity_gate_blocked':done['blocked_capacity'],'final_commands':results,'linux_all_commands_passed':done['all_commands_passed'],
    'windows_full_exit_code':int((OUT/'windows-after.exit.txt').read_text(encoding='utf-8-sig').strip()),
    'production_enabled':False,'m3_overall':'HOLD',
}
(OUT/'delivery-validation.json').write_text(json.dumps(record,ensure_ascii=False,indent=2),encoding='utf8')
artifact={str(p.relative_to(DOCS)).replace('\\','/'):sha(p) for p in sorted(OUT.rglob('*')) if p.is_file() and p.name!='artifact-sha256.json'}
for number in range(33,39):
    matching=list(DOCS.glob(str(number)+'_*.md'));assert len(matching)==1,number
    artifact[matching[0].name]=sha(matching[0])
(OUT/'artifact-sha256.json').write_text(json.dumps({'algorithm':'SHA256','files':artifact},ensure_ascii=False,indent=2),encoding='utf8')
print(json.dumps({'candidate':candidate,'test_inputs_match':True,'prior_artifacts_unchanged':len(previous),'changed_runtime_or_test_files':len(changed),'linux_all_commands_passed':done['all_commands_passed'],'windows_exit':record['windows_full_exit_code'],'m3_overall':'HOLD','capacity':capacity},ensure_ascii=False))
