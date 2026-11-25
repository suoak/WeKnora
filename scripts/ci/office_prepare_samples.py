"""Generate full valid Office canaries, retaining separate public/vendor provenance."""
import hashlib,json,shutil
from pathlib import Path
from docx import Document
from openpyxl import Workbook
from pptx import Presentation
from pptx.util import Inches
from office_test_remote import ROOT

OUT=ROOT/'docs/qa/office-test-deployment/private/samples';OUT.mkdir(parents=True,exist_ok=True)
for ext in ('docx','xlsx','pptx'):
    canary='officecanary normal document '+ext+' isolated acceptance 20261125'
    if ext=='docx':
        obj=Document();obj.add_heading('Office acceptance canary',0);obj.add_paragraph(canary);obj.save(OUT/('normal.'+ext))
    elif ext=='xlsx':
        obj=Workbook();obj.active['A1']=canary;obj.save(OUT/('normal.'+ext))
        obj.active.protection.sheet=True;obj.active.protection.password='test-edit-only';obj.save(OUT/'sheet-protected.xlsx')
    else:
        obj=Presentation();slide=obj.slides.add_slide(obj.slide_layouts[1]);slide.shapes.title.text='Office acceptance canary';slide.placeholders[1].text=canary;obj.save(OUT/('normal.'+ext))
# The Word edit restriction is a valid edit-protection setting, not an open password.
from docx.oxml import OxmlElement
from docx.oxml.ns import qn
obj=Document(OUT/'normal.docx');protection=OxmlElement('w:documentProtection');protection.set(qn('w:edit'),'readOnly');protection.set(qn('w:enforcement'),'1');obj.settings.element.append(protection);obj.save(OUT/'modify-protected.docx')
source=ROOT/'internal/officeguard/testdata'
for p in (source/'vendor').glob('*'):
    if p.suffix in ('.doc','.xls','.ppt','.docx','.xlsx'):shutil.copyfile(p,OUT/p.name)
for name in ('broken.docx','cycle.doc','unknown-ole.doc','header-only.xls','unknown.wps'):
    shutil.copyfile(source/name,OUT/name)
public=Path(__import__('tempfile').gettempdir())/'WPS_Spout_test.xlsx'
if public.exists():
    assert hashlib.sha256(public.read_bytes()).hexdigest()=='6f30d148bec68721b552710901d58ac27cdaa7ed9170e6ba555e6f298f708511'
    shutil.copyfile(public,OUT/public.name)
manifest=[{'name':p.name,'bytes':p.stat().st_size,'sha256':hashlib.sha256(p.read_bytes()).hexdigest(),
    'producer':'Python Office library' if p.name.startswith(('normal.','modify-protected.','sheet-protected.')) else 'public vendor fixture; see vendor/provenance.json' if (source/'vendor'/p.name).exists() else 'synthetic or separately recorded public WPS sample'} for p in OUT.iterdir() if p.is_file()]
(OUT.parent/'sample-manifest.json').write_text(json.dumps(manifest,indent=2)+'\n')
print(json.dumps({'sample_count':len(manifest),'directory':str(OUT)}))
