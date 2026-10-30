"""Synthetic offline checks; no production or provider network requests."""
import copy
import hashlib
import time
import unittest
from types import SimpleNamespace
from unittest.mock import patch

from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey

import feishu_continuity as c


class ContinuityApprovalTests(unittest.TestCase):
    def setUp(self):
        self.signer = Ed25519PrivateKey.from_private_bytes(b"k" * 32)
        public = self.signer.public_key().public_bytes(serialization.Encoding.Raw, serialization.PublicFormat.Raw)
        key_id = hashlib.sha256(public).hexdigest()
        source = "5c7bc873-0ba3-43c7-90a0-32c23e6bd529"
        self.args = SimpleNamespace(namespace="synthetic", trip="feishu_headers_io_failure", incident="1-0",
                                    runtime_sha="a" * 40, expected_pending=6, source=source,
                                    approved_source=[source], valid_seconds=900, reference="synthetic-review",
                                    approval_key_file="offline-private", evidence_manifest="synthetic-evidence")
        old = dict(namespace="synthetic", trip=self.args.trip, incident_id="1-0", app_sha="a" * 40,
                   worker_id="worker:1", membership_revision="7", approval_key_id=key_id,
                   actor_id="operator", queue_sha256="b" * 64,
                   incarnation="bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")
        self.original = dict(plan=old, plan_sha256=c.recovery.digest(old))
        now = int(time.time() * 1000)
        p = dict(version=1, namespace="synthetic", runtime_sha="a" * 40, worker_id="worker:1",
                 membership_revision="8", approval_key_id=key_id, actor_id="operator",
                 historical_pending=6, historical_queue_sha256="b" * 64,
                 recovery_plan_sha256=self.original["plan_sha256"],
                 incarnation="aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", created_ms=now, expires_ms=now + 3600000,
                 sources=[dict(source_id=source, config_sha256="c" * 64, facts_sha256="d" * 64)])
        self.envelope = dict(plan=p, plan_sha256=c.recovery.digest(p))

    def test_separate_signature_binds_current_policy_without_network(self):
        private = self.signer.private_bytes(serialization.Encoding.PEM, serialization.PrivateFormat.PKCS8,
                                           serialization.NoEncryption())
        with patch.object(c.recovery, "restricted_read", return_value=private), \
                patch.object(c.recovery, "reviewed_evidence", return_value="e" * 64), \
                patch.object(c.recovery, "request", side_effect=AssertionError("network forbidden")):
            a = c.sign(self.envelope, self.original, self.args)
        signature = a["signature"]
        a["signature"] = ""
        self.signer.public_key().verify(bytes.fromhex(signature), c.recovery.canonical(a))
        self.assertEqual(a["operation"], "approved_sources")
        self.assertEqual(a["incarnation"], self.envelope["plan"]["incarnation"])

    def test_recomputed_digest_does_not_allow_changed_incident_or_queue(self):
        for key, value in [("runtime_sha", "f" * 40), ("historical_queue_sha256", "f" * 64),
                           ("historical_pending", 5), ("recovery_plan_sha256", "f" * 64),
                           ("membership_revision", "7"), ("membership_revision", "9"),
                           ("incarnation", self.original["plan"]["incarnation"]), ("expires_ms", 1)]:
            with self.subTest(key=key):
                e = copy.deepcopy(self.envelope)
                e["plan"][key] = value
                e["plan_sha256"] = c.recovery.digest(e["plan"])
                self.assertRaises(ValueError, c.validate, e, self.original, self.args)

    def test_source_expansion_and_duplicate_are_rejected(self):
        for source in ["aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", self.args.source]:
            e = copy.deepcopy(self.envelope)
            e["plan"]["sources"].append(dict(source_id=source, config_sha256="c" * 64, facts_sha256="d" * 64))
            e["plan_sha256"] = c.recovery.digest(e["plan"])
            self.assertRaises(ValueError, c.validate, e, self.original, self.args)


if __name__ == "__main__":
    unittest.main()
