from pathlib import Path
import importlib.util,logging,json,hashlib,tarfile,subprocess,sys,shlex,paramiko
root=Path('E:/code/WeKnora-worktrees/knowhub-mcp-b4-rc3-20261021');config_path=Path('E:/AIData/Codex/knowhub-b7b-current.json');config=json.loads(config_path.read_text());remote=config['remote']
mode=sys.argv[1];phase=sys.argv[2] if len(sys.argv)>2 else 'focused'
assert phase in ('focused','all','python','integration','clients')
spec=importlib.util.spec_from_file_location('crypto','E:/AIData/Codex/sourceid-ssh.py');crypto=importlib.util.module_from_spec(spec);spec.loader.exec_module(crypto);logging.disable(logging.CRITICAL)
client=paramiko.SSHClient();client.load_host_keys(str(Path.home()/'.ssh/known_hosts'));client.set_missing_host_key_policy(paramiko.RejectPolicy())
password=crypto.crypt(Path('E:/AIData/Codex/secure/sourceid-test-host.ssh.dpapi').read_bytes(),False).decode();client.connect('172.30.232.42',username='aqyfzx',password=password,allow_agent=False,look_for_keys=False,timeout=10,auth_timeout=15);password=None
try:
 if mode in ('poll','collect'):
  with client.open_sftp()as ftp:
   selected=['results-'+config['candidate_sha256'][:16]+'-'+config['phase']] if mode=='poll' else [n for n in ftp.listdir(remote) if n.startswith('results-')]
   for name in selected:
    out=root/'docs/integration/knowhub-mcp-portal/evidence/batch7/continued/linux'/name;out.mkdir(parents=True,exist_ok=True)
    for f in ftp.listdir(remote+'/'+name):
     if f.endswith(('.log','.json','.jsonl','.txt')):ftp.get(remote+'/'+name+'/'+f,str(out/f))
    path=out/'results.json';print(name+': '+(json.dumps([{'name':r['name'],'exit_code':r['exit_code']} for r in json.loads(path.read_text())])if path.exists()else 'RUNNING'));print('DONE='+str((out/'done.json').exists()))

 else:
  names=set(subprocess.check_output(['git','ls-files'],cwd=root).decode().splitlines());names.update(subprocess.check_output(['git','ls-files','--others','--exclude-standard'],cwd=root).decode().splitlines())
  names={n for n in names if (root/n).is_file() and not n.startswith('docs/integration/knowhub-mcp-portal/evidence/')}
  files={n:hashlib.sha256((root/n).read_bytes()).hexdigest()for n in sorted(names)};digest=hashlib.sha256(json.dumps(files,sort_keys=True).encode()).hexdigest()
  config.update(candidate_sha256=digest,phase=phase,base_sha=subprocess.check_output(['git','rev-parse','HEAD'],cwd=root).decode().strip())
  dest=Path('E:/AIData/Codex')/config['task'];dest.mkdir(exist_ok=True)
  archive=dest/(digest+'.tar.gz')
  with tarfile.open(archive,'w:gz',compresslevel=1)as tar:
   for n in sorted(names):tar.add(root/n,arcname=n,recursive=False)
  with tarfile.open(archive,'r:gz')as verification:
   actual={member.name:hashlib.sha256(verification.extractfile(member).read()).hexdigest() for member in verification.getmembers() if member.isfile()}
  assert actual==files,'source changed while making candidate archive; retry after freezing edits'
  manifest=dict(config,files=files,archive_sha256=hashlib.sha256(archive.read_bytes()).hexdigest())
  evidence=root/'docs/integration/knowhub-mcp-portal/evidence/batch7/continued/candidates';evidence.mkdir(exist_ok=True)
  (evidence/(digest+'.json')).write_text(json.dumps(manifest,indent=2),encoding='utf8');config_path.write_text(json.dumps(config),encoding='utf8')
  with client.open_sftp()as ftp:
   ftp.put(str(archive),remote+'/'+archive.name)
   with ftp.open(remote+'/run-config.json','w')as f:f.write(json.dumps(config))
   ftp.put(str(root/'docs/integration/knowhub-mcp-portal/tools/batch7_linux.py'),remote+'/batch7_linux.py')
  source='/work/source/'+digest[:16]
  commands=[shlex.join(['docker','run','--rm','--network','none','-v',config['volume']+':/work','--entrypoint','mkdir','knowhub-build/golang:1.26-bookworm-ustc','-p',source]),shlex.join(['docker','run','--rm','-i','--network','none','-v',config['volume']+':/work','--entrypoint','tar','knowhub-build/golang:1.26-bookworm-ustc','-xzf','-','-C',source])+' < '+shlex.quote(remote+'/'+archive.name),'cd '+shlex.quote(remote)+' && nohup python3 batch7_linux.py > '+shlex.quote('launcher-'+digest[:16]+'-'+phase+'.log')+' 2>&1 < /dev/null &']
  for command in commands:
   stdin,stdout,stderr=client.exec_command(command);code=stdout.channel.recv_exit_status();error=stderr.read().decode();stdin.close();stdout.close();stderr.close();assert code==0,error
  print(json.dumps({'started':True,'candidate_sha256':digest,'phase':phase,'volume':config['volume']}))
finally:client.close()
