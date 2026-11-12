import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import Mock, patch

from usage_outbox import UsageOutbox


class UsageOutboxTests(unittest.TestCase):
    def test_unfinished_admission_recovers_as_interrupted(self):
        with tempfile.TemporaryDirectory() as directory:
            headers = {"X-API-Key": "secret"}
            outbox = UsageOutbox("http://backend/usage", directory, start=False)
            outbox.enqueue({"event_key": "unfinished", "tool_name": "hybrid_search", "success": True}, headers, active=True)
            restarted = UsageOutbox("http://backend/usage", directory, start=False)
            restarted.register(headers)
            with patch("usage_outbox.requests.post", return_value=Mock()) as send:
                restarted.drain()
                payload = send.call_args.kwargs["json"]
                self.assertEqual(payload["error_code"], "interrupted")
                self.assertFalse(payload["success"])

    def test_restart_retries_same_event_without_persisting_credentials(self):
        with tempfile.TemporaryDirectory() as directory:
            headers = {"X-API-Key": "private-credential", "X-Tenant-ID": "100"}
            outbox = UsageOutbox("http://backend/usage", directory, start=False)
            outbox.enqueue({"event_key": "one", "tool_name": "hybrid_search"}, headers)
            pending = next(Path(directory).glob("*.json"))
            self.assertNotIn("private-credential", pending.read_text())
            restarted = UsageOutbox("http://backend/usage", directory, start=False)
            with patch("usage_outbox.requests.post") as send:
                restarted.drain()
                send.assert_not_called()
                restarted.register(headers)
                send.return_value = Mock()
                restarted.drain()
                self.assertEqual(send.call_args.kwargs["json"]["event_key"], "one")
                self.assertFalse(pending.exists())

    def test_failed_delivery_is_retained_and_retried_with_identical_key(self):
        with tempfile.TemporaryDirectory() as directory:
            outbox = UsageOutbox("http://backend/usage", directory, start=False)
            outbox.enqueue({"event_key": "stable"}, {"X-API-Key": "secret"})
            with patch("usage_outbox.requests.post", side_effect=RuntimeError("offline")):
                outbox.drain()
            pending = next(Path(directory).glob("*.json"))
            item = json.loads(pending.read_text())
            self.assertEqual(item["attempts"], 1)
            self.assertEqual(item["payload"]["event_key"], "stable")
            item["retry_after"] = 0
            pending.write_text(json.dumps(item))
            with patch("usage_outbox.requests.post", return_value=Mock()) as send:
                outbox.drain()
                self.assertEqual(send.call_args.kwargs["json"]["event_key"], "stable")
            self.assertFalse(pending.exists())


if __name__ == "__main__":
    unittest.main()
