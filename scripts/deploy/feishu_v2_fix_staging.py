"""Upgrade only the existing isolated test project, preserving users and OAuth.

Requires five passing exact-source gates, verified images, a stopped-worker
database snapshot and a successful restore. Original public entry is untouched.
Credentials are read through stdin; reports never include environment values.
"""
import argparse
import base64
import hashlib
import json
import re
import sys
import textwrap
from pathlib import Path, PurePosixPath

import paramiko

REMOTE = r"""
import json,pathlib,subprocess
root=pathlib.Path(ROOT);site=root/'isolated-site';proof=json.loads((site/'preparation.json').read_text())
project='knowhub-feishu-v2-'+OLD_SHA[:12]
if proof['project']!=project or root.is_symlink():raise ValueError('isolated_project_mismatch')
compose=['docker','compose','--project-name',project,'--file',str(site/'compose.json')]
def run(args):return subprocess.run(args,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True,timeout=45)
volume=project+'_staging';images=[]
for service in ['app','worker2']:
 cid=run(compose+['ps','--quiet',service]).stdout.strip()
 record=json.loads(run(['docker','inspect',cid]).stdout)[0]
 if record['Config']['Labels'].get('com.docker.compose.project')!=project:raise ValueError('container_project_mismatch')
 mount=[m for m in record['Mounts'] if m['Destination']=='/data/feishu-staging']
 if len(mount)!=1 or mount[0].get('Type')!='volume' or mount[0].get('Name')!=volume or not mount[0]['RW']:raise ValueError('unexpected_staging_mount')
 images.append(record['Image'])
if len(set(images))!=1:raise ValueError('worker_image_drift')
v=json.loads(run(['docker','volume','inspect',volume]).stdout)[0]
if v.get('Labels',{}).get('com.docker.compose.project')!=project:raise ValueError('staging_volume_not_owned')
command=['docker','run','--rm','--pull=never','--network=none','--read-only','--user','0:0','--cap-drop=ALL','--cap-add=CHOWN','--cap-add=FOWNER','--mount','type=volume,source='+volume+',destination=/staging','--entrypoint','/bin/sh',images[0],'-c',
 'test ! -L /staging && chown 1000:1000 /staging && chmod 0700 /staging && stat -c "%u:%g:%a" /staging']
p=run(command)
if p.returncode or p.stdout.strip()!='1000:1000:700':raise ValueError('staging_owner_fix_failed')
print(json.dumps({'status':'FIXED','scope':'isolated_staging_volume_root_only','owner_uid':1000,'owner_gid':1000,'mode':'0700','recursive_change':False,'business_rows_modified':False,'tasks_reset':False,'knowledge_ids_replaced':False}))
"""


def read_json(path):
    raw = path.read_bytes()
    return json.loads(raw.decode('utf-16') if raw.startswith((b'\xff\xfe', b'\xfe\xff')) else raw.decode('utf-8-sig'))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--manifest', type=Path, required=True)
    parser.add_argument('--candidate-manifest', type=Path, required=True)
    parser.add_argument('--release-gates', type=Path)
    parser.add_argument('--test-username')
    parser.add_argument('--mode', choices=['prepare', 'apply', 'finish', 'status', 'rollback'], required=True)
    args = parser.parse_args()
    old, new = read_json(args.manifest), read_json(args.candidate_manifest)
    for manifest in [old, new]:
        sha = manifest['source_sha']
        root = PurePosixPath(manifest['source_directory']).parent
        if (not re.fullmatch('[a-f0-9]{40}', sha) or root.parent != PurePosixPath('/mnt/sata0/knowhub-feishu-v2-builds')
                or not re.fullmatch(sha[:12] + r'-[a-f0-9]{8}', root.name)):
            raise SystemExit('invalid source manifest')
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
        values = {'ROOT': str(PurePosixPath(old['source_directory']).parent), 'CANDIDATE': str(PurePosixPath(new['source_directory']).parent),
                  'OLD_SHA': old['source_sha'], 'SHA': new['source_sha'], 'MODE': args.mode,
                  'GATES': read_json(args.release_gates) if args.release_gates else {}, 'TEST_USERNAME':args.test_username}
        guarded = 'try:\n' + textwrap.indent(REMOTE, ' ')
        guarded += "\nexcept Exception as error:\n print(json.dumps({'status':'BLOCKED','reason':str(error) if isinstance(error,ValueError) else 'isolated_upgrade_operation_failed','error_type':type(error).__name__}))\n raise SystemExit(2)\n"
        code = base64.b64encode(('\n'.join(k + '=' + repr(v) for k, v in values.items()) + '\n' + guarded).encode()).decode()
        _, out, _ = client.exec_command("python3 -c \"import base64;exec(base64.b64decode('" + code + "'))\"", timeout=600)
        result = json.loads(out.read(65536))
        print(json.dumps(result, indent=2))
        return 2 if result.get('status') == 'BLOCKED' else 0
    except (OSError, ValueError, KeyError, TypeError, paramiko.SSHException) as error:
        print(json.dumps({'status': 'BLOCKED', 'reason': 'verified_isolated_upgrade_unavailable', 'error_type': type(error).__name__}))
        return 2
    finally:
        client.close()


if __name__ == '__main__':
    sys.exit(main())
