"""Real Python MCP -> loopback Go APP fixture, never a mock identity validator.

Prerequisite: compile internal/middleware tests with CGO=1 and set
PERSONAL_MCP_APP_TEST_BINARY to that test executable. No production URL/DSN
configuration is accepted. Missing prerequisites are explicit SKIP, not PASS.
"""
import asyncio
import contextlib
import io
import ipaddress
import json
import logging
import os
from pathlib import Path
import queue
import socket
import subprocess
import sys
import threading
import unittest
from urllib.parse import urlsplit


def loopback_only(url):
    parsed = urlsplit(url)
    if parsed.scheme != "http" or parsed.username or parsed.password:
        raise ValueError("Only credential-free HTTP loopback test URLs allowed")
    try:
        valid = ipaddress.ip_address(parsed.hostname).is_loopback
    except ValueError:
        valid = False
    if not valid or not parsed.port:
        raise ValueError("Non-loopback upstream forbidden")
    return url


class LocalSafetyTests(unittest.TestCase):
    def test_remote_and_credential_urls_rejected(self):
        for url in ("https://example.invalid", "http://192.0.2.1:80", "http://user:pass@127.0.0.1:8080", "http://localhost:8080"):
            with self.assertRaises(ValueError):
                loopback_only(url)
        self.assertEqual(loopback_only("http://127.0.0.1:8080"), "http://127.0.0.1:8080")


class PersonalMCPRealGatewayTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        if sys.version_info < (3, 10):
            raise unittest.SkipTest("Gateway source requires modern Python; installed Python <3.10")
        binary = os.environ.get("PERSONAL_MCP_APP_TEST_BINARY", "")
        if not binary or not Path(binary).is_file():
            raise unittest.SkipTest("NOT_EXECUTED: CGO-enabled real Go APP fixture binary unavailable")
        # Explicit test-local configuration, no production secret or APP address.
        cls.env_before = dict(os.environ)
        os.environ.update({"WEKNORA_API_KEY":"", "WEKNORA_TENANT_ID":"", "MCP_AUTH_MODE":"weknora_api_key", "MCP_SERVER_AUTH_TOKEN":"", "SYSTEM_AES_KEY":"0123456789abcdef0123456789abcdef", "JWT_SECRET":"synthetic-personal-mcp-tests-not-production"})
        cls.logs = io.StringIO()
        cls.log_handler = logging.StreamHandler(cls.logs)
        logging.getLogger().addHandler(cls.log_handler)
        child_env = dict(os.environ, PERSONAL_MCP_TEST_BRIDGE="1")
        cls.child = subprocess.Popen([binary,"-test.run=^TestPersonalMCPLoopbackBridge$","-test.v"], stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True, env=child_env)
        cls.lines = queue.Queue()
        def drain():
            for line in cls.child.stdout:
                cls.lines.put(line.strip())
        cls.reader = threading.Thread(target=drain, daemon=True); cls.reader.start()
        cls.app_url = None
        try:
            for _ in range(100):
                line = cls.lines.get(timeout=30)
                if line.startswith("PERSONAL_MCP_LOOPBACK="):
                    cls.app_url = loopback_only(line.split("=",1)[1]); break
            if not cls.app_url:
                raise RuntimeError("Go APP fixture did not announce isolated readiness")
            import requests
            cls.requests = requests
            response = requests.get(cls.app_url+"/__test/credentials", timeout=10)
            response.raise_for_status(); cls.keys = response.json()  # synthetic, memory only
            os.environ["WEKNORA_BASE_URL"] = cls.app_url+"/api/v1"
            import weknora_mcp_server as srv
            import uvicorn
            cls.srv = srv
            # Override only configuration, not authentication/authorization logic.
            srv.WEKNORA_BASE_URL = cls.app_url+"/api/v1"
            srv.client = srv.WeKnoraClient(srv.WEKNORA_BASE_URL, "")
            cls.ready = threading.Event()
            app = srv.mcp.streamable_http_app(stateless_http=True)
            wrapped = srv.MCPAuthMiddleware(app, "", auth_mode="weknora_api_key")
            class ReadyServer(uvicorn.Server):
                async def startup(self, sockets=None):
                    await super().startup(sockets=sockets)
                    if self.started:
                        cls.ready.set()
            cls.sock = socket.socket(); cls.sock.bind(("127.0.0.1",0)); cls.sock.listen(128)
            cls.gateway = ReadyServer(uvicorn.Config(wrapped, host="127.0.0.1",port=cls.sock.getsockname()[1],log_level="warning",access_log=False))
            cls.thread = threading.Thread(target=lambda: cls.gateway.run(sockets=[cls.sock]),daemon=True); cls.thread.start()
            if not cls.ready.wait(20):
                raise RuntimeError("Real MCP gateway startup did not finish")
            cls.mcp_url = "http://127.0.0.1:%d/mcp" % cls.sock.getsockname()[1]
            cls.sessions = {}
        except BaseException:
            cls._cleanup(); raise

    @classmethod
    def _cleanup(cls):
        for session in getattr(cls,"sessions",{}).values():
            session.close()
        if hasattr(cls,"gateway"):
            cls.gateway.should_exit = True
            cls.thread.join(timeout=15)
        if getattr(cls,"app_url",None):
            with contextlib.suppress(Exception):
                cls.requests.post(cls.app_url+"/__test/control", json={"action":"stop"},timeout=5)
        if hasattr(cls,"child"):
            try: cls.child.wait(timeout=10)
            except subprocess.TimeoutExpired: cls.child.terminate(); cls.child.wait(timeout=5)
            if cls.child.stdout:
                cls.child.stdout.close()
        if hasattr(cls,"log_handler"):
            logging.getLogger().removeHandler(cls.log_handler)
        if hasattr(cls,"env_before"):
            os.environ.clear(); os.environ.update(cls.env_before)

    @classmethod
    def tearDownClass(cls):
        cls._cleanup()

    def control(self, action):
        response = self.requests.post(self.app_url+"/__test/control",json={"action":action},timeout=10)
        self.assertEqual(response.status_code,200)

    def call(self, alias="alice-1", tenant="1", kb="aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"):
        headers={"Authorization":"Bearer "+self.keys[alias],"Accept":"application/json, text/event-stream","Content-Type":"application/json"}
        if tenant is not None: headers["X-Tenant-ID"] = tenant
        payload={"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"hybrid_search","arguments":{"kb_id":kb,"query":"synthetic"}}}
        session = self.sessions.setdefault(alias, self.requests.Session())
        # Actual MCP initialization before tool calls; no identity stub is used.
        initialize = {"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"synthetic-lifecycle","version":"1"}}}
        initialized = session.post(self.mcp_url, headers=headers, json=initialize, timeout=15)
        if initialized.status_code != 200:
            return initialized.status_code, None
        session_id = initialized.headers.get("Mcp-Session-Id")
        if session_id:
            headers["Mcp-Session-Id"] = session_id
        headers["MCP-Protocol-Version"] = "2025-03-26"
        session.post(self.mcp_url, headers=headers, json={"jsonrpc":"2.0","method":"notifications/initialized"}, timeout=15).raise_for_status()
        response=session.post(self.mcp_url,headers=headers,json=payload,timeout=15)
        body=None
        if response.status_code==200:
            if response.headers.get("content-type","").startswith("text/event-stream"):
                events=[line[6:] for line in response.text.splitlines() if line.startswith("data: ")]
                if events: body=json.loads(events[-1])
            else: body=response.json()
        return response.status_code,body

    def count(self):
        response=self.requests.get(self.app_url+"/__test/count",timeout=10);response.raise_for_status()
        return response.json()["retrieved"]

    def test_lifecycle_real_identity_and_recovery(self):
        # One ordered test preserves fixture mutations, without reload or fake auth.
        for alias in ("alice-1","alice-2","bob-1"):
            before=self.count(); status,body=self.call(alias);self.assertEqual(status,200)
            self.assertFalse(body["result"].get("isError",False));self.assertEqual(self.count(),before+1)
        self.control("disable");before=self.count();status,_=self.call();self.assertEqual(status,401);self.assertEqual(self.count(),before)
        self.control("restore");status,body=self.call();self.assertEqual(status,200);self.assertFalse(body["result"].get("isError",False))
        self.control("remove");before=self.count();status,_=self.call();self.assertEqual(status,401);self.assertEqual(self.count(),before)
        status,body=self.call(tenant="2",kb="bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb");self.assertEqual(status,200);self.assertFalse(body["result"].get("isError",False))
        self.control("rejoin");status,body=self.call();self.assertEqual(status,200);self.assertFalse(body["result"].get("isError",False))
        status,_=self.call(tenant=None);self.assertEqual(status,409)
        status,_=self.call(tenant="3");self.assertEqual(status,401)
        # Reused HTTP connection pool, reinitialized stateless MCP, old credential after rotate.
        # This does not claim validation of an outstanding stateful SSE connection.
        self.control("rotate");status,_=self.call();self.assertEqual(status,401)
        response=self.requests.get(self.app_url+"/__test/credentials",timeout=10);response.raise_for_status();self.keys.update(response.json())
        status,body=self.call("rotated");self.assertEqual(status,200);self.assertFalse(body["result"].get("isError",False))
        self.control("revoke");status,_=self.call("rotated");self.assertEqual(status,401)
        for alias in ("alice-2","bob-1"):
            status,body=self.call(alias);self.assertEqual(status,200);self.assertFalse(body["result"].get("isError",False))
        # Deliberately withhold log text even on failure.
        self.assertFalse(any(token in self.logs.getvalue() for token in self.keys.values()),"Synthetic credential appeared in gateway logs (content withheld)")

    def test_resource_denial_is_tool_error_not_transport_success(self):
        before = self.count()
        status, body = self.call("alice-2", tenant="1", kb="bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")
        self.assertEqual(status, 200)
        self.assertTrue(body["result"].get("isError", False))
        self.assertEqual(self.count(), before)

    def test_non_personal_keys_passthrough_characterization(self):
        # Real APP validates both credential types; this records compatibility,
        # not endorsement of accepting machine credentials in a personal gateway.
        for alias in ("tenant-key", "platform-key"):
            before = self.count()
            status, body = self.call(alias)
            self.assertEqual(status, 200)
            self.assertFalse(body["result"].get("isError",False))
            self.assertEqual(self.count(), before+1)


if __name__ == "__main__":
    unittest.main(verbosity=2)
