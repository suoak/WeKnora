"""Offline safety checks. Uses only synthetic files and mocks, never production."""
import hashlib
import hmac
import importlib.util
import json
from pathlib import Path
import tempfile
import time
from types import SimpleNamespace
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('recovery', Path(__file__).with_name('feishu_recovery.py'))
r = importlib.util.module_from_spec(spec)
spec.loader.exec_module(r)

class RecoveryClientTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        now = int(time.time()*1000)
        self.args = SimpleNamespace(expected_pending=6, namespace='synthetic', trip='feishu_headers_io_failure', incident='1-0', runtime_sha='a'*40,
            source='5c7bc873-0ba3-43c7-90a0-32c23e6bd529', reference='test-review', operation='reset', valid_seconds=60,
            approval_key_file=str(self.root/'key'), evidence_manifest=str(self.root/'evidence.json'),
            base_url='https://example.invalid', session_token_file=str(self.root/'session'))
        for name in ('key', 'session'):
            p=self.root/name;p.write_bytes(b'k'*32);p.chmod(0o600)
        plan=dict(version=1, namespace=self.args.namespace, trip=self.args.trip, incident_id='1-0', app_sha='a'*40,
                  queue_pending_count=6, actor_id='operator', expires_ms=now+300000,
                  facts=dict(source_id=self.args.source, selected_resource_count=1, eligible=True, live_executions=0, live_publications=0))
        self.envelope=dict(plan=plan, plan_sha256=r.digest(plan))
        artifacts=[]
        for kind in ('backup_restore','isolation_safety','source_authorization','runtime_identity'):
            p=self.root/(kind+'.txt');p.write_text('synthetic offline test')
            artifacts.append(dict(kind=kind,file=p.name,sha256=hashlib.sha256(p.read_bytes()).hexdigest()))
        (self.root/'evidence.json').write_text(json.dumps(dict(artifacts=artifacts,reviewer='synthetic',review_outcome='accepted')))

    def test_offline_approval_signature_has_no_network(self):
        with patch.object(r.urllib.request,'build_opener',side_effect=AssertionError('network forbidden')), patch.object(r,'restricted_read',return_value=b'k'*32):
            a=r.approve(self.envelope,self.args)
        signature=a.pop('signature');a['signature']=''
        self.assertEqual(signature,hmac.new(b'k'*32,r.canonical(a),hashlib.sha256).hexdigest())

    def test_plan_changes_fail_even_if_digest_recomputed(self):
        for field in ('namespace','trip','incident_id','app_sha'):
            with self.subTest(field=field):
                obj=json.loads(json.dumps(self.envelope));obj['plan'][field]='wrong';obj['plan_sha256']=r.digest(obj['plan'])
                self.assertRaises(ValueError,r.validate_plan,obj,self.args)

    def test_expired_and_tampered_plan_fail(self):
        obj=json.loads(json.dumps(self.envelope));obj['plan']['expires_ms']=1
        self.assertRaises(ValueError,r.validate_plan,obj,self.args)
        self.envelope['plan_sha256']='0'*64
        self.assertRaises(ValueError,r.validate_plan,self.envelope,self.args)

    def test_live_execution_and_ineligible_sources_fail(self):
        for key,value in [('eligible',False),('live_executions',1),('live_publications',1)]:
            obj=json.loads(json.dumps(self.envelope));obj['plan']['facts'][key]=value;obj['plan_sha256']=r.digest(obj['plan'])
            self.assertRaises(ValueError,r.validate_plan,obj,self.args)

    def test_evidence_integrity_and_review_required(self):
        p=self.root/'backup_restore.txt';p.write_text('changed')
        self.assertRaises(ValueError,r.approve,self.envelope,self.args)

    def test_existing_artifact_never_overwritten(self):
        p=self.root/'plan.json';r.write_new(p,{'old':True})
        self.assertRaises(FileExistsError,r.write_new,p,{'new':True})
        self.assertEqual(json.loads(p.read_text()),{'old':True})

    def test_insecure_or_credential_urls_fail_before_network(self):
        for url in ('http://production.invalid','https://user:pass@example.invalid','https://example.invalid?token=x'):
            self.args.base_url=url
            self.assertRaises(ValueError,r.request,self.args,'status')

    def test_redirect_is_rejected(self):
        self.assertRaises(ValueError,r.NoRedirect().redirect_request,None,None,302,'moved',{},'https://other.invalid')

    def test_canonical_html_matches_go_json(self):
        self.assertEqual(r.canonical({'z':'<&>','a':'\u2028'}), b'{"a":"\\u2028","z":"\\u003c\\u0026\\u003e"}')

    def test_secret_permissions_fail_closed(self):
        p=self.root/'key';p.chmod(0o644)
        self.assertRaises(ValueError,r.restricted_read,p)

    def test_evidence_cannot_escape_manifest_directory(self):
        p=self.root/'evidence.json';manifest=json.loads(p.read_text())
        manifest['artifacts'][0]['file']='../key';p.write_text(json.dumps(manifest))
        self.assertRaises(ValueError,r.reviewed_evidence,p)

    def test_reference_cannot_be_url(self):
        self.args.reference='https://example.invalid/secret'
        self.assertRaises(ValueError,r.approve,self.envelope,self.args)

    def test_windows_secret_read_fails_closed(self):
        if r.os.name == 'nt':
            self.assertRaises(ValueError,r.restricted_read,self.root/'key')

if __name__=='__main__':unittest.main()
