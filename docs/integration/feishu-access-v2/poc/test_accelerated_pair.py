"""Safety boundaries of the currently authorized pair's operator tools."""
from pathlib import Path
import sys
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parents[4] / 'scripts' / 'deploy'))
import feishu_v2_accelerated_recovery as recovery
import feishu_v2_pair_operator as pair


class AcceleratedPairTests(unittest.TestCase):
    def test_recovery_requires_fresh_backup_before_cas(self):
        remote = recovery.program()
        compile(remote, 'recovery', 'exec')
        self.assertLess(remote.index('verified_backup_required'),
                        remote.index("redis('EVAL',RESET,len(keys())"))
        self.assertIn('source_or_index_changed_while_stopped', remote)
        self.assertIn('full_run_not_fresh', remote)
        self.assertIn('accelerated-control-recovery-20261027', remote)
        self.assertNotRegex(remote, r"(?<!test_)redis\('DEL'")

    def test_rebind_verifies_restored_rows_and_uses_bounded_http_window(self):
        remote = pair.program('rebind')
        compile(remote, 'pair', 'exec')
        self.assertIn('restore_rows_mismatch', remote)
        self.assertIn("key='user_id' if table=='feishu_connections' else 'id'", remote)
        self.assertIn('hours=23', remote)
        self.assertIn('exact_http_test_exception_required', remote)
        self.assertIn('accelerated-two-source-20261027', remote)


if __name__ == '__main__':
    unittest.main()
