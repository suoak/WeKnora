"""Upgrade only our disposable preview after exact-SHA Linux and native CI pass.

No migrations or production configuration change belongs to this operation.
"""
import hashlib,json,stat,subprocess
from office_test_remote import ROOT,REMOTE_ROOT,connect,execute
SHA='668d067ec75a2a351ace4f7f0797be77af2b24ae'
PRIVATE=ROOT/'docs/qa/office-test-deployment/private'
GH='E:/AIData/Scoop/apps/gh/2.97.0/bin/gh.exe'

def main():
    for run_id in ('45530472812','45530472495','45530538647'):
        state=json.loads(subprocess.check_output([GH,'run','view',run_id,'--repo','suoak/WeKnora','--json','status,conclusion,headSha']))
        assert state['status']=='completed' and state['conclusion']=='success','CI required before deployment: '+run_id
        if run_id!='45530538647':assert state['headSha']==SHA
        else:assert state['headSha']=='5f7ee5896efebe29b2cf9611ab568105300478b3','unexpected runtime packager'
    images={}
    for name in ('app','frontend'):
        folder=PRIVATE/SHA[:9]/name;archive=folder/(name+'-image.tar.gz')
        assert (folder/'source-sha.txt').read_text().strip()==SHA
        digest=hashlib.sha256()
        with archive.open('rb') as f:
            while data:=f.read(1024*1024):digest.update(data)
        assert digest.hexdigest()==(folder/(archive.name+'.sha256')).read_text().split()[0]
        images[name]={'id':(folder/'image-id.txt').read_text().strip(),'archive_sha256':digest.hexdigest(),'bytes':archive.stat().st_size}
    with connect() as client:
        code='import pathlib,os;p=pathlib.Path('+repr(REMOTE_ROOT)+');assert p.resolve()==p and p.stat().st_uid==os.geteuid();(p/'+repr('candidate-'+SHA[:9])+').mkdir(mode=0o700,exist_ok=True)'
        assert execute(client,code)==0
        with client.open_sftp() as sftp:
            for name in images:
                local=PRIVATE/SHA[:9]/name/(name+'-image.tar.gz');remote=REMOTE_ROOT+'/candidate-'+SHA[:9]+'/'+local.name
                try:
                    existing=sftp.lstat(remote);assert stat.S_ISREG(existing.st_mode) and existing.st_size==local.stat().st_size
                except FileNotFoundError:
                    print(json.dumps({'stage':'transfer_candidate','component':name,'bytes':local.stat().st_size}),flush=True)
                    with sftp.open(remote,'wx') as out,local.open('rb') as incoming:
                        out.set_pipelined(True)
                        while data:=incoming.read(1024*1024):out.write(data)
                    sftp.chmod(remote,0o600)
            # Only encrypted customer bytes, no plaintext business document.
            sample=__import__('pathlib').Path('D:/RjDir/admin_d95c8884-f44c-4529-8f3b-48d5788371c1/Downloads/IPD-1.20.3-DAR-T05-TR评审要素表-应用软件.xlsx')
            assert hashlib.sha256(sample.read_bytes()).hexdigest()=='5f6b9ff1ba9ca08d6e99055f660078080f7098dc5751a0f55736059dfdbc6f4b'
            customer_local=PRIVATE/'samples/user-drm.xlsx'
            if not customer_local.exists():customer_local.write_bytes(sample.read_bytes())
            for local in [customer_local,*__import__('pathlib').Path('E:/code/WeKnora-office-drm/internal/officeguard/testdata').glob('drm*')]:
                remote=REMOTE_ROOT+'/samples/'+local.name
                try:sftp.lstat(remote)
                except FileNotFoundError:
                    with sftp.open(remote,'wx') as f:f.write(local.read_bytes())
                    sftp.chmod(remote,0o600)
        code='ROOT='+repr(REMOTE_ROOT)+'\nSHA='+repr(SHA)+'\nIMAGES='+repr(images)+'\n'+REMOTE
        raise SystemExit(execute(client,code,500))

REMOTE=r'''
import datetime,hashlib,json,os,pathlib,socket,subprocess
root=pathlib.Path(ROOT);site=root/'site';project='knowhub-office-guard-test'
assert socket.gethostname()=='aqyfzx10' and os.geteuid()==1000 and root.resolve()==root
def run(args,timeout=60):
 p=subprocess.run(args,capture_output=True,text=True,timeout=timeout)
 assert p.returncode==0,p.stderr[-2000:]
 return p.stdout
command=['docker','compose','-p',project,'-f',str(site/'compose.json')]
proof=json.loads((site/'deployment-proof.json').read_text());assert proof['project']==project
assert proof['source_sha'] in ('4ffd7442da82675bba2b63f3f0dfe31995712070','ca1c058c0887b9b2c00899bb6c7da6201761415a',SHA)
for name,m in IMAGES.items():
 archive=root/('candidate-'+SHA[:9])/(name+'-image.tar.gz');assert not archive.is_symlink()
 h=hashlib.sha256()
 with archive.open('rb') as f:
  while data:=f.read(1024*1024):h.update(data)
 assert h.hexdigest()==m['archive_sha256']
 run(['docker','load','-i',str(archive)],240)
 v=json.loads(run(['docker','image','inspect',m['id']]))[0]
 assert v['Id']==m['id']
 if name=='app':assert v['Config']['Labels']['org.opencontainers.image.revision']==SHA
plan=json.loads((site/'compose.json').read_text())
if proof['source_sha']!=SHA:
 # Own project config is versioned before changing its two application images.
 for name in ('compose.json','deployment-proof.json'):
  backup=site/(name+'.'+proof['source_sha'][:9])
  fd=os.open(backup,os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
  with os.fdopen(fd,'w') as f:f.write((site/name).read_text())
 for name,m in IMAGES.items():plan['services'][name]['image']=m['id'];plan['services'][name]['labels']['knowhub.source-sha']=SHA
 temporary=site/'compose-upgrade.json'
 fd=os.open(temporary,os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
 with os.fdopen(fd,'w') as f:json.dump(plan,f,indent=2)
 temporary.replace(site/'compose.json')
 proof['previous_source_sha']=proof['source_sha'];proof['source_sha']=SHA
 proof['previous_images']=proof['images'];proof['images']={**proof['images'],**IMAGES}
 proof['upgrade_utc']=datetime.datetime.now(datetime.timezone.utc).isoformat()
 temporary=site/'deployment-proof.pending.json'
 fd=os.open(temporary,os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
 with os.fdopen(fd,'w') as f:json.dump(proof,f,indent=2)
 temporary.replace(site/'deployment-proof.json')
run(command+['config','--quiet'])
print(run(command+['up','-d','--no-build','--pull','never','app','frontend'],240))
for component in ('app','frontend'):
 cid=run(command+['ps','-q',component]).strip();row=json.loads(run(['docker','inspect',cid]))[0]
 assert row['Image']==IMAGES[component]['id'] and row['State']['Running']
 if component=='app':assert row['State']['Health']['Status']=='healthy'
print(json.dumps({'result':'DEPLOYED','source_sha':SHA,'images':IMAGES,'project':project,'existing_projects_changed':False,'production_deployed':False}))
'''
if __name__=='__main__':main()
