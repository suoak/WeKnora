"""Deterministic isolated embedding fixture. No external API or production data.

This validates ingestion/index plumbing, not semantic-model quality.
"""
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import hashlib
import json
import math
import re

class Handler(BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def reply(self, value, status=200):
        raw=json.dumps(value).encode()
        self.send_response(status)
        self.send_header('Content-Type', 'application/json')
        self.send_header('Content-Length', str(len(raw)))
        self.end_headers()
        self.wfile.write(raw)

    def do_GET(self):
        self.reply({'object':'list','data':[{'id':'office-test-embedding','object':'model','owned_by':'isolated-fixture'}]})

    def do_POST(self):
        size=int(self.headers.get('Content-Length','0'))
        if size>2*1024*1024:
            return self.reply({'error':'fixture input bound'},413)
        body=json.loads(self.rfile.read(size))
        if self.path.rstrip('/').endswith('chat/completions'):
            # Controlled model response for required KB configuration and
            # optional ingestion summaries. No quality/production-LLM claim.
            return self.reply({'id':'office-fixture','object':'chat.completion','model':'office-test-chat',
                'choices':[{'index':0,'message':{'role':'assistant','content':'Office isolated test document.'},'finish_reason':'stop'}],
                'usage':{'prompt_tokens':1,'completion_tokens':1,'total_tokens':2}})
        if self.path.rstrip('/').endswith('embeddings'):
            inputs=body.get('input',[])
            if isinstance(inputs,str):inputs=[inputs]
            vectors=[]
            for i,text in enumerate(inputs):
                v=[0.0]*1024
                for word in re.findall(r'\w+',str(text).lower()):
                    digest=hashlib.sha256(word.encode()).digest()
                    v[int.from_bytes(digest[:4],'big')%1024]+=1.0
                norm=math.sqrt(sum(x*x for x in v)) or 1
                vectors.append({'object':'embedding','index':i,'embedding':[x/norm for x in v]})
            return self.reply({'object':'list','model':'office-test-embedding','data':vectors,'usage':{'prompt_tokens':1,'total_tokens':1}})
        return self.reply({'error':'unsupported fixture endpoint'},404)

if __name__=='__main__':
    ThreadingHTTPServer(('0.0.0.0',9000),Handler).serve_forever()
