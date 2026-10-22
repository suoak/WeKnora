"""Offline seventh-batch evidence checks. Does not contact any remote system."""
import hashlib,json,re,subprocess,sys,tarfile
from pathlib import Path

ROOT=Path(__file__).resolve().parents[4];DOCS=ROOT/'docs/integration/knowhub-mcp-portal';OUT=DOCS/'evidence/batch7'
def sha(p):return hashlib.sha256(p.read_bytes()).hexdigest()
def digest(fs):return hashlib.sha256(json.dumps(fs,sort_keys=True).encode()).hexdigest()
candidate=sys.argv[1]
frozen=json.loads((OUT/'candidates'/(candidate+'.json')).read_text(encoding='utf8'));assert digest(frozen['files'])==candidate
prior=json.loads((OUT/'prior-source-snapshot.json').read_text(encoding='utf8'))
archive=Path(prior['archive_path']);assert sha(archive)==prior['archive_sha256']
with tarfile.open(archive,'r:gz')as tar:
    members={m.name:hashlib.sha256(tar.extractfile(m).read()).hexdigest()for m in tar.getmembers()if m.isfile()}
assert members==prior['files'],'preserved source archive does not match initial SHA256 manifest'
preserved=0
for n,h in prior['files'].items():
    if n.startswith('docs/integration/knowhub-mcp-portal/'):
        assert (ROOT/n).is_file() and sha(ROOT/n)==h,n
        preserved+=1
names=set(subprocess.check_output(['git','ls-files'],cwd=ROOT).decode().splitlines())|set(subprocess.check_output(['git','ls-files','--others','--exclude-standard'],cwd=ROOT).decode().splitlines())
current={n:sha(ROOT/n)for n in sorted(names)if (ROOT/n).is_file()and not n.startswith('docs/integration/knowhub-mcp-portal/evidence/')}
checker='docs/integration/knowhub-mcp-portal/tools/verify_batch7_delivery.py'
is_input=lambda n:n!=checker and (not n.startswith('docs/')or n.startswith('docs/integration/knowhub-mcp-portal/tools/'))
inputs={n:h for n,h in current.items()if is_input(n)}
assert all(frozen['files'].get(n)==h for n,h in inputs.items()),'current executable/test inputs differ from final candidate'
assert all(n in inputs for n in frozen['files']if is_input(n))
changed={n:{'previous_sha256':prior['files'].get(n),'current_sha256':h,'status':'modified'if n in prior['files']else'added'}for n,h in current.items()if prior['files'].get(n)!=h}
assert not any(n.startswith('migrations/')for n in changed),'new migration requires independent checks'
final=OUT/'linux'/('results-'+candidate[:16]+'-all')
done=json.loads((final/'done.json').read_text());commands=json.loads((final/'results.json').read_text())
assert done['completed'] and done['candidate_sha256']==candidate
ordinary=OUT/'linux'/('results-'+candidate[:16]+'-integration')
plain_done=json.loads((ordinary/'done.json').read_text());plain_commands=json.loads((ordinary/'results.json').read_text())
assert plain_done['completed']and plain_done['candidate_sha256']==candidate
assert done['all_commands_passed']and plain_done['all_commands_passed'],'final complete suites did not pass'
windows_files=list((OUT/'local').glob('*windows-python-results.json'));assert len(windows_files)==1
windows=json.loads(windows_files[0].read_text());assert windows[0]['exit_code']==1
assert all(sha(ROOT/n)==h for n,h in windows[0]['inputs'].items())
front_files=list((OUT/'local').glob('*frontend-results.json'));assert len(front_files)==1
front=json.loads(front_files[0].read_text());assert all(x['exit_code']==0 for x in front)
assert all(sha(ROOT/n)==h for row in front for n,h in row['inputs'].items())
production=json.loads((OUT/'production-inventory-readonly.json').read_text());assert production['ssh_exit_code']==0 and production['read_only']and not production['production_changes']
worksheet=json.loads((OUT/'first-confirmation-worksheet.json').read_text());assert worksheet['counts']['public_mcp_eligible_kbs']==0 and not worksheet['approval_performed']
positive=[]
for line in (OUT/'clients/codex-positive.log').read_text(encoding='utf8').splitlines():
    try:row=json.loads(line)
    except ValueError:continue
    if row.get('type')=='item.completed'and row.get('item',{}).get('type')=='mcp_tool_call':positive.append(row['item'])
expected={'list_knowledge_bases','search_knowledge','grep_chunks','list_documents','read_document'}
assert expected.issubset({r['tool']for r in positive if r['status']=='completed'})
assert any(r['tool']=='read_document'and r['arguments'].get('knowledge_id')=='sensitive'and r['status']=='failed'for r in positive)
for name in ('positive','other','withdrawn'):
    assert json.loads((OUT/f'clients/codex-{name}-result.json').read_text())['exit_code']==0
revoked=json.loads((OUT/'clients/codex-revoked-result.json').read_text());assert revoked['exit_code']!=0
assert 'HTTP 401' in (OUT/'clients/codex-revoked.log').read_text(encoding='utf8')
http=json.loads((OUT/'clients/revocation-http.json').read_text());assert http['events'][0]['status']==200 and http['events'][-1]['status']==200 and any(x['status']==401 for x in http['events'])
workmate=json.loads((OUT/'clients/workmate-result.json').read_text(encoding='utf8'));wm=json.loads(workmate['connection']['body'])
assert workmate['connection']['status']==200 and wm['data']['success']and {t['name']for t in wm['data']['tools']}==expected
assert not workmate['actual_tools_call']
assert 'Not logged in' in (OUT/'clients/claude.log').read_text(encoding='utf8')
runtime=lambda n:n.endswith('.go')and not n.endswith('_test.go')and not n.startswith('internal/testutil/')
client_bindings=[]
for name in ('positive','other','withdrawn','revoked'):
    row=json.loads((OUT/f'clients/codex-{name}-result.json').read_text());m=json.loads((OUT/'candidates'/(row['candidate_sha256']+'.json')).read_text())
    assert all(m['files'].get(n)==h for n,h in current.items()if runtime(n))
    client_bindings.append({'scenario':name,'candidate_sha256':row['candidate_sha256'],'application_go_runtime_matches_final_candidate':True})
observations=[]
for p in (OUT/'linux').glob('results-*/capacity.jsonl'):
    observations.extend(json.loads(line)for line in p.read_text().splitlines()if line.strip())
capacity={'samples':len(observations),'min_available_bytes':{mount:min(r['filesystems'][mount]['available_bytes']for r in observations if 'filesystems'in r)for mount in ('/','/mnt/sata0')},'observed_task_high_water_bytes':max((r['observed_task_bytes']for r in observations if 'observed_task_bytes'in r),default=None),'monitor_errors':[r for r in observations if'monitor_error'in r],'instantaneous_exact_peak':'NOT_MEASURED','blocked_final':done['blocked_capacity']or plain_done['blocked_capacity']}
assert not capacity['monitor_errors']and not capacity['blocked_final']
bad=[str(p.relative_to(OUT))for p in OUT.rglob('*')if p.is_file()and p.suffix in ('.log','.json','.jsonl','.md')and re.search(rb'mcp_[0-9a-fA-F]{64}',p.read_bytes())]
assert not bad,'plaintext synthetic credentials in evidence'
readback=json.loads((OUT/'remote-source-readback.json').read_text());assert any(x['candidate']==candidate for x in readback)
assert all(x['exit_code']==0 and x['all_source_files_unchanged_after_tests']for x in readback)
self_outputs={'evidence/batch7/delivery-validation.json','evidence/batch7/change-inventory.json','evidence/batch7/artifact-sha256.json'}
for number in range(46,53):
    p=next(DOCS.glob(str(number)+'_*.md'))
    for target in re.findall(r'\]\(([^)]+)\)',p.read_text(encoding='utf8')):
        if not target.startswith(('https:','http:','#')) and target not in self_outputs:assert (p.parent/target).exists(),(p.name,target)
record={'base_sha':frozen['base_sha'],'branch':subprocess.check_output(['git','branch','--show-current'],cwd=ROOT).decode().strip(),'candidate_sha256':candidate,'all_current_test_inputs_match_candidate':True,'test_inputs_sha256':digest(inputs),'current_delivery_source_sha256':digest(current),'source_files':current,'changed_source':changed,'prior_snapshot_files_verified':len(members),'prior_documentation_and_evidence_files_preserved':preserved,'final_commands':commands,'ordinary_pg_commands':plain_commands,'linux_all_passed':done['all_commands_passed']and plain_done['all_commands_passed'],'windows':windows,'frontend':front,'client_scope_bindings':client_bindings,'workmate':'CONNECT_AND_LIST_PASS_TOOL_CALL_NOT_TESTED','claude':'BLOCKED_CLIENT_AUTH','capacity':capacity,'remote_source_readback':readback,'production_enabled':False,'enterprise_mvp':'HOLD_FORMAL_IDENTITY_AUTHORITY_MODEL_QUALITY_AND_CLIENT_GATES','mixed_sensitive_m3':'HOLD'}
(OUT/'delivery-validation.json').write_text(json.dumps(record,ensure_ascii=False,indent=2),encoding='utf8')
(OUT/'change-inventory.json').write_text(json.dumps(changed,ensure_ascii=False,indent=2),encoding='utf8')
artifacts={str(p.relative_to(DOCS)).replace('\\','/'):sha(p)for p in sorted(OUT.rglob('*'))if p.is_file()and p.name!='artifact-sha256.json'}
for number in range(46,53):
    p=next(DOCS.glob(str(number)+'_*.md'));artifacts[p.name]=sha(p)
(OUT/'artifact-sha256.json').write_text(json.dumps({'algorithm':'SHA256','files':artifacts},ensure_ascii=False,indent=2),encoding='utf8')
print(json.dumps({'candidate':candidate,'linux_all_passed':record['linux_all_passed'],'preserved_files':preserved,'changed_source_files':len(changed),'windows_exit':1,'capacity':capacity,'mvp':'HOLD','m3':'HOLD'},ensure_ascii=False))
