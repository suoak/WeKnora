"""Deterministic structural fixtures, not complete Office documents or vendor samples.
Run: python internal/officeguard/testdata/generate.py
"""
import io
import json
import struct
import zipfile
from pathlib import Path

ROOT = Path(__file__).parent
manifest = []


def save(name, data, state, frontend=None):
    (ROOT / name).write_bytes(data)
    manifest.append(dict(name=name, state=state, frontend=frontend or state))


def cfb(streams, mini=False):
    # header; FAT sector 0; directory sector 1; regular streams or mini stream
    h = bytearray(512)
    h[:8] = bytes.fromhex('d0cf11e0a1b11ae1')
    struct.pack_into('<HHHHH', h, 24, 62, 3, 65534, 9, 6)
    struct.pack_into('<IIIIIIIII', h, 40, 0, 1, 1, 0, 4096, 3 if mini else 0xfffffffe, 1 if mini else 0, 0xfffffffe, 0)
    for i in range(109):
        struct.pack_into('<I', h, 76 + i * 4, 0 if i == 0 else 0xffffffff)
    fat = [0xffffffff] * 128
    fat[0], fat[1] = 0xfffffffd, 0xfffffffe
    directory = bytearray(512)

    def entry(idx, name, kind, start, size, right=0xffffffff, child=0xffffffff):
        off = idx * 128
        encoded = (name + '\0').encode('utf-16le')
        directory[off:off + len(encoded)] = encoded
        struct.pack_into('<HBBIII', directory, off + 64, len(encoded), kind, 1, 0xffffffff, right, child)
        struct.pack_into('<IQ', directory, off + 116, start, size)

    chunks = []
    if mini:
        small = bytearray(512)
        mf = [0xffffffff] * 128
        pos = 0
        for i, (name, data) in enumerate(streams):
            sectors = max(1, (len(data) + 63) // 64)
            small[pos * 64:pos * 64 + len(data)] = data
            for j in range(sectors):
                mf[pos + j] = pos + j + 1 if j + 1 < sectors else 0xfffffffe
            entry(i + 1, name, 2, pos, len(data), i + 2 if i + 1 < len(streams) else 0xffffffff)
            pos += sectors
        fat[2] = fat[3] = 0xfffffffe
        entry(0, 'Root Entry', 5, 2, 512, child=1)
        chunks = [small, struct.pack('<128I', *mf)]
    else:
        sid = 2
        entry(0, 'Root Entry', 5, 0xfffffffe, 0, child=1)
        for i, (name, data) in enumerate(streams):
            data = data.ljust(4096, b'\0')
            entry(i + 1, name, 2, sid, len(data), i + 2 if i + 1 < len(streams) else 0xffffffff)
            sectors = (len(data) + 511) // 512
            for j in range(sectors):
                fat[sid + j] = sid + j + 1 if j + 1 < sectors else 0xfffffffe
            chunks.append(data.ljust(sectors * 512, b'\0'))
            sid += sectors
    return bytes(h) + struct.pack('<128I', *fat) + bytes(directory) + b''.join(chunks)


def zipdoc(parts):
    out = io.BytesIO()
    with zipfile.ZipFile(out, 'w', compression=zipfile.ZIP_DEFLATED) as z:
        for name, content in parts.items():
            z.writestr(zipfile.ZipInfo(name, date_time=(2020, 1, 1, 0, 0, 0)), content)
    return out.getvalue()


for ext, part in [('docx', 'word/document.xml'), ('xlsx', 'xl/workbook.xml'), ('xlsm', 'xl/workbook.xml'), ('pptx', 'ppt/presentation.xml')]:
    save('normal.' + ext, zipdoc({part: '<root/>', '[Content_Types].xml': '<Types/>'}), 'readable')
    save('encrypted.' + ext, cfb([('EncryptionInfo', struct.pack('<HHI', 4, 4, 64)), ('EncryptedPackage', b'1234567890')], mini=True), 'encrypted')
save('modify-protected.docx', zipdoc({'word/document.xml': '<doc/>', 'word/settings.xml': '<w:documentProtection w:enforcement="1"/>'}), 'protected')
save('sheet-protected.xlsx', zipdoc({'xl/workbook.xml': '<workbook/>', 'xl/worksheets/sheet1.xml': '<sheetProtection password="ABCD"/>'}), 'protected')
for flags, name, state in [(0, 'normal', 'readable'), (256, 'encrypted', 'encrypted'), (2048, 'modify-protected', 'protected'), (33024, 'xor', 'encrypted')]:
    fib = bytearray(32)
    struct.pack_into('<H', fib, 0, 0xa5ec)
    struct.pack_into('<H', fib, 10, flags)
    save(name + '.doc', cfb([('WordDocument', bytes(fib))]), state)
for name, record, state in [('normal', b'', 'readable'), ('encrypted', struct.pack('<HHHH', 47, 4, 0, 0), 'encrypted'), ('sheet-protected', struct.pack('<HHH', 18, 2, 1), 'protected')]:
    data = struct.pack('<HHHH', 0x809, 4, 0x600, 5) + record + struct.pack('<HH', 10, 0)
    save(name + '.xls', cfb([('Workbook', data)], mini=True), state)
for name, token in [('normal', 0xe391c05f), ('encrypted', 0xf3d1c4df)]:
    data = struct.pack('<HHIIII', 0, 0xff6, 12, 20, token, 0)
    save(name + '.ppt', cfb([('Current User', data)], mini=True), 'encrypted' if name == 'encrypted' else 'readable')
save('disguised.txt', (ROOT / 'encrypted.docx').read_bytes(), 'encrypted')
save('wps-ooxml.docx', (ROOT / 'encrypted.docx').read_bytes(), 'encrypted')
save('unknown.wps', b'WPS proprietary content', 'unknown')
save('broken.docx', b'PK\x03\x04truncated', 'invalid')
save('header-only.xls', bytes.fromhex('d0cf11e0a1b11ae1'), 'invalid')
save('unknown-ole.doc', cfb([('Other', b'data')]), 'unknown')
bad = bytearray((ROOT / 'normal.doc').read_bytes())
struct.pack_into('<I', bad, 512 + 4, 1)
save('cycle.doc', bad, 'invalid')
save('spoof-names.docx', cfb([('EncryptionInfo', b'bad info'), ('EncryptedPackage', b'bad package')]), 'unknown')
# Synthetic IRM hierarchy. It carries no customer document or license payload.
def drm_fixture():
    raw = bytearray(cfb([('EncryptedPackage', b'opaque-package'),
        ('DRMEncryptedDataSpace', struct.pack('<III', 8, 1, 42) + 'DRMEncryptedTransform'.encode('utf-16le') + b'\0\0'),
        ('\x06Primary', b'\0'*8 + 'Microsoft.Metadata.DRMTransform'.encode('utf-16le'))]))
    # Eight directory entries require two chained directory sectors. Place the
    # second at unused sector 26 (three existing 8-sector streams start at 2).
    raw.extend(bytes(512)); struct.pack_into('<I', raw, 512+4, 26)
    struct.pack_into('<I', raw, 512+26*4, 0xfffffffe)
    directory=bytearray(1024)
    nodes=[('Root Entry',5,0xfffffffe,0,1,0xffffffff),
        ('EncryptedPackage',2,2,4096,0xffffffff,2),
        ('\x06DataSpaces',1,0xfffffffe,0,3,0xffffffff),
        ('DataSpaceInfo',1,0xfffffffe,0,4,5),
        ('DRMEncryptedDataSpace',2,10,4096,0xffffffff,0xffffffff),
        ('TransformInfo',1,0xfffffffe,0,6,0xffffffff),
        ('DRMEncryptedTransform',1,0xfffffffe,0,7,0xffffffff),
        ('\x06Primary',2,18,4096,0xffffffff,0xffffffff)]
    for i,(name,kind,start,size,child,right) in enumerate(nodes):
        pos=i*128; name=(name+'\0').encode('utf-16le');directory[pos:pos+len(name)]=name
        struct.pack_into('<HBBIII',directory,pos+64,len(name),kind,1,0xffffffff,right,child)
        struct.pack_into('<IQ',directory,pos+116,start,size)
    raw[1024:1536]=directory[:512];raw[(26+1)*512:(26+2)*512]=directory[512:]
    return raw
drm=drm_fixture();save('drm.xlsx',drm,'drm');save('drm-disguised.txt',drm,'drm')
stale=bytearray(drm);struct.pack_into('<I',stale,1024+128+72,0xffffffff)
save('drm-stale.xlsx',stale,'invalid')
spoof=bytearray(drm);spoof[(10+1)*512+12]^=1
save('drm-spoof.xlsx',spoof,'invalid')
(ROOT / 'manifest.json').write_text(json.dumps(manifest, indent=2) + '\n', encoding='utf-8')
