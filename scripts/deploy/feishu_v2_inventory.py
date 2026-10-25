"""Read SSH credentials from stdin and collect a bounded, read-only inventory.

No credential environment values, container logs, application data or private
key material are read. SSH_CONNECTION supplies only address/port metadata.
Authentication failures are reported without exception text.
"""
import base64
import argparse
import json
import subprocess
import sys
from pathlib import Path

import paramiko

REMOTE = r'''
import datetime,hashlib,json,os,pathlib,re,shutil,subprocess
incomplete=[]
def run(args):
 try: p=subprocess.run(args,stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=25,text=True)
 except subprocess.TimeoutExpired:
  incomplete.append({'operation':args[:3],'reason':'read_only_probe_timeout'})
  return None
 if p.returncode: return None
 return p.stdout.strip()
rows=[]
for line in (run(['docker','ps','-a','--format','{{json .}}']) or '').splitlines():
 d=json.loads(line)
 if any(s in d.get('Names','').lower() for s in ['knowhub','weknora','feishu']):
  rows.append({k:d.get(k) for k in ['Names','Image','Status','Ports']})
projects=run(['docker','compose','ls','--format','json'])
images=[]
build_bases=[]
for line in (run(['docker','images','--format','{{json .}}']) or '').splitlines():
 d=json.loads(line)
 if any(s in d.get('Repository','').lower() for s in ['knowhub-build','node','nginx','debian']):
  build_bases.append({k:d.get(k) for k in ['Repository','Tag','ID','Size']})
 if any(s in d.get('Repository','').lower() for s in ['knowhub','weknora','paradedb','golang','redis']):
  images.append({k:d.get(k) for k in ['Repository','Tag','ID','Size']})
root=pathlib.Path('/home/aqyfzx/ai')
directories=[str(p) for p in root.iterdir() if p.is_dir() and ('knowhub' in p.name.lower() or 'WeKnora' in p.name)] if root.is_dir() else []
disk=shutil.disk_usage(str(root) if root.is_dir() else '/home')
docker_root=run(['docker','info','--format','{{.DockerRootDir}}'])
build_image='knowhub-build/golang:1.26-feishu-py311-ensurepip-20261022'
build_raw=run(['docker','image','inspect',build_image])
entry_raw=run(['docker','inspect','knowhub-company-entry-entry-1'])
entry=None
if entry_raw:
 d=json.loads(entry_raw)[0]
 entry={'image':d.get('Image'),'mounts':[{k:m.get(k) for k in ['Type','Source','Destination','RW']} for m in d.get('Mounts',[])], 'ports':d.get('NetworkSettings',{}).get('Ports',{})}
 for m in d.get('Mounts',[]):
  if m.get('Type')=='bind' and m.get('Destination')=='/etc/nginx/nginx.conf':
   p=pathlib.Path(m['Source'])
   if p.is_file() and p.stat().st_size<131072:
    entry['routing_directives']=re.findall(r'(?m)^\s*(?:listen|server_name|proxy_pass)\s+[^;\n]*;',p.read_text())[:50]
build=None
runtime_raw=run(['docker','image','inspect','knowhub-feishu-main/app:ce699bb5065e'])
runtime=None
if runtime_raw:
 d=json.loads(runtime_raw)[0]
 runtime={k:d.get(k) for k in ['Id','RepoDigests','Architecture','Os','Created']}
if build_raw:
 d=json.loads(build_raw)[0]
 build={k:d.get(k) for k in ['Id','RepoDigests','Architecture','Os','Created']}
 if d.get('Id')=='sha256:33b84179859a3a275976e22ecf184643e4835de0a230d635f6ad88e299634ef0':
  build['toolchains']=run(['docker','run','--rm','--network','none','--read-only','--cap-drop','ALL','--pids-limit','64','--entrypoint','/bin/sh',d['Id'],'-c','go version && gcc -dumpfullversion && python3 --version'])
native=[]
for name in ['/home/aqyfzx/ai/knowhub-rc3-final-20261020/committed-source3/third_party/anydoc-go/lib/linux_amd64_gnu/libanydoc_go.a']:
 p=pathlib.Path(name)
 if p.is_file():
  h=hashlib.sha256()
  with p.open('rb') as f:
   for part in iter(lambda:f.read(1024*1024),b''): h.update(part)
  native.append({'path':name,'bytes':p.stat().st_size,'sha256':h.hexdigest()})
comparison=[]
baseline=pathlib.Path('/home/aqyfzx/ai/knowhub-rc3-final-20261020/committed-source3')
for relative,expected in EXPECTED_NATIVE_SOURCE.items():
 p=baseline/relative
 actual=hashlib.sha256(p.read_bytes().replace(b'\r\n',b'\n')).hexdigest() if p.is_file() else None
 comparison.append({'file':relative,'match':actual==expected,'expected':expected,'actual':actual})
storage=[]
infrastructure_images=[]
for tag in ['paradedb/paradedb:v0.22.2-pg17','redis:7.0-alpine','nginx:1.30.3-alpine']:
 raw=run(['docker','image','inspect',tag])
 if raw:
  item=json.loads(raw)[0]
  infrastructure_images.append({'tag':tag,'id':item['Id'],'repo_digests':item.get('RepoDigests',[])})
for name in ['/home/aqyfzx/ai/WeKnora-release-build','/mnt/sata0',docker_root]:
 if name and pathlib.Path(name).exists():
  try:
   usage=shutil.disk_usage(name)
   storage.append({'path':name,'resolved':str(pathlib.Path(name).resolve()),'free_bytes':usage.free})
  except OSError: storage.append({'path':name,'status':'not_readable'})
print(json.dumps({'checked_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'incomplete_probes':incomplete,
 'docker':run(['docker','--version']),'compose':run(['docker','compose','version']),
 'projects':json.loads(projects) if projects else [],'containers':rows[:80],
 'images':images[:60],'build_bases':build_bases[:40],'candidate_runtime':runtime,
 'infrastructure_images':infrastructure_images,
 'ssh_addresses':os.environ.get('SSH_CONNECTION','').split(),
 'candidate_directories':directories[:80],
 'listening_tcp':run(['ss','-ltn']), 'disk_free_bytes':disk.free,
 'build_directory_exists':(root/'WeKnora-release-build').is_dir(),'storage':storage,
 'build_image':build,'native_candidates':native,'native_source_comparison':comparison,'entry_80_8080':entry,
 'cache_directories':{p:pathlib.Path(p).is_dir() for p in ['/home/aqyfzx/go/pkg/mod','/mnt/sata0']},
 'cache_volume_names':run(['docker','volume','ls','--format','{{.Name}}','--filter','name=knowhub-mcp-b7'])},indent=2))
'''


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--host',required=True)
    parser.add_argument('--user',required=True)
    parser.add_argument('--source-root',type=Path)
    args=parser.parse_args()
    client = paramiko.SSHClient()
    try:
        credential = json.loads(sys.stdin.read(16384))
        if credential.get("host") != args.host or credential.get("user") != args.user:
            raise ValueError("unexpected_target")
        client.load_system_host_keys()
        known = Path.home() / ".ssh" / "known_hosts"
        if known.is_file():
            client.load_host_keys(str(known))
        client.set_missing_host_key_policy(paramiko.RejectPolicy())
        client.connect(credential["host"], port=22, username=credential["user"], password=credential["password"],
                       timeout=15, auth_timeout=15, banner_timeout=15, allow_agent=False, look_for_keys=False)
        credential.clear()
        native_source = {}
        if args.source_root:
            files = ["Cargo.lock", "Cargo.toml", "build.rs", "src/lib.rs", "src/model.rs", "src/asset_links.rs", "include/anydoc.h"]
            files = ["third_party/anydoc-go/" + name for name in files] + ["scripts/build-anydoc-lib.sh"]
            import hashlib
            for relative in files:
                blob = subprocess.run(["git", "show", "HEAD:" + relative], cwd=args.source_root, check=True, capture_output=True).stdout
                native_source[relative] = hashlib.sha256(blob.replace(b'\r\n',b'\n')).hexdigest()
        prefix = "EXPECTED_NATIVE_SOURCE=" + repr(native_source) + "\n"
        import textwrap
        guarded = 'try:\n' + textwrap.indent(REMOTE, ' ')
        guarded += "\nexcept Exception as error:\n print(json.dumps({'status':'BLOCKED',"
        guarded += "'remote_error_type':type(error).__name__,'reason':'bounded_inventory_failed'}))\n raise SystemExit(2)\n"
        code = base64.b64encode((prefix + guarded).encode()).decode()
        command = "python3 -c \"import base64;exec(base64.b64decode('" + code + "'))\""
        _, stdout, stderr = client.exec_command(command, timeout=90)
        raw = stdout.read(2 * 1024 * 1024)
        if stdout.channel.recv_exit_status() != 0:
            try:
                failure = json.loads(raw)
                if failure.get('remote_error_type'):
                    print(json.dumps(failure))
                    return 2
            except ValueError:
                pass
            raise ValueError("inventory_failed")
        report = json.loads(raw)
        print(json.dumps(report, indent=2))
        return 0
    except paramiko.BadHostKeyException:
        print(json.dumps({"status": "BLOCKED", "reason": "ssh_host_key_mismatch"}))
    except paramiko.SSHException:
        print(json.dumps({"status": "BLOCKED", "reason": "ssh_connection_or_host_key_failed"}))
    except (OSError, ValueError, TypeError, KeyError):
        print(json.dumps({"status": "BLOCKED", "reason": "inventory_or_credentials_unavailable"}))
    finally:
        client.close()
    return 2


if __name__ == "__main__":
    sys.exit(main())
