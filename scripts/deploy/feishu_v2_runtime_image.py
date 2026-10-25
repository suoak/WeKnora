"""Package a successfully compiled exact-SHA binary on the isolated test host.

The pinned Debian runtime is an already verified local image. No application
service, database, existing environment file or entry route is modified.
"""
import argparse
import base64
import json
import re
import sys
import textwrap
from pathlib import Path, PurePosixPath

import paramiko

DOCKERFILE = '''FROM knowhub-feishu-v2/runtime-base:0bb2ceb0b9d98d926762f75f25fc3377d03e4b15679797d2c8dafd523a78afd7
LABEL org.opencontainers.image.revision="__SHA__"
LABEL knowhub.runtime-base="sha256:0bb2ceb0b9d98d926762f75f25fc3377d03e4b15679797d2c8dafd523a78afd7"
WORKDIR /app
COPY WeKnora /app/WeKnora
COPY config /app/config
COPY scripts /app/scripts
COPY migrations /app/migrations
COPY dataset/samples /app/dataset/samples
RUN chmod 755 /app/WeKnora /app/scripts/*.sh
ENTRYPOINT ["./scripts/docker-entrypoint.sh"]
CMD ["./WeKnora"]
'''

REMOTE = r'''
import hashlib,json,pathlib,shutil,subprocess,os
root=pathlib.Path(ROOT)
base=pathlib.Path('/mnt/sata0/knowhub-feishu-v2-builds')
if root.is_symlink() or root.resolve().parent!=base.resolve(): raise ValueError('unexpected_build_root')
source=root/'source'
manifest=json.loads((root/'source-manifest.json').read_text())
if manifest['source_sha']!=SHA or manifest['source_directory']!=str(source): raise ValueError('source_manifest_mismatch')
def run(args,timeout=30):
 return subprocess.run(args,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True,timeout=timeout)
builder='knowhub-feishu-v2-build-'+root.name+'-'+str(ATTEMPT)
record=run(['docker','inspect',builder])
if record.returncode: raise ValueError('candidate_build_not_found')
record=json.loads(record.stdout)[0]
if record['Config'].get('Labels',{}).get('knowhub.source-sha')!=SHA:
 raise ValueError('candidate_source_mismatch')
if record['State']['Status']!='exited' or record['State']['ExitCode']!=0:
 raise ValueError('candidate_build_not_successful')
binary=source/'WeKnora'
if binary.is_symlink() or binary.stat().st_size>1024**3: raise ValueError('unexpected_binary')
digest=hashlib.sha256(binary.read_bytes()).hexdigest()
if (source/'candidate-server.sha256').read_text().split()[0]!=digest:
 raise ValueError('candidate_binary_checksum_mismatch')
build_info=(source/'candidate-server-build-info.txt').read_text()
if SHA not in build_info or 'anydoc' not in build_info: raise ValueError('candidate_build_info_mismatch')
image='knowhub-feishu-v2/app:'+SHA
base_id='sha256:0bb2ceb0b9d98d926762f75f25fc3377d03e4b15679797d2c8dafd523a78afd7'
base_tag='knowhub-feishu-v2/runtime-base:'+base_id.split(':')[1]
base_image=run(['docker','image','inspect',base_id])
if base_image.returncode or json.loads(base_image.stdout)[0]['Id']!=base_id:
 raise ValueError('pinned_runtime_base_unavailable')
base_tagged=run(['docker','image','inspect',base_tag])
if base_tagged.returncode==0 and json.loads(base_tagged.stdout)[0]['Id']!=base_id:
 raise ValueError('existing_runtime_base_tag_mismatch')
if base_tagged.returncode and run(['docker','tag',base_id,base_tag]).returncode:
 raise ValueError('pinned_local_runtime_base_tag_failed')
suffix='-'+str(PACKAGE_ATTEMPT)
context=root/('runtime-image'+suffix)
if context.is_symlink(): raise ValueError('unexpected_runtime_context')
if not context.exists():
 context.mkdir(mode=0o700)
 shutil.copyfile(str(binary),str(context/'WeKnora'))
 (context/'WeKnora').chmod(0o755)
 for folder in ['config','scripts','migrations','dataset/samples']:
  incoming=source/folder
  for path in incoming.rglob('*'):
   if path.is_symlink(): raise ValueError('unexpected_runtime_source_symlink')
  shutil.copytree(str(incoming),str(context/folder))
 (context/'Dockerfile').write_text(DOCKERFILE)
elif (context/'Dockerfile').read_text()!=DOCKERFILE:
 raise ValueError('runtime_dockerfile_mismatch')
if hashlib.sha256((context/'WeKnora').read_bytes()).hexdigest()!=digest:
 raise ValueError('runtime_binary_checksum_mismatch')
name='knowhub-feishu-v2-runtime-package-'+root.name
script=root/('package-runtime'+suffix+'.sh')
if not script.exists():
 with script.open('x') as out:
  out.write('#!/bin/bash\nset -euo pipefail\n')
  out.write('DOCKER_BUILDKIT=0 docker build --network=none --pull=false -t '+image+' '+str(context)+' > '+str(root/('runtime-image-build'+suffix+'.log'))+' 2>&1\n')
  out.write("docker run --rm --pull=never --network=none --read-only --cap-drop=ALL --pids-limit=64 --entrypoint=/bin/sh "+image+
   " -c 'ldd /app/WeKnora && sha256sum /app/WeKnora' > "+str(root/('runtime-link-check'+suffix+'.txt'))+' 2>&1\n')
  out.write("! grep -E 'not found|version .*not found' "+str(root/('runtime-link-check'+suffix+'.txt'))+'\n')
  out.write('docker image inspect --format=\'{{.Id}}\' '+image+' > '+str(root/'runtime-image.id')+'\n')
script.chmod(0o700)
# Detached host build writes only the fresh build context and image tag.
# Polling is idempotent; do not launch a second Docker build on repeat calls.
pidfile=root/('runtime-package'+suffix+'.pid')
done=root/('runtime-package'+suffix+'.exit')
if not pidfile.exists():
 wrapper='bash '+str(script)+'; result=$?; printf "%s" "$result" > '+str(done)
 child=subprocess.Popen(['/bin/bash','-c',wrapper],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,
                        stdin=subprocess.DEVNULL,start_new_session=True)
 pidfile.write_text(str(child.pid))
status='running'
result={'source_sha':SHA,'image_tag':image,'binary_sha256':digest,'application_services_started':False}
if done.exists():
 code=int(done.read_text())
 status='PASS' if code==0 else 'FAILED'
 result['exit_code']=code
 if code==0:
  result['image_id']=(root/'runtime-image.id').read_text().strip()
  result['link_check']=(root/('runtime-link-check'+suffix+'.txt')).read_text()[-10000:]
 else:
  for filename in ['runtime-image-build'+suffix+'.log','runtime-link-check'+suffix+'.txt']:
   p=root/filename
   if p.is_file(): result[filename]=p.read_text()[-5000:]
result['status']=status
if status=='running':
 log=root/('runtime-image-build'+suffix+'.log')
 if log.is_file(): result['build_log_tail']=log.read_text()[-2500:]
print(json.dumps(result))
'''


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--manifest', type=Path, required=True)
    parser.add_argument('--attempt', type=int, choices=range(1, 11), default=1)
    parser.add_argument('--package-attempt', type=int, choices=range(1, 11), default=1)
    args = parser.parse_args()
    raw = args.manifest.read_bytes()
    manifest = json.loads(raw.decode('utf-16') if raw.startswith((b'\xff\xfe', b'\xfe\xff')) else raw.decode('utf-8-sig'))
    sha = manifest['source_sha']
    root = PurePosixPath(manifest['source_directory']).parent
    if (not re.fullmatch('[a-f0-9]{40}', sha) or root.parent != PurePosixPath('/mnt/sata0/knowhub-feishu-v2-builds')
            or not re.fullmatch(sha[:12] + r'-[a-f0-9]{8}', root.name)):
        raise SystemExit('invalid staged source manifest')
    client = paramiko.SSHClient()
    try:
        credential = json.loads(sys.stdin.read(16384))
        if credential.get('host') != '172.30.232.42' or credential.get('user') != 'aqyfzx':
            raise ValueError('unexpected_target')
        client.load_system_host_keys()
        known = Path.home() / '.ssh' / 'known_hosts'
        if known.is_file():
            client.load_host_keys(str(known))
        client.set_missing_host_key_policy(paramiko.RejectPolicy())
        client.connect(credential['host'], username=credential['user'], password=credential['password'],
                       timeout=15, auth_timeout=15, banner_timeout=15, allow_agent=False, look_for_keys=False)
        credential.clear()
        prefix = 'ROOT=' + repr(str(root)) + '\nSHA=' + repr(sha) + '\nATTEMPT=' + repr(args.attempt) + '\n'
        prefix += 'PACKAGE_ATTEMPT=' + repr(args.package_attempt) + '\n'
        prefix += 'DOCKERFILE=' + repr(DOCKERFILE.replace('__SHA__', sha)) + '\n'
        guarded = 'try:\n' + textwrap.indent(REMOTE, ' ')
        guarded += "\nexcept Exception as error:\n print(json.dumps({'status':'BLOCKED','error_type':type(error).__name__,"
        guarded += "'reason':str(error) if isinstance(error,ValueError) else 'runtime_package_operation_failed'}))\n raise SystemExit(2)\n"
        code = base64.b64encode((prefix + guarded).encode()).decode()
        _, out, _ = client.exec_command("python3 -c \"import base64;exec(base64.b64decode('" + code + "'))\"", timeout=90)
        report = json.loads(out.read(65536))
        print(json.dumps(report, indent=2))
        return 1 if report.get('status') in ('FAILED', 'BLOCKED') else 0
    except (OSError, ValueError, KeyError, TypeError, paramiko.SSHException):
        print(json.dumps({'status': 'BLOCKED', 'reason': 'verified_runtime_packaging_unavailable'}))
        return 2
    finally:
        client.close()


if __name__ == '__main__':
    sys.exit(main())
