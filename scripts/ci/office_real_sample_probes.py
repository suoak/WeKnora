"""Probe actual authorized WPS-produced / IRM files without uploading cleartext."""
import hashlib,json,subprocess,xml.etree.ElementTree as ET
from pathlib import Path
from office_test_remote import ROOT
PRIVATE=ROOT/'docs/qa/office-test-deployment/private'
inventory=json.loads((PRIVATE/'local-sample-inventory.json').read_text(encoding='utf-8'))
selected=[r for r in inventory if r['kind']=='DRM' or 'wps' in r.get('producer_xml','').lower()]
paths=[r['path'] for r in selected]
(PRIVATE/'real-sample-paths.json').write_text(json.dumps(paths),encoding='utf-8')
go='C:/Users/admin/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.11.windows-amd64/bin/go.exe'
proc=subprocess.run([go,'run','./scripts/officeguard-probe',*paths],cwd='E:/code/WeKnora-office-drm',capture_output=True,text=True,encoding='utf-8',timeout=180)
assert proc.returncode==0,proc.stderr[-1000:]
backend=[json.loads(line) for line in proc.stdout.splitlines()]
js='''import {File} from 'node:buffer';import fs from 'node:fs';
import {inspectOffice} from 'file:///E:/code/WeKnora-office-drm/frontend/src/utils/officeEncryption.ts';
const files=JSON.parse(fs.readFileSync('docs/qa/office-test-deployment/private/real-sample-paths.json','utf8'));
for(const p of files){const b=fs.readFileSync(p);console.log(JSON.stringify({classification:await inspectOffice(new File([b],p.split(/[\\\\/]/).pop()))}));}'''
proc=subprocess.run(['node','--experimental-strip-types','--input-type=module','-e',js],cwd=ROOT,capture_output=True,text=True,encoding='utf-8',timeout=180)
assert proc.returncode==0,proc.stderr[-1000:]
frontend=[json.loads(line) for line in proc.stdout.splitlines()]
assert len(backend)==len(frontend)==len(selected)
result=[]
for row,back,front in zip(selected,backend,frontend):
    p=Path(row['path']);app=''
    if row.get('producer_xml'):
        metadata=ET.fromstring(row['producer_xml']);element=metadata.find('{*}Application');app=element.text if element is not None else ''
    result.append({'sample_id':hashlib.sha256(p.read_bytes()).hexdigest(),'extension':p.suffix.lower(),'bytes':p.stat().st_size,
        'producer_metadata':app or 'Not visible without DRM authorization; not claimed as WPS',
        'expected_family':row['kind'],'backend':back['classification'],'frontend':front['classification'],
        'scope':'Local actual-file format identification only; no cleartext business file uploaded or parser acceptance claimed'})
out=ROOT/'docs/qa/office-test-deployment/real-sample-probes.json'
out.write_text(json.dumps({'source_sha':'668d067ec75a2a351ace4f7f0797be77af2b24ae','samples':result},ensure_ascii=False,indent=2),encoding='utf-8')
print(json.dumps({'count':len(result),'classifications':{s:sum(r['backend']==s for r in result) for s in {r['backend'] for r in result}},'mismatches':sum(r['backend']!=r['frontend'] for r in result)}))
