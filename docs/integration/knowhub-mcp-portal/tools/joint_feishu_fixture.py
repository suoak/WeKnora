"""Synthetic Feishu protocol fixture, private candidate network only.

No production credentials, document IDs or outbound network calls. Control
endpoints are deliberately unauthenticated and must never be exposed publicly.
"""
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import threading
import time

lock = threading.Lock()
state = {kind: dict(revision=1792555200, text=kind+' initialcanary ordinary text',
                    missing=False, status=200, block_status=200, delay=0, blocks=0,
                    calls=0) for kind in ['wiki', 'drive']}

class Handler(BaseHTTPRequestHandler):
    def respond(self, data, status=200):
        body = json.dumps(data).encode()
        self.send_response(status)
        self.send_header('Content-Type', 'application/json')
        self.send_header('Content-Length', str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def fault(self,status,message):
        # Keep HTTP status and Feishu error category consistent. A 5xx fixture
        # must not simultaneously claim the known permission-denied code.
        code={401:99991663,403:99991672,404:1663,429:99991400}.get(status,10000)
        self.respond({'code':code,'msg':message},status)

    def do_POST(self):
        size = int(self.headers.get('Content-Length', '0'))
        if size > 1024*1024:
            self.send_error(413); return
        data = json.loads(self.rfile.read(size) or b'{}')
        if self.path.startswith('/control/'):
            kind = self.path.rsplit('/', 1)[1]
            if kind not in state:
                self.send_error(404); return
            assert set(data) <= {'revision', 'text', 'missing', 'status', 'block_status', 'delay'}
            with lock:
                state[kind].update(data)
            self.respond({'synthetic': True}); return
        if self.path.startswith('/open-apis/drive/v1/export_tasks'):
            token=data.get('token','');kind='wiki' if 'wiki' in token else 'drive'
            with lock:current=dict(state[kind])
            if current['block_status']!=200:
                self.fault(current['block_status'],'synthetic same document export fault');return
            self.respond({'code':10000,'msg':'synthetic export success is outside this blocks fixture'},501);return
        if 'tenant_access_token' in self.path:
            self.respond({'code': 0, 'tenant_access_token': 'synthetic-only', 'expire': 7200}); return
        self.send_error(404)

    def do_GET(self):
        if self.path == '/state':
            with lock:
                snapshot = json.loads(json.dumps(state))
            self.respond(snapshot); return
        kind = 'wiki' if '/wiki/' in self.path or 'wiki-document' in self.path or 'wiki-object' in self.path else 'drive'
        with lock:
            state[kind]['calls'] += 1
            current = dict(state[kind])
        if '/nodes' in self.path or self.path.startswith('/open-apis/drive/v1/files'):
            if current['status'] != 200:
                self.fault(current['status'],'synthetic source fault'); return
            if kind == 'wiki':
                rows = [] if current['missing'] else [{'node_token': 'wiki-document', 'obj_token': 'wiki-object', 'obj_type': 'docx', 'title': 'Synthetic Wiki', 'obj_edit_time': str(current['revision'])}]
                self.respond({'code': 0, 'data': {'items': rows, 'has_more': False}})
            else:
                rows = [] if current['missing'] else [{'token': 'drive-document', 'type': 'docx', 'name': 'Synthetic Drive', 'modified_time': str(current['revision']), 'url': 'http://feishu:8000/docx/drive-document'}]
                self.respond({'code': 0, 'data': {'files': rows, 'has_more': False}})
            return
        if '/blocks' in self.path:
            with lock:
                state[kind]['blocks'] += 1
            time.sleep(min(10, max(0, current['delay'])))
            if current['block_status'] != 200:
                self.fault(current['block_status'],'synthetic parse/export fault'); return
            self.respond({'code': 0, 'data': {'items': [
                {'block_id': 'page', 'block_type': 1, 'children': ['text']},
                {'block_id': 'text', 'parent_id': 'page', 'block_type': 2, 'text': {'elements': [{'text_run': {'content': current['text']}}]}}
            ], 'has_more': False}}); return
        self.send_error(404)

    def log_message(self, fmt, *args):
        print('SYNTHETIC_FEISHU', self.command, self.path.split('?')[0], flush=True)

ThreadingHTTPServer(('0.0.0.0', 8000), Handler).serve_forever()
