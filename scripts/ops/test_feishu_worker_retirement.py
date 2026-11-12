"""Only retirement safety paths; requires a new, explicitly owned isolated Redis."""
import base64
import copy
import json
import os
import socket
import unittest
from types import SimpleNamespace
from unittest.mock import patch

from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey

import feishu_worker_retirement as tool


class Redis:
    def __init__(self):
        host, port = os.environ["KNOWHUB_RETIREMENT_TEST_REDIS"].rsplit(":", 1)
        self.socket = socket.create_connection((host, int(port)), timeout=5)
        self.stream = self.socket.makefile("rb")

    def reply(self):
        line = self.stream.readline(); kind = line[:1]; value = line[1:-2]
        if kind == b"+":
            return value
        if kind == b"-":
            raise ValueError("isolated Redis rejected operation")
        if kind == b":":
            return int(value)
        if kind == b"$":
            n = int(value)
            if n < 0:
                return None
            data = self.stream.read(n)
            assert len(data) == n and self.stream.read(2) == b"\r\n"
            return data
        if kind == b"*":
            n = int(value)
            return None if n < 0 else [self.reply() for _ in range(n)]
        raise ValueError("unexpected test Redis response")

    def cmd(self, *args):
        args = [x if isinstance(x, bytes) else str(x).encode() for x in args]
        self.socket.sendall(b"*" + str(len(args)).encode() + b"\r\n" + b"".join(
            b"$" + str(len(x)).encode() + b"\r\n" + x + b"\r\n" for x in args))
        return self.reply()

    def close(self):
        self.stream.close(); self.socket.close()


def pb_number(n):
    out = bytearray()
    while n >= 128:
        out.append((n & 127) | 128); n >>= 7
    out.append(n)
    return bytes(out)


def pb(field, value):
    if isinstance(value, bytes):
        return pb_number(field * 8 + 2) + pb_number(len(value)) + value
    return pb_number(field * 8) + pb_number(value)


@unittest.skipUnless(os.environ.get("KNOWHUB_OWNED_RETIREMENT_REDIS") == "1", "owned isolated Redis required")
class RetirementTests(unittest.TestCase):
    def setUp(self):
        self.redis = Redis()
        self.addCleanup(self.redis.close)
        # This DB belongs to the new no-volume, no-host-port test container only.
        self.redis.cmd("FLUSHDB")
        self.prefix = "feishu:safety:{synthetic-retirement}:"
        self.old = "aaaaaaaaaaaa:1"; self.current = "bbbbbbbbbbbb:1"
        seconds, micro = self.redis.cmd("TIME"); self.now = int(seconds) * 1000 + int(micro) // 1000
        self.redis.cmd("HSET", self.prefix + "workers", self.old, "old-fingerprint:old-incarnation",
                       self.current, "current-fingerprint:current-incarnation")
        self.redis.cmd("ZADD", self.prefix + "leases", self.now - 60000, self.old,
                       self.now + 60000, self.current)
        self.redis.cmd("HSET", self.prefix + "admitted", self.old, "old-fingerprint:old-incarnation")
        self.redis.cmd("ZADD", self.prefix + "joining", self.now - 10000, self.old)
        self.redis.cmd("SET", self.prefix + "membership_revision", "7")
        self.redis.cmd("SET", self.prefix + "trip", "feishu_headers_io_failure")
        self.redis.cmd("SET", "asynq:{sync}:paused", "feishu_safety_stop")
        self.redis.cmd("XADD", self.prefix + "audit", "1-0", "event", "trip", "reason", "feishu_headers_io_failure")
        for i in range(6):
            id = "historical-" + str(i)
            self.redis.cmd("RPUSH", "asynq:{sync}:pending", id)
            self.redis.cmd("HSET", "asynq:{sync}:t:" + id, b"msg", bytes([i, 0xff, 0, 1]),
                           b"state", b"pending", b"\xfe", b"binary-field")
        self.server_key = "asynq:servers:{synthetic}"
        server = pb(1, b"bbbbbbbbbbbb") + pb(2, 1) + pb(4, 4) + pb(7, b"active") + pb(9, 0)
        server += pb(5, pb(1, b"sync") + pb(2, 2)) + pb(5, pb(1, b"approved_sync") + pb(2, 1))
        self.redis.cmd("SET", self.server_key, server)
        self.redis.cmd("ZADD", "asynq:servers", self.now + 60000, self.server_key)
        self.snap = self.snapshot()
        tool.validate_snapshot(self.snap, self.old, self.current, "1-0", "feishu_headers_io_failure")
        self.key = Ed25519PrivateKey.from_private_bytes(b"k" * 32)
        public = self.key.public_key().public_bytes(serialization.Encoding.PEM,
                                                   serialization.PublicFormat.SubjectPublicKeyInfo)
        with patch.object(tool, "restricted", return_value=public):
            self.public, self.key_id = tool.public_key("synthetic-public")
        self.proof = dict(current_id="b" * 64, current_image="synthetic", old_absent=True,
                          old_finished_at=None, workload={"claimed": 0, "publication_leases": 0})
        signed_snap = copy.deepcopy(self.snap)
        del signed_snap["audit"]; del signed_snap["inflight"]
        self.plan = dict(domain=tool.DOMAIN, namespace="synthetic-retirement", runtime_sha="a" * 40,
                         old_worker=self.old, current_worker=self.current, incident="1-0",
                         trip="feishu_headers_io_failure", actor="ssh:" + str(os.getuid()),
                         approval_key_id=self.key_id, created_ms=self.now, expires_ms=self.now + 60000,
                         snapshot=signed_snap, proof=self.proof)
        private = self.key.private_bytes(serialization.Encoding.PEM, serialization.PrivateFormat.PKCS8,
                                         serialization.NoEncryption())
        self.approval = tool.sign(self.plan, private, "synthetic-reviewed-operation")
        self.args = SimpleNamespace(**{k: self.plan[k] for k in
                                      ("namespace", "runtime_sha", "old_worker", "current_worker", "incident", "trip")})

    def snapshot(self):
        raw = self.redis.cmd("EVAL_RO", tool.SNAPSHOT_LUA, 0, self.prefix)
        def encode(value):
            if isinstance(value, bytes):
                return {"b64": base64.b64encode(value).decode()}
            return [encode(x) for x in value] if isinstance(value, list) else value
        return tool.snapshot_rows(encode(raw))

    def execute(self, approval=None):
        # Exercise actual signature validation and the exact production Lua using
        # injected, already-observed container/SQL proof. No production connection.
        def redis_app(code):
            scope = dict(cmd=self.redis.cmd, json=json, base64=base64, enc=lambda value: value)
            import contextlib, io
            out = io.StringIO()
            def encode(value):
                if isinstance(value, bytes):
                    return {"b64": base64.b64encode(value).decode()}
                return [encode(x) for x in value] if isinstance(value, list) else value
            scope["enc"] = encode
            with contextlib.redirect_stdout(out):
                exec(code, scope)
            return json.loads(out.getvalue())
        with patch.object(tool, "collect", return_value=(self.snapshot(), self.proof, "synthetic-public")), \
                patch.object(tool, "public_key", return_value=(self.public, self.key_id)), \
                patch.object(tool, "redis_app", side_effect=redis_app):
            return tool.execute(self.args, self.plan, approval or self.approval)

    def untouched(self):
        self.assertEqual(self.redis.cmd("HLEN", self.prefix + "workers"), 2)
        self.assertEqual(self.redis.cmd("GET", self.prefix + "membership_revision"), b"7")
        self.assertEqual(self.redis.cmd("LLEN", "asynq:{sync}:pending"), 6)
        self.assertEqual(self.redis.cmd("GET", self.prefix + "trip"), b"feishu_headers_io_failure")
        self.assertIsNone(self.redis.cmd("GET", self.prefix + "permit"))

    def test_signed_retirement_changes_only_old_membership_and_revision(self):
        before = self.snapshot()
        result = self.execute()
        after = self.snapshot()
        self.assertEqual(result["membership_revision"], "8")
        self.assertEqual(after["workers"], {self.current: before["workers"][self.current]})
        self.assertEqual(after["leases"][self.current], before["leases"][self.current])
        for key in ("trip", "permit", "pause", "ids", "tasks", "servers", "audit", "recovery"):
            self.assertEqual(before[key], after[key], key)
        self.assertEqual(self.redis.cmd("XLEN", self.prefix + "control_audit"), 1)
        self.assertRaises(ValueError, self.execute)

    def test_late_old_lease_renewal_is_rejected(self):
        self.redis.cmd("ZADD", self.prefix + "leases", self.now + 60000, self.old)
        self.assertRaises(ValueError, self.execute)
        self.untouched()

    def test_queue_or_consumer_change_is_rejected(self):
        for key, field, value in [("asynq:{sync}:t:historical-0", "msg", "changed"),
                                  (self.prefix + "workers", self.current, "new-incarnation")]:
            previous = self.redis.cmd("HGET", key, field)
            self.redis.cmd("HSET", key, field, value)
            self.assertRaises(ValueError, self.execute)
            self.untouched()
            self.redis.cmd("HSET", key, field, previous)
        self.redis.cmd("SET", self.server_key, "changed-consumer")
        self.assertRaises(ValueError, self.execute)
        self.untouched()

    def test_audit_failure_does_not_partially_retire(self):
        self.redis.cmd("SET", self.prefix + "control_audit", "fault-injection")
        self.assertRaises(ValueError, self.execute)
        self.untouched()

    def test_signature_and_runtime_proof_changes_are_rejected(self):
        bad = dict(self.approval, signature="00" * 64)
        from cryptography.exceptions import InvalidSignature
        self.assertRaises(InvalidSignature, self.execute, bad)
        self.proof["workload"]["claimed"] = 1
        self.assertRaises(ValueError, self.execute)
        self.untouched()

    def test_noncanonical_revision_is_rejected_before_mutation(self):
        self.redis.cmd("SET", self.prefix + "membership_revision", "07")
        snap = self.snapshot()
        self.assertRaises(ValueError, tool.validate_snapshot, snap, self.old, self.current,
                          "1-0", "feishu_headers_io_failure")
        self.assertEqual(self.redis.cmd("HLEN", self.prefix + "workers"), 2)

    def reapprove_current_snapshot(self):
        snap = self.snapshot()
        tool.validate_snapshot(snap, self.old, self.current, "1-0", "feishu_headers_io_failure")
        del snap["audit"]; del snap["inflight"]
        self.plan["snapshot"] = snap
        self.plan["created_ms"] = snap["now"]
        self.plan["expires_ms"] = snap["now"] + 60000
        private = self.key.private_bytes(serialization.Encoding.PEM, serialization.PrivateFormat.PKCS8,
                                         serialization.NoEncryption())
        self.approval = tool.sign(self.plan, private, "synthetic-reviewed-operation")

    def add_expired_members(self):
        for character in "cdef":
            worker = character * 12 + ":1"
            self.redis.cmd("HSET", self.prefix + "workers", worker, "retained-expired-incarnation")
            self.redis.cmd("ZADD", self.prefix + "leases", self.now - 60000, worker)

    def test_six_members_three_tasks_retained_review_and_busy_wiki(self):
        self.add_expired_members()
        self.redis.cmd("LTRIM", "asynq:{sync}:pending", 0, 2)
        recovery = json.dumps({"phase": "awaiting_review", "expires_ms": self.now - 1000})
        self.redis.cmd("SET", self.prefix + "recovery", recovery)
        wiki_key = "asynq:servers:{synthetic-wiki}"
        wiki_server = pb(1, b"bbbbbbbbbbbb") + pb(2, 1) + pb(7, b"active") + pb(9, 2)
        wiki_server += pb(5, pb(1, b"wiki") + pb(2, 1))
        self.redis.cmd("SET", wiki_key, wiki_server)
        self.redis.cmd("ZADD", "asynq:servers", self.now + 60000, wiki_key)
        self.reapprove_current_snapshot()
        before = self.snapshot()
        self.execute()
        after = self.snapshot()
        self.assertEqual(set(after["workers"]), set(before["workers"]) - {self.old})
        for worker in after["workers"]:
            self.assertEqual(after["workers"][worker], before["workers"][worker])
            self.assertEqual(after["leases"][worker], before["leases"][worker])
        for key in ("trip", "permit", "pause", "ids", "tasks", "servers", "audit", "recovery"):
            self.assertEqual(before[key], after[key], key)

    def test_another_member_lease_renewal_rejects_signed_retirement(self):
        self.add_expired_members()
        self.reapprove_current_snapshot()
        self.redis.cmd("ZADD", self.prefix + "leases", self.now + 60000, "c" * 12 + ":1")
        self.assertRaises(ValueError, self.execute)
        self.assertEqual(self.redis.cmd("HLEN", self.prefix + "workers"), 6)
        self.assertEqual(self.redis.cmd("XLEN", self.prefix + "control_audit"), 0)

    def test_other_live_member_or_live_recovery_is_not_retirable(self):
        self.add_expired_members()
        worker = "c" * 12 + ":1"
        self.redis.cmd("ZADD", self.prefix + "leases", self.now + 60000, worker)
        self.assertRaises(ValueError, tool.validate_snapshot, self.snapshot(), self.old, self.current,
                          "1-0", "feishu_headers_io_failure")
        self.redis.cmd("ZADD", self.prefix + "leases", self.now - 60000, worker)
        for state in ({"phase": "running", "expires_ms": self.now - 1000},
                      {"phase": "awaiting_review", "expires_ms": self.now + 60000}):
            self.redis.cmd("SET", self.prefix + "recovery", json.dumps(state))
            self.assertRaises(ValueError, tool.validate_snapshot, self.snapshot(), self.old, self.current,
                              "1-0", "feishu_headers_io_failure")
        self.assertEqual(self.redis.cmd("HLEN", self.prefix + "workers"), 6)

    def test_retained_recovery_change_rejects_execution(self):
        self.redis.cmd("SET", self.prefix + "recovery",
                       json.dumps({"phase": "awaiting_review", "expires_ms": self.now - 1000}))
        self.reapprove_current_snapshot()
        self.redis.cmd("SET", self.prefix + "recovery",
                       json.dumps({"phase": "awaiting_review", "expires_ms": self.now - 2000}))
        self.assertRaises(ValueError, self.execute)
        self.untouched()

    def test_empty_shared_pending_queue_is_retained(self):
        self.redis.cmd("DEL", "asynq:{sync}:pending")
        self.reapprove_current_snapshot()
        self.execute()
        self.assertEqual(self.redis.cmd("LLEN", "asynq:{sync}:pending"), 0)
        self.assertEqual(self.redis.cmd("GET", self.prefix + "trip"), b"feishu_headers_io_failure")
        self.assertIsNone(self.redis.cmd("GET", self.prefix + "permit"))

    def test_heartbeat_queue_map_reordering_is_not_a_consumer_change(self):
        raw = self.redis.cmd("GET", self.server_key)
        first = pb(5, pb(1, b"sync") + pb(2, 2))
        second = pb(5, pb(1, b"approved_sync") + pb(2, 1))
        self.assertIn(first + second, raw)
        self.redis.cmd("SET", self.server_key, raw.replace(first + second, second + first))
        self.execute()
        self.assertEqual(self.redis.cmd("HLEN", self.prefix + "workers"), 1)
        self.assertEqual(self.redis.cmd("GET", self.prefix + "trip"), b"feishu_headers_io_failure")
        self.assertEqual(self.redis.cmd("LLEN", "asynq:{sync}:pending"), 6)

    def test_changed_priority_active_count_and_malformed_consumer_stay_closed(self):
        original = self.redis.cmd("GET", self.server_key)
        queue = pb(5, pb(1, b"sync") + pb(2, 2))
        changed = original.replace(queue, pb(5, pb(1, b"sync") + pb(2, 3)))
        for raw in (changed, original + pb(9, 1), original + b"\x80", original + b"\x2a\xff"):
            self.redis.cmd("SET", self.server_key, raw)
            self.assertRaises(ValueError, self.execute)
            self.untouched()


if __name__ == "__main__":
    unittest.main()
