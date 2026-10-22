"""Operate only the new joint candidate; all source runtimes remain immutable."""
from pathlib import Path
import hashlib,json,shlex,sys
from v1_host import connect
root=Path(__file__).resolve().parents[4];out=root/'docs/integration/knowhub-mcp-portal/evidence/feishu-joint'
c=json.loads((out/'current.json').read_text());remote=c['remote'];assert remote.startswith('/home/aqyfzx/ai/WeKnora-release-build/knowhub-v1-feishu-20261022/')
mode=sys.argv[1]
with connect() as ssh:
 if mode.startswith('start-'):
  stage=mode[6:];script={'build':'joint_build_linux.py','deploy':'joint_deploy_linux.py','acceptance':'joint_accept_linux.py','feishu':'joint_feishu_accept_linux.py','feishu-post':'joint_feishu_post_accept_linux.py','rollback':'joint_rollback_linux.py','worker':'joint_worker_fault_accept_linux.py','oldimage':'joint_old_image_probe_linux.py','hotfix':'joint_hotfix_probe_linux.py'}[stage]
  payload=(Path(__file__).parent/script).read_bytes();digest=hashlib.sha256(payload).hexdigest()
  frozen=json.loads((out/(c['candidate_sha256']+'.json')).read_text())
  # Harness corrections, if needed, are separate hash-addressed artifacts.
  scriptpath=remote+'/'+stage+'-harness-'+digest+'.py'
  with ssh.open_sftp() as ftp:
   with ftp.open(scriptpath,'wb') as f:f.write(payload)
  (out/(stage+'-harness-'+digest+'.json')).write_text(json.dumps({'candidate_sha256':c['candidate_sha256'],'harness_sha256':digest,'matches_frozen':frozen['files'].get('docs/integration/knowhub-mcp-portal/tools/'+script)==digest,'remote':scriptpath},indent=2))
  if stage=='hotfix':
   hotout=Path('E:/code/WeKnora-worktrees/feishu-safe-incremental-20261021/docs/integration/knowhub-mcp-portal/evidence/feishu-incremental')
   hotcurrent=json.loads((hotout/'hotfix-frozen-current.json').read_text(encoding='utf8'))
   hotimage=json.loads((hotout/('hotfix-build-result-'+hotcurrent['candidate_sha256']+'.json')).read_text(encoding='utf8'))
   assert hotimage['exit_code']==0 and hotimage['candidate_sha256']==hotcurrent['candidate_sha256'] and hotimage['runtime_source_sha256']==hotcurrent['runtime_source_sha256']
   assert not hotimage['includes_personal_Go_MCP']
   with ssh.open_sftp()as ftp:
    with ftp.open(remote+'/independent-hotfix-image.json','w')as f:f.write(json.dumps(hotimage,indent=2))
   (out/('independent-hotfix-reference-'+hotcurrent['candidate_sha256']+'.json')).write_text(json.dumps(hotimage,indent=2),encoding='utf8')
  if stage=='feishu':
   probe=(Path(__file__).parent/'joint_python_source_probe.py').read_bytes();probe_sha=hashlib.sha256(probe).hexdigest();probe_path=remote+'/python-source-probe-harness-'+probe_sha+'.py'
   with ssh.open_sftp() as ftp:
    with ftp.open(probe_path,'wb') as f:f.write(probe)
    with ftp.open(remote+'/python-source-probe-current.json','w') as f:f.write(json.dumps({'path':probe_path,'sha256':probe_sha}))
   (out/('python-source-probe-'+probe_sha+'.json')).write_text(json.dumps({'candidate_sha256':c['candidate_sha256'],'harness_sha256':probe_sha,'path':probe_path,'reason':'use the same mcp2 HTTP client factory as existing verified SDK tests; no server/authentication change'},indent=2))
  command='nohup '+shlex.join(['python3',scriptpath,remote]+(sys.argv[2:3] if stage in ('feishu','feishu-post') else []))+' > '+shlex.quote(remote+'/'+stage+'-launcher-'+digest+'.log')+' 2>&1 </dev/null &'
  i,o,e=ssh.exec_command(command,timeout=15);code=o.channel.recv_exit_status();error=e.read().decode();i.close();o.close();e.close();assert code==0,error;print(stage+' started '+digest)
 elif mode=='collect':
  with ssh.open_sftp() as ftp:
   for stage in ftp.listdir(remote+'/results'):
    dest=out/(stage+'-'+c['candidate_sha256'][:16]);dest.mkdir(exist_ok=True)
    for name in ftp.listdir(remote+'/results/'+stage):
     if name.endswith(('.json','.log','.txt')):ftp.get(remote+'/results/'+stage+'/'+name,str(dest/name))
    result=dest/'results.json'
    if result.exists():print(stage,[(r['name'],r['exit_code']) for r in json.loads(result.read_text())])
    print(stage,'done=',(dest/'done.json').exists())
 else:raise ValueError('unsupported operation')
