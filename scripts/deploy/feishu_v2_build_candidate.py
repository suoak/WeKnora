"""Compile a staged exact-SHA candidate in an isolated, resource-limited container.

Reads SSH credentials from stdin. This never starts application services or
connects to databases, Redis, model gateways or existing application storage.
"""
import argparse
import base64
import json
import hashlib
import re
import sys
from pathlib import Path, PurePosixPath

import paramiko

BUILD_SCRIPT = r'''#!/bin/bash
set -euo pipefail
export GOTOOLCHAIN=go1.26.9
export GOSUMDB=sum.golang.org
export GOENV=off
export GOPROXY='https://goproxy.cn|https://proxy.golang.org|direct'
export GOPATH=/cache/go
export GOCACHE=/cache/go-build
export CARGO_HOME=/cache/cargo
export RUSTUP_HOME=/cache/rustup
export CARGO_BUILD_JOBS=2
export GOMAXPROCS=2
export PATH=/cache/cargo/bin:$PATH
mkdir -p /cache/cargo /cache/rustup /cache/go-build
if [ -f /native-package/go1.26.9.linux-amd64.tar.gz ]; then
 mkdir -p /cache/go-sdk
 if [ ! -x /cache/go-sdk/bin/go ]; then
  tar -xzf /native-package/go1.26.9.linux-amd64.tar.gz -C /cache/go-sdk --no-same-owner --no-same-permissions
 fi
 export PATH=/cache/go-sdk/bin:$PATH
 export GOROOT=/cache/go-sdk
 export GOTOOLCHAIN=local
elif [ ! -x /cache/cargo/bin/cargo ]; then
 curl --proto '=https' --tlsv1.2 -fsSL --retry 3 --connect-timeout 15 --max-time 180 \
  https://sh.rustup.rs -o /cache/rustup-init.sh
 sh /cache/rustup-init.sh -y --no-modify-path --profile minimal --default-toolchain 1.99.1
fi
cd /workspace
go version > candidate-go-version.txt
if [ -f /native-package/go1.26.9.linux-amd64.tar.gz ]; then
 test "$(go version)" = 'go version go1.26.9 linux/amd64'
else
 rustc --version > candidate-rust-version.txt
 bash scripts/build-anydoc-lib.sh
fi
sha256sum third_party/anydoc-go/lib/linux_amd64_gnu/libanydoc_go.a > candidate-native.sha256
if [ -d /baseline-modcache/cache/download ]; then
 python3 - <<'PY'
import pathlib,shutil
baseline=pathlib.Path('/baseline-modcache/cache/download')
destination=pathlib.Path('/cache/go/pkg/mod/cache/download')
def escape(value):
 return ''.join('!'+c.lower() if 'A'<=c<='Z' else c for c in value)
copied=0
for line in pathlib.Path('go.sum').read_text().splitlines():
 module,version,_=line.split()
 version=version.removesuffix('/go.mod')
 relative=pathlib.PurePosixPath(escape(module))/'@v'/escape(version)
 if relative.is_absolute() or '..' in relative.parts: raise ValueError('invalid_module_path')
 for extension in ['.info','.mod','.zip','.ziphash']:
  src=baseline/(str(relative)+extension)
  dst=destination/(str(relative)+extension)
  if src.is_file() and not src.is_symlink() and not dst.exists():
   src.resolve().relative_to(baseline.resolve())
   dst.parent.mkdir(parents=True,exist_ok=True)
   shutil.copyfile(src,dst)
   copied+=1
print('Copied checksum-addressed module cache files:',copied)
PY
fi
go mod download github.com/mattn/go-sqlite3
go mod verify
sqlite_module=$(go list -m -f '{{.Dir}}' github.com/mattn/go-sqlite3)
test -n "$sqlite_module"
mkdir -p /cache/sqlite-headers
cp "$sqlite_module/sqlite3-binding.h" /cache/sqlite-headers/sqlite3.h
cp "$sqlite_module/sqlite3ext.h" /cache/sqlite-headers/sqlite3ext.h
export CPATH=/cache/sqlite-headers
sha256sum /cache/sqlite-headers/*.h > candidate-sqlite-headers.sha256
export VERSION=feishu-v2-isolated-candidate
export COMMIT_ID=__SOURCE_SHA__
export BUILD_TIME
BUILD_TIME=$(date -u +%Y-%m-%dT%H:%M:%SZ)
export GO_VERSION
GO_VERSION=$(go version)
make build-prod GO_BUILD_TAGS=anydoc
go build -tags anydoc -o feishu-diagnostic ./cmd/feishu-diagnostic
sha256sum feishu-diagnostic > candidate-diagnostic.sha256
sha256sum WeKnora > candidate-server.sha256
go version -m WeKnora > candidate-server-build-info.txt
'''

REMOTE = r'''
import base64,hashlib,json,os,pathlib,re,shutil,subprocess,tarfile
root=pathlib.Path(BUILD_ROOT)
source=root/'source'
base=pathlib.Path('/mnt/sata0/knowhub-feishu-v2-builds')
if root.is_symlink() or root.resolve().parent!=base.resolve() or source.is_symlink():
 raise ValueError('unexpected_build_root')
manifest=json.loads((root/'source-manifest.json').read_text())
if manifest['source_sha']!=SOURCE_SHA or manifest['source_directory']!=str(source):
 raise ValueError('source_manifest_mismatch')
image='sha256:33b84179859a3a275976e22ecf184643e4835de0a230d635f6ad88e299634ef0'
name='knowhub-feishu-v2-build-'+root.name+'-'+str(ATTEMPT)
def run(args):
 return subprocess.run(args,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True,timeout=30)
existing=run(['docker','inspect',name])
if existing.returncode==0:
 record=json.loads(existing.stdout)[0]
 if record['Config'].get('Labels',{}).get('knowhub.source-sha')!=SOURCE_SHA:
  raise ValueError('container_source_mismatch')
else:
 available=0
 for line in pathlib.Path('/proc/meminfo').read_text().splitlines():
  if line.startswith('MemAvailable:'): available=int(line.split()[1])*1024
 if available<8*1024**3: raise ValueError('insufficient_free_build_memory')
 package=root/'native-package'
 if NATIVE_MANIFEST is not None:
  if package.is_symlink() or package.resolve().parent!=root.resolve(): raise ValueError('unexpected_package_path')
  if json.loads((package/'manifest.json').read_text())!=NATIVE_MANIFEST: raise ValueError('native_manifest_mismatch')
  for filename,digest in [('libanydoc_go.a',NATIVE_MANIFEST['library_sha256']),
    ('go1.26.9.linux-amd64.tar.gz',NATIVE_MANIFEST['go_sdk_sha256'])]:
   payload=package/filename
   if payload.is_symlink() or hashlib.sha256(payload.read_bytes()).hexdigest()!=digest:
    raise ValueError('native_package_checksum_mismatch')
  for filename,digest in NATIVE_MANIFEST['native_source_sha256'].items():
   path=source/filename
   path.resolve().relative_to(source.resolve())
   if hashlib.sha256(path.read_bytes()).hexdigest()!=digest:
    print(json.dumps({'remote_error_type':'ValueError','reason':'native_source_mismatch','file':filename}))
    raise SystemExit(2)
  with tarfile.open(str(package/'go1.26.9.linux-amd64.tar.gz'),'r:gz') as archive:
   entries=archive.getmembers()
   if len(entries)>30000 or sum(e.size for e in entries)>1024**3: raise ValueError('sdk_archive_limit')
   for entry in entries:
    path=pathlib.PurePosixPath(entry.name)
    if path.is_absolute() or '..' in path.parts or not (entry.isdir() or entry.isfile()):
     raise ValueError('unsafe_sdk_archive')
  target=source/'third_party/anydoc-go/lib/linux_amd64_gnu/libanydoc_go.a'
  target.parent.mkdir(parents=True,exist_ok=True)
  if target.exists():
   if target.is_symlink() or hashlib.sha256(target.read_bytes()).hexdigest()!=NATIVE_MANIFEST['library_sha256']:
    raise ValueError('existing_native_library_mismatch')
  else:
   with target.open('xb') as output, (package/'libanydoc_go.a').open('rb') as incoming:
    shutil.copyfileobj(incoming,output)
 script=root/('compile-candidate-'+str(ATTEMPT)+'.sh')
 with script.open('xb') as f: f.write(base64.b64decode(SCRIPT))
 cache='knowhub-feishu-v2-build-cache-'+root.name+'-'+str(ATTEMPT)
 temp='knowhub-feishu-v2-build-temp-'+root.name+'-'+str(ATTEMPT)
 volumes=[cache,temp]
 if REUSE_ATTEMPT is not None:
  reuse_root=pathlib.Path(REUSE_ROOT) if REUSE_ROOT else root
  reuse_sha=REUSE_SHA or SOURCE_SHA
  if reuse_root.is_symlink() or reuse_root.resolve().parent!=base.resolve():raise ValueError('unexpected_cache_root')
  owner_manifest=json.loads((reuse_root/'source-manifest.json').read_text())
  if owner_manifest['source_sha']!=reuse_sha:raise ValueError('cache_manifest_identity_mismatch')
  prior=run(['docker','inspect','knowhub-feishu-v2-build-'+reuse_root.name+'-'+str(REUSE_ATTEMPT)])
  if prior.returncode: raise ValueError('cache_owner_build_missing')
  prior=json.loads(prior.stdout)[0]
  if prior['State']['Status']!='exited' or prior['Config'].get('Labels',{}).get('knowhub.source-sha')!=reuse_sha:
   raise ValueError('cache_owner_build_not_stopped')
  if reuse_sha!=SOURCE_SHA and (prior['State']['ExitCode']!=0 or prior['Image']!=image
   or prior['Config'].get('Labels',{}).get('knowhub.phase')!='compile-only'):
   raise ValueError('cross_source_cache_requires_successful_owned_builder')
  # A successful builder may itself have reused an older owned cache. Inspect
  # its actual mount instead of inventing a volume owned by that newer SHA.
  mounted=[m for m in prior.get('Mounts',[]) if m.get('Destination')=='/cache' and m.get('Type')=='volume']
  if len(mounted)!=1:raise ValueError('cache_owner_mount_ambiguous')
  cache=mounted[0].get('Name','')
  match=re.fullmatch(r'knowhub-feishu-v2-build-cache-([a-f0-9]{12}-[a-f0-9]{8})-(\d+)',cache)
  if not match:raise ValueError('cache_volume_outside_build_namespace')
  volume=run(['docker','volume','inspect',cache])
  owner_root=base/match[1]
  if owner_root.is_symlink() or owner_root.resolve().parent!=base.resolve():
   raise ValueError('cache_original_owner_outside_build_root')
  owner_sha=json.loads((owner_root/'source-manifest.json').read_text()).get('source_sha','')
  if not re.fullmatch('[a-f0-9]{40}',owner_sha) or not owner_sha.startswith(match[1][:12]):
   raise ValueError('cache_original_owner_manifest_mismatch')
  if volume.returncode or json.loads(volume.stdout)[0].get('Labels',{}).get('knowhub.source-sha')!=owner_sha:
   raise ValueError('cache_source_identity_mismatch')
  active=run(['docker','ps','--filter','volume='+cache,'--format','{{.ID}}'])
  if active.returncode or active.stdout.strip():raise ValueError('cache_must_have_no_running_consumers')
  volumes=[temp]
 for volume in volumes:
  if run(['docker','volume','inspect',volume]).returncode==0:
   raise ValueError('unexpected_existing_build_volume')
  if run(['docker','volume','create','--label','knowhub.source-sha='+SOURCE_SHA,volume]).returncode:
   raise ValueError('build_volume_creation_failed')
 command=['docker','run','-d','--pull=never','--name',name,
  '--label','knowhub.source-sha='+SOURCE_SHA,'--label','knowhub.phase=compile-only',
  '--cpus','2','--memory','4g','--pids-limit','256','--read-only',
  '--cap-drop','ALL','--cap-add','DAC_OVERRIDE','--security-opt','no-new-privileges:true',
  '--volume',str(source)+':/workspace', '--volume',str(script)+':/compile-candidate.sh:ro',
  '--volume',cache+':/cache','--volume',temp+':/tmp','--workdir','/workspace',
  '--entrypoint','/bin/bash',image,'/compile-candidate.sh']
 if NATIVE_MANIFEST is not None:
  command[command.index('--workdir'):command.index('--workdir')]=['--volume',str(package)+':/native-package:ro']
 baseline=pathlib.Path('/home/aqyfzx/go/pkg/mod')
 if baseline.is_dir() and not baseline.is_symlink():
  command[command.index('--workdir'):command.index('--workdir')]=['--volume',str(baseline)+':/baseline-modcache:ro']
 started=run(command)
 if started.returncode: raise ValueError('candidate_build_start_failed')
 record=json.loads(run(['docker','inspect',name]).stdout)[0]
state=record['State']
result={'source_sha':SOURCE_SHA,'build_directory':str(root),'build_container':name,
 'build_image':record['Image'],'status':state['Status'],'exit_code':state.get('ExitCode'),
 'started_at':state.get('StartedAt'),'oom_killed':state.get('OOMKilled'),
 'application_services_started':False}
for filename in ['candidate-go-version.txt','candidate-rust-version.txt','candidate-native.sha256','candidate-server.sha256']:
 path=source/filename
 if path.is_file(): result[filename]=path.read_text().strip()
if state['Status']=='running':
 import re
 progress=run(['docker','logs','--tail','12',name])
 result['build_log_tail']=re.sub(r'(https?://)[^\s/@]+@',r'\1[redacted]@',(progress.stdout+progress.stderr)[-4000:])
 processes=run(['docker','top',name,'-eo','comm'])
 result['build_process_names']=processes.stdout[-1500:] if processes.returncode==0 else 'unavailable'
if state['Status']=='exited' and state.get('ExitCode')!=0:
 import re,urllib.parse
 tail=run(['docker','logs','--tail','35',name])
 result['build_log_tail']=re.sub(r'(https?://)[^\s/@]+@',r'\1[redacted]@',(tail.stdout+tail.stderr)[-12000:])
 proxy_metadata={}
 for value in record['Config'].get('Env',[]):
  key,_,raw=value.partition('=')
  if key.lower() in ['http_proxy','https_proxy','all_proxy'] and raw:
   uri=urllib.parse.urlparse(raw)
   proxy_metadata[key]={'scheme':uri.scheme,'host':uri.hostname,'port':uri.port,'has_auth':bool(uri.username)}
 result['build_container_proxy_metadata']=proxy_metadata
 if state.get('ExitCode')==35:
  probe="import json,os,urllib.parse; names=['HTTPS_PROXY','HTTP_PROXY','ALL_PROXY','https_proxy','http_proxy','all_proxy']; print(json.dumps({n:{'scheme':urllib.parse.urlparse(os.environ[n]).scheme,'host':urllib.parse.urlparse(os.environ[n]).hostname,'port':urllib.parse.urlparse(os.environ[n]).port,'has_auth':bool(urllib.parse.urlparse(os.environ[n]).username)} for n in names if os.environ.get(n)}))"
  diagnostic=run(['docker','run','--rm','--pull=never','--network','none','--read-only','--cap-drop','ALL',
   '--pids-limit','32','--entrypoint','python3',image,'-c',probe])
  if diagnostic.returncode==0: result['build_image_proxy_metadata']=json.loads(diagnostic.stdout)
print(json.dumps(result))
'''


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--manifest", type=Path, required=True)
    parser.add_argument("--native-package", type=Path)
    parser.add_argument("--attempt", type=int, choices=range(1, 11), default=1)
    parser.add_argument("--reuse-cache-attempt", type=int, choices=range(1, 11))
    parser.add_argument("--reuse-cache-manifest", type=Path)
    args = parser.parse_args()
    if args.reuse_cache_manifest and args.reuse_cache_attempt is None:
        parser.error('cache manifest requires an explicit stopped build attempt')
    if not args.reuse_cache_manifest and args.reuse_cache_attempt is not None and args.reuse_cache_attempt >= args.attempt:
        parser.error('cache reuse must refer to an earlier stopped attempt of this source')
    encoded_manifest = args.manifest.read_bytes()
    manifest = json.loads(encoded_manifest.decode("utf-16") if encoded_manifest.startswith((b"\xff\xfe", b"\xfe\xff"))
                          else encoded_manifest.decode("utf-8-sig"))
    sha = manifest["source_sha"]
    root = PurePosixPath(manifest["source_directory"]).parent
    if not re.fullmatch(r"[a-f0-9]{40}", sha) or root.parent != PurePosixPath("/mnt/sata0/knowhub-feishu-v2-builds"):
        raise SystemExit("invalid staged source manifest")
    if not re.fullmatch(sha[:12] + r"-[a-f0-9]{8}", root.name):
        raise SystemExit("invalid isolated build directory")
    reuse_root, reuse_sha = None, None
    if args.reuse_cache_manifest:
        raw=args.reuse_cache_manifest.read_bytes()
        reuse=json.loads(raw.decode('utf-16') if raw.startswith((b'\xff\xfe',b'\xfe\xff')) else raw.decode('utf-8-sig'))
        reuse_sha=reuse['source_sha'];reuse_root=PurePosixPath(reuse['source_directory']).parent
        if (not re.fullmatch('[a-f0-9]{40}',reuse_sha) or reuse_root.parent!=root.parent
            or not re.fullmatch(reuse_sha[:12]+r'-[a-f0-9]{8}',reuse_root.name)):
            raise SystemExit('invalid cache owner manifest')
    native_manifest = None
    if args.native_package:
        native_manifest = json.loads((args.native_package / 'manifest.json').read_text(encoding='utf-8'))
        if (native_manifest.get('source_sha') != sha or native_manifest.get('target') != 'x86_64-unknown-linux-gnu'
                or native_manifest.get('qualification') != 'native_build_and_cargo_audit_only'
                or native_manifest.get('go_version') != 'go version go1.26.9 linux/amd64'
                or not native_manifest.get('rust_version', '').startswith('rustc 1.99.1 ')
                or native_manifest.get('audit_version') not in ('cargo-audit 0.22.2', 'cargo-audit-audit 0.22.2')):
            raise SystemExit('native package provenance mismatch')
        files = native_manifest.get('native_source_sha256', {})
        if not files or any(not re.fullmatch(r'[a-f0-9]{64}', digest) for digest in files.values()):
            raise SystemExit('invalid native source fingerprints')
        for filename, digest_key, size_key in [('libanydoc_go.a', 'library_sha256', 'library_bytes'),
                                               ('go1.26.9.linux-amd64.tar.gz', 'go_sdk_sha256', 'go_sdk_bytes')]:
            payload = args.native_package / filename
            if (payload.is_symlink() or not 0 < payload.stat().st_size <= 256 * 1024**2
                    or payload.stat().st_size != native_manifest[size_key]
                    or hashlib.sha256(payload.read_bytes()).hexdigest() != native_manifest[digest_key]):
                raise SystemExit('native package checksum mismatch')
    client = paramiko.SSHClient()
    phase = 'verified_ssh_connect'
    try:
        credential = json.loads(sys.stdin.read(16384))
        if credential.get("host") != "172.30.232.42" or credential.get("user") != "aqyfzx":
            raise ValueError("unexpected_target")
        client.load_system_host_keys()
        known = Path.home() / ".ssh" / "known_hosts"
        if known.is_file():
            client.load_host_keys(str(known))
        client.set_missing_host_key_policy(paramiko.RejectPolicy())
        client.connect(credential["host"], port=22, username=credential["user"], password=credential["password"],
                       timeout=15, auth_timeout=15, banner_timeout=15, allow_agent=False, look_for_keys=False)
        credential.clear()
        if args.native_package:
            phase = 'upload_verified_native_package'
            check = "import pathlib; p=pathlib.Path(" + repr(str(root)) + "); "
            check += "assert not p.is_symlink() and str(p.resolve().parent)=='/mnt/sata0/knowhub-feishu-v2-builds'; "
            check += "q=p/'native-package'; assert not q.is_symlink(); q.mkdir(mode=0o700,exist_ok=True)"
            code = base64.b64encode(check.encode()).decode()
            _, checked, _ = client.exec_command(
                "python3 -c \"import base64;exec(base64.b64decode('" + code + "'))\"", timeout=20)
            if checked.channel.recv_exit_status():
                raise ValueError('native_package_directory_unavailable')
            with client.open_sftp() as transfer:
                for filename in ['libanydoc_go.a', 'go1.26.9.linux-amd64.tar.gz', 'manifest.json']:
                    local = args.native_package / filename
                    remote = str(root / 'native-package' / filename)
                    try:
                        existing = transfer.lstat(remote)
                        import stat
                        if not stat.S_ISREG(existing.st_mode) or existing.st_size != local.stat().st_size:
                            raise ValueError('existing_package_payload_mismatch')
                    except FileNotFoundError:
                        with transfer.open(remote, 'wx') as output, local.open('rb') as incoming:
                            output.set_pipelined(True)
                            while part := incoming.read(1024 * 1024):
                                output.write(part)
        script = base64.b64encode(BUILD_SCRIPT.replace("__SOURCE_SHA__", sha).encode()).decode()
        prefix = "BUILD_ROOT=" + repr(str(root)) + "\nSOURCE_SHA=" + repr(sha) + "\nSCRIPT=" + repr(script) + "\n"
        prefix += 'NATIVE_MANIFEST=' + repr(native_manifest) + '\nATTEMPT=' + repr(args.attempt) + '\n'
        prefix += 'REUSE_ATTEMPT=' + repr(args.reuse_cache_attempt) + '\n'
        prefix += 'REUSE_ROOT=' + repr(str(reuse_root) if reuse_root else None) + '\nREUSE_SHA=' + repr(reuse_sha) + '\n'
        import textwrap
        guarded = 'try:\n' + textwrap.indent(REMOTE, ' ')
        guarded += "\nexcept Exception as error:\n print(json.dumps({'remote_error_type':type(error).__name__,"
        guarded += "'reason':str(error) if isinstance(error,ValueError) else 'remote_operation_failed'}))\n raise SystemExit(2)\n"
        program = base64.b64encode((prefix + guarded).encode()).decode()
        phase = 'verify_package_and_start_build'
        _, out, _ = client.exec_command(
            "python3 -c \"import base64;exec(base64.b64decode('" + program + "'))\"", timeout=120)
        raw = out.read(65536)
        if out.channel.recv_exit_status() != 0:
            try:
                failure = json.loads(raw)
                if failure.get('remote_error_type'):
                    print(json.dumps({'status': 'BLOCKED', 'phase': phase, **failure}))
                    return 2
            except ValueError:
                pass
            raise ValueError("candidate_compile_operation_failed")
        report = json.loads(raw)
        print(json.dumps(report, indent=2))
        return 1 if report["status"] == "exited" and report["exit_code"] else 0
    except (OSError, ValueError, KeyError, TypeError, paramiko.SSHException) as error:
        print(json.dumps({"status": "BLOCKED", "reason": "isolated_candidate_compile_unavailable",
                          "phase": phase, "error_type": type(error).__name__, "errno": getattr(error, 'errno', None)}))
        return 2
    finally:
        client.close()


if __name__ == "__main__":
    sys.exit(main())
