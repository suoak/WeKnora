"""Incident-bound Feishu operations client. Never connects to Redis directly.

plan/status are read-only. approve is an OFFLINE approver operation, requiring
an explicit reviewed evidence manifest and an independent restricted key file.
execute requires a separately issued approval; it never signs one implicitly.
There is deliberately no global resume, namespace switch or automatic retry.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid


def canonical(value):
    raw = json.dumps(value, sort_keys=True, ensure_ascii=False, separators=(",", ":"))
    for char, escaped in [("<", "\\u003c"), (">", "\\u003e"), ("&", "\\u0026"),
                          ("\u2028", "\\u2028"), ("\u2029", "\\u2029")]:
        raw = raw.replace(char, escaped)
    return raw.encode("utf-8")


def digest(value):
    return hashlib.sha256(canonical(value)).hexdigest()


def restricted_read(path):
    if os.name == "nt":
        raise ValueError("run secret-bearing operations on the managed Unix host; Windows ACL attestation is not implemented")
    path = Path(path)
    info = path.stat()
    if not stat.S_ISREG(info.st_mode) or path.is_symlink():
        raise ValueError("restricted regular file required")
    if os.name != "nt" and stat.S_IMODE(info.st_mode) & 0o077:
        raise ValueError("secret file must have mode 0600 or stricter")
    if info.st_size > 1024 * 1024:
        raise ValueError("restricted file is unexpectedly large")
    return path.read_bytes()


def write_new(path, value):
    # An existing approval or plan is never overwritten.
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(descriptor, "w", encoding="utf-8") as stream:
        json.dump(value, stream, ensure_ascii=False, indent=2)


def validate_plan(envelope, args, now_ms=None):
    plan = envelope["plan"]
    now = int(time.time() * 1000) if now_ms is None else now_ms
    checks = {
        "namespace": args.namespace,
        "trip": args.trip,
        "incident_id": args.incident,
        "app_sha": args.runtime_sha,
    }
    if plan.get("version") != 1 or any(plan.get(key) != expected for key, expected in checks.items()):
        raise ValueError("incident/runtime identity mismatch")
    if str(uuid.UUID(args.source)) != args.source or plan["facts"]["source_id"] != args.source:
        raise ValueError("source identity mismatch")
    if not 1 <= plan["facts"].get("selected_resource_count",0) <= 64:
        raise ValueError("explicit bounded source resource selection required")
    if not plan["facts"]["eligible"] or plan["facts"]["live_executions"] or plan["facts"]["live_publications"]:
        raise ValueError("source or execution is not eligible")
    if not 0 < plan["expires_ms"] - now <= 15 * 60 * 1000:
        raise ValueError("plan expired or window too large")
    if plan.get("queue_pending_count") != args.expected_pending:
        raise ValueError("shared queue count differs from reviewed incident baseline")
    if envelope["plan_sha256"] != digest(plan):
        raise ValueError("plan digest mismatch")
    return plan


def reviewed_evidence(path):
    """Check artifact integrity. Semantic approval remains the approver's duty."""
    raw = Path(path).read_bytes()
    manifest = json.loads(raw)
    required = {"backup_restore", "isolation_safety", "source_authorization", "runtime_identity"}
    artifacts = manifest.get("artifacts", [])
    if {item.get("kind") for item in artifacts} != required:
        raise ValueError("all four reviewed evidence categories are required")
    for item in artifacts:
        root=Path(path).parent.resolve()
        relative=Path(item["file"])
        if relative.is_absolute() or ".." in relative.parts:
            raise ValueError("evidence must be a contained relative artifact")
        artifact=(root/relative).resolve()
        if not artifact.is_relative_to(root):
            raise ValueError("evidence symlink escapes its review directory")
        if not artifact.is_file() or hashlib.sha256(artifact.read_bytes()).hexdigest() != item["sha256"]:
            raise ValueError("evidence artifact integrity mismatch")
    if manifest.get("review_outcome") != "accepted" or not manifest.get("reviewer"):
        raise ValueError("independent review has not accepted the evidence")
    return hashlib.sha256(raw).hexdigest()


def approve(envelope, args):
    plan = validate_plan(envelope, args)
    if not re.fullmatch(r"[A-Za-z0-9:._/-]{1,180}", args.reference) or "://" in args.reference:
        raise ValueError("use a bounded approval reference, not a URL or credential")
    key = restricted_read(args.approval_key_file)
    from cryptography.hazmat.primitives import serialization
    from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
    signer=serialization.load_pem_private_key(key,password=None)
    if not isinstance(signer,Ed25519PrivateKey):
        raise ValueError("an offline Ed25519 private key is required")
    public=signer.public_key().public_bytes(serialization.Encoding.Raw,serialization.PublicFormat.Raw)
    key_id=hashlib.sha256(public).hexdigest()
    if plan.get("approval_key_id")!=key_id:
        raise ValueError("offline signer does not match the reviewed runtime public key")
    incarnation=plan["incarnation"]
    if args.operation=="canary":
        if not getattr(args,"recovery_status_file",None):
            raise ValueError("canary needs the post-reset runtime status, not a pre-signed approval")
        status=json.loads(Path(args.recovery_status_file).read_text(encoding="utf-8"))
        state=status.get("state",{})
        if (status.get("trip_latched") is not False or status.get("sync_paused") is not True or
            status.get("permit_ready") is not True or state.get("phase")!="frozen" or
            state.get("plan_sha256")!=digest(plan) or state.get("source_id")!=args.source or
            state.get("runtime_sha")!=args.runtime_sha or state.get("worker_id")!=plan["worker_id"]):
            raise ValueError("review the exact frozen post-reset process before canary approval")
        incarnation=state["incarnation"]
    if str(uuid.UUID(incarnation))!=incarnation:
        raise ValueError("canonical reviewed process incarnation required")
    record = {
        "version": 2, "algorithm":"Ed25519", "key_id":key_id,"incarnation":incarnation, "operation": args.operation,
        "plan_sha256": digest(plan), "actor_id": plan["actor_id"],
        "reference": args.reference, "evidence_sha256": reviewed_evidence(args.evidence_manifest),
        "expires_ms": min(plan["expires_ms"], int(time.time() * 1000) + args.valid_seconds * 1000),
        "signature": "",
    }
    if not 1 <= args.valid_seconds <= 900:
        raise ValueError("approval must be valid for 1..900 seconds")
    record["signature"] = signer.sign(canonical(record)).hex()
    return record


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        raise ValueError("redirect forbidden for authenticated recovery requests")


def request(args, operation, body=None):
    base = urllib.parse.urlsplit(args.base_url)
    if base.scheme != "https" and not (base.scheme == "http" and base.hostname in ("127.0.0.1", "localhost", "::1")):
        raise ValueError("HTTPS or an authenticated local SSH tunnel is required")
    if base.username or base.password or base.query or base.fragment:
        raise ValueError("credentials/query are forbidden in base URL")
    if str(uuid.UUID(args.source)) != args.source:
        raise ValueError("canonical source UUID required")
    token = restricted_read(args.session_token_file).decode().strip()
    if not token or "\n" in token or "\r" in token:
        raise ValueError("invalid session token file")
    url = args.base_url.rstrip("/") + "/api/v1/datasource/" + args.source + "/safety-recovery/" + operation
    req = urllib.request.Request(url, data=canonical(body) if body is not None else None,
                                 headers={"Authorization": "Bearer " + token, "Content-Type": "application/json"})
    # No retry: a lost acknowledgment may follow a committed transition/run.
    with urllib.request.build_opener(NoRedirect()).open(req, timeout=900 if operation == "execute" else 15) as response:
        return json.load(response)


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=["plan", "status", "approve", "execute"])
    parser.add_argument("--source", required=True)
    parser.add_argument("--namespace", required=True)
    parser.add_argument("--trip", required=True)
    parser.add_argument("--incident", required=True)
    parser.add_argument("--runtime-sha", required=True)
    parser.add_argument("--expected-pending", type=int, default=6)
    parser.add_argument("--base-url")
    parser.add_argument("--session-token-file")
    parser.add_argument("--plan-file")
    parser.add_argument("--approval-file")
    parser.add_argument("--approval-key-file")
    parser.add_argument("--recovery-status-file")
    parser.add_argument("--evidence-manifest")
    parser.add_argument("--reference")
    parser.add_argument("--operation", choices=["reset", "canary"])
    parser.add_argument("--valid-seconds", type=int, default=300)
    parser.add_argument("--output")
    args = parser.parse_args(argv)
    if not re.fullmatch(r"[a-f0-9]{40}", args.runtime_sha):
        parser.error("exact runtime commit SHA required")
    if args.action in ("plan", "status", "execute") and not all([args.base_url, args.session_token_file]):
        parser.error("API operations require base URL and restricted session file")
    if args.action in ("approve", "execute") and not args.plan_file:
        parser.error("a saved incident plan is required")
    if args.action == "plan" and not args.output:
        parser.error("plan requires a new output file")
    if args.action == "execute" and not args.approval_file:
        parser.error("execute requires independent approval file")
    if args.action == "plan":
        result = request(args, "plan"); validate_plan(result, args)
        write_new(args.output, result)
        print(json.dumps({"plan_sha256": result["plan_sha256"], "production_writes": False}))
    elif args.action == "status":
        result=request(args,"status")
        if args.output:write_new(args.output,result)
        print(json.dumps(result))
    else:
        envelope = json.loads(Path(args.plan_file).read_text(encoding="utf-8"))
        plan = validate_plan(envelope, args)
        if args.action == "approve":
            if not all([args.operation, args.reference, args.approval_key_file, args.evidence_manifest, args.output]):
                parser.error("offline approval requires operation/reference/key/evidence/output")
            record = approve(envelope, args);write_new(args.output, record)
            print(json.dumps({"operation": record["operation"], "plan_sha256": record["plan_sha256"], "production_writes": False}))
        else:
            approval = json.loads(Path(args.approval_file).read_text(encoding="utf-8"))
            if approval["plan_sha256"] != digest(plan) or approval["operation"] not in ["reset", "canary"]:
                raise ValueError("independent matching approval required")
            print(json.dumps(request(args, "execute", {"plan": plan, "approval": approval})))


if __name__ == "__main__":
    try:
        main()
    except urllib.error.HTTPError as error:
        print(json.dumps({"error": "recovery_http_rejected", "http_status": error.code}), file=sys.stderr)
        raise SystemExit(1)
    except Exception as error:
        # Do not echo URLs, tokens, remote exception messages or secret paths.
        print(json.dumps({"error": "recovery_client_failed", "category": type(error).__name__}), file=sys.stderr)
        raise SystemExit(1)
