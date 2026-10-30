"""Separate, offline-signed approval of a bounded Feishu continuation lane.

This client never connects to Redis and never resumes the shared sync queue.
The original recovery plan binds the incident; the new plan binds the current
post-canary process, source configuration/authority and unchanged queue bytes.
"""
import argparse
import json
from pathlib import Path
import re
import sys
import time
import uuid

import feishu_recovery as recovery


def validate(envelope, original, args, now_ms=None):
    p = envelope["plan"]
    old = original["plan"]
    now = int(time.time() * 1000) if now_ms is None else now_ms
    if (original["plan_sha256"] != recovery.digest(old) or
            old.get("namespace") != args.namespace or old.get("trip") != args.trip or
            old.get("incident_id") != args.incident or old.get("app_sha") != args.runtime_sha):
        raise ValueError("reviewed original incident does not match")
    if (p.get("version") != 1 or envelope["plan_sha256"] != recovery.digest(p) or
            p.get("namespace") != args.namespace or p.get("runtime_sha") != args.runtime_sha or
            p.get("recovery_plan_sha256") != recovery.digest(old) or
            p.get("historical_pending") != args.expected_pending or
            p.get("historical_queue_sha256") != old.get("queue_sha256") or
            p.get("worker_id") != old.get("worker_id") or
            p.get("membership_revision") != str(int(old["membership_revision"]) + 1) or
            p.get("approval_key_id") != old.get("approval_key_id") or
            p.get("actor_id") != old.get("actor_id")):
        raise ValueError("continuation incident/runtime/queue identity mismatch")
    if (p["created_ms"] > now + 5000 or p["expires_ms"] <= now or
            not 0 < p["expires_ms"] - p["created_ms"] <= 3600000):
        raise ValueError("continuation approval window invalid")
    if (str(uuid.UUID(p["incarnation"])) != p["incarnation"] or
            p["incarnation"] == old.get("incarnation")):
        raise ValueError("current canonical process identity required")
    sources = p.get("sources", [])
    ids = [s["source_id"] for s in sources]
    if (not 1 <= len(ids) <= 4 or ids != sorted(set(ids)) or
            sorted(args.approved_source) != ids or args.source not in ids):
        raise ValueError("exact approved source selection mismatch")
    for s in sources:
        if str(uuid.UUID(s["source_id"])) != s["source_id"]:
            raise ValueError("canonical source identity required")
        if any(not re.fullmatch(r"[0-9a-f]{64}", s.get(k, ""))
               for k in ("config_sha256", "facts_sha256")):
            raise ValueError("source configuration/authority fingerprint required")
    return p


def sign(envelope, original, args):
    p = validate(envelope, original, args)
    if not 1 <= args.valid_seconds <= 3600:
        raise ValueError("continuation approval must be valid for 1..3600 seconds")
    if not re.fullmatch(r"[A-Za-z0-9:._/-]{1,180}", args.reference) or "://" in args.reference:
        raise ValueError("bounded approval reference required")
    from cryptography.hazmat.primitives import serialization
    from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
    signer = serialization.load_pem_private_key(recovery.restricted_read(args.approval_key_file), password=None)
    if not isinstance(signer, Ed25519PrivateKey):
        raise ValueError("offline Ed25519 signer required")
    import hashlib
    public = signer.public_key().public_bytes(serialization.Encoding.Raw, serialization.PublicFormat.Raw)
    key_id = hashlib.sha256(public).hexdigest()
    if key_id != p["approval_key_id"]:
        raise ValueError("signer differs from runtime trust anchor")
    a = {"version": 2, "algorithm": "Ed25519", "key_id": key_id,
         "incarnation": p["incarnation"], "operation": "approved_sources",
         "plan_sha256": recovery.digest(p), "actor_id": p["actor_id"],
         "reference": args.reference, "evidence_sha256": recovery.reviewed_evidence(args.evidence_manifest),
         "expires_ms": min(p["expires_ms"], int(time.time() * 1000) + args.valid_seconds * 1000),
         "signature": ""}
    a["signature"] = signer.sign(recovery.canonical(a)).hex()
    return a


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=["plan", "approve", "activate"])
    for key in ("source", "namespace", "trip", "incident", "runtime-sha", "original-plan-file", "output"):
        parser.add_argument("--" + key, required=True)
    parser.add_argument("--approved-source", action="append", required=True)
    parser.add_argument("--expected-pending", type=int, default=6)
    parser.add_argument("--base-url")
    parser.add_argument("--session-token-file")
    parser.add_argument("--plan-file")
    parser.add_argument("--approval-file")
    parser.add_argument("--approval-key-file")
    parser.add_argument("--evidence-manifest")
    parser.add_argument("--reference")
    parser.add_argument("--valid-seconds", type=int, default=900)
    args = parser.parse_args(argv)
    if not re.fullmatch(r"[0-9a-f]{40}", args.runtime_sha):
        raise ValueError("exact runtime commit SHA required")
    original = json.loads(Path(args.original_plan_file).read_text(encoding="utf-8"))
    if args.action == "plan":
        if not args.base_url or not args.session_token_file:
            parser.error("plan requires an authenticated local tunnel or HTTPS session")
        result = recovery.request(args, "continuity/plan", {"sources": args.approved_source})
        validate(result, original, args)
    else:
        if not args.plan_file:
            parser.error("reviewed saved continuation plan required")
        envelope = json.loads(Path(args.plan_file).read_text(encoding="utf-8"))
        p = validate(envelope, original, args)
        if args.action == "approve":
            if not all([args.reference, args.approval_key_file, args.evidence_manifest]):
                parser.error("offline approval requires separate key, reference and reviewed evidence")
            result = sign(envelope, original, args)
        else:
            if not all([args.approval_file, args.base_url, args.session_token_file]):
                parser.error("activation requires a separately signed approval and authenticated session")
            a = json.loads(Path(args.approval_file).read_text(encoding="utf-8"))
            if a.get("operation") != "approved_sources" or a.get("plan_sha256") != recovery.digest(p):
                raise ValueError("matching independent approval required")
            result = recovery.request(args, "continuity/activate", {"plan": p, "approval": a})
    recovery.write_new(args.output, result)
    print(json.dumps({"operation": args.action, "shared_queue_resumed": False}))


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        print(json.dumps({"error": "continuity_client_failed", "category": type(error).__name__}), file=sys.stderr)
        raise SystemExit(1)
