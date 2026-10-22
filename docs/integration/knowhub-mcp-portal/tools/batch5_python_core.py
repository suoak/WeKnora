"""Candidate legacy Python MCP -> actual existing Knowledge/Chunk REST stack.

Transient keys are received in memory/environment and never printed. This is
single-worker transport certification, not retrieval or multi-worker approval.
"""
import asyncio
from contextlib import AsyncExitStack
import importlib.metadata
import json
import os
from pathlib import Path
import socket
import subprocess
import sys
import tempfile
import time
import urllib.request

BACKEND=os.environ['KNOWHUB_M5_BACKEND'];NONCE=os.environ['KNOWHUB_M5_CONTROL']
KEYS=json.loads(os.environ['KNOWHUB_M5_KEYS'])
os.environ['KNOWHUB_M3_BACKEND']=BACKEND;os.environ['KNOWHUB_M3_CONTROL']=NONCE
# Reuse only the already-tested redacted wire tracer, not the earlier adapter.
from batch4_python_http import trace_factory, trace
from mcp import ClientSession
from mcp.client.sse import sse_client
from mcp.client.streamable_http import streamable_http_client
from mcp.shared.exceptions import MCPError

ROOT=Path(__file__).resolve().parents[4]
PUBLIC='PUBLIC_M5_APPROVED';CANARY='SECRET_M5_SENSITIVE_CANARY'

def control(action):
    req=urllib.request.Request(BACKEND+'/__test/control',data=json.dumps({'action':action}).encode(),headers={'Content-Type':'application/json','X-Test-Control':NONCE},method='POST')
    with urllib.request.urlopen(req,timeout=5) as response:return json.loads(response.read())

async def connect(stack,url,transport,key):
    headers={'Authorization':'Bearer '+KEYS[str(key)],'X-Tenant-ID':'1' if key in (1,3) else '99','X-From-MCP':'false','User-Agent':'synthetic-web'}
    if transport=='sse':
        read,write=await stack.enter_async_context(sse_client(url+'/sse',headers=headers,httpx_client_factory=trace_factory))
    else:
        client=await stack.enter_async_context(trace_factory(headers=headers))
        read,write=await stack.enter_async_context(streamable_http_client(url+'/mcp',http_client=client))
    session=await stack.enter_async_context(ClientSession(read,write));await session.initialize();return session

async def call(session,tool,args,expected=None):
    result=await asyncio.wait_for(session.call_tool(tool,args),10)
    body=result.model_dump_json()
    if expected:
        assert not result.is_error,body
        assert expected in body,body
    else:
        assert result.is_error or '403' in body,body
        assert CANARY not in body,body
    trace(event='actual_rest_tool',tool=tool,args=args,allowed=bool(expected),status='PASS')

async def exercise(url,transport):
    control('reset')
    async with AsyncExitStack() as stack:
        a=await connect(stack,url,transport,1)
        for tool in ('get_knowledge','list_chunks'):
            await call(a,tool,{'knowledge_id':'public-doc'},PUBLIC)
            for doc in ('sensitive','denied-doc','unknown-doc','blocked'):await call(a,tool,{'knowledge_id':doc})
        await call(a,'wiki_read_page',{'kb_id':'public-kb','slug':'historical'})
        await call(a,'hybrid_search',{'kb_id':'public-kb','query':CANARY})
        for key in (2,3,7,9):
            denied=await connect(stack,url,transport,key)
            await call(denied,'get_knowledge',{'knowledge_id':'sensitive' if key!=3 else 'public-doc'})
        d=await connect(stack,url,transport,4)
        await call(d,'get_knowledge',{'knowledge_id':'sensitive'},CANARY)
        await call(d,'list_chunks',{'knowledge_id':'sensitive'},CANARY)
        await call(d,'get_knowledge',{'knowledge_id':'blocked'})
        control('revoke-grant')
        await call(d,'list_chunks',{'knowledge_id':'sensitive'})
    control('reset')
    async with AsyncExitStack() as stack:
        d=await connect(stack,url,transport,4)
        await call(d,'get_knowledge',{'knowledge_id':'sensitive'},CANARY)
        control('revoke-key');before=control('status');started=time.monotonic()
        try:await asyncio.wait_for(d.call_tool('get_knowledge',{'knowledge_id':'sensitive'}),5)
        except MCPError as error:
            elapsed=time.monotonic()-started
            assert elapsed<3,elapsed
            assert control('status')['knowledge_calls']==before['knowledge_calls'],'REST dispatched after Key revoked'
            trace(event='sdk_auth_failure',transport=transport,exception=type(error).__name__,code=error.code,elapsed_seconds=elapsed,knowledge_rest_delta=0,status='PASS')
        else:raise AssertionError('expected bounded authentication failure')
    trace(event='client_cleanup',transport=transport,status='PASS')
    async with AsyncExitStack() as stack:
        fresh=await connect(stack,url,transport,1)
        await call(fresh,'get_knowledge',{'knowledge_id':'public-doc'},PUBLIC)
    trace(event='actual_rest_transport',transport=transport,status='PASS',workers=1)

def main():
    trace(event='environment',python=sys.version,mcp=importlib.metadata.version('mcp'))
    for transport in ('http','sse'):
        with socket.socket() as listener:listener.bind(('127.0.0.1',0));port=listener.getsockname()[1]
        env=dict(os.environ,MCP_AUTH_MODE='weknora_api_key',WEKNORA_API_KEY='',WEKNORA_BASE_URL=BACKEND+'/api/v1',WEKNORA_TENANT_ID='',PYTHONUNBUFFERED='1')
        with tempfile.TemporaryFile() as log:
            process=subprocess.Popen([sys.executable,str(ROOT/'mcp-server/weknora_mcp_server.py'),'--transport',transport,'--host','127.0.0.1','--port',str(port)],env=env,stdout=log,stderr=subprocess.STDOUT)
            try:
                for _ in range(100):
                    if process.poll() is not None:raise RuntimeError('candidate MCP terminated')
                    try:
                        with socket.create_connection(('127.0.0.1',port),timeout=.2):break
                    except OSError:time.sleep(.1)
                else:raise RuntimeError('MCP readiness timed out')
                asyncio.run(asyncio.wait_for(exercise('http://127.0.0.1:'+str(port),transport),150))
            except BaseException:
                log.seek(0);sys.stderr.write(log.read().decode(errors='replace'));raise
            finally:
                process.terminate()
                try:process.wait(timeout=10)
                except subprocess.TimeoutExpired:process.kill();process.wait(timeout=10)

if __name__=='__main__':main()
