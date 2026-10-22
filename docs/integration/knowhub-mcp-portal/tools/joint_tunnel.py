"""Temporary loopback tunnel to ONLY the newly deployed V1 test frontend."""
from pathlib import Path
import json,select,socketserver,threading,time
from v1_host import connect

root=Path(__file__).resolve().parents[4]
cfg=json.loads((root/'docs/integration/knowhub-mcp-portal/evidence/feishu-joint/current.json').read_text())
private=Path('E:/AIData/Codex/knowhub-v1-feishu-private')/cfg['candidate_sha256'][:16];private.mkdir(parents=True,exist_ok=True)
with connect()as ssh:
    with ssh.open_sftp()as ftp:
        with ftp.open(cfg['remote']+'/results/deploy/instance.json')as f:instance=json.loads(f.read())
        with ftp.open(cfg['remote']+'/private/client.json')as f:client=json.loads(f.read())
    port=instance['port']
    class Forward(socketserver.BaseRequestHandler):
        def handle(self):
            channel=ssh.get_transport().open_channel('direct-tcpip',('127.0.0.1',port),self.request.getpeername())
            try:
                while True:
                    readable,_,_=select.select([self.request,channel],[],[],30)
                    if not readable:return
                    for source in readable:
                        data=source.recv(65536)
                        if not data:return
                        (channel if source is self.request else self.request).sendall(data)
            finally:channel.close()
    class Server(socketserver.ThreadingTCPServer):daemon_threads=True;allow_reuse_address=False
    with Server(('127.0.0.1',port),Forward)as server:
        client['url']='http://127.0.0.1:'+str(port);client['remote']=cfg['remote'];client['private_directory']=str(private)
        (private/'client.json').write_text(json.dumps(client),encoding='utf8')
        threading.Thread(target=server.serve_forever,daemon=True).start()
        print(json.dumps({'tunnel':'READY','test_only':True,'url':client['url'],'private_directory':str(private)}),flush=True)
        deadline=time.monotonic()+3600
        while time.monotonic()<deadline and not (private/'stop-tunnel').exists():time.sleep(1)
        server.shutdown()
