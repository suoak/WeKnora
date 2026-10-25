"""One bounded synthetic embedding request; credential arrives on stdin only."""
import argparse
import json
import math
import sys
import urllib.error
import urllib.request


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--execute", action="store_true")
    args = parser.parse_args()
    if not args.execute:
        print(json.dumps({"status": "NOT_EXECUTED", "requests": 0}))
        return 2
    try:
        credential = json.loads(sys.stdin.read(16384))
        if credential.get("base_url") != "https://uniapi.ruijie.com.cn/v1":
            raise ValueError("unexpected_gateway")
        key = credential["api_key"]
        if not isinstance(key, str) or not key or "\n" in key or "\r" in key:
            raise ValueError("invalid_credential")
        body = json.dumps({"model": "text-embedding-v4", "input": ["KnowHub Feishu V2 isolated connectivity check"]}).encode()
        request = urllib.request.Request(credential["base_url"] + "/embeddings", data=body,
                                         headers={"Content-Type": "application/json", "Authorization": "Bearer " + key})
        credential.clear()
        key = None
        with urllib.request.build_opener(NoRedirect()).open(request, timeout=30) as response:
            raw = response.read(2 * 1024 * 1024 + 1)
            if len(raw) > 2 * 1024 * 1024:
                raise ValueError("response_too_large")
            result = json.loads(raw)
            rows = result["data"]
            vector = rows[0]["embedding"]
            if len(rows) != 1 or not vector or not all(isinstance(v, (int, float)) and math.isfinite(v) for v in vector):
                raise ValueError("invalid_embedding")
            print(json.dumps({"status": "PASS", "requests": 1, "http_status": response.status,
                              "requested_model": "text-embedding-v4", "returned_model": result.get("model"),
                              "dimensions": len(vector), "scope": "synthetic_gateway_connectivity_only"}))
            return 0
    except urllib.error.HTTPError as error:
        print(json.dumps({"status": "FAILED", "requests": 1, "http_status": error.code,
                          "reason": "gateway_request_rejected"}))
    except (OSError, ValueError, KeyError, TypeError, IndexError):
        print(json.dumps({"status": "FAILED", "reason": "gateway_or_response_unavailable"}))
    return 1


if __name__ == "__main__":
    sys.exit(main())
