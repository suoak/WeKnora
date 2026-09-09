import asyncio
import importlib.util
import os
from pathlib import Path
import unittest
from unittest.mock import patch


MODULE_PATH = Path(__file__).with_name("weknora_mcp_server.py")


def load_module():
    spec = importlib.util.spec_from_file_location("weknora_usage_test_module", MODULE_PATH)
    module = importlib.util.module_from_spec(spec)
    assert spec.loader
    spec.loader.exec_module(module)
    return module


class UsageTrackingTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.module = load_module()

    def test_one_report_wraps_multiple_internal_requests(self):
        reports = []

        def capture(payload, **metadata):
            reports.append((payload, metadata))

        @self.module.tracked_tool
        def tool(knowledge_base_id):
            self.module.client._request("GET", "/one")
            self.module.client._request("GET", "/two")
            return "ok"

        with patch.object(self.module.client, "_request", return_value={}) as request, patch.object(
            self.module.client, "report_mcp_usage", side_effect=capture
        ):
            self.assertEqual(tool("kb-1"), "ok")
        self.assertEqual(request.call_count, 2)
        self.assertEqual(len(reports), 1)
        self.assertTrue(reports[0][0]["success"])
        self.assertEqual(reports[0][0]["knowledge_base_ids"], ["kb-1"])

    def test_failure_is_reported_once_and_reraised(self):
        reports = []

        def capture(payload, **metadata):
            reports.append((payload, metadata))

        @self.module.tracked_tool
        def tool():
            raise ValueError("private request content")

        with patch.object(self.module.client, "report_mcp_usage", side_effect=capture):
            with self.assertRaises(ValueError):
                tool()
        self.assertEqual(len(reports), 1)
        self.assertFalse(reports[0][0]["success"])
        self.assertEqual(reports[0][0]["error_code"], "ValueError")
        self.assertNotIn("private request content", str(reports[0][0]))

    def test_report_failure_does_not_change_tool_success(self):
        @self.module.tracked_tool
        async def tool():
            return "success"

        with patch.object(self.module.client, "report_mcp_usage", side_effect=RuntimeError("offline")):
            self.assertEqual(asyncio.run(tool()), "success")

    def test_shared_mode_sends_only_safe_unattributed_flag(self):
        reports = []

        def capture(payload, **metadata):
            reports.append((payload, metadata))

        @self.module.tracked_tool
        def tool(query):
            return query

        with patch.dict(os.environ, {"MCP_AUTH_MODE": "shared"}), patch.object(
            self.module.client, "report_mcp_usage", side_effect=capture
        ):
            tool("secret query")
        payload, metadata = reports[0]
        self.assertTrue(metadata["shared_gateway"])
        self.assertNotIn("shared_gateway", payload)
        self.assertNotIn("query", payload)
        self.assertNotIn("api_key", payload)


if __name__ == "__main__":
    unittest.main()
