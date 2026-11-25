"""Publish bounded, credential-free summaries; retain full logs in ignored storage."""
import collections,datetime,hashlib,json,pathlib,shutil
from office_test_remote import ROOT
OUT=ROOT/'docs/qa/office-test-deployment';PRIVATE=OUT/'private'
SHA='668d067ec75a2a351ace4f7f0797be77af2b24ae'
def write(path,value):path.write_text(json.dumps(value,ensure_ascii=False,indent=2)+'\n',encoding='utf-8')
for p in OUT.glob('preflight-*.json'):
    if p.name=='preflight-summary.json':continue
    v=json.loads(p.read_text(encoding='utf-8'))
    summary={'observed_utc':v['observed_utc'],'hostname':v['hostname'],'uid':v['user_uid'],'task_directory':v['task_directory'],
        'free_ports':v['free_ports'],'resource_checks':[r for r in v['checks'] if r['argv'][0] in ('df','free','nproc','uptime')],
        'existing_services_changed':False}
    write(OUT/'preflight-summary.json',summary)
    p.replace(PRIVATE/p.name)
p=OUT/'network-preflight.json'
if p.exists():p.replace(PRIVATE/p.name)
browser=PRIVATE/'browser';target=OUT/'browser';target.mkdir(exist_ok=True)
for name in ('results.json','browser_password_no_request.png','browser_drm_no_request.png','browser_real_user_drm_no_request.png','batch-selected.png','batch-after.png'):
    if (browser/name).exists():shutil.copyfile(browser/name,target/name)
summary={'source_sha':SHA,'linux_run':45530472812,'native_run':45530472495,'image_run':45530538647,
    'linux':{},'native':{},'full_logs':'private/ci-45530472812 and private/ci-45530472495; retained locally, not committed',
    'sha_note':'These are test PASS events including named parent/subtest events, not a claim of independent scenarios.'}
for kind,folder in [('linux',PRIVATE/'ci-45530472812'),('native',PRIVATE/'ci-45530472495')]:
    for p in folder.rglob('*.jsonl'):
        events=[json.loads(line) for line in p.read_text(encoding='utf-8').splitlines() if line.startswith('{')]
        summary[kind][p.name]={action:sum(e.get('Action')==action and bool(e.get('Test')) for e in events) for action in ('pass','fail','skip')}
    for p in folder.rglob('source-sha.txt'):assert p.read_text().strip()==SHA
tests=next((PRIVATE/'ci-45530472812').rglob('tests.txt')).read_text(encoding='utf-8')
summary['frontend_totals']=tests[tests.rfind('ℹ tests '):]
write(OUT/'final-ci-summary.json',summary)
source=PRIVATE/'sample-manifest.json'
if source.exists():shutil.copyfile(source,OUT/'generated-public-sample-manifest.json')
index=[]
for p in sorted(PRIVATE.rglob('*')):
    if p.is_file() and (p.suffix in ('.log','.txt','.jsonl') or p.name in ('image-id.txt','source-sha.txt')):
        h=hashlib.sha256()
        with p.open('rb') as f:
            while data:=f.read(1024*1024):h.update(data)
        index.append({'path':str(p.relative_to(OUT)).replace('\\','/'),'bytes':p.stat().st_size,'sha256':h.hexdigest()})
write(OUT/'log-integrity-manifest.json',index)
print(json.dumps({'published':'bounded summaries, own-test screenshots, log hashes; no tokens or session files','logs_indexed':len(index)}))
