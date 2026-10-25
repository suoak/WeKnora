"""Bounded, GET-only Feishu permission probe. No application/DB/queue writes.

No credentials are read unless --execute is supplied. Fixture IDs belong in a
private manifest; tokens and the expected user open_id belong in environment
variables. Reports intentionally exclude URLs, names, bodies and credentials.
"""
import argparse
import datetime
import hashlib
import json
import os
import re
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path

ORIGIN = "https://open.feishu.cn"
MAX_BYTES = 1024 * 1024
MAX_CASES = 20
ID = re.compile(r"^[A-Za-z0-9_-]{1,256}$")


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def endpoint(case):
    """Accept operations/identifiers, never a caller-provided URL or path."""
    op = case.get("operation")
    params = {}

    def ident(key):
        value = case.get(key, "")
        if not isinstance(value, str) or not ID.fullmatch(value):
            raise ValueError("invalid_fixture_identifier")
        return value

    if op == "root":
        path = "/drive/explorer/v2/root_folder/meta"
    elif op == "folder":
        path = "/drive/v1/files"
        params = {"folder_token": ident("resource"), "page_size": 20}
    elif op == "wiki_spaces":
        path, params = "/wiki/v2/spaces", {"page_size": 20}
    elif op == "wiki_nodes":
        path = "/wiki/v2/spaces/" + ident("resource") + "/nodes"
        params = {"page_size": 20}
        if "parent" in case:
            params["parent_node_token"] = ident("parent")
    elif op == "wiki_node":
        path, params = "/wiki/v2/spaces/get_node", {"token": ident("resource")}
    elif op == "docx":
        path = "/docx/v1/documents/" + ident("resource")
    elif op == "docx_blocks":
        path = "/docx/v1/documents/" + ident("resource") + "/blocks"
        params = {"page_size": 20}
    elif op == "sheet":
        path = "/sheets/v3/spreadsheets/" + ident("resource")
    elif op == "sheet_values":
        # One small range; no unbounded sheet read or formula evaluation.
        path = "/sheets/v2/spreadsheets/" + ident("resource") + "/values/" + ident("sheet_id") + "!A1:B2"
    elif op == "bitable_tables":
        path = "/bitable/v1/apps/" + ident("resource") + "/tables"
        params = {"page_size": 20}
    elif op == "bitable_records":
        path = "/bitable/v1/apps/" + ident("resource") + "/tables/" + ident("table_id") + "/records"
        params = {"page_size": 20}
    elif op == "collaborators":
        kind = case.get("resource_type")
        if kind not in ("docx", "sheet", "bitable", "file", "folder"):
            raise ValueError("invalid_resource_type")
        path = "/drive/v1/permissions/" + ident("resource") + "/members"
        params = {"type": kind}
    else:
        raise ValueError("unsupported_readonly_operation")
    return ORIGIN + "/open-apis" + path + ("?" + urllib.parse.urlencode(params) if params else "")


def request_json(opener, url, token):
    req = urllib.request.Request(url, headers={"Authorization": "Bearer " + token}, method="GET")
    try:
        response = opener.open(req, timeout=15)
    except urllib.error.HTTPError as exc:
        response = exc
    except (OSError, ValueError):
        return {"transport": "unavailable"}, None
    with response:
        status = response.getcode()
        # Never read a redirect body or follow it with an Authorization header.
        if 300 <= status < 400:
            return {"http_status": status, "transport": "redirect_rejected"}, None
        raw = response.read(MAX_BYTES + 1)
    if len(raw) > MAX_BYTES:
        return {"http_status": status, "transport": "response_too_large"}, None
    try:
        body = json.loads(raw)
    except (ValueError, UnicodeError):
        return {"http_status": status, "transport": "invalid_json"}, None
    if not isinstance(body, dict) or type(body.get("code")) is not int:
        return {"http_status": status, "transport": "invalid_envelope"}, None
    result = {"http_status": status, "provider_code": body["code"]}
    return result, body


def validate_manifest(manifest):
    if manifest.get("identity") not in ("user_oauth", "application_grant"):
        raise ValueError("identity_required")
    roles = ("A_owner", "B_reader", "C_no_access")
    if manifest["identity"] == "application_grant":
        roles += ("application_probe",)
    if manifest.get("role") not in roles:
        raise ValueError("fixture_role_required")
    cases = manifest.get("cases")
    if not isinstance(cases, list) or not 1 <= len(cases) <= MAX_CASES:
        raise ValueError("bounded_cases_required")
    for case in cases:
        endpoint(case)
        if case.get("expected") not in ("allowed", "denied"):
            raise ValueError("expected_outcome_required")
        codes = case.get("denial_codes", [])
        if case["expected"] == "denied" and (not isinstance(codes, list) or not codes or
                any(type(code) is not int or code <= 0 for code in codes)):
            raise ValueError("explicit_permission_denial_codes_required")


def has_expected_data(operation, body):
    data = body.get("data") if isinstance(body, dict) else None
    if not isinstance(data, dict):
        return False
    fields = {"root": ("token", str), "folder": ("files", list),
              "wiki_spaces": ("items", list), "wiki_nodes": ("items", list),
              "wiki_node": ("node", dict), "docx": ("document", dict),
              "docx_blocks": ("items", list), "sheet": ("spreadsheet", dict),
              "sheet_values": ("valueRange", dict), "bitable_tables": ("items", list),
              "bitable_records": ("items", list), "collaborators": ("members", list)}
    field, kind = fields[operation]
    return isinstance(data.get(field), kind) and (kind is not str or bool(data[field]))


def run(manifest, execute=False, env=None, opener=None, delay=time.sleep):
    validate_manifest(manifest)
    report = {"status": "BLOCKED", "executed": False, "identity": manifest["identity"],
              "role": manifest["role"], "cases": [],
              "time_utc": datetime.datetime.now(datetime.timezone.utc).isoformat(),
              "scope": "one_page_reads_only_not_import_export_or_oauth_acceptance"}
    if not execute:
        report["reason"] = "execution_not_requested"
        return report
    env = os.environ if env is None else env
    token = env.get("FEISHU_POC_USER_TOKEN" if manifest["identity"] == "user_oauth" else "FEISHU_POC_APP_TOKEN", "")
    expected_id = env.get("FEISHU_POC_EXPECTED_OPEN_ID", "")
    if not token or "\n" in token or "\r" in token:
        report["reason"] = "test_identity_token_required"
        return report
    if manifest["identity"] == "user_oauth" and not expected_id:
        report["reason"] = "expected_user_identity_required"
        return report
    opener = opener or urllib.request.build_opener(NoRedirect())
    if manifest["identity"] == "user_oauth":
        result, body = request_json(opener, ORIGIN + "/open-apis/authen/v1/user_info", token)
        if result.get("http_status") != 200 or result.get("provider_code") != 0 or not isinstance(body.get("data"), dict) or body["data"].get("open_id") != expected_id:
            report["reason"] = "user_identity_not_verified"
            return report
        delay(0.5)
    report["executed"] = True
    for index, case in enumerate(manifest["cases"]):
        if index:
            delay(0.5)
        result, body = request_json(opener, endpoint(case), token)
        allowed = result.get("http_status") == 200 and result.get("provider_code") == 0 and has_expected_data(case["operation"], body)
        denied = result.get("http_status") in (200, 400, 403, 404) and result.get("provider_code") in case.get("denial_codes", [])
        matches = allowed if case["expected"] == "allowed" else denied
        report["cases"].append({"case": index + 1, "operation": case["operation"],
                                "fixture_hash": hashlib.sha256(json.dumps(case, sort_keys=True).encode()).hexdigest(),
                                "expected": case["expected"], "status": "PASS" if matches else "FAIL", **result})
        if allowed and "has_more" in body["data"]:
            report["cases"][-1]["has_more"] = body["data"]["has_more"] is True
    report["status"] = "PASS" if all(c["status"] == "PASS" for c in report["cases"]) else "FAIL"
    return report


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("manifest", type=Path)
    parser.add_argument("--execute", action="store_true")
    parser.add_argument("--output", type=Path)
    args = parser.parse_args()
    try:
        report = run(json.loads(args.manifest.read_text(encoding="utf-8")), args.execute)
    except (ValueError, TypeError, KeyError, AttributeError, OSError):
        report = {"status": "BLOCKED", "executed": False, "reason": "invalid_manifest_or_transport"}
    encoded = json.dumps(report, indent=2, ensure_ascii=False) + "\n"
    if args.output:
        args.output.write_text(encoded, encoding="utf-8")
    else:
        print(encoded, end="")
    return 0 if report["status"] == "PASS" else 2


if __name__ == "__main__":
    sys.exit(main())
