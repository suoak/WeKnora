"""Use the existing pinned, CurrentUser-DPAPI ordinary-user connection.

No credentials are accepted on the command line or copied into evidence.
Remote scripts must implement their own scope checks before mutations.
"""
import argparse
import importlib.util
import shlex
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
REMOTE_ROOT = '/mnt/sata0/knowhub-feishu-v2-builds/office-guard-4ffd7442d-20261125'

def connect():
    spec = importlib.util.spec_from_file_location('known_test_host', ROOT/'docs/integration/knowhub-mcp-portal/tools/v1_host.py')
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module.connect()

def execute(client, code, timeout=600):
    _, out, err = client.exec_command('python3 -u -c '+shlex.quote(code), timeout=timeout)
    # Drain both streams incrementally: image imports must not fill SSH buffers.
    channel = out.channel
    import time
    deadline = time.monotonic()+timeout
    while not channel.exit_status_ready() or channel.recv_ready() or channel.recv_stderr_ready():
        if channel.recv_ready():
            print(channel.recv(65536).decode('utf-8', 'replace'), end='', flush=True)
        if channel.recv_stderr_ready():
            print(channel.recv_stderr(65536).decode('utf-8', 'replace'), end='', flush=True)
        if time.monotonic()>deadline:
            channel.close()
            raise TimeoutError('remote operation timed out; inspect state before retry')
        time.sleep(.05)
    return channel.recv_exit_status()

if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('script', type=Path)
    args = parser.parse_args()
    with connect() as client:
        raise SystemExit(execute(client, args.script.read_text(encoding='utf-8')))
