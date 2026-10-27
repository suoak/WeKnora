import io
import json
import unittest
import urllib.error
from unittest.mock import patch
import contextlib

import readonly_probe as probe
import application_credentials_probe as app_probe


class Response(io.BytesIO):
    def __init__(self, data, code=200):
        super().__init__(data)
        self.code = code

    def getcode(self):
        return self.code


class Opener:
    def __init__(self, responses):
        self.responses = list(responses)
        self.requests = []

    def open(self, req, timeout):
        assert timeout == 15
        assert req.method == "GET"
        assert req.full_url.startswith(probe.ORIGIN + "/open-apis/")
        self.requests.append(req)
        return self.responses.pop(0)


class ReadonlyProbeTests(unittest.TestCase):
    def manifest(self, expected="allowed", **kwargs):
        return {"identity": "user_oauth", "role": "B_reader", "cases": [
            {"operation": "docx_blocks", "resource": "fixture", "expected": expected, **kwargs}]}

    def execute(self, manifest, responses, expected_id="owner"):
        opener = Opener([Response(b'{"code":0,"data":{"open_id":"owner"}}')] + responses)
        report = probe.run(manifest, True, {"FEISHU_POC_USER_TOKEN": "sensitive-token",
                           "FEISHU_POC_EXPECTED_OPEN_ID": expected_id}, opener, lambda _: None)
        return report, opener

    def test_default_and_missing_credentials_never_call_network(self):
        opener = Opener([])
        for execute in (False, True):
            report = probe.run(self.manifest(), execute, {}, opener)
            self.assertEqual(report["status"], "BLOCKED")
        self.assertEqual(opener.requests, [])

    def test_success_has_no_content_identity_or_token_in_report(self):
        report, opener = self.execute(self.manifest(), [Response(b'{"code":0,"data":{"items":["private-content"]}}')])
        self.assertEqual(report["status"], "PASS")
        self.assertEqual(len(opener.requests), 2)
        for private in ("sensitive-token", "private-content", '"owner"', '"fixture"'):
            self.assertNotIn(private, json.dumps(report))

    def test_identity_mismatch_stops_before_resource_read(self):
        report, opener = self.execute(self.manifest(), [], "different-owner")
        self.assertEqual(report["status"], "BLOCKED")
        self.assertEqual(len(opener.requests), 1)

    def test_oauth_success_does_not_prove_resource_read(self):
        for body, status in [(b'{"code":1770032}', 403), (b'{}', 200),
                             (b'{"code":0}', 200), (b'login-html', 200),
                             (b'{"code":true}', 200), (b'{"code":0,"data":{}}', 302),
                             (b'x' * (probe.MAX_BYTES + 1), 200)]:
            with self.subTest(status=status, length=len(body)):
                report, _ = self.execute(self.manifest(), [Response(body, status)])
                self.assertEqual(report["status"], "FAIL")

    def test_only_explicit_permission_denial_counts_as_expected_denial(self):
        manifest = self.manifest("denied", denial_codes=[1770032])
        for code, expected in [(1770032, "PASS"), (99991663, "FAIL"), (99991400, "FAIL"), (0, "FAIL")]:
            report, _ = self.execute(manifest, [Response(json.dumps({"code": code}).encode(), 403)])
            self.assertEqual(report["status"], expected)

    def test_invalid_fixture_and_unbounded_manifest_rejected(self):
        for resource in ("../other", "a?token=secret", "https://evil.test", "a/b", "a\n", ""):
            manifest = self.manifest()
            manifest["cases"][0]["resource"] = resource
            with self.assertRaises(ValueError):
                probe.validate_manifest(manifest)
        manifest = self.manifest()
        manifest["cases"] *= 21
        with self.assertRaises(ValueError):
            probe.validate_manifest(manifest)
        with self.assertRaises(ValueError):
            probe.validate_manifest(self.manifest("denied"))
        with self.assertRaises(ValueError):
            probe.endpoint({"operation": "export_task"})

    def test_no_redirect_handler(self):
        self.assertIsNone(probe.NoRedirect().redirect_request(None, None, 302, "", {}, "https://evil.test"))

    def test_application_credential_probe_does_not_output_secret_or_token(self):
        class AppOpener:
            def __init__(self):
                self.calls = []

            def open(self, req, timeout):
                self.calls.append(req)
                if req.method == "POST":
                    self_test.assertEqual(req.full_url, probe.ORIGIN + "/open-apis/auth/v3/tenant_access_token/internal")
                    return Response(b'{"code":0,"tenant_access_token":"private-app-token"}')
                self_test.assertEqual(req.method, "GET")
                if req.full_url.endswith("root_folder/meta"):
                    return Response(b'{"code":0,"data":{"token":"private-root-token"}}')
                return Response(b'{"code":0,"data":{"items":[],"has_more":false}}')

        self_test = self
        opener = AppOpener()
        output = io.StringIO()
        with patch("sys.stdin", io.StringIO('{"app_id":"cli_test","app_secret":"private-app-secret"}')), \
                patch("urllib.request.build_opener", return_value=opener), contextlib.redirect_stdout(output):
            self.assertEqual(app_probe.main(), 0)
        report = output.getvalue()
        for private in ("private-app-secret", "private-app-token", "private-root-token"):
            self.assertNotIn(private, report)
        self.assertEqual(len(opener.calls), 3)
        self.assertEqual(json.loads(report)["employee_oauth"], "BLOCKED")

    def test_user_cannot_select_application_probe_role(self):
        manifest = self.manifest()
        manifest["role"] = "application_probe"
        with self.assertRaises(ValueError):
            probe.validate_manifest(manifest)
        self.assertFalse(probe.has_expected_data("root", {"data": {"token": ""}}))


if __name__ == "__main__":
    unittest.main()
