"""Stage test inputs and the verified GoJieba runtime dictionary, then retry own app."""
import hashlib,json,stat
from pathlib import Path
from office_test_remote import ROOT,REMOTE_ROOT,connect,execute

PRIVATE=ROOT/'docs/qa/office-test-deployment/private'
DICT=Path('C:/Users/admin/go/pkg/mod/github.com/yanyiwu/gojieba@v1.4.7/deps/cppjieba/dict')
REMOTE='ROOT='+repr(REMOTE_ROOT)+'\n'+r'''
import json,hashlib,os,pathlib,socket,subprocess
root=pathlib.Path(ROOT);site=root/'site'
assert socket.gethostname()=='aqyfzx10' and os.geteuid()==1000 and root.resolve()==root
project='knowhub-office-guard-test'
compose=['docker','compose','-p',project,'-f',str(site/'compose.json')]
manifest=json.loads((root/'dictionary-manifest.json').read_text())
for row in manifest:
 p=root/'jieba-dict'/row['name'];assert not p.is_symlink() and hashlib.sha256(p.read_bytes()).hexdigest()==row['sha256']
plan=json.loads((site/'compose.json').read_text())
mount=str(root/'jieba-dict')+':/app/jieba-dict:ro'
if mount not in plan['services']['app']['volumes']:
 running=subprocess.check_output(compose+['ps','--status','running','-q','app'],text=True).strip()
 assert not running,'dictionary fix requires stopped failed app'
 plan['services']['app']['volumes'].append(mount)
 plan['services']['app']['environment']={'JIEBA_DICT_DIR':'/app/jieba-dict'}
 pending=site/'compose-dictionary.json'
 fd=os.open(pending,os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
 with os.fdopen(fd,'w') as f:json.dump(plan,f,indent=2)
 pending.replace(site/'compose.json')
 print(json.dumps({'runtime_config_change':'read-only dictionary from candidate Go dependency','dictionary':manifest,'application_binary_unchanged':True}))
p=subprocess.run(compose+['config','--quiet'],capture_output=True,text=True);assert p.returncode==0
p=subprocess.run(compose+['up','-d','--no-build','--pull','never','app','frontend'],capture_output=True,text=True,timeout=240)
print(p.stdout+p.stderr);assert p.returncode==0
print(json.dumps({'state':'STARTED','project':project}))
'''

def main():
    verified=(PRIVATE/'local-module-verify.txt').read_text(encoding='utf-16').strip()
    assert verified=='all modules verified','runtime dictionaries require module integrity verification'
    manifest=[{'name':p.name,'sha256':hashlib.sha256(p.read_bytes()).hexdigest(),'bytes':p.stat().st_size} for p in DICT.glob('*.utf8')]
    mf=PRIVATE/'dictionary-manifest.json';mf.write_text(json.dumps(manifest,indent=2)+'\n')
    with connect() as client:
        check='import os,pathlib;root=pathlib.Path('+repr(REMOTE_ROOT)+');assert root.resolve()==root and root.stat().st_uid==os.geteuid();'
        check+='[(root/n).mkdir(mode=0o755,exist_ok=True) for n in ["samples","jieba-dict"]]'
        assert execute(client,check)==0
        with client.open_sftp() as sftp:
            uploads=[(p,'jieba-dict/'+p.name) for p in DICT.glob('*.utf8')]
            uploads.extend((p,'samples/'+p.name) for p in (PRIVATE/'samples').iterdir() if p.is_file())
            uploads.extend([(mf,mf.name),(ROOT/'scripts/ci/office_site_acceptance.py','office_site_acceptance.py')])
            for local,name in uploads:
                remote=REMOTE_ROOT+'/'+name
                try:
                    old=sftp.lstat(remote);assert stat.S_ISREG(old.st_mode) and old.st_size==local.stat().st_size
                except FileNotFoundError:
                    with sftp.open(remote,'wx') as out,local.open('rb') as src:
                        out.set_pipelined(True)
                        while data:=src.read(1024*1024):out.write(data)
                    sftp.chmod(remote,0o644 if name.startswith('jieba-dict/') else 0o600)
        raise SystemExit(execute(client,REMOTE,400))

if __name__=='__main__':main()
