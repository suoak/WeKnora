"""Build a schema-compatible hardened fallback; this does NOT approve rollback.

No APP process, database, network change, or production image probe is started.
Only a later successful isolated denial/forward/restore suite may whitelist it.
"""
from pathlib import Path
import hashlib,json,shlex,sys
from v1_host import connect

root=Path(__file__).resolve().parents[4]
assert root.name=='knowhub-v1-wiki-dynamic-20261022'
out=root/'docs/integration/knowhub-mcp-portal/evidence/v1-wiki-dynamic'
names=['migrations/migration-map.json']+[
 'migrations/'+d+'/003037_personal_dynamic_wiki.'+direction+'.sql'
 for d in ('versioned','sqlite')for direction in ('up','down')]
files={n:hashlib.sha256((root/n).read_bytes()).hexdigest()for n in names}
base='knowhub-feishu-rc3/app:02a8c5eb187764a9'
baseid='sha256:0a0117c920c4fed402e7f744578b539a5ef5e6ef8da7c141ff5e9ac15031b73d'
sha=hashlib.sha256(json.dumps({'base':baseid,'files':files},sort_keys=True).encode()).hexdigest()
remote='/home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-safe-rollback-20261022/'+sha[:16]
manifest={'material_sha256':sha,'base':base,'base_id':baseid,'files':files,'remote':remote,
 'rollback_approved':False,'reason':'requires isolated same-link and REST denial, forward and restore evidence',
 'schema':3037,'binary_source':'independent RC3 Feishu hardened patch; no new personal MCP product features'}
with connect()as ssh:
 command='mkdir -p '+shlex.quote(remote.rsplit('/',1)[0])+' && mkdir '+shlex.quote(remote)
 _,o,e=ssh.exec_command(command);assert o.channel.recv_exit_status()==0,e.read().decode();o.close();e.close()
 with ssh.open_sftp()as f:
  for n in names:
   path=remote+'/'+n
   # Ancestors are fixed task-owned paths, not user-composed shell commands.
   _,o,e=ssh.exec_command('mkdir -p '+shlex.quote(path.rsplit('/',1)[0]));assert o.channel.recv_exit_status()==0;e.close();o.close()
   f.put(str(root/n),path)
  dockerfile='FROM '+base+'\nCOPY migrations /app/migrations\nLABEL knowhub.rollback.material="'+sha+'" knowhub.rollback.approved="false"\n'
  with f.open(remote+'/Dockerfile','w')as h:h.write(dockerfile)
  with f.open(remote+'/manifest.json','w')as h:h.write(json.dumps(manifest,indent=2))
 script=r'''
from pathlib import Path
import hashlib,json,subprocess
b=Path(BASE);m=json.loads((b/'manifest.json').read_text())
assert json.loads(subprocess.check_output(['docker','image','inspect',m['base']]))[0]['Id']==m['base_id']
assert all(hashlib.sha256((b/n).read_bytes()).hexdigest()==h for n,h in m['files'].items())
image='knowhub-safe-rollback/app:'+m['material_sha256'][:16]
args=['docker','build','--network','none','--pull=false','-t',image,str(b)]
with(b/'build.log').open('wb')as f:code=subprocess.run(args,stdout=f,stderr=subprocess.STDOUT,timeout=120).returncode
result=dict(m,argv=args,exit_code=code,image=image)
if code==0:result['image_id']=json.loads(subprocess.check_output(['docker','image','inspect',image]))[0]['Id']
(b/'result.json').write_text(json.dumps(result,indent=2))
'''.replace('BASE',repr(remote))
 with ssh.open_sftp()as f:
  with f.open(remote+'/build.py','w')as h:h.write(script)
 _,o,e=ssh.exec_command('python3 '+shlex.quote(remote+'/build.py'));code=o.channel.recv_exit_status();assert code==0,e.read().decode();o.close();e.close()
 with ssh.open_sftp()as f:
  for n in ('manifest.json','result.json','build.log'):f.get(remote+'/'+n,str(out/('safe-rollback-'+sha[:16]+'-'+n)))
result=json.loads((out/('safe-rollback-'+sha[:16]+'-result.json')).read_text())
assert result['exit_code']==0
(out/'safe-rollback-current.json').write_text(json.dumps(result,indent=2))
print(json.dumps({k:result[k]for k in('image','image_id','rollback_approved')}))
