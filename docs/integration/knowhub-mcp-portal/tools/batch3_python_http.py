"""Real unchanged Python MCP -> isolated Go ACL fixture, HTTP and legacy SSE.

The backend is the tagged Go integration fixture, NOT the full application.
"""
import asyncio
from contextlib import AsyncExitStack
import json
import os
from pathlib import Path
import socket
import subprocess
import sys
import tempfile
import time
import urllib.request

from mcp import ClientSession
from mcp.client.sse import sse_client
from mcp.client.streamable_http import streamable_http_client
from mcp.shared._httpx_utils import create_mcp_http_client
from mcp.shared.exceptions import MCPError

BACKEND=os.environ['KNOWHUB_M3_BACKEND']
NONCE=os.environ['KNOWHUB_M3_CONTROL']
ROOT=Path(__file__).resolve().parents[4]

def control(action):
    request=urllib.request.Request(BACKEND+'/__test/control',data=json.dumps({'action':action}).encode(),headers={'Content-Type':'application/json','X-Test-Control':NONCE},method='POST')
    with urllib.request.urlopen(request,timeout=10) as response:
        assert response.status==200
        return json.loads(response.read())

async def connect(stack,url,transport,key):
    headers={'Authorization':'Bearer '+key,'X-From-MCP':'false','User-Agent':'synthetic-web-browser'}
    if transport=='sse':
        read,write=await stack.enter_async_context(sse_client(url+'/sse',headers=headers))
    else:
        client=await stack.enter_async_context(create_mcp_http_client(headers=headers))
        read,write=await stack.enter_async_context(streamable_http_client(url+'/mcp',http_client=client))
    session=await stack.enter_async_context(ClientSession(read,write))
    await session.initialize()
    return session

async def call(session,name,args,allowed=False,canary=None):
    result=await session.call_tool(name,args)
    text=result.model_dump_json()
    if allowed:
        assert not result.is_error,text
        assert canary in text,text
    else:
        assert result.is_error or '403' in text,text
        assert 'SECRET_M3_CANARY' not in text,text
    print(json.dumps({'tool':name,'args':args,'allowed':allowed,'status':'PASS'}),flush=True)
    return text

async def exercise(url,transport):
    control('reset')
    async with AsyncExitStack() as stack:
        a=await connect(stack,url,transport,'m3-A')
        await call(a,'get_knowledge',{'knowledge_id':'doc'},True,'ERR_M2_42')
        await call(a,'list_chunks',{'knowledge_id':'doc'},True,'ERR_M2_42')
        await call(a,'get_knowledge',{'knowledge_id':'sensitive'})
        await call(a,'list_chunks',{'knowledge_id':'sensitive'})
        await call(a,'get_knowledge',{'knowledge_id':'blocked'})
        await call(a,'get_knowledge',{'knowledge_id':'not-approved'})
        await call(a,'wiki_read_page',{'kb_id':'public-kb','slug':'historical'})
        await call(a,'hybrid_search',{'kb_id':'public-kb','query':'ERR_M2_42'},True,'ERR_M2_42')
        b=await connect(stack,url,transport,'m3-B')
        await call(b,'get_knowledge',{'knowledge_id':'sensitive'},True,'SECRET_M3_CANARY')
        control('revoke-grant')
        await call(b,'get_knowledge',{'knowledge_id':'sensitive'})
        old=await connect(stack,url,transport,'m3-old')
        await call(old,'get_knowledge',{'knowledge_id':'doc'})
        full=await connect(stack,url,transport,'m3-full')
        await call(full,'get_knowledge',{'knowledge_id':'blocked'})
    print(json.dumps({'transport':transport,'group':'ordinary-and-document-ACL','status':'PASS','fixture':'synthetic Go ACL boundary','production':False}),flush=True)

async def probe_key_revocation(url,transport):
    control('reset')
    async with AsyncExitStack() as stack:
        b=await connect(stack,url,transport,'m3-B')
        await call(b,'get_knowledge',{'knowledge_id':'doc'},True,'ERR_M2_42')
        control('revoke-key')
        denied_before=control('status')['denied_b']
        # Existing SSE connections can outlive key admission. The backend
        # still rejects the next tool REST call using live credential facts.
        try:
            result=await asyncio.wait_for(b.call_tool('get_knowledge',{'knowledge_id':'doc'}),10)
            text=result.model_dump_json()
            assert result.is_error or '401' in text or '403' in text,text
            assert 'ERR_M2_42' not in text,text
        except MCPError:
            # SDK 2.2 hides HTTP 401 as -32603. Do not treat a generic error
            # as proof: require the Go backend's observer to have recorded a
            # NEW rejected B auth/me or knowledge/doc request after revocation.
            assert control('status')['denied_b']>denied_before
        except asyncio.TimeoutError:
            rejected=control('status')['denied_b']>denied_before
            print(json.dumps({'transport':transport,'group':'key-revocation','status':'FAIL','backend_denied':rejected,'reason':'SDK still waiting after 10 seconds; timeout is not a denial PASS'}),flush=True)
            raise AssertionError('Key revocation did not complete the MCP call within 10 seconds')
        assert control('status')['denied_b']>denied_before
    print(json.dumps({'transport':transport,'group':'key-revocation','status':'PASS','production':False}),flush=True)

def main():
    for transport in ('http','sse'):
        with socket.socket() as listener:
            listener.bind(('127.0.0.1',0));port=listener.getsockname()[1]
        env=dict(os.environ,MCP_AUTH_MODE='weknora_api_key',WEKNORA_API_KEY='',WEKNORA_BASE_URL=BACKEND+'/api/v1',WEKNORA_TENANT_ID='',PYTHONUNBUFFERED='1')
        with tempfile.TemporaryFile() as log:
            process=subprocess.Popen([sys.executable,str(ROOT/'mcp-server/weknora_mcp_server.py'),'--transport',transport,'--host','127.0.0.1','--port',str(port)],env=env,stdout=log,stderr=subprocess.STDOUT)
            try:
                for _ in range(100):
                    if process.poll() is not None:raise RuntimeError('MCP process terminated')
                    try:
                        with socket.create_connection(('127.0.0.1',port),timeout=.2):break
                    except OSError:time.sleep(.1)
                else:raise RuntimeError('MCP readiness timed out')
                asyncio.run(asyncio.wait_for(exercise('http://127.0.0.1:'+str(port),transport),120))
                asyncio.run(asyncio.wait_for(probe_key_revocation('http://127.0.0.1:'+str(port),transport),30))
            except BaseException:
                log.seek(0);sys.stderr.write(log.read().decode(errors='replace'));raise
            finally:
                process.terminate()
                try:process.wait(timeout=10)
                except subprocess.TimeoutExpired:process.kill();process.wait(timeout=10)

if __name__=='__main__':main()
