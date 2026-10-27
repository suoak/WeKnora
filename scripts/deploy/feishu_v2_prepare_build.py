"""Stage an exact committed source archive in a new isolated test build directory.

SSH authentication is read only from stdin. No application services are changed.
A verified, offline one-shot container may initialize the new empty directory;
no application environment, database, storage or secrets are copied.
"""
import argparse
import base64
import hashlib
import json
import re
import stat
import subprocess
import sys
import tempfile
from pathlib import Path

import paramiko

REMOTE = r'''
import hashlib,json,os,pathlib,tarfile
archive=pathlib.Path(ARCHIVE)
root=archive.parent
allowed=pathlib.Path('/mnt/sata0/knowhub-feishu-v2-builds')
if root.resolve().parent != allowed.resolve() or allowed.is_symlink() or root.is_symlink():
 raise ValueError('unexpected_build_directory')
digest=hashlib.sha256()
with archive.open('rb') as f:
 for chunk in iter(lambda:f.read(1048576),b''): digest.update(chunk)
if digest.hexdigest()!=EXPECTED_DIGEST: raise ValueError('archive_digest_mismatch')
source=root/'source'
source.mkdir(mode=0o700)
with tarfile.open(archive,'r:') as tar:
 members=tar.getmembers()
 for member in members:
  target=source/member.name
  try: target.resolve().relative_to(source.resolve())
  except ValueError: raise ValueError('unsafe_source_archive_member')
  if not (member.isdir() or member.isfile()):
   raise ValueError('unsafe_source_archive_member')
 for member in members:
  target=source/member.name
  if member.isdir(): target.mkdir(parents=True,exist_ok=True,mode=0o755)
  else:
   target.parent.mkdir(parents=True,exist_ok=True,mode=0o755)
   with target.open('xb') as out,tar.extractfile(member) as content:
    for chunk in iter(lambda:content.read(1048576),b''): out.write(chunk)
   target.chmod(0o755 if member.mode&0o111 else 0o644)
manifest={'source_sha':SOURCE_SHA,'source_archive_sha256':EXPECTED_DIGEST,
 'source_directory':str(source),'phase':'source_staged_only','deployed':False}
(root/'source-manifest.json').write_text(json.dumps(manifest,indent=2)+'\n')
print(json.dumps(manifest))
'''

PREPARE_ROOT = r'''
import json,os,pathlib,subprocess
root=pathlib.Path('/mnt/sata0/knowhub-feishu-v2-builds')
if not root.exists() and not root.is_symlink():
 try: root.mkdir(mode=0o700)
 except PermissionError:
  # The verified build image initializes only this new empty bind directory.
  # No application or production mounts, network or Docker socket are exposed.
  image='sha256:33b84179859a3a275976e22ecf184643e4835de0a230d635f6ad88e299634ef0'
  command=['docker','run','--rm','--pull=never','--network','none','--read-only',
   '--cap-drop','ALL','--cap-add','CHOWN','--pids-limit','32','--user','0:0',
   '--volume',str(root)+':/newbuild','--entrypoint','/bin/sh',image,'-c',
   'chmod 700 /newbuild && chown '+str(os.getuid())+':'+str(os.getgid())+' /newbuild']
  result=subprocess.run(command,stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=30)
  if result.returncode: raise ValueError('isolated_root_initialization_failed')
if root.is_symlink() or root.resolve()!=root or root.stat().st_uid!=os.getuid():
 raise ValueError('unexpected_build_root_owner_or_path')
print(json.dumps({'owned_isolated_build_root':str(root)}))
'''


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source-root", type=Path, required=True)
    parser.add_argument("--sha", required=True)
    args = parser.parse_args()
    if not re.fullmatch(r"[a-f0-9]{40}", args.sha):
        parser.error("a full committed SHA is required")
    client = paramiko.SSHClient()
    phase = "credential_input"
    try:
        credential = json.loads(sys.stdin.read(16384))
        if credential.get("host") != "172.30.232.42" or credential.get("user") != "aqyfzx":
            raise ValueError("unexpected_target")
        actual = subprocess.run(["git", "rev-parse", "HEAD"], cwd=args.source_root,
                                check=True, capture_output=True, text=True).stdout.strip()
        if actual != args.sha:
            raise ValueError("source_head_changed")
        client.load_system_host_keys()
        known = Path.home() / ".ssh" / "known_hosts"
        if known.is_file():
            client.load_host_keys(str(known))
        client.set_missing_host_key_policy(paramiko.RejectPolicy())
        phase = "verified_ssh"
        client.connect(credential["host"], port=22, username=credential["user"], password=credential["password"],
                       timeout=15, auth_timeout=15, banner_timeout=15, allow_agent=False, look_for_keys=False)
        credential.clear()
        phase = "initialize_isolated_build_root"
        encoded_root = base64.b64encode(PREPARE_ROOT.encode()).decode()
        _, root_out, _ = client.exec_command(
            "python3 -c \"import base64;exec(base64.b64decode('" + encoded_root + "'))\"", timeout=45)
        root_report = root_out.read(4096)
        if root_out.channel.recv_exit_status() != 0 or not json.loads(root_report).get("owned_isolated_build_root"):
            raise ValueError("isolated_root_initialization_failed")
        with tempfile.TemporaryDirectory(prefix="feishu-v2-source-") as local:
            phase = "local_git_archive"
            archive = Path(local) / "source.tar"
            subprocess.run(["git", "-c", "core.autocrlf=false", "archive", "--format=tar", "--output", str(archive), args.sha],
                           cwd=args.source_root, check=True, capture_output=True)
            if archive.stat().st_size > 1024 * 1024 * 1024:
                raise ValueError("source_archive_too_large")
            digest = hashlib.sha256()
            with archive.open("rb") as src:
                for chunk in iter(lambda: src.read(1048576), b""):
                    digest.update(chunk)
            import uuid
            root = "/mnt/sata0/knowhub-feishu-v2-builds/" + args.sha[:12] + "-" + uuid.uuid4().hex[:8]
            sftp = client.open_sftp()
            try:
                phase = "create_isolated_build_directory"
                try:
                    sftp.lstat("/mnt/sata0/knowhub-feishu-v2-builds")
                except FileNotFoundError:
                    sftp.mkdir("/mnt/sata0/knowhub-feishu-v2-builds", mode=0o700)
                remote_base = "/mnt/sata0/knowhub-feishu-v2-builds"
                if stat.S_ISLNK(sftp.lstat(remote_base).st_mode) or sftp.normalize(remote_base) != remote_base:
                    raise ValueError("unexpected_build_directory")
                # A fresh unpredictable directory; never overwrite an older build.
                sftp.mkdir(root, mode=0o700)
                remote_archive = root + "/source.tar"
                phase = "upload_source_archive"
                sftp.put(str(archive), remote_archive)
                sftp.chmod(remote_archive, 0o600)
            finally:
                sftp.close()
            prefix = "ARCHIVE=" + repr(remote_archive) + "\nEXPECTED_DIGEST=" + repr(digest.hexdigest())
            prefix += "\nSOURCE_SHA=" + repr(args.sha) + "\n"
            import textwrap
            remote_guard = "try:\n" + textwrap.indent(REMOTE, " ")
            remote_guard += "\nexcept Exception as error:\n print(json.dumps({'remote_error_type':type(error).__name__}))\n raise SystemExit(2)\n"
            code = base64.b64encode((prefix + remote_guard).encode()).decode()
            phase = "verify_and_extract_source"
            _, stdout, _ = client.exec_command(
                "python3 -c \"import base64;exec(base64.b64decode('" + code + "'))\"", timeout=120)
            report = stdout.read(65536)
            if stdout.channel.recv_exit_status() != 0:
                try:
                    remote_failure = json.loads(report)
                    if remote_failure.get("remote_error_type"):
                        print(json.dumps({"status":"BLOCKED", "phase":phase,
                                          "remote_error_type":remote_failure["remote_error_type"],
                                          "build_directory":root}))
                        return 2
                except ValueError:
                    pass
                raise ValueError("source_staging_failed")
            print(json.dumps(json.loads(report), indent=2))
            return 0
    except (OSError, ValueError, KeyError, TypeError, paramiko.SSHException, subprocess.SubprocessError) as error:
        print(json.dumps({"status": "BLOCKED", "reason": "source_or_verified_ssh_unavailable",
                          "phase": phase, "error_type": type(error).__name__, "errno": getattr(error, "errno", None)}))
        return 2
    finally:
        client.close()


if __name__ == "__main__":
    sys.exit(main())
