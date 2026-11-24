"""Read-only reachability evidence; never infer isolation from an HTTP success."""
import argparse
import datetime
import json
import subprocess
import urllib.error
import urllib.parse
import urllib.request

p=argparse.ArgumentParser()
p.add_argument('--url',required=True)
p.add_argument('--ssh-target')
args=p.parse_args()
u=urllib.parse.urlsplit(args.url)
assert u.scheme in ('http','https') and not u.username and not u.password and not u.query
report={'observed_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),
        'http':[], 'isolation_verified':False,'writes_performed':False,'acceptance':'NOT_RUN'}
for path in ('/','/health','/api/v1/health'):
    url=urllib.parse.urlunsplit((u.scheme,u.netloc,path,'',''))
    try:
        with urllib.request.urlopen(url,timeout=8) as response:
            report['http'].append({'url':url,'status':response.status})
    except urllib.error.HTTPError as e:
        report['http'].append({'url':url,'status':e.code})
    except (urllib.error.URLError,TimeoutError) as e:
        report['http'].append({'url':url,'error_type':type(e).__name__})
if args.ssh_target:
    assert not args.ssh_target.startswith('-')
    try:
        r=subprocess.run(['ssh','-o','BatchMode=yes','-o','ConnectTimeout=8','-o',
                          'StrictHostKeyChecking=yes',args.ssh_target,'uname -s'],
                         capture_output=True,text=True,timeout=12)
        report['ssh']={'target':args.ssh_target,'exit_code':r.returncode,
                       'authenticated':r.returncode==0,
                       'permission_denied':'Permission denied' in r.stderr}
    except subprocess.TimeoutExpired:
        report['ssh']={'target':args.ssh_target,'error':'timeout'}
print(json.dumps(report,indent=2))
