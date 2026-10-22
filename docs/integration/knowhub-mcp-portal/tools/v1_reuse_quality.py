"""Bind prior successful tests only when every runtime/test input is identical.

Used for an operational build-runner correction, not to inherit a PASS over a
source change. Original test logs and candidate IDs stay unchanged.
"""
from pathlib import Path
import json,shlex,sys
from v1_host import connect

root=Path(__file__).resolve().parents[4];out=root/'docs/integration/knowhub-mcp-portal/evidence/v1'
current=json.loads((out/'current.json').read_text());new=json.loads((out/(current['candidate_sha256']+'.json')).read_text())
old=json.loads((out/(sys.argv[1]+'.json')).read_text())
assert new['candidate_sha256']!=old['candidate_sha256']
def inputs(m):
    return {n:h for n,h in m['files'].items()if n.startswith(('internal/','cmd/','migrations/','config/','frontend/','mcp-server/'))or n in ('go.mod','go.sum')}
assert inputs(new)==inputs(old),'Changed runtime/test/config input requires a new test run'
with connect()as ssh:
    with ssh.open_sftp()as ftp:
        with ftp.open(old['remote']+'/results/development/done.json')as f:done=json.loads(f.read())
        assert done['all_passed']
        command=shlex.join(['mkdir','-p',new['remote']+'/results/development'])
        i,o,e=ssh.exec_command(command);assert o.channel.recv_exit_status()==0;i.close();o.close();e.close()
        proof={'all_passed':True,'candidate_sha256':new['candidate_sha256'],'tested_candidate_sha256':old['candidate_sha256'],'tested_results':old['remote']+'/results/development/results.json','input_files_verified':len(inputs(new)),'execution':'IDENTICAL_INPUTS_NOT_RERUN','actual_results':done}
        with ftp.open(new['remote']+'/results/development/done.json','w')as f:f.write(json.dumps(proof,indent=2))
    (out/('quality-equivalence-'+new['candidate_sha256'][:16]+'.json')).write_text(json.dumps(proof,indent=2),encoding='utf8')
print(json.dumps({'source_inputs_identical':True,'tested_candidate':old['candidate_sha256'],'candidate':new['candidate_sha256']}))
