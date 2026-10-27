"""Write the authorized gateway into the new test project's private model config.

The key arrives through encrypted SSH stdin and is never printed, put into
Docker arguments/environment, or written inside the source/image context.
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
import json,os,pathlib,subprocess
credential=json.loads(__import__('sys').stdin.read(16384))
if credential.get('base_url')!='https://uniapi.ruijie.com.cn/v1': raise ValueError('unexpected_gateway')
key=credential['api_key']
if not isinstance(key,str) or not key or '\n' in key or '\r' in key: raise ValueError('invalid_gateway_credential')
root=pathlib.Path(ROOT)
if root.is_symlink() or str(root.resolve().parent)!='/mnt/sata0/knowhub-feishu-v2-builds':
 raise ValueError('unexpected_build_root')
release=root/'isolated-site'
proof=json.loads((release/'preparation.json').read_text())
project='knowhub-feishu-v2-'+SHA[:12]
if proof['source_sha']!=SHA or proof['project']!=project: raise ValueError('project_identity_mismatch')
compose_path=release/'compose.json'
compose=json.loads(compose_path.read_text())
if compose['name']!=project: raise ValueError('compose_identity_mismatch')
model={'id':'builtin-feishu-v2-embedding','type':'Embedding','source':'remote','name':'text-embedding-v4',
 'description':'Isolated Feishu V2 test gateway',
 'parameters':{'base_url':credential['base_url'],'api_key':key,'provider':'generic','max_concurrency':1,
 'extra_config':{'embedding_max_batch_size':'10'},
 'embedding_parameters':{'dimension':1024,'truncate_prompt_tokens':0}}}
content=json.dumps({'builtin_models':[model]},indent=2)+'\n'
target=release/'builtin_models.yaml'
if target.is_symlink(): raise ValueError('unexpected_model_config_path')
if target.exists() and target.read_text()!=content: raise ValueError('existing_gateway_config_differs')
if not target.exists():
 p=subprocess.run(['docker','compose','--project-name',project,'--file',str(compose_path),
  'ps','--status','running','--quiet','app','worker2'],stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True,timeout=30)
 if p.returncode or p.stdout.strip(): raise ValueError('gateway_must_be_configured_before_app_start')
 fd=os.open(str(target),os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
 with os.fdopen(fd,'w') as output: output.write(content)
 # The private parent directory is 0700 on the host. Only the two explicit
 # readonly file mounts expose this config to appuser in the new containers.
 target.chmod(0o644)
for service in ['app','worker2']:
 mount='./builtin_models.yaml:/app/config/builtin_models.yaml:ro'
 if mount not in compose['services'][service]['volumes']:compose['services'][service]['volumes'].append(mount)
temporary=release/'compose.gateway.json'
with temporary.open('x') as output:json.dump(compose,output,indent=2)
temporary.chmod(0o600)
p=subprocess.run(['docker','compose','--project-name',project,'--file',str(temporary),'config','--quiet'],
 stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=30)
if p.returncode:raise ValueError('gateway_compose_validation_failed')
temporary.replace(compose_path)
credential.clear();key=None;model=None;content=None
print(json.dumps({'status':'PREPARED','source_sha':SHA,'project':project,
 'model':'text-embedding-v4','dimensions':1024,'model_calls_performed':0,'application_restarted':False,
 'scope':'private_isolated_builtin_model_configuration_only'}))
'''


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--manifest',type=Path,required=True)
    args=parser.parse_args();raw=args.manifest.read_bytes()
    manifest=json.loads(raw.decode('utf-16') if raw.startswith((b'\xff\xfe',b'\xfe\xff')) else raw.decode('utf-8-sig'))
    sha=manifest['source_sha'];root=PurePosixPath(manifest['source_directory']).parent
    if (not re.fullmatch('[a-f0-9]{40}',sha) or root.parent!=PurePosixPath('/mnt/sata0/knowhub-feishu-v2-builds')
            or not re.fullmatch(sha[:12]+r'-[a-f0-9]{8}',root.name)):raise SystemExit('invalid staged source manifest')
    client=paramiko.SSHClient()
    try:
        credential=json.loads(sys.stdin.read(32768));gateway=credential.pop('gateway')
        if credential.get('host')!='172.30.232.42' or credential.get('user')!='aqyfzx':raise ValueError('unexpected_target')
        client.load_system_host_keys();known=Path.home()/'.ssh'/'known_hosts'
        if known.is_file():client.load_host_keys(str(known))
        client.set_missing_host_key_policy(paramiko.RejectPolicy())
        client.connect(credential['host'],username=credential['user'],password=credential['password'],
                       timeout=15,auth_timeout=15,banner_timeout=15,allow_agent=False,look_for_keys=False)
        credential.clear()
        guarded='try:\n'+textwrap.indent(REMOTE,' ')
        guarded+="\nexcept Exception as error:\n print(json.dumps({'status':'BLOCKED','error_type':type(error).__name__,"
        guarded+="'reason':str(error) if isinstance(error,ValueError) else 'isolated_gateway_config_failed'}))\n raise SystemExit(2)\n"
        prefix='ROOT='+repr(str(root))+'\nSHA='+repr(sha)+'\n'
        code=base64.b64encode((prefix+guarded).encode()).decode()
        incoming,out,_=client.exec_command("python3 -c \"import base64;exec(base64.b64decode('"+code+"'))\"",timeout=90)
        incoming.write(json.dumps(gateway));incoming.channel.shutdown_write();gateway.clear()
        result=json.loads(out.read(65536));print(json.dumps(result,indent=2))
        return 0 if result.get('status')=='PREPARED' else 2
    except (OSError,ValueError,KeyError,TypeError,paramiko.SSHException):
        print(json.dumps({'status':'BLOCKED','reason':'verified_gateway_configuration_unavailable'}));return 2
    finally:client.close()


if __name__=='__main__':sys.exit(main())
