"""Denied POSTs never dispatch; error relay is bound to a live server channel."""
import asyncio
import hashlib
import json
import unittest

from weknora_mcp_server import MCPAuthMiddleware


class SSEAuthRevocationTest(unittest.IsolatedAsyncioTestCase):
    async def test_error_is_bound_to_principal_workspace_and_session(self):
        dispatched = []
        frames = []
        async def app(scope, receive, send):
            dispatched.append(scope)
        async def invalid(*args):
            return False
        middleware = MCPAuthMiddleware(app, "", "weknora_api_key", invalid)
        principal = hashlib.sha256(b"synthetic:test-workspace").hexdigest()
        async def channel(message):
            frames.append(message)
        middleware._sse_auth_channels[(principal, "server-session")] = channel

        async def request(key="synthetic", tenant="test-workspace", session="server-session", body=None):
            sent = []
            async def receive():
                return {"type": "http.request", "body": json.dumps(body or {
                    "jsonrpc": "2.0", "id": 17, "method": "tools/call"}).encode()}
            async def send(message):
                sent.append(message)
            await middleware({"type": "http", "method": "POST", "path": "/sse/messages/",
                "query_string": ("session_id="+session).encode(), "headers": [
                    (b"authorization", ("Bearer "+key).encode()), (b"x-tenant-id", tenant.encode())]}, receive, send)
            self.assertEqual(sent[0]["status"], 401)
            self.assertEqual(dispatched, [])
        await request(key="other")
        await request(tenant="other")
        await request(session="other")
        await request(body={"jsonrpc": "2.0", "method": "notifications/initialized"})
        await request(body={"jsonrpc": "2.0", "id": True})
        self.assertEqual(frames, [])
        await request()
        self.assertEqual(len(frames), 1)
        wire = frames[0]["body"].decode()
        error = json.loads(wire.split("data: ")[1].strip())
        self.assertEqual(error["id"], 17)
        self.assertEqual(error["error"]["code"], -32001)
        self.assertNotIn("synthetic", wire)

    async def test_live_channel_removed_on_disconnect(self):
        ready = asyncio.Event()
        stop = asyncio.Event()
        async def app(scope, receive, send):
            await send({"type": "http.response.start", "status": 200, "headers": []})
            await send({"type": "http.response.body", "body": b"event: endpoint\r\ndata: /sse/messages/?session_id=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\r\n\r\n", "more_body": True})
            ready.set()
            await stop.wait()
        async def valid(*args):return True
        async def receive():return {"type": "http.request", "body": b""}
        async def send(message):pass
        middleware = MCPAuthMiddleware(app, "", "weknora_api_key", valid)
        task = asyncio.create_task(middleware({"type": "http", "method": "GET", "path": "/sse",
            "headers": [(b"authorization", b"Bearer synthetic")]}, receive, send))
        await asyncio.wait_for(ready.wait(), 2)
        self.assertEqual(len(middleware._sse_auth_channels), 1)
        stop.set()
        await task
        self.assertEqual(middleware._sse_auth_channels, {})
