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
for major, minor in [(2, 3), (2, 4), (3, 4), (5, 4)]:
    save('unknown-version-%d.%d.docx' % (major, minor), cfb([
        ('EncryptionInfo', struct.pack('<HHI', major, minor, 64)),
        ('EncryptedPackage', b'1234567890')], mini=True), 'unknown')
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

# Fully synthetic classic binary IRM structures, no production bytes/licenses.
def classic_fixture(viewer=False, embedded=False):
    def lp(s):
        b=s.encode('utf-16le'); return struct.pack('<I',len(b))+b+bytes((-len(b))%4)
    def mapping(stream,space):
        b=struct.pack('<II',1,0)+lp(stream)+lp(space)
        return struct.pack('<I',len(b)+4)+b
    m=mapping('\x09DRMContent','\x09DRMDataSpace')
    if viewer:m+=mapping('\x09DRMViewerContent','\x09LZXDRMDataSpace')
    m=struct.pack('<II',8,2 if viewer else 1)+m
    primary=struct.pack('<II',88,1)+lp('{C73DFACD-061F-43B0-8B64-0C620D2A8B50}')+lp('Microsoft.Metadata.DRMTransform')+struct.pack('<III',1,1,1)
    fib=struct.pack('<H',0xa5ec)+bytes(30)
    data={('WordDocument',):fib.ljust(4096,b'\0'), ('\x09DRMContent',):b'opaque'.ljust(4096,b'\0'),
          ('\x06DataSpaces','DataSpaceInfo','\x09DRMDataSpace'):struct.pack('<II',8,1)+lp('\x09DRMTransform'),
          ('\x06DataSpaces','DataSpaceMap'):m,
          ('\x06DataSpaces','TransformInfo','\x09DRMTransform','\x06Primary'):primary}
    if viewer:data[('\x09DRMViewerContent',)]=bytes(4096)
    if embedded:data={('Embedded',)+k:v for k,v in data.items() if k!=('WordDocument',)}|{('WordDocument',):fib.ljust(4096,b'\0')}
    paths=[()]
    for path in data:
        for i in range(1,len(path)+1):
            if path[:i] not in paths:paths.append(path[:i])
    nd=(len(paths)+3)//4;fat=[0xffffffff]*128;fat[0]=0xfffffffd
    for i in range(1,nd+1):fat[i]=i+1 if i<nd else 0xfffffffe
    chunks=[];sid=nd+1;small=bytearray();mf=[0xffffffff]*128;locations={}
    for path,b in data.items():
        if len(b)<4096:
            start=len(small)//64;count=(len(b)+63)//64
            for j in range(count):mf[start+j]=start+j+1 if j+1<count else 0xfffffffe
            locations[path]=(start,len(b));small+=b+bytes((-len(b))%64)
        else:
            count=(len(b)+511)//512;locations[path]=(sid,len(b))
            for j in range(count):fat[sid+j]=sid+j+1 if j+1<count else 0xfffffffe
            chunks.append(b+bytes((-len(b))%512));sid+=count
    root_start=sid;root_size=len(small);count=(len(small)+511)//512
    for j in range(count):fat[sid+j]=sid+j+1 if j+1<count else 0xfffffffe
    chunks.append(small+bytes((-len(small))%512));sid+=count
    mini_start=sid;fat[sid]=0xfffffffe;chunks.append(struct.pack('<128I',*mf))
    directory=bytearray(nd*512)
    for i,path in enumerate(paths):
        kind=5 if not path else 2 if path in data else 1
        siblings=[x for x,p in enumerate(paths) if p and p[:-1]==path[:-1]] if path else []
        right=siblings[siblings.index(i)+1] if path and siblings.index(i)+1<len(siblings) else 0xffffffff
        children=[x for x,p in enumerate(paths) if p and p[:-1]==path]
        start,size=(root_start,root_size) if not path else locations.get(path,(0xfffffffe,0))
        name=('Root Entry' if not path else path[-1])+'\0';name=name.encode('utf-16le');off=i*128
        directory[off:off+len(name)]=name
        struct.pack_into('<HBBIII',directory,off+64,len(name),kind,1,0xffffffff,right,children[0] if children else 0xffffffff)
        struct.pack_into('<IQ',directory,off+116,start,size)
    h=bytearray(512);h[:8]=bytes.fromhex('d0cf11e0a1b11ae1')
    struct.pack_into('<HHHHH',h,24,62,3,65534,9,6)
    struct.pack_into('<IIIIIIIII',h,40,0,1,1,0,4096,mini_start,1,0xfffffffe,0)
    for i in range(109):struct.pack_into('<I',h,76+i*4,0 if i==0 else 0xffffffff)
    return bytearray(h)+struct.pack('<128I',*fat)+directory+b''.join(chunks)
classic=classic_fixture()
save('classic-irm.doc',classic,'drm')
save('classic-irm-viewer.doc',classic_fixture(True),'drm')
save('classic-irm-disguised.txt',classic,'drm')
save('classic-irm-embedded.doc',classic_fixture(embedded=True),'readable')
# A same-named root stream is insufficient without a valid relationship.
spoof=bytearray(classic);where=spoof.index('\x09DRMDataSpace'.encode('utf-16le'),(1+struct.unpack_from('<I',spoof,48)[0])*512)
# Rename the live definition; leave the stream and map intact.
spoof[where+2]^=1
save('classic-irm-spoof.doc',spoof,'readable')
stale=bytearray(classic);struct.pack_into('<I',stale,1024+128+72,0xffffffff)
save('classic-irm-stale.doc',stale,'readable')
(ROOT / 'manifest.json').write_text(json.dumps(manifest, indent=2) + '\n', encoding='utf-8')
