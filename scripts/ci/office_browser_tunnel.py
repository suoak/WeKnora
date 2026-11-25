"""Keep a localhost-only SSH forward alive while exercising the real preview SPA."""
import json,select,socketserver,subprocess,threading
from office_test_remote import ROOT,REMOTE_ROOT,connect
PRIVATE=ROOT/'docs/qa/office-test-deployment/private'
with connect() as client:
    with client.open_sftp() as sftp:
        with sftp.open(REMOTE_ROOT+'/site/deployment-proof.json') as f:proof=json.load(f)
        candidate=proof['source_sha']
        assert candidate in ('ca1c058c0887b9b2c00899bb6c7da6201761415a','668d067ec75a2a351ace4f7f0797be77af2b24ae')
        sftp.get(REMOTE_ROOT+'/site/browser-session-'+candidate[:9]+'.json',str(PRIVATE/'browser-session.json'))
    class Forward(socketserver.BaseRequestHandler):
        def handle(self):
            remote=client.get_transport().open_channel('direct-tcpip',('127.0.0.1',39781),self.request.getpeername())
            if remote is None:return
            try:
                while True:
                    readable,_,_=select.select([self.request,remote],[],[],30)
                    if not readable:continue
                    for source in readable:
                        data=source.recv(65536)
                        if not data:return
                        (remote if source is self.request else self.request).sendall(data)
            finally:remote.close()
    class Server(socketserver.ThreadingTCPServer):
        allow_reuse_address=True
        daemon_threads=True
    with Server(('127.0.0.1',49681),Forward) as server:
        thread=threading.Thread(target=server.serve_forever,daemon=True);thread.start()
        try:
            raise SystemExit(subprocess.run(['node',str(ROOT/'scripts/ci/office_browser_acceptance.cjs')],cwd=ROOT).returncode)
        finally:server.shutdown()
