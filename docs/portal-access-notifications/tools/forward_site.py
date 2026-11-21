"""Open a bounded local SSH preview for this isolated site's real account tests.

The runtime file contains generated test passwords and stays outside Git.
No request bodies, authorization headers or credentials are logged.
"""
import argparse
import json
import os
from pathlib import Path
import select
import socketserver
import time

from isolated_site import remote
from remote_readonly import connect


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--manifest', type=Path, required=True)
    parser.add_argument('--runtime', type=Path, required=True)
    args = parser.parse_args()
    if args.runtime.exists():
        raise ValueError('runtime_evidence_already_exists')
    manifest = json.loads(args.manifest.read_text(encoding='utf-8'))
    client = connect('test')
    proof = remote(client, manifest, "proof=json.loads((root/'manifest.json').read_text()); print(json.dumps(proof))")
    if proof.get('status') != 'DEPLOYED' or proof['source_sha'] != manifest['source_sha']:
        raise ValueError('isolated_deployment_not_verified')
    port = proof['site']['frontend_port']
    with client.open_sftp() as transfer:
        with transfer.open(proof['remote_root'] + '/site/accounts.json', 'r') as incoming:
            accounts = json.loads(incoming.read())
    transport = client.get_transport()

    class Handler(socketserver.BaseRequestHandler):
        def handle(self):
            channel = transport.open_channel('direct-tcpip', ('127.0.0.1', port), self.client_address)
            try:
                while True:
                    ready, _, _ = select.select([self.request, channel], [], [], 30)
                    if self.request in ready:
                        data = self.request.recv(65536)
                        if not data:
                            break
                        channel.sendall(data)
                    if channel in ready:
                        data = channel.recv(65536)
                        if not data:
                            break
                        self.request.sendall(data)
            finally:
                channel.close()

    class Server(socketserver.ThreadingTCPServer):
        daemon_threads = True

    server = Server(('127.0.0.1', 0), Handler)
    server.timeout = 1
    url = 'http://127.0.0.1:' + str(server.server_address[1])
    runtime = {'url': url, 'source_sha': proof['source_sha'], 'fixture': proof['fixture'],
               'accounts': accounts, 'site': proof['site']}
    args.runtime.parent.mkdir(parents=True, exist_ok=True)
    fd = os.open(args.runtime, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, 'w', encoding='utf-8') as output:
        json.dump(runtime, output, ensure_ascii=False, indent=2)
    print(json.dumps({'url': url, 'source_sha': proof['source_sha'], 'private_runtime_file': str(args.runtime),
                      'lifetime_seconds': 3600}), flush=True)
    deadline = time.monotonic() + 3600
    try:
        while time.monotonic() < deadline and not Path(str(args.runtime) + '.stop').exists():
            server.handle_request()
    finally:
        server.server_close()
        client.close()


if __name__ == '__main__':
    main()
