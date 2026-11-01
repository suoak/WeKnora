"""Offline-signed normal Feishu policy; never connects to Redis.

New policies list exact sources across independently authorized tenants. A
15-minute signature-consumption window is distinct from the signed 1..30 day
authorization. Only the controller renews 15-second health leases; neither this
tool nor health renewal extends a source grant. No global resume exists.
"""
import argparse
import hashlib
import json
from pathlib import Path
import re
import time
import uuid

import feishu_recovery as recovery


def validate(envelope, original, args, now_ms=None):
    p = envelope["plan"]
    old = original["plan"]
    now = int(time.time() * 1000) if now_ms is None else now_ms
    if original["plan_sha256"] != recovery.digest(old) or envelope["plan_sha256"] != recovery.digest(p):
        raise ValueError("plan digest mismatch")
    if (p.get("version") != 1 or p.get("namespace") != old.get("namespace") or
            p.get("runtime_sha") != old.get("app_sha") or p.get("actor_id") != old.get("actor_id") or
            p.get("worker_id") != old.get("worker_id") or p.get("approval_key_id") != old.get("approval_key_id") or
            p.get("recovery_plan_sha256") != recovery.digest(old) or
            p.get("historical_queue_sha256") != old.get("queue_sha256") or
            p.get("historical_pending") != old.get("queue_pending_count") or
            p.get("membership_revision") != str(int(old["membership_revision"]) + 1)):
        raise ValueError("incident/runtime/actor/queue binding changed")
    if (str(uuid.UUID(p["incarnation"])) != p["incarnation"] or p["incarnation"] == old["incarnation"] or
            not 0 < p["expires_ms"] - now <= 900000 or p["created_ms"] > now + 5000 or
            not 0 < p["expires_ms"] - p["created_ms"] <= 900000 or
            not p["expires_ms"] < p["authorization_until_ms"] <= p["created_ms"] + 30 * 86400000 or
            p.get("policy_revision", 0) < 1 or bool(p.get("revocation_only")) != args.revoke_only):
        raise ValueError("policy window, revision or operation invalid")
    entries = p.get("managed_sources", [])
    ids = [s["source_id"] for s in entries]
    if (len(ids) > 64 or (not ids and not args.revoke_only) or ids != sorted(set(ids)) or
            ids != sorted(args.approved_source) or len(entries) != len(p.get("sources", []))):
        raise ValueError("exact source approval required")
    for entry, base in zip(entries, p["sources"]):
        authority = entry["authority"]
        if (str(uuid.UUID(entry["source_id"])) != entry["source_id"] or
                {k: entry[k] for k in ("source_id", "config_sha256", "facts_sha256")} != base or
                authority.get("source_id") != entry["source_id"] or authority.get("actor_id") != p["actor_id"] or
                authority.get("source_status") != "active" or authority.get("access_state") not in ("active", "unmanaged") or
                authority.get("tenant_id", 0) <= 0 or
                str(uuid.UUID(authority["knowledge_base_id"])) != authority["knowledge_base_id"] or
                authority.get("source_type") not in ("feishu", "feishu_drive", "lark", "lark_drive") or
                any(not re.fullmatch(r"[a-f0-9]{64}", value) for value in
                    (entry["config_sha256"], entry["facts_sha256"], authority["authority_sha256"]))):
            raise ValueError("source identity or authority incomplete")
    return p


def approve(envelope, original, args):
    p = validate(envelope, original, args)
    if not re.fullmatch(r"[A-Za-z0-9:._/-]{1,180}", args.reference) or "://" in args.reference:
        raise ValueError("bounded approval reference required")
    from cryptography.hazmat.primitives import serialization
    from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
    signer = serialization.load_pem_private_key(recovery.restricted_read(args.approval_key_file), password=None)
    if not isinstance(signer, Ed25519PrivateKey):
        raise ValueError("offline Ed25519 key required")
    raw = signer.public_key().public_bytes(serialization.Encoding.Raw, serialization.PublicFormat.Raw)
    kid = hashlib.sha256(raw).hexdigest()
    if kid != p["approval_key_id"]:
        raise ValueError("wrong signer")
    a = dict(version=2, algorithm="Ed25519", key_id=kid, incarnation=p["incarnation"], operation="steady_sources",
             plan_sha256=recovery.digest(p), actor_id=p["actor_id"], reference=args.reference,
             evidence_sha256=recovery.reviewed_evidence(args.evidence_manifest),
             expires_ms=p["expires_ms"], signature="")
    a["signature"] = signer.sign(recovery.canonical(a)).hex()
    return a


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("plan", "approve", "activate"))
    for name in ("source", "original-plan-file", "output"):
        parser.add_argument("--" + name, required=True)
    parser.add_argument("--approved-source", action="append", default=[])
    parser.add_argument("--authorization-days", type=int, default=7)
    parser.add_argument("--revoke-only", action="store_true")
    for name in ("plan-file", "approval-file", "approval-key-file", "reference", "evidence-manifest",
                 "base-url", "session-token-file"):
        parser.add_argument("--" + name)
    args = parser.parse_args()
    old = json.loads(Path(args.original_plan_file).read_text())
    if args.action == "plan":
        result = recovery.request(args, "steady/plan", {
            "sources": args.approved_source, "authorization_days": args.authorization_days})
        validate(result, old, args)
    else:
        envelope = json.loads(Path(args.plan_file).read_text())
        validate(envelope, old, args)
        if args.action == "approve":
            result = approve(envelope, old, args)
        else:
            approval = json.loads(Path(args.approval_file).read_text())
            result = recovery.request(args, "steady/activate", {"plan": envelope["plan"], "approval": approval})
    recovery.write_new(args.output, result)
    print(json.dumps({"operation": args.action, "shared_queue_resumed": False, "automatic_retry": False}))


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        print(json.dumps({"error": "steady_policy_rejected", "category": type(error).__name__}))
        raise SystemExit(1)
