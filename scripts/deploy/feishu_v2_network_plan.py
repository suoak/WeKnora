"""Select a non-overlapping subnet for this project's unstarted Compose network.

This never changes Docker daemon settings, existing networks or host routes.
It updates only the new project's Compose plan, before any container is running.
"""
import argparse
import base64
import json
import re
import sys
import textwrap
from pathlib import Path, PurePosixPath

import paramiko

REMOTE = r'''
import ipaddress,json,pathlib,subprocess
root=pathlib.Path(ROOT)
if root.is_symlink() or str(root.resolve().parent)!='/mnt/sata0/knowhub-feishu-v2-builds':
 raise ValueError('unexpected_build_root')
release=root/'isolated-site'
proof=json.loads((release/'preparation.json').read_text())
project='knowhub-feishu-v2-'+SHA[:12]
if proof['source_sha']!=SHA or proof['project']!=project: raise ValueError('project_identity_mismatch')
def run(args):
 return subprocess.run(args,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True,timeout=30)
containers=run(['docker','ps','-q','--filter','label=com.docker.compose.project='+project])
if containers.returncode or containers.stdout.strip(): raise ValueError('project_containers_must_not_be_running')
networks=run(['docker','network','ls','-q'])
if networks.returncode: raise ValueError('network_inventory_failed')
ids=networks.stdout.split()
if len(ids)>1000: raise ValueError('network_inventory_limit')
occupied=[]
for offset in range(0,len(ids),100):
 result=run(['docker','network','inspect']+ids[offset:offset+100])
 if result.returncode: raise ValueError('network_inspection_failed')
 for network in json.loads(result.stdout):
  for config in network.get('IPAM',{}).get('Config') or []:
   if config.get('Subnet'):
    subnet=ipaddress.ip_network(config['Subnet'],strict=False)
    if subnet.version==4: occupied.append(subnet)
routes=run(['ip','-j','-4','route','show','table','all'])
if routes.returncode: raise ValueError('host_route_inventory_failed')
route_ranges=[]
for route in json.loads(routes.stdout):
 destination=route.get('dst','default')
 if destination=='default': continue
 subnet=ipaddress.ip_network(destination,strict=False)
 if subnet.prefixlen: route_ranges.append(subnet)
compose_path=release/'compose.json'
compose=json.loads(compose_path.read_text())
if compose.get('name')!=project: raise ValueError('compose_project_identity_mismatch')
if compose['networks']['default'].get('ipam'):
 subnet=compose['networks']['default']['ipam']['config'][0]['subnet']
 print(json.dumps({'status':'PREPARED','subnet':subnet,'existing_networks_changed':False,'already_selected':True}))
 raise SystemExit(0)
candidate=None
# Prefer RFC1918; shared non-global RFC6598 ranges are the fallback only if
# broad corporate routes already cover all otherwise unused private ranges.
pools=['10.251.250.0/24','10.252.250.0/24','172.31.250.0/24','192.168.253.0/24',
       '100.127.253.0/24','100.127.254.0/24']
for pool in pools:
 for subnet in ipaddress.ip_network(pool).subnets(new_prefix=28):
  if not any(subnet.overlaps(other) for other in occupied+route_ranges):
   candidate=subnet
   break
 if candidate: break
if candidate is None: raise ValueError('no_nonoverlapping_isolated_subnet_available')
compose['networks']['default']['ipam']={'config':[{'subnet':str(candidate)}]}
temporary=release/'compose.network-plan.json'
with temporary.open('x') as output: json.dump(compose,output,indent=2)
temporary.chmod(0o600)
checked=run(['docker','compose','--project-name',project,'--file',str(temporary),'config','--quiet'])
if checked.returncode: raise ValueError('network_compose_plan_validation_failed')
temporary.replace(compose_path)
proof['network_subnet']=str(candidate)
(release/'preparation.json').write_text(json.dumps(proof,indent=2)+'\n')
result={'status':'PREPARED','source_sha':SHA,'project':project,'subnet':str(candidate),
 'docker_networks_checked':len(ids),'host_nondefault_routes_checked':len(route_ranges),
 'existing_networks_changed':False,'daemon_configuration_changed':False,'containers_started':False}
(release/'network-plan.json').write_text(json.dumps(result,indent=2)+'\n')
print(json.dumps(result))
'''


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--manifest',type=Path,required=True)
    args=parser.parse_args()
    raw=args.manifest.read_bytes()
    manifest=json.loads(raw.decode('utf-16') if raw.startswith((b'\xff\xfe',b'\xfe\xff')) else raw.decode('utf-8-sig'))
    sha=manifest['source_sha'];root=PurePosixPath(manifest['source_directory']).parent
    if (not re.fullmatch('[a-f0-9]{40}',sha) or root.parent!=PurePosixPath('/mnt/sata0/knowhub-feishu-v2-builds')
            or not re.fullmatch(sha[:12]+r'-[a-f0-9]{8}',root.name)):
        raise SystemExit('invalid staged source manifest')
    client=paramiko.SSHClient()
    try:
        credential=json.loads(sys.stdin.read(16384))
        if credential.get('host')!='172.30.232.42' or credential.get('user')!='aqyfzx': raise ValueError('unexpected_target')
        client.load_system_host_keys();known=Path.home()/'.ssh'/'known_hosts'
        if known.is_file():client.load_host_keys(str(known))
        client.set_missing_host_key_policy(paramiko.RejectPolicy())
        client.connect(credential['host'],username=credential['user'],password=credential['password'],
                       timeout=15,auth_timeout=15,banner_timeout=15,allow_agent=False,look_for_keys=False)
        credential.clear()
        prefix='ROOT='+repr(str(root))+'\nSHA='+repr(sha)+'\n'
        guarded='try:\n'+textwrap.indent(REMOTE,' ')
        guarded+="\nexcept Exception as error:\n print(json.dumps({'status':'BLOCKED','error_type':type(error).__name__,"
        guarded+="'reason':str(error) if isinstance(error,ValueError) else 'network_plan_failed'}))\n raise SystemExit(2)\n"
        code=base64.b64encode((prefix+guarded).encode()).decode()
        _,out,_=client.exec_command("python3 -c \"import base64;exec(base64.b64decode('"+code+"'))\"",timeout=120)
        result=json.loads(out.read(65536));print(json.dumps(result,indent=2))
        return 0 if result.get('status')=='PREPARED' else 2
    except (OSError,ValueError,KeyError,TypeError,paramiko.SSHException):
        print(json.dumps({'status':'BLOCKED','reason':'verified_network_planning_unavailable'}));return 2
    finally:client.close()


if __name__=='__main__':sys.exit(main())
