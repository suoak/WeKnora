"""Explicitly synthetic model; never represents formal Embedding acceptance."""
from http.server import BaseHTTPRequestHandler,ThreadingHTTPServer
import json

class Handler(BaseHTTPRequestHandler):
    def do_POST(self):
        size=int(self.headers.get('Content-Length','0'))
        if size>1024*1024:self.send_error(413);return
        raw=self.rfile.read(size)
        if b'V1_URL_SECRET' in raw or b'SECRET_M5_SENSITIVE_CANARY' in raw:
            self.send_error(403);return
        if self.path!='/v1/embeddings':self.send_error(404);return
        request=json.loads(raw);texts=request.get('input',[])
        if not isinstance(texts,list):texts=[texts]
        vector=[1.0]+[0.0]*1023
        body=json.dumps({'object':'list','data':[{'index':i,'embedding':vector}for i in range(len(texts))]}).encode()
        self.send_response(200);self.send_header('Content-Type','application/json');self.send_header('Content-Length',str(len(body)));self.end_headers();self.wfile.write(body)
    def log_message(self,format,*args): print('SYNTHETIC_MODEL',self.command,self.path,flush=True)

ThreadingHTTPServer(('0.0.0.0',8000),Handler).serve_forever()
