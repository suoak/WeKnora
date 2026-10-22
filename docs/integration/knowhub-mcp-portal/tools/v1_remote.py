"""Start/poll bounded development jobs on the approved build host."""
from pathlib import Path
import json,shlex,sys,hashlib
from v1_host import connect

root=Path(__file__).resolve().parents[4];out=root/'docs/integration/knowhub-mcp-portal/evidence/v1'
current=json.loads((out/'current.json').read_text());remote=current['remote'];mode=sys.argv[1]
assert mode in ('start-development','start-build','start-deploy','start-acceptance','start-rollback','collect')
with connect()as ssh:
    if mode.startswith('start-'):
        stage=mode.removeprefix('start-') if hasattr(mode,'removeprefix')else mode[6:]
        script={'development':'v1_development_linux.py','build':'v1_build_linux.py','deploy':'v1_deploy_linux.py','acceptance':'v1_accept_linux.py','rollback':'v1_rollback_linux.py'}[stage]
        scriptpath=remote+'/source/docs/integration/knowhub-mcp-portal/tools/'+script
        if stage in ('acceptance','build','deploy'):
            # Test-runner corrections are separate artifacts, never changes to
            # the frozen application or previously executed test assertions.
            payload=(Path(__file__).parent/script).read_bytes()
            digest=hashlib.sha256(payload).hexdigest()
            scriptpath=remote+'/'+stage+'-harness-'+digest+'.py'
            with ssh.open_sftp()as ftp:
                with ftp.open(scriptpath,'wb')as f:f.write(payload)
                if stage=='deploy':
                    composition=(Path(__file__).parent/'v1_test.compose.yml').read_bytes()
                    with ftp.open(remote+'/deployment-compose.yml','wb')as f:f.write(composition)
                    (out/('deploy-compose-'+current['candidate_sha256'][:16]+'.json')).write_text(json.dumps({'sha256':hashlib.sha256(composition).hexdigest(),'candidate_sha256':current['candidate_sha256'],'scope':'only new project IPAM, no global network configuration'},indent=2))
            (out/(stage+'-harness-'+current['candidate_sha256'][:16]+'.json')).write_text(json.dumps({'candidate_sha256':current['candidate_sha256'],'harness_sha256':digest,'remote_path':scriptpath,'reason':'build: lockfile-verified cache and resumable logs; acceptance: disposable restartable Redis required by unchanged assertions'},indent=2))
        command=shlex.join(['python3',scriptpath,remote])
        with ssh.open_sftp()as ftp:
            try:
                with ftp.open(remote+'/'+stage+'-launcher.log','rb')as f:previous=f.read()
                if previous:
                    with ftp.open(remote+'/results/'+stage+'/launcher-'+hashlib.sha256(previous).hexdigest()+'.log','wb')as f:f.write(previous)
            except FileNotFoundError:pass
        command='nohup '+command+' > '+shlex.quote(remote+'/'+stage+'-launcher.log')+' 2>&1 < /dev/null &'
        i,o,e=ssh.exec_command(command);assert o.channel.recv_exit_status()==0;i.close();o.close();e.close();print(stage+' started')
    else:
        with ssh.open_sftp()as ftp:
            for stage in ftp.listdir(remote+'/results'):
                target=out/(stage+'-'+current['candidate_sha256'][:16]);target.mkdir(exist_ok=True)
                for name in ftp.listdir(remote+'/results/'+stage):
                    ftp.get(remote+'/results/'+stage+'/'+name,str(target/name))
                p=target/'results.json'
                if p.exists():print(stage,json.dumps([{k:r.get(k)for k in ['name','exit_code']}for r in json.loads(p.read_text())]))
                print(stage,'DONE', (target/'done.json').exists())
