"""Protected-process exact email pattern check; returns booleans, never raw logs."""
import datetime, json, subprocess, sys

data = json.load(sys.stdin)
assert set(data) == {'email'}
needle = data['email'].strip().lower().encode()
assert needle and b'@' in needle
rows = {}
for service in ('app', 'frontend', 'postgres'):
    ids = subprocess.check_output(['docker', 'ps', '-q', '--filter', 'label=com.docker.compose.project=knowhub-sourceid-business', '--filter', 'label=com.docker.compose.service='+service]).decode().split()
    assert len(ids) == 1
    p = subprocess.run(['docker', 'logs', '--since', '2026-10-15T16:44:00Z', '--tail', '2000', ids[0]], stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=20)
    assert p.returncode == 0 and len(p.stdout) < 8*1024*1024
    rows[service] = {'bytes_checked': len(p.stdout), 'employee_email_found': needle in p.stdout.lower()}
    assert not rows[service]['employee_email_found']
needle = data = None
print(json.dumps({'result':'PASS','utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'scope':'bounded post-preparation logs in protected remote memory','checks':rows,'raw_logs_saved':False,'employee_values_exported':False,'data_changed':False}))
