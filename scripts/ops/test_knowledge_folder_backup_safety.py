"""Exercise the rollback script's actual inline guards without Docker or secrets."""
import ast
import copy
import datetime
import os
from pathlib import Path
import shutil
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch


SCRIPT = Path(__file__).with_name("rollback_knowledge_folders.sh")
INLINE = SCRIPT.read_text(encoding="utf-8").split("<<'PY'\n", 1)[1].split("\nPY\n", 1)[0]
TREE = ast.parse(INLINE)
FUNCTIONS = [n for n in TREE.body if isinstance(n, ast.FunctionDef)]
NAMESPACE = {"pathlib": __import__("pathlib"), "shutil": shutil, "datetime": datetime}
exec(compile(ast.Module(body=FUNCTIONS, type_ignores=[]), str(SCRIPT), "exec"), NAMESPACE)


@unittest.skipUnless(os.name == "posix", "production directory modes require POSIX")
class BackupDirectorySafety(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.allowed = (self.root / "home", self.root / "data")
        for p in self.allowed:
            p.mkdir(mode=0o700)

    def validate(self, p):
        NAMESPACE["validate_release_directory"](p, self.allowed)

    def test_private_directory_on_either_approved_filesystem(self):
        for root in self.allowed:
            p = root / "knowhub-folders-release-validated"
            p.mkdir(mode=0o700)
            self.validate(p)

    def test_rejects_parent_itself(self):
        with self.assertRaises(AssertionError):
            self.validate(self.allowed[0])

    def test_rejects_unapproved_root(self):
        p = self.root / "knowhub-folders-release-outside"
        p.mkdir(mode=0o700)
        with self.assertRaises(AssertionError):
            self.validate(p)

    def test_rejects_nested_directory(self):
        parent = self.allowed[1] / "other"
        parent.mkdir(mode=0o700)
        p = parent / "knowhub-folders-release-nested"
        p.mkdir(mode=0o700)
        with self.assertRaises(AssertionError):
            self.validate(p)

    def test_rejects_symlink(self):
        target = self.allowed[1] / "knowhub-folders-release-target"
        target.mkdir(mode=0o700)
        link = self.allowed[1] / "knowhub-folders-release-link"
        link.symlink_to(target, target_is_directory=True)
        with self.assertRaises(AssertionError):
            self.validate(link)

    def test_rejects_publicly_accessible_directory(self):
        p = self.allowed[1] / "knowhub-folders-release-public"
        p.mkdir(mode=0o755)
        p.chmod(0o755)
        with self.assertRaises(AssertionError):
            self.validate(p)

    def test_rejects_wrong_name(self):
        p = self.allowed[1] / "unrelated-backup"
        p.mkdir(mode=0o700)
        with self.assertRaises(AssertionError):
            self.validate(p)


class BackupCapacitySafety(unittest.TestCase):
    def check(self, free, database, folders):
        with patch.object(shutil, "disk_usage", return_value=SimpleNamespace(free=free)):
            NAMESPACE["require_backup_space"](Path("/not-used"), database, folders)

    def test_reserves_space_for_independent_directory_dump(self):
        gib = 1024**3
        with self.assertRaises(AssertionError):
            self.check(56 * gib, 50 * gib, 3 * gib)
        self.check(58 * gib, 50 * gib, 3 * gib)

    def test_requires_minimum_headroom_for_small_database(self):
        gib = 1024**3
        with self.assertRaises(AssertionError):
            self.check(5 * gib, gib, 0)
        self.check(6 * gib, gib, 0)

    def test_rejects_negative_size(self):
        with self.assertRaises(AssertionError):
            self.check(100 * 1024**3, -1, 0)



class RuntimeRollbackSafety(unittest.TestCase):
    def setUp(self):
        self.release = {'paired_source_sha': 'release-sha', 'images': {'app': {'image_config_digest': 'config-id'}}}
        self.mapping = {'status': 'VERIFIED', 'source_sha': 'release-sha', 'images': {'app': {
            'source_sha': 'release-sha', 'image_config_digest': 'config-id',
            'engine_image_id': 'manifest-id', 'image_manifest_digest': 'manifest-id',
            'raw_config_digest_verified': True, 'raw_manifest_digest_verified': True, 'rootfs_diff_ids_verified': 15}}}

    def check_image(self, image, mapping):
        NAMESPACE['validate_current_image'](image, 'baseline-id', self.release, 'app', mapping)

    def test_classic_and_original_images_need_no_manifest_mapping(self):
        self.check_image('config-id', None)
        self.check_image('baseline-id', None)

    def test_manifest_requires_verified_exact_source_config_and_layers(self):
        self.check_image('manifest-id', self.mapping)
        for key, value in [('source_sha', 'other-source'), ('image_config_digest', 'other-config'),
                           ('raw_config_digest_verified', False), ('raw_manifest_digest_verified', False),
                           ('rootfs_diff_ids_verified', 0), ('engine_image_id', 'other-manifest')]:
            mapped = copy.deepcopy(self.mapping)
            mapped['images']['app'][key] = value
            with self.subTest(key=key), self.assertRaises(AssertionError):
                self.check_image('manifest-id', mapped)

    def test_unmapped_or_unrelated_engine_image_rejected(self):
        with self.assertRaises(AssertionError):
            self.check_image('manifest-id', None)
        with self.assertRaises(AssertionError):
            self.check_image('unrelated-id', self.mapping)

    def test_expired_near_expiry_and_unapproved_sid_rejected_before_stop(self):
        now = datetime.datetime(2026, 11, 27, tzinfo=datetime.timezone.utc)
        env = {'SOURCEID_VALIDATION_STRATEGY': 'code_flow_tls_userinfo',
               'SOURCEID_TLS_USERINFO_EXPIRES_AT': (now + datetime.timedelta(hours=1)).isoformat(),
               'SOURCEID_TLS_USERINFO_APPROVAL_REF': 'explicit-approval'}
        NAMESPACE['require_valid_sid_policy'](env, now)
        for expiry in [now - datetime.timedelta(seconds=1), now + datetime.timedelta(minutes=10)]:
            with self.assertRaises(AssertionError):
                NAMESPACE['require_valid_sid_policy']({**env, 'SOURCEID_TLS_USERINFO_EXPIRES_AT': expiry.isoformat()}, now)
        with self.assertRaises(AssertionError):
            NAMESPACE['require_valid_sid_policy']({**env, 'SOURCEID_TLS_USERINFO_APPROVAL_REF': ''}, now)


if __name__ == "__main__":
    unittest.main()
