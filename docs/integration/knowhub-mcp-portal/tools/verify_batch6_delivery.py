"""Offline source binding and preserved evidence verification for batch six."""
import ast,hashlib,json,re,subprocess,sys
from pathlib import Path

ROOT=Path(__file__).resolve().parents[4];DOCS=ROOT/'docs/integration/knowhub-mcp-portal';OUT=DOCS/'evidence/batch6'
candidate=sys.argv[1]
full_candidate=sys.argv[2] if len(sys.argv)>2 else candidate
def sha(p):return hashlib.sha256(p.read_bytes()).hexdigest()
def digest(files):return hashlib.sha256(json.dumps(files,sort_keys=True).encode()).hexdigest()
frozen=json.loads((OUT/'candidates'/(candidate+'.json')).read_text())
assert digest(frozen['files'])==candidate
prior=json.loads((OUT/'prior-source-snapshot.json').read_text())
snapshot=json.loads((OUT/'prior-snapshot-verification.json').read_text());assert snapshot['all_members_match_initial_manifest'] and snapshot['archive_sha256_matches']
preserved={}
for batch in (4,5):
    rows=json.loads((DOCS/('evidence/batch'+str(batch)+'/artifact-sha256.json')).read_text())['files']
    assert all((DOCS/n).is_file() and sha(DOCS/n)==value for n,value in rows.items())
    preserved['batch'+str(batch)]=len(rows)
early=json.loads((DOCS/'evidence/batch4/preserved-prior-evidence.json').read_text())
assert all((ROOT/row['path']).is_file() and sha(ROOT/row['path'])==row['sha256'] for row in early)
preserved['batches1_to3']=len(early)
names=set(subprocess.check_output(['git','ls-files'],cwd=ROOT).decode().splitlines());names.update(subprocess.check_output(['git','ls-files','--others','--exclude-standard'],cwd=ROOT).decode().splitlines())
files={n:sha(ROOT/n) for n in sorted(names) if (ROOT/n).is_file() and not n.startswith('docs/integration/knowhub-mcp-portal/evidence/')}
checker='docs/integration/knowhub-mcp-portal/tools/verify_batch6_delivery.py'
def is_input(n):return n!=checker and (not n.startswith('docs/') or n.startswith('docs/integration/knowhub-mcp-portal/tools/'))
inputs={n:value for n,value in files.items() if is_input(n)}
assert all(frozen['files'].get(n)==value for n,value in inputs.items()),'current source differs from tested candidate'
assert all(n in inputs for n in frozen['files'] if is_input(n))
full_frozen=json.loads((OUT/'candidates'/(full_candidate+'.json')).read_text())
assert digest(full_frozen['files'])==full_candidate
full_delta=[n for n,h in inputs.items() if full_frozen['files'].get(n)!=h]
supplemental=[]
if candidate!=full_candidate:
    # The only allowed overlay is two new state assertions in this explicitly
    # tagged integration test. Any application, harness or other test drift fails.
    assert full_delta==['internal/router/portal_mcp_postgres_test.go'],full_delta
    supplement_dir=OUT/'linux'/('results-'+candidate[:16]+'-integration')
    supplement_done=json.loads((supplement_dir/'done.json').read_text())
    supplemental=json.loads((supplement_dir/'results.json').read_text())
    assert supplement_done['completed'] and supplement_done['candidate_sha256']==candidate
else:assert not full_delta
changed={n:{'status':'modified' if n in prior['files'] else 'added','previous_sha256':prior['files'].get(n),'current_sha256':h} for n,h in files.items() if prior['files'].get(n)!=h}
assert not any(n.startswith('migrations/') for n in changed),'migration changes require separate acceptance'
result_dir=OUT/'linux'/('results-'+full_candidate[:16]+'-all')
done=json.loads((result_dir/'done.json').read_text());results=json.loads((result_dir/'results.json').read_text())
assert done['completed'] and done['candidate_sha256']==full_candidate
observations=[]
for p in (OUT/'linux').glob('results-*/capacity.jsonl'):
    observations.extend(json.loads(line) for line in p.read_text().splitlines() if line.strip())
usage=[r['observed_task_bytes'] for r in observations if 'observed_task_bytes' in r]
capacity={'samples':len(observations),'min_available_bytes':{mount:min(r['filesystems'][mount]['available_bytes'] for r in observations if 'filesystems' in r) for mount in ['/','/mnt/sata0']},'observed_task_high_water_bytes':max(usage) if usage else None,'instantaneous_exact_peak':'NOT_MEASURED','old_peak':'NOT_MEASURED','monitor_errors':[r for r in observations if 'monitor_error' in r],'final_blocked_capacity':done['blocked_capacity']}
bad=[]
for p in OUT.rglob('*.log'):
    if re.search(rb'mcp_[0-9a-fA-F]{64}',p.read_bytes()):bad.append(str(p.relative_to(OUT)))
assert not bad,'generated credentials appeared in logs'
for n in files:
    if n.startswith('docs/integration/knowhub-mcp-portal/tools/batch6_') and n.endswith('.py'):ast.parse((ROOT/n).read_text(encoding='utf8'))
coverage=json.loads((OUT/'production-coverage-shape-readonly.json').read_text())
assert coverage['read_only'] and not coverage['production_changes'] and coverage['ssh_exit_code']==0
readback=json.loads((OUT/'remote-source-readback.json').read_text())
assert {r['candidate'] for r in readback}=={candidate,full_candidate}
assert all(r['all_source_files_unchanged_after_tests'] and not r['mismatched_files'] and r['exit_code']==0 for r in readback)
passed=done['all_commands_passed'] and all(row['exit_code']==0 for row in supplemental)
record={'base_sha':subprocess.check_output(['git','rev-parse','HEAD'],cwd=ROOT).decode().strip(),'branch':subprocess.check_output(['git','branch','--show-current'],cwd=ROOT).decode().strip(),'frozen_test_candidate_sha256':candidate,'full_suite_candidate_sha256':full_candidate,'supplemental_tagged_test_delta':full_delta,'all_current_runtime_matches_full_suite':True,'test_inputs_sha256':digest(inputs),'current_delivery_source_sha256':digest(files),'all_current_test_inputs_match_recorded_test_candidates':True,'source_files':files,'test_inputs':inputs,'changed_source':changed,'preserved_artifacts_verified':preserved,'snapshot_files_verified':snapshot['verified_files'],'final_commands':results,'supplemental_commands':supplemental,'linux_all_commands_passed':passed,'capacity':capacity,'windows_full_exit':int((OUT/'windows-python-full.exit.txt').read_text()),'synthetic_credential_log_matches':0,'production_coverage':coverage,'production_enabled':False,'m3_overall':'HOLD'}
record['remote_source_readback']=readback
(OUT/'delivery-validation.json').write_text(json.dumps(record,ensure_ascii=False,indent=2),encoding='utf8')
(OUT/'change-inventory.json').write_text(json.dumps(changed,ensure_ascii=False,indent=2),encoding='utf8')
artifacts={str(p.relative_to(DOCS)).replace('\\','/'):sha(p) for p in sorted(OUT.rglob('*')) if p.is_file() and p.name!='artifact-sha256.json'}
for number in range(39,46):
    paths=list(DOCS.glob(str(number)+'_*.md'));assert len(paths)==1,number
    artifacts[paths[0].name]=sha(paths[0])
(OUT/'artifact-sha256.json').write_text(json.dumps({'algorithm':'SHA256','files':artifacts},ensure_ascii=False,indent=2),encoding='utf8')
print(json.dumps({'candidate':candidate,'full_suite_candidate':full_candidate,'inputs_match_recorded_candidates':True,'preserved':preserved,'changed_source_files':len(changed),'linux_all_commands_passed':passed,'windows_exit':record['windows_full_exit'],'capacity':capacity,'m3':'HOLD'},ensure_ascii=False))
