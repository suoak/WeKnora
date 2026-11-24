"""Archive GitHub CI metadata and complete logs without shell/binary re-encoding."""
import argparse
import json
from pathlib import Path
import subprocess

p=argparse.ArgumentParser()
p.add_argument('--gh',default='gh')
p.add_argument('--repo',default='suoak/WeKnora')
p.add_argument('--run',required=True,type=int)
p.add_argument('--out',required=True,type=Path)
a=p.parse_args()
a.out.mkdir(parents=True,exist_ok=True)
def call(*args):
    r=subprocess.run([a.gh,*args],capture_output=True)
    if r.returncode:
        raise RuntimeError(r.stderr.decode('utf-8',errors='replace'))
    return r.stdout
metadata=call('api',f'repos/{a.repo}/actions/runs/{a.run}')
state=json.loads(metadata)
assert state['status']=='completed','archive only terminal runs, including failures/cancellations'
prefix=a.out/str(a.run)
prefix.with_suffix('.json').write_bytes(metadata)
logs=call('api',f'repos/{a.repo}/actions/runs/{a.run}/logs')
assert logs.startswith(b'PK'),'expected ZIP logs'
prefix.with_suffix('.zip').write_bytes(logs)
print(json.dumps({'run':a.run,'commit':state['head_sha'],'conclusion':state['conclusion'],
                  'url':state['html_url'],'log_bytes':len(logs)}))
