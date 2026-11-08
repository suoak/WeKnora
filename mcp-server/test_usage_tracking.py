import asyncio
import importlib.util
import os
from pathlib import Path
import unittest
from unittest.mock import Mock, patch


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

        with patch.object(self.module, "_active_mcp_transport", "http"), patch.dict(os.environ, {"MCP_AUTH_MODE": "shared"}), patch.object(
            self.module.client, "report_mcp_usage", side_effect=capture
        ):
            tool("secret query")
        payload, metadata = reports[0]
        self.assertTrue(metadata["shared_gateway"])
        self.assertNotIn("shared_gateway", payload)
        self.assertNotIn("query", payload)
        self.assertNotIn("api_key", payload)

    def test_kb_aliases_and_returned_errors(self):
        self.assertEqual(self.module._safe_resource_ids({"kb_id": "kb-1"})[0], ["kb-1"])
        reports = []

        @self.module.tracked_tool
        def tool():
            return {"isError": True, "content": "private answer"}

        with patch.object(self.module.client, "report_mcp_usage", side_effect=lambda payload, **kw: reports.append(payload)):
            self.assertTrue(tool()["isError"])
        self.assertEqual(len(reports), 1)
        self.assertFalse(reports[0]["success"])
        self.assertEqual(reports[0]["error_code"], "tool_error")
        self.assertNotIn("private answer", str(reports))

    def test_cancellation_reports_once_and_preserves_cancellation(self):
        reports = []

        @self.module.tracked_tool
        async def tool():
            raise asyncio.CancelledError()

        with patch.object(self.module.client, "report_mcp_usage", side_effect=lambda payload, **kw: reports.append(payload)):
            with self.assertRaises(asyncio.CancelledError):
                asyncio.run(tool())
        self.assertEqual(len(reports), 1)
        self.assertEqual(reports[0]["error_code"], "canceled")

    def test_search_scope_and_hits_are_separate(self):
        reports = []

        @self.module.tracked_tool
        def hybrid_search(kb_id):
            state = self.module._usage_resources.get()
            state["targets"].update(["a", "b", "c", "d", "e"])
            return {"results": [{"knowledge_base_id": "a", "knowledge_id": "doc-a"}, {"knowledge_base_id": "b", "knowledge_id": "doc-b"}]}

        with patch.object(self.module.client, "report_mcp_usage", side_effect=lambda payload, **kw: reports.append(payload)):
            hybrid_search("display-name")
        self.assertEqual(len(reports), 1)
        self.assertEqual(len(reports[0]["target_knowledge_base_ids"]), 5)
        self.assertEqual(set(reports[0]["hit_knowledge_base_ids"]), {"a", "b"})
        self.assertEqual(reports[0]["knowledge_base_ids"], [])

    def test_protocol_callback_covers_validation_and_deduplicates_function_reports(self):
        reports = []
        calls = []

        @self.module.mcp.tool()
        def usage_protocol_probe(required_value: str) -> dict:
            calls.append(required_value)
            return {"success": True}

        entry = self.module.mcp._lowlevel_server.get_request_handler("tools/call")
        async def invoke(name, arguments):
            return await entry.handler(Mock(), entry.params_type(name=name, arguments=arguments))

        with patch.object(self.module.client, "report_mcp_usage", side_effect=lambda payload, **kw: reports.append(payload)):
            asyncio.run(invoke("usage_protocol_probe", {"required_value": "private question"}))
            asyncio.run(invoke("usage_protocol_probe", {}))
            asyncio.run(invoke("unregistered-private-question", {}))
        self.assertEqual(calls, ["private question"])
        self.assertEqual(len(reports), 3)
        self.assertTrue(reports[0]["success"])
        self.assertEqual(reports[1]["error_code"], "invalid_arguments")
        self.assertEqual(reports[2]["tool_name"], "unknown")
        self.assertEqual(reports[2]["error_code"], "unknown_tool")
        self.assertNotIn("private question", str(reports))


if __name__ == "__main__":
    unittest.main()
