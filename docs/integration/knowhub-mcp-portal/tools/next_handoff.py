"""Package immutable candidate + reports + public evidence, excluding secrets."""
from pathlib import Path
import datetime,hashlib,json,re,shlex,tarfile
from v1_host import connect

root=Path(__file__).resolve().parents[4];docs=root/'docs/integration/knowhub-mcp-portal';e=docs/'evidence/v1-wiki-dynamic'
c=json.loads((e/'current.json').read_text());short=c['candidate_sha256'][:16]
c=json.loads((e/(c['candidate_sha256']+'.json')).read_text())
gate=json.loads((e/('delivery-gates-'+short+'.json')).read_text())
assert gate['isolated_synthetic_gate_pass']and gate['candidate_sha256']==c['candidate_sha256']
source=Path(c['archive']);assert hashlib.sha256(source.read_bytes()).hexdigest()==c['archive_sha256']
stamp=datetime.datetime.now(datetime.timezone.utc).strftime('%Y%m%dT%H%M%SZ')
archive=Path('E:/AIData/Codex')/('knowhub-wiki-dynamic-handoff-'+short+'-'+stamp+'.tar.gz')
assert not archive.exists()
items={'frozen-source/'+source.name:source}
for name in ['V1_WIKI_DYNAMIC_EXECUTION.md','V1_WIKI_DYNAMIC_SAFETY_DECISIONS.md','V1_WIKI_DYNAMIC_REQUIREMENTS.md','V1_WIKI_DYNAMIC_FINAL_DELIVERY.md','V1_WIKI_DYNAMIC_TEST_RESULTS.md']:
    items['reports/'+name]=docs/name
for p in e.rglob('*'):
    if p.is_file():items['evidence/'+str(p.relative_to(e)).replace('\\','/')]=p
for p in (docs/'tools').glob('next_*'):
    if p.is_file():items['executed-harness/'+p.name]=p
# Credentials remain in protected local/server private directories, never in
# this artifact. Check known local test credentials in addition to patterns.
secrets=set()
def harvest(v):
    if isinstance(v,dict):
        for k,x in v.items():
            if k in ('token','old_token','admin_jwt','owner_jwt','password')and isinstance(x,str)and len(x)>16:secrets.add(x.encode())
            else:harvest(x)
    elif isinstance(v,list):
        for x in v:harvest(x)
for p in Path('E:/AIData/Codex/knowhub-v1-wiki-dynamic-private').glob('*/client.json'):harvest(json.loads(p.read_text(encoding='utf8')))
patterns=[rb'-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----',rb'gh[pousr]_[A-Za-z0-9]{36,}',rb'sk-proj-[A-Za-z0-9_-]{40,}',rb'eyJ[A-Za-z0-9_-]{12,}\.[A-Za-z0-9_-]{15,}\.[A-Za-z0-9_-]{20,}']
for name,p in items.items():
    if name.startswith('frozen-source/'):continue # Frozen source separately scanned and immutable.
    raw=p.read_bytes()
    assert not any(secret in raw for secret in secrets), 'Known credential in '+name
    assert not any(re.search(pattern,raw)for pattern in patterns), 'Credential pattern in '+name
manifest={'candidate_sha256':c['candidate_sha256'],'runtime_source_sha256':c['runtime_source_sha256'],'base_sha':c['base_sha'],
          'source_archive_sha256':c['archive_sha256'],'files':{n:hashlib.sha256(p.read_bytes()).hexdigest()for n,p in sorted(items.items())},
          'created_utc':stamp,'production_authorized':False,'images_exported':False,
          'image_material':'Exact image IDs and source/binary hashes in runtime-inventory, build images/artifacts manifests; images retained on build host',
          'secret_check':{'known_test_secret_scan':True,'public_artifact_patterns':True,'private_files_included':False}}
data=json.dumps(manifest,indent=2).encode()
with tarfile.open(archive,'w:gz',compresslevel=1)as tf:
    for n,p in sorted(items.items()):tf.add(p,arcname=n,recursive=False)
    import io
    info=tarfile.TarInfo('handoff-manifest.json');info.size=len(data);tf.addfile(info,io.BytesIO(data))
with tarfile.open(archive)as tf:
    actual={x.name:hashlib.sha256(tf.extractfile(x).read()).hexdigest()for x in tf if x.isfile()and x.name!='handoff-manifest.json'}
    assert actual==manifest['files']
sha=hashlib.sha256(archive.read_bytes()).hexdigest()
remote='/home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-wiki-dynamic-handoff-20261022/'+archive.name
with connect()as ssh:
    _,o,x=ssh.exec_command('mkdir -p '+shlex.quote(remote.rsplit('/',1)[0])+' && test ! -e '+shlex.quote(remote));assert o.channel.recv_exit_status()==0,x.read().decode()
    with ssh.open_sftp()as ftp:ftp.put(str(archive),remote)
    _,o,x=ssh.exec_command('sha256sum '+shlex.quote(remote));assert o.channel.recv_exit_status()==0,x.read().decode();assert o.read().decode().split()[0]==sha
receipt={'candidate_sha256':c['candidate_sha256'],'runtime_source_sha256':c['runtime_source_sha256'],'archive':str(archive),'archive_sha256':sha,
         'remote':remote,'remote_sha256_verified':True,'file_count':len(items),'production_approved':False,'manifest_sha256':hashlib.sha256(data).hexdigest()}
(e/('handoff-receipt-'+short+'.json')).write_text(json.dumps(receipt,indent=2),encoding='utf8')
print(json.dumps(receipt))
