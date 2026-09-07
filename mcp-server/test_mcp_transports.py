#!/usr/bin/env python3
"""Regression tests for MCP 2.x transport compatibility."""

import asyncio
import os
import subprocess
import sys
import threading
import unittest
from unittest import mock
from pathlib import Path

MCP_SERVER_DIR = Path(__file__).resolve().parent


class TransportRegressionTest(unittest.TestCase):
    def test_http_transport_is_stateless(self):
        import weknora_mcp_server as srv
        from mcp.server import MCPServer

        probe = MCPServer("probe")
        probe.streamable_http_app(
            host="127.0.0.1", stateless_http=srv.STREAMABLE_HTTP_STATELESS
        )
        self.assertTrue(probe.session_manager.stateless)

    def test_sse_message_path_matches_legacy_mount(self):
        import weknora_mcp_server as srv
        from mcp.server import MCPServer
        from starlette.routing import Mount, Route

        probe = MCPServer("probe")
        app = probe.sse_app(host="127.0.0.1", message_path=srv.SSE_MESSAGE_PATH)
        mount_paths = [
            route.path for route in app.routes if isinstance(route, (Mount, Route))
        ]
        self.assertIn("/sse", mount_paths)
        self.assertIn(
            srv.SSE_MESSAGE_PATH.rstrip("/"),
            {path.rstrip("/") for path in mount_paths},
        )

    def test_weknora_client_session_is_thread_local(self):
        from weknora_mcp_server import WeKnoraClient

        client = WeKnoraClient("http://localhost:8080/api/v1", "test-key")
        barrier = threading.Barrier(2)
        sessions: dict[str, object] = {}

        def worker(name: str) -> None:
            barrier.wait()
            sessions[name] = client.session

        threads = [threading.Thread(target=worker, args=(name,)) for name in ("a", "b")]
        for thread in threads:
            thread.start()
        for thread in threads:
            thread.join()

        self.assertEqual(len(sessions), 2)
        self.assertIsNot(sessions["a"], sessions["b"])

    def test_client_auth_header_uses_request_scoped_key(self):
        import weknora_mcp_server as srv

        client = srv.WeKnoraClient("http://localhost:8080/api/v1", "static-key")
        self.assertEqual(client._auth_headers()["X-API-Key"], "static-key")

        marker = srv._request_api_key.set("person-a-key")
        try:
            self.assertEqual(client._auth_headers()["X-API-Key"], "person-a-key")
        finally:
            srv._request_api_key.reset(marker)

        self.assertEqual(client._auth_headers()["X-API-Key"], "static-key")

    def test_passthrough_mode_does_not_require_shared_secret(self):
        import weknora_mcp_server as srv

        with mock.patch.dict(
            os.environ, {"MCP_AUTH_MODE": "weknora_api_key"}, clear=False
        ):
            self.assertEqual(srv.require_network_transport_auth("http"), "")

    def test_passthrough_key_validation_uses_auth_me(self):
        import weknora_mcp_server as srv

        response = mock.Mock(ok=True)
        with mock.patch.object(srv.requests, "get", return_value=response) as get:
            self.assertTrue(asyncio.run(srv.validate_weknora_api_key("person-a-key")))

        get.assert_called_once()
        self.assertEqual(get.call_args.args[0], f"{srv.WEKNORA_BASE_URL}/auth/me")
        self.assertEqual(get.call_args.kwargs["headers"], {"X-API-Key": "person-a-key"})

    def test_passthrough_key_validation_forwards_workspace(self):
        import weknora_mcp_server as srv

        response = mock.Mock(ok=True, status_code=200)
        with mock.patch.object(srv.requests, "get", return_value=response) as get:
            self.assertTrue(asyncio.run(srv.validate_weknora_api_key("person-a-key", "42")))

        self.assertEqual(
            get.call_args.kwargs["headers"],
            {"X-API-Key": "person-a-key", "X-Tenant-ID": "42"},
        )

    def test_passthrough_validation_preserves_tenant_required(self):
        import weknora_mcp_server as srv

        response = mock.Mock(ok=False, status_code=409)
        with mock.patch.object(srv.requests, "get", return_value=response):
            with self.assertRaises(srv.WorkspaceRequiredError):
                asyncio.run(srv.validate_weknora_api_key("person-a-key"))


class MCPAuthMiddlewareTest(unittest.TestCase):
    @staticmethod
    async def _call(headers: list[tuple[bytes, bytes]]):
        import weknora_mcp_server as srv

        observed: list[tuple[str, str, str]] = []
        sent: list[dict] = []

        async def app(scope, receive, send):
            observed.append(
                (
                    srv._request_api_key.get(),
                    scope["user"].access_token.client_id,
                    srv._request_tenant_id.get(),
                )
            )
            await send({"type": "http.response.start", "status": 204, "headers": []})
            await send({"type": "http.response.body", "body": b""})

        async def validate(api_key: str) -> bool:
            return api_key == "person-a-key"

        middleware = srv.MCPAuthMiddleware(
            app,
            token="",
            auth_mode="weknora_api_key",
            api_key_validator=validate,
        )
        scope = {"type": "http", "headers": headers}

        async def receive():
            return {"type": "http.request", "body": b"", "more_body": False}

        async def send(message):
            sent.append(message)

        await middleware(scope, receive, send)
        return observed, sent

    def test_passthrough_mode_isolates_bearer_key_in_request_context(self):
        observed, sent = asyncio.run(
            self._call([(b"authorization", b"Bearer person-a-key")])
        )
        self.assertEqual(observed[0][0], "person-a-key")
        self.assertTrue(observed[0][1].startswith("weknora-api-key:"))
        self.assertNotIn("person-a-key", observed[0][1])
        self.assertEqual(sent[0]["status"], 204)

    def test_passthrough_mode_rejects_missing_key(self):
        observed, sent = asyncio.run(self._call([]))
        self.assertEqual(observed, [])
        self.assertEqual(sent[0]["status"], 401)

    def test_passthrough_mode_rejects_invalid_key(self):
        observed, sent = asyncio.run(
            self._call([(b"x-mcp-auth-token", b"not-a-weknora-key")])
        )
        self.assertEqual(observed, [])
        self.assertEqual(sent[0]["status"], 401)

    def test_passthrough_mode_isolates_workspace_in_request_context(self):
        observed, sent = asyncio.run(
            self._call([
                (b"authorization", b"Bearer person-a-key"),
                (b"x-tenant-id", b"42"),
            ])
        )
        self.assertEqual(observed[0][2], "42")
        self.assertEqual(sent[0]["status"], 204)


class StdioToolsListTest(unittest.TestCase):
    def test_tools_list_returns_31_tools(self):
        async def _run() -> int:
            from mcp import ClientSession, StdioServerParameters
            from mcp.client.stdio import stdio_client

            params = StdioServerParameters(
                command=sys.executable,
                args=[str(MCP_SERVER_DIR / "weknora_mcp_server.py")],
                env={
                    **os.environ,
                    "WEKNORA_API_KEY": "test-key",
                },
            )
            async with stdio_client(params) as (read, write):
                async with ClientSession(read, write) as session:
                    await session.initialize()
                    tools = await session.list_tools()
                    return len(tools.tools)

        count = asyncio.run(_run())
        self.assertEqual(count, 31)


class HttpStatelessSmokeTest(unittest.TestCase):
    def test_initialize_does_not_require_mcp_session_id(self):
        import time

        port = 19876
        env = {
            **os.environ,
            "MCP_SERVER_AUTH_TOKEN": "test-token",
            "WEKNORA_API_KEY": "test-key",
        }
        proc = subprocess.Popen(
            [
                sys.executable,
                "weknora_mcp_server.py",
                "--transport",
                "http",
                "--host",
                "127.0.0.1",
                "--port",
                str(port),
            ],
            cwd=MCP_SERVER_DIR,
            env=env,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )
        try:
            deadline = time.time() + 10
            while time.time() < deadline:
                probe = subprocess.run(
                    [
                        "curl",
                        "--noproxy",
                        "*",
                        "-s",
                        "-D",
                        "-",
                        "-o",
                        "/dev/null",
                        "-X",
                        "POST",
                        f"http://127.0.0.1:{port}/mcp",
                        "-H",
                        "Authorization: Bearer test-token",
                        "-H",
                        "Content-Type: application/json",
                        "-H",
                        "Accept: application/json, text/event-stream",
                        "-d",
                        (
                            '{"jsonrpc":"2.0","id":1,"method":"initialize",'
                            '"params":{"protocolVersion":"2025-03-26",'
                            '"capabilities":{},"clientInfo":{"name":"t","version":"1"}}}'
                        ),
                    ],
                    capture_output=True,
                    text=True,
                )
                if probe.returncode == 0 and "HTTP/" in probe.stdout:
                    break
                time.sleep(0.2)
            else:
                self.fail("HTTP server did not become ready in time")

            headers = probe.stdout.lower()
            self.assertIn("200 ok", headers)
            self.assertNotIn("mcp-session-id:", headers)
        finally:
            proc.terminate()
            try:
                proc.wait(timeout=5)
            except subprocess.TimeoutExpired:
                proc.kill()


if __name__ == "__main__":
    unittest.main()
