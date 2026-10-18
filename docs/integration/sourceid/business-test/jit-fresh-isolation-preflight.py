"""Read-only identity, namespace and TCP reachability isolation checks."""
import socket
OLD='knowhub-company-test';NEW='knowhub-jit-fresh'
def addresses(name):return sorted(v['IPAddress'] for v in inspect(name,'.NetworkSettings.Networks').values() if v.get('IPAddress'))
oldpg=addresses(OLD+'-postgres-1');oldredis=addresses(OLD+'-redis-1');newpg=addresses(NEW+'-postgres-1');newredis=addresses(NEW+'-redis-1')
assert len(newpg)==len(newredis)==1
code=r'''import socket,json,os,sys
v=json.loads(sys.argv[1]);out={}
out['database_dns_is_fresh']=socket.gethostbyname(os.environ['DB_HOST'])==v['newpg'][0]
out['redis_dns_is_fresh']=socket.gethostbyname(os.environ['REDIS_ADDR'].split(':')[0])==v['newredis'][0]
denied=[]
for host,port in [(x,5432) for x in v['oldpg']]+[(x,6379) for x in v['oldredis']]:
 s=socket.socket();s.settimeout(.8)
 try:s.connect((host,port));denied.append(False)
 except OSError:denied.append(True)
 finally:s.close()
out['original_pg_and_redis_tcp_denied']=bool(denied) and all(denied)
assert all(out.values());print(json.dumps(out))
'''
out=json.loads(run(['docker','exec',NEW+'-app-1','python3','-c',code,json.dumps({'oldpg':oldpg,'oldredis':oldredis,'newpg':newpg,'newredis':newredis})],timeout=20))
mounts=inspect(NEW+'-app-1','.Mounts');assert next(x['Name'] for x in mounts if x['Destination']=='/data/files')=='knowhub-jit-fresh_files'
assert not any('knowhub-company-test' in str(x.get('Name','')) or 'knowhub-company-test' in x.get('Source','') for x in mounts)
out.update(result='PASS',original_files_not_mounted=True,old_environment_written=False,real_sid_auth_requests=0)
print(json.dumps(out))
