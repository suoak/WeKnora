import copy
from pathlib import Path
import subprocess
import tempfile
import unittest

from verify_release_provenance import git, verify


class ReleaseProvenanceTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.repo = Path(self.directory.name)
        subprocess.run(["git", "init", "-b", "main", str(self.repo)], check=True, capture_output=True)
        git(self.repo, "config", "user.name", "Provenance test")
        git(self.repo, "config", "user.email", "test@example.invalid")
        git(self.repo, "commit", "--allow-empty", "-m", "main")
        self.main = git(self.repo, "rev-parse", "HEAD")
        git(self.repo, "remote", "add", "origin", str(self.repo))
        self.app_id = "sha256:" + "a" * 64
        self.front_id = "sha256:" + "b" * 64
        self.app = self.image(self.app_id)
        self.front = self.image(self.front_id)

    def image(self, image_id):
        return {"Id": image_id, "Config": {
            "Labels": {"org.opencontainers.image.revision": self.main},
            "Env": ["SECRET=must-never-enter-receipt"],
        }}

    def check(self, app=None, front=None):
        return verify(self.repo, app or self.app, front or self.front,
                      self.app_id, self.front_id, self.main)

    def test_exact_pair_records_only_public_identity(self):
        receipt = self.check()
        self.assertEqual(receipt["paired_source_sha"], self.main)
        self.assertTrue(receipt["images"]["frontend"]["contains_main"])
        self.assertNotIn("SECRET", str(receipt))

    def test_digest_mismatch_rejected(self):
        broken = copy.deepcopy(self.app)
        broken["Id"] = self.front_id
        with self.assertRaisesRegex(ValueError, "digest mismatch"):
            self.check(app=broken)

    def test_source_pair_mismatch_rejected(self):
        broken = copy.deepcopy(self.front)
        broken["Config"]["Labels"]["org.opencontainers.image.revision"] = "c" * 40
        with self.assertRaisesRegex(ValueError, "same validated source"):
            self.check(front=broken)

    def test_new_main_invalidates_old_release(self):
        git(self.repo, "commit", "--allow-empty", "-m", "new main")
        with self.assertRaisesRegex(ValueError, "remote main changed"):
            self.check()
        new_main = git(self.repo, "rev-parse", "HEAD")
        with self.assertRaisesRegex(ValueError, "omits current main"):
            verify(self.repo, self.app, self.front, self.app_id, self.front_id, new_main)

    def test_missing_source_label_rejected(self):
        broken = copy.deepcopy(self.app)
        broken["Config"]["Labels"] = {}
        with self.assertRaisesRegex(ValueError, "source SHA missing"):
            self.check(app=broken)


if __name__ == "__main__":
    unittest.main()
