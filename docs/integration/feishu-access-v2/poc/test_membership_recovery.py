"""Exercise the generated second-incident recovery's queue admission offline."""
import ast
import json
from pathlib import Path
import re
import sys
import unittest
from unittest import mock

sys.path.insert(0, str(Path(__file__).resolve().parents[4] / 'scripts' / 'deploy'))
import feishu_v2_membership_recovery as recovery


class MembershipRecoveryTests(unittest.TestCase):
    def test_stopped_operator_enrollment_disables_document_probes(self):
        import feishu_v2_sync_publication_policy as policy
        with mock.patch.object(sys, 'argv', ['policy', '--operation', 'prepare_reconciliation',
                '--approval-reference', recovery.APPROVAL]), \
                mock.patch.object(policy.probe.transport, 'main', return_value=0), \
                mock.patch.object(policy.probe.transport, 'REMOTE', ''):
            self.assertEqual(0, policy.main())
            remote = policy.probe.transport.REMOTE
            self.assertTrue(remote.startswith('COMPARE_CONTENT_PATHS=False\n'))
            self.assertIn("compose+['ps','--all','--quiet','app']", remote)
            compile(remote, '<stopped-policy-operator>', 'exec')

    def test_reset_requires_verified_backup_before_latch_mutation(self):
        program = recovery.program()
        self.assertLess(program.index('verified_recovery_backup_required'),
                        program.index("redis('EVAL',RESET,len(keys())"))
        self.assertIn('recovery_restored_counts_mismatch', program)
        self.assertIn('recovery_backup_changed', program)

    def test_only_terminal_existing_deliveries_survive_reset(self):
        tree = ast.parse(recovery.program())
        function = next(n for n in tree.body if isinstance(n, ast.FunctionDef) and n.name == 'terminal_pending_ids')
        def check(payload, terminal):
            names = {
                'queue': 'asynq:{sync}:', 'state': {'sources': [{'id': 'sheet'}, {'id': 'bitable'}]},
                'redis': lambda command, *_: [b'task'] if command == 'LRANGE' else json.dumps(payload).encode(),
                'fields': lambda raw: {1: [b'datasource:sync'], 2: [raw]},
                'sql': lambda query: int(terminal), 'json': json, 're': re,
            }
            exec(compile(ast.Module(body=[function], type_ignores=[]), '<terminal-queue>', 'exec'), names)
            return names['terminal_pending_ids']()
        item = dict(data_source_id='sheet', sync_log_id='canceled-log', force_full=False)
        self.assertEqual(['task'], check(item, True))
        for payload, terminal in [(item, False), ({**item, 'force_full': True}, True),
                ({**item, 'data_source_id': 'outside'}, True), ({**item, 'sync_log_id': "unsafe'"}, True)]:
            with self.subTest(payload=payload, terminal=terminal), self.assertRaises(ValueError):
                check(payload, terminal)
        self.assertIn('PENDING_CHANGED', recovery.reset_program())
        self.assertNotIn("redis.call('DEL',KEYS[10])", recovery.reset_program())

    def check_pending(self, payloads, log_pending=True):
        tree = ast.parse(recovery.program())
        function = next(n for n in tree.body if isinstance(n, ast.FunctionDef) and n.name == 'pending_in_scope')
        identifiers = [str(i).encode() for i in range(len(payloads))]
        def redis(command, key, *args):
            if command == 'LRANGE':
                return identifiers
            if command == 'HGET':
                return json.dumps(payloads[int(key.split(':')[-1])]).encode()
            self.fail('unexpected command: ' + command)
        def fields(raw):
            return {1: [b'datasource:sync'], 2: [raw]}
        names = {
            'queue_counts': lambda: dict(active=0, pending=len(payloads), scheduled=0, retry=0),
            'redis': redis, 'queue': 'asynq:{sync}:', 'state': {'sources': [{'id': 'sheet'}, {'id': 'bitable'}]},
            'fields': fields, 'json': json, 're': re, 'RECONCILIATION_LOG_HASH': '123456abcdef',
            'sql': lambda _: int(log_pending),
            'mask': lambda value: {'bitable': '1dcf458346ba', 'approved-log': '123456abcdef'}.get(value, 'other'),
        }
        exec(compile(ast.Module(body=[function], type_ignores=[]), '<recovery-queue>', 'exec'), names)
        return names['pending_in_scope']()

    def test_only_approved_full_run_and_existing_natural_source(self):
        full = dict(data_source_id='bitable', sync_log_id='approved-log',
                    force_full=True, trigger='approved_reconciliation')
        self.assertEqual(['0', '1'], self.check_pending([full, dict(data_source_id='sheet', force_full=False)]))
        for changed in [dict(data_source_id='sheet'), dict(sync_log_id='another-log'),
                        dict(trigger='manual'), dict(data_source_id='outside')]:
            with self.subTest(changed=changed), self.assertRaises(ValueError):
                self.check_pending([{**full, **changed}])
        with self.assertRaises(ValueError):
            self.check_pending([full], log_pending=False)
        with self.assertRaises(ValueError):
            self.check_pending([dict(data_source_id='sheet', force_full=False)])

    def test_old_approval_cannot_execute_new_incident(self):
        with mock.patch.object(sys, 'argv', ['recovery', '--operation', 'reset',
                '--approval-reference', 'user:call_085aca217ce54805be1d8372feec744b:approved_two_sources']), \
                mock.patch.object(recovery.prior.transport, 'main', side_effect=AssertionError('transport reached')):
            with self.assertRaisesRegex(SystemExit, 'new explicit report25 approval required'):
                recovery.main()

    def test_separate_incident_preserves_cas_and_hold(self):
        program = recovery.program()
        self.assertIn("SHA!=" + repr(recovery.FIXED_RUNTIME), program)
        self.assertIn("approved_fixed_runtime_required", program)
        tree = ast.parse(program)
        dispatch = next(n for n in tree.body if isinstance(n, ast.FunctionDef) and n.name == 'redis')
        allowed = set(ast.literal_eval(dispatch.body[0].test.comparators[0]))
        self.assertTrue({'HLEN', 'LRANGE', 'HGET', 'EVAL'} <= allowed)
        self.assertFalse({'DEL', 'SET', 'HSET', 'HDEL', 'ZREM'} & allowed)
        # The reusable diagnostic audit itself must continue refusing mutation.
        self.assertNotIn("'EVAL'", recovery.prior.audit.REMOTE[:recovery.prior.audit.REMOTE.index('clock=')])
        self.assertNotIn('automatic_permit_consumer_lost', program)
        self.assertIn('approved-membership-recovery-report25', program)
        self.assertIn("row['legacy_access_state']!='hold'", program)
        self.assertIn("row['legacy_access_state']!='unmanaged'", program)
        for requirement in ['CONTROLLER_NOT_QUIESCENT', 'MEMBERSHIP_CHANGED', 'QUEUE_NOT_QUIESCENT']:
            self.assertIn(requirement, recovery.prior.RESET)
        for requirement in ['NO_LIVE_PERMIT', 'NO_LIVE_LEASE', 'INCARNATION_CHANGED', 'PENDING_CHANGED']:
            self.assertIn(requirement, recovery.prior.RESUME)


if __name__ == '__main__':
    unittest.main()
