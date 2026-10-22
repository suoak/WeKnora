"""Pinned build-host access; never reads production credentials or targets."""
from pathlib import Path
import importlib.util
import logging
import paramiko


def connect():
    spec = importlib.util.spec_from_file_location('crypto', 'E:/AIData/Codex/sourceid-ssh.py')
    crypto = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(crypto)
    logging.getLogger('paramiko').disabled = True
    client = paramiko.SSHClient()
    client.load_host_keys(str(Path.home() / '.ssh/known_hosts'))
    client.set_missing_host_key_policy(paramiko.RejectPolicy())
    password = crypto.crypt(Path('E:/AIData/Codex/secure/sourceid-test-host.ssh.dpapi').read_bytes(), False).decode()
    client.connect('172.30.232.42', username='aqyfzx', password=password,
                   allow_agent=False, look_for_keys=False, timeout=15)
    return client
