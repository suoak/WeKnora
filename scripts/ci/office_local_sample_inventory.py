"""Read only authorized Downloads file headers and Office producer metadata.

Never extracts document content, licenses, credentials or source OAuth data.
Detailed paths remain in ignored local evidence.
"""
from pathlib import Path
import hashlib,json,zipfile
import olefile
from office_test_remote import ROOT

folder=Path('D:/RjDir/admin_d95c8884-f44c-4529-8f3b-48d5788371c1/Downloads')
rows=[]
for p in folder.iterdir():
    if not p.is_file() or p.suffix.lower() not in ('.docx','.xlsx','.xlsm','.pptx','.doc','.xls','.ppt','.wps','.et','.dps') or p.stat().st_size>100*1024**2:continue
    with p.open('rb') as f:head=f.read(8)
    row={'path':str(p),'bytes':p.stat().st_size,'producer_xml':'','kind':'other'}
    try:
        if head.startswith(b'PK\x03\x04'):
            with zipfile.ZipFile(p) as z:
                info=z.getinfo('docProps/app.xml')
                if info.file_size<=65536:row['producer_xml']=z.read(info).decode('utf-8','replace')[:4096]
            row['kind']='OOXML'
        elif head==bytes.fromhex('d0cf11e0a1b11ae1'):
            with olefile.OleFileIO(p) as c:
                names={'/'.join(x) for x in c.listdir()}
            row['kind']='DRM' if '\x06DataSpaces/DataSpaceInfo/DRMEncryptedDataSpace' in names and 'EncryptedPackage' in names else 'password_encrypted' if {'EncryptionInfo','EncryptedPackage'}<=names else 'legacy_or_unknown'
    except (OSError,ValueError,KeyError,zipfile.BadZipFile):row['kind']='metadata_unavailable'
    rows.append(row)
out=ROOT/'docs/qa/office-test-deployment/private/local-sample-inventory.json'
out.write_text(json.dumps(rows,ensure_ascii=False,indent=2),encoding='utf-8')
counts={k:sum(r['kind']==k for r in rows) for k in sorted({r['kind'] for r in rows})}
wps=[r for r in rows if 'wps' in r['producer_xml'].lower()]
print(json.dumps({'file_count':len(rows),'counts':counts,'wps_producer_count':len(wps),'wps_producer_versions':sorted({r['producer_xml'] for r in wps})[:3]},ensure_ascii=True))
