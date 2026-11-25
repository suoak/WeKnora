"""Update our deterministic model fixture to support required KB chat settings."""
import stat
from office_test_remote import ROOT,REMOTE_ROOT,connect,execute
with connect() as client:
    with client.open_sftp() as sftp:
        remote=REMOTE_ROOT+'/model-fixture.py'
        row=sftp.lstat(remote);assert stat.S_ISREG(row.st_mode) and row.st_uid==1000
        with sftp.open(remote,'w') as f:f.write((ROOT/'scripts/ci/office_model_fixture.py').read_bytes())
        sftp.chmod(remote,0o644)
    code='ROOT='+repr(REMOTE_ROOT)+'\n'+r'''
import json,os,pathlib,socket,subprocess
assert socket.gethostname()=='aqyfzx10' and os.geteuid()==1000
site=pathlib.Path(ROOT)/'site'
command=['docker','compose','-p','knowhub-office-guard-test','-f',str(site/'compose.json')]
p=subprocess.run(command+['restart','model-fixture'],capture_output=True,text=True,timeout=30)
assert p.returncode==0
print(json.dumps({'fixture_restarted':True,'production_model_or_credentials_used':False,'purpose':'required test KB summary-model configuration; deterministic pipeline validation only'}))
'''
    raise SystemExit(execute(client,code,60))
