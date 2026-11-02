"""Synthetic scope/signature checks. No production, Redis or provider access."""
import copy
import unittest
import time
from types import SimpleNamespace

import feishu_steady as tool


class SteadyPlanTests(unittest.TestCase):
    def setUp(self):
        now = int(time.time() * 1000)
        old = dict(namespace="synthetic", app_sha="a" * 40, actor_id="operator", worker_id="worker:1",
                   approval_key_id="b" * 64, queue_sha256="c" * 64, queue_pending_count=6,
                   membership_revision="7", incarnation="aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
        self.original = dict(plan=old, plan_sha256=tool.recovery.digest(old))
        entry = dict(source_id="bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", config_sha256="d" * 64,
                     facts_sha256="e" * 64)
        managed = dict(entry, authority=dict(source_id=entry["source_id"], tenant_id=2,
                       knowledge_base_id="cccccccc-cccc-4ccc-8ccc-cccccccccccc", actor_id="operator",
                       source_type="feishu_drive", source_status="active", access_state="unmanaged",
                       authority_sha256="f" * 64))
        p = dict(version=1, namespace=old["namespace"], runtime_sha=old["app_sha"], actor_id="operator",
                 worker_id=old["worker_id"], approval_key_id=old["approval_key_id"],
                 recovery_plan_sha256=self.original["plan_sha256"], historical_queue_sha256=old["queue_sha256"],
                 historical_pending=6, membership_revision="8", policy_revision=1, revocation_only=False,
                 incarnation="dddddddd-dddd-4ddd-8ddd-dddddddddddd", created_ms=now, expires_ms=now + 900000,
                 authorization_until_ms=now + 7 * 86400000, sources=[entry], managed_sources=[managed])
        self.envelope = dict(plan=p, plan_sha256=tool.recovery.digest(p))
        self.args = SimpleNamespace(revoke_only=False, approved_source=[entry["source_id"]])

    def test_exact_source_in_another_independently_managed_space(self):
        self.assertEqual(tool.validate(self.envelope, self.original, self.args)["managed_sources"][0]
                         ["authority"]["tenant_id"], 2)

    def test_pending_terminal_proof_is_bounded_and_digest_bound(self):
        item = copy.deepcopy(self.envelope)
        item["plan"].update(terminal_pending_count=2, terminal_pending_sha256="a" * 64)
        item["plan_sha256"] = tool.recovery.digest(item["plan"])
        self.assertEqual(tool.validate(item, self.original, self.args)["terminal_pending_count"], 2)
        for count, digest in [(65, "a" * 64), (1, ""), (0, "a" * 64), (1, "not-a-digest")]:
            item["plan"].update(terminal_pending_count=count, terminal_pending_sha256=digest)
            item["plan_sha256"] = tool.recovery.digest(item["plan"])
            with self.assertRaises(ValueError):
                tool.validate(item, self.original, self.args)

    def test_actor_private_scope_and_windows_cannot_silently_change(self):
        mutations = [lambda p: p.update(actor_id="other"),
                     lambda p: p.update(incarnation=self.original["plan"]["incarnation"]),
                     lambda p: p.update(expires_ms=p["created_ms"] - 1),
                     lambda p: p.update(authorization_until_ms=p["created_ms"] + 31 * 86400000),
                     lambda p: p["managed_sources"][0]["authority"].update(actor_id="other"),
                     lambda p: p["managed_sources"][0]["authority"].update(source_status="error"),
                     lambda p: p["managed_sources"][0]["authority"].update(access_state="hold")]
        for mutate in mutations:
            item = copy.deepcopy(self.envelope)
            mutate(item["plan"])
            item["plan_sha256"] = tool.recovery.digest(item["plan"])
            with self.assertRaises(ValueError):
                tool.validate(item, self.original, self.args)

    def test_unlisted_source_and_unrequested_revocation_rejected(self):
        self.args.approved_source = []
        with self.assertRaises(ValueError):
            tool.validate(self.envelope, self.original, self.args)
        self.args.approved_source = [self.envelope["plan"]["sources"][0]["source_id"]]
        self.args.revoke_only = True
        with self.assertRaises(ValueError):
            tool.validate(self.envelope, self.original, self.args)

    def test_hold_reconciliation_requires_an_explicit_distinct_source_purpose(self):
        item = copy.deepcopy(self.envelope)
        p = item["plan"]
        entry = p["managed_sources"][0]
        entry["reconciliation"] = True
        entry["reconcile_until_ms"] = p["created_ms"] + 3600000
        entry["authority"].update(source_status="error", access_state="hold")
        p["sources"][0]["reconciliation"] = True
        item["plan_sha256"] = tool.recovery.digest(p)
        with self.assertRaises(ValueError):
            tool.validate(item, self.original, self.args)
        self.args.reconcile_source = self.args.approved_source
        self.args.approved_source = []
        self.assertEqual(tool.validate(item, self.original, self.args)["sources"][0]["reconciliation"], True)
        entry["authority"]["access_state"] = "revoked"
        item["plan_sha256"] = tool.recovery.digest(p)
        with self.assertRaises(ValueError):
            tool.validate(item, self.original, self.args)


if __name__ == "__main__":
    unittest.main()
