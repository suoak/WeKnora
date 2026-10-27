"""Read App ID/Secret from stdin; test tenant token and bounded GET operations.

Input must come from a trusted secret provider, never a committed fixture.
Tokens remain in memory. The output contains no credentials or document data.
This does not verify employee OAuth, document content, export or import.
"""
import json
import sys
import urllib.error
import urllib.request

import readonly_probe as probe


def main():
    result = {"status": "BLOCKED", "reason": "application_credentials_not_verified"}
    try:
        credentials = json.loads(sys.stdin.read(8193))
        app_id, secret = credentials.get("app_id"), credentials.get("app_secret")
        if not isinstance(app_id, str) or not app_id.startswith("cli_") or not isinstance(secret, str) or not secret:
            raise ValueError("invalid_credentials")
        opener = urllib.request.build_opener(probe.NoRedirect())
        req = urllib.request.Request(probe.ORIGIN + "/open-apis/auth/v3/tenant_access_token/internal",
                                     data=json.dumps({"app_id": app_id, "app_secret": secret}).encode(),
                                     headers={"Content-Type": "application/json"}, method="POST")
        with opener.open(req, timeout=15) as response:
            raw = response.read(probe.MAX_BYTES + 1)
            if len(raw) > probe.MAX_BYTES or response.getcode() != 200:
                raise ValueError("invalid_response")
        body = json.loads(raw)
        token = body.get("tenant_access_token")
        if type(body.get("code")) is not int or body["code"] != 0 or not isinstance(token, str) or not token:
            result["provider_code"] = body.get("code") if type(body.get("code")) is int else None
        else:
            manifest = {"identity": "application_grant", "role": "application_probe", "cases": [
                {"operation": "root", "expected": "allowed"},
                {"operation": "wiki_spaces", "expected": "allowed"}]}
            result = probe.run(manifest, True, {"FEISHU_POC_APP_TOKEN": token}, opener)
            result["application_credentials"] = "PASS"
            result["employee_oauth"] = "BLOCKED"
            result["document_content_export_import"] = "BLOCKED"
    except (ValueError, TypeError, AttributeError, OSError):
        # HTTP/provider exceptions can embed private response bodies or URLs.
        pass
    print(json.dumps(result, indent=2))
    return 0 if result["status"] == "PASS" else 2


if __name__ == "__main__":
    sys.exit(main())
