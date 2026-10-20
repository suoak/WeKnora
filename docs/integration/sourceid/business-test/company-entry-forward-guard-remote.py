"""Only new entry destination addresses/ports in DOCKER-USER; no global flush."""
import ipaddress,os
B=pathlib.Path('/home/aqyfzx/ai/knowhub-company-entry-20261017');entry='knowhub-company-entry-entry-1'
assert inspect(entry,'.Config.Labels')['com.docker.compose.project']=='knowhub-company-entry'
ips=[v['IPAddress'] for v in inspect(entry,'.NetworkSettings.Networks').values()];assert len(ips)==2
source=str(ipaddress.ip_address(os.environ['SSH_CONNECTION'].split()[0]));assert ipaddress.ip_address(source).is_private
tooling='/home/aqyfzx/ai/knowhub-oa-sso-pilot-832b6652a/egress/nettools'
image=inspect('knowhub-company-test-app-1','.Image')
code=r'''
import os,subprocess,json
def root():os.chroot('/nettools');os.chdir('/')
def rule(*args,check=True):
 p=subprocess.run(['/usr/sbin/iptables','-w','5',*args],preexec_fn=root,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
 if check and p.returncode:raise RuntimeError('entry_forward_guard_failed')
 return p.returncode
chain='KH_COMPANY_ENTRY'
exists=rule('-S',chain,check=False)==0
if not exists:
 rule('-N',chain)
 rule('-A',chain,'-s',SOURCE+'/32','-j','ACCEPT')
 rule('-A',chain,'-j','DROP')
for ip in IPS:
 args=['-d',ip+'/32','-p','tcp','-m','multiport','--dports','80,8080','-j',chain]
 if rule('-C','DOCKER-USER',*args,check=False):rule('-I','DOCKER-USER','1',*args)
# Docker userland-proxy hairpins can terminate in host INPUT before creating
# a new host-to-container connection. Cover that path as well, target host
# address and the two newly approved entry ports only.
for interface in ('docker0','br+'):
 args=['-i',interface,'-d','172.30.232.42/32','-p','tcp','-m','multiport','--dports','80,8080','-j','DROP']
 if rule('-C','INPUT',*args,check=False):rule('-I','INPUT','1',*args)
print(json.dumps({'result':'PASS','targeted_entry_addresses':len(IPS),'global_rules_flushed':False,'host_paths_changed':False}))
'''.replace('SOURCE',repr(source)).replace('IPS',repr(ips))
result=json.loads(run(['docker','run','--rm','--network','host','--cap-add','NET_ADMIN','--tmpfs','/nettools/run','--log-driver','none','--mount','type=bind,source='+tooling+',target=/nettools,readonly','--entrypoint','python3',image,'-c',code],timeout=30))
(B/'private/entry-forward-guard.py').write_text(code)
cleanup=code[:code.index("chain='KH_COMPANY_ENTRY'")]+"\nfor ip in "+repr(ips)+":\n args=['-d',ip+'/32','-p','tcp','-m','multiport','--dports','80,8080','-j','KH_COMPANY_ENTRY']\n if rule('-C','DOCKER-USER',*args,check=False)==0:rule('-D','DOCKER-USER',*args)\nrule('-F','KH_COMPANY_ENTRY');rule('-X','KH_COMPANY_ENTRY')\n"
cleanup+="\nfor interface in ('docker0','br+'):\n args=['-i',interface,'-d','172.30.232.42/32','-p','tcp','-m','multiport','--dports','80,8080','-j','DROP']\n if rule('-C','INPUT',*args,check=False)==0:rule('-D','INPUT',*args)\n"
(B/'private/remove-entry-forward-guard.py').write_text(cleanup)
result['reapply_and_remove_scripts_saved_private']=True
(B/'entry-forward-guard-result.json').write_text(json.dumps(result));print(json.dumps(result))
