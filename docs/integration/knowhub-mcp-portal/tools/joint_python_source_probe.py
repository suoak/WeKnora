"""Real legacy Python SSE/HTTP SDK calls to the candidate APP, synthetic only."""
import asyncio
from contextlib import AsyncExitStack
import json, os, socket, subprocess, sys, tempfile, time
from pathlib import Path
from mcp import ClientSession
from mcp.client.sse import sse_client
from mcp.client.streamable_http import streamable_http_client
from mcp.shared._httpx_utils import create_mcp_http_client

root = Path('/source')
kid, expected = sys.argv[1], sys.argv[2] == 'allow'
assert kid and expected in (True,False)
key = os.environ['JOINT_PYTHON_PROBE_KEY']
results = []

async def exercise(url, transport):
    headers = {'Authorization':'Bearer '+key,'X-Tenant-ID':'99','X-From-MCP':'false'}
    async with AsyncExitStack() as stack:
        if transport == 'sse':
            read,write = await stack.enter_async_context(sse_client(url+'/sse',headers=headers))
        else:
            client = await stack.enter_async_context(create_mcp_http_client(headers=headers))
            streams = await stack.enter_async_context(streamable_http_client(url+'/mcp',http_client=client))
            read,write = streams[0],streams[1]
        session = await stack.enter_async_context(ClientSession(read,write))
        await session.initialize()
        for tool in ['get_knowledge','list_chunks']:
            started = time.monotonic()
            result = await asyncio.wait_for(session.call_tool(tool,{'knowledge_id':kid}),15)
            raw = result.model_dump_json()
            if expected:
                assert not result.is_error and kid in raw, 'allowed legacy read did not return the requested document identity'
                if tool == 'list_chunks':
                    assert 'initialcanary' in raw or 'updatedcanary' in raw, 'actual source Chunk text missing'
                else:
                    assert 'Synthetic Wiki' in raw or 'Synthetic Drive' in raw, 'actual source detail/title missing'
            else:
                assert result.is_error or '403' in raw or '404' in raw, 'legacy source read was not denied'
                assert not any(value in raw for value in ['initialcanary','updatedcanary','Synthetic Wiki','Synthetic Drive']), 'source body or title escaped HOLD'
            results.append({'transport':transport,'tool':tool,'expected_allow':expected,'seconds':round(time.monotonic()-started,3),'pass':True})

for transport in ['sse','http']:
    with socket.socket() as sock:
        sock.bind(('127.0.0.1',0)); port = sock.getsockname()[1]
    env = dict(os.environ,MCP_AUTH_MODE='weknora_api_key',WEKNORA_API_KEY='',WEKNORA_BASE_URL='http://app:8080/api/v1',WEKNORA_TENANT_ID='',PYTHONUNBUFFERED='1')
    with tempfile.TemporaryFile() as log:
        process = subprocess.Popen([sys.executable,str(root/'mcp-server/weknora_mcp_server.py'),'--transport',transport,'--host','127.0.0.1','--port',str(port)],env=env,stdout=log,stderr=subprocess.STDOUT)
        try:
            for _ in range(100):
                if process.poll() is not None: raise RuntimeError('legacy candidate process terminated')
                try:
                    with socket.create_connection(('127.0.0.1',port),timeout=.2): break
                except OSError: time.sleep(.1)
            else: raise RuntimeError('legacy transport readiness timed out')
            asyncio.run(asyncio.wait_for(exercise('http://127.0.0.1:'+str(port),transport),90))
        finally:
            process.terminate()
            try: process.wait(timeout=10)
            except subprocess.TimeoutExpired: process.kill(); process.wait(timeout=10)
print(json.dumps({'exit_code':0,'real_legacy_sdk':True,'real_candidate_APP':True,'synthetic_data':True,'calls':results}))
