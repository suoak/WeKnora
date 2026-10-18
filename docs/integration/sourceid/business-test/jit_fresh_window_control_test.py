import copy
import unittest
from datetime import datetime, timezone
from jit_fresh_window_control import prepare, restore, approved, SCOPES

NOW = datetime(2026, 10, 18, 3, tzinfo=timezone.utc)


def grants():
    # Fictional in-memory test fixture, never a deployable approval record.
    return {name: {'confirmed': True, 'reference': 'unit-fixture-not-for-deployment',
                   'responsible': 'unit-fixture', 'approved_at': '2026-10-18T02:00:00+00:00',
                   'valid_until': '2026-10-18T04:00:00+00:00'} for name in SCOPES}


class Port:
    def __init__(self, fail=None):
        self.calls = []; self.fail = fail
        self.target = {'host': '172.30.232.42', 'original_project': 'knowhub-company-test',
                       'fresh_project': 'knowhub-jit-fresh',
                       'revision': '5f988be51779f1a22bd9d027b7503fe6a8749f62',
                       'schema': 3030, 'dirty': False, 'fresh_empty': True,
                       'original_active_transactions': 0, 'fresh_active_transactions': 0,
                       'separate_db_redis_files_keys': True, 'candidate_syntax_ok': True}

    def readonly_preflight(self):
        self.calls.append('readonly_preflight'); return copy.deepcopy(self.target)

    def __getattr__(self, name):
        def action(*args):
            self.calls.append(name)
            if name == self.fail: raise RuntimeError('do_not_export_private_error')
            return True
        return action


class WindowTests(unittest.TestCase):
    def test_default_inert(self):
        p = Port(); self.assertEqual(prepare(p)['result'], 'PREPARED_NOT_SWITCHED')
        self.assertEqual(p.calls, ['readonly_preflight'])

    def test_each_scope_required_before_mutation(self):
        for scope in SCOPES:
            with self.subTest(scope=scope):
                g = grants(); g[scope]['confirmed'] = False; p = Port()
                with self.assertRaises(ValueError): prepare(p, g, True, NOW)
                self.assertEqual(p.calls, ['readonly_preflight'])

    def test_candidates_or_old_existing_approval_not_accepted(self):
        for g in (None, {}, {'existing_account': grants()['jit_policy']}):
            p = Port()
            with self.assertRaises(ValueError): prepare(p, g, True, NOW)
            self.assertEqual(p.calls, ['readonly_preflight'])

    def test_target_drift_stops(self):
        for key, value in (('host', '10.51.134.114'), ('fresh_empty', False),
                           ('separate_db_redis_files_keys', False), ('schema', 3029),
                           ('dirty', True), ('original_active_transactions', 1)):
            with self.subTest(key=key):
                p = Port(); p.target[key] = value
                with self.assertRaises(ValueError): prepare(p)
                self.assertEqual(p.calls, ['readonly_preflight'])

    def test_policy_requires_real_reference_and_responsible(self):
        for field, value in (('reference', 'synthetic'), ('responsible', ''),
                             ('approved_at', 'not-a-time'), ('valid_until', '2026-10-18T03:00:00+00:00')):
            with self.subTest(field=field):
                g = grants(); g['jit_policy'][field] = value
                with self.assertRaises(ValueError): approved(g, NOW)

    def test_ready_does_not_start_authentication(self):
        p = Port(); result = prepare(p, grants(), True, NOW)
        self.assertEqual(result['result'], 'READY_FOR_USER')
        self.assertFalse(result['automatic_login']); self.assertEqual(result['real_sid_requests'], 0)
        self.assertLess(p.calls.index('arm_restore_watchdog'), p.calls.index('configure_fresh_approved_policy'))
        self.assertLess(p.calls.index('pause_entry_and_drain_connections'), p.calls.index('reload_gateway'))

    def test_unverified_approval_source_never_reaches_backup_or_mutation(self):
        p = Port('verify_authority_and_approval_sources')
        with self.assertRaises(RuntimeError): prepare(p, grants(), True, NOW)
        self.assertEqual(p.calls, ['readonly_preflight', 'verify_authority_and_approval_sources'])

    def test_each_switch_failure_recovers_without_retry(self):
        for failure in ('arm_restore_watchdog', 'configure_fresh_approved_policy',
                        'verify_fresh_effective_policy', 'prepare_whitelisted_observer',
                        'pause_entry_and_drain_connections', 'connect_fresh_gateway_network',
                        'install_candidate_and_validate', 'reload_gateway',
                        'verify_all_routes_without_credentials', 'start_single_transaction_observer'):
            with self.subTest(failure=failure):
                p = Port(failure); r = prepare(p, grants(), True, NOW)
                self.assertEqual(r['result'], 'PREPARATION_FAILED_NO_LOGIN')
                self.assertEqual(r['recovery']['result'], 'RESTORED')
                self.assertEqual(p.calls.count(failure), 1)

    def test_gate_close_failure_does_not_skip_gateway_restore(self):
        p = Port('close_fresh_gates'); r = restore(p)
        self.assertEqual(r['result'], 'MANUAL_RECOVERY_REQUIRED')
        self.assertIn('restore_original_gateway', p.calls)
        self.assertLess(p.calls.index('save_safe_evidence'), p.calls.index('close_fresh_gates'))

    def test_ambiguous_revocation_does_not_skip_route_restore(self):
        p = Port('revoke_exact_new_session'); r = restore(p)
        self.assertEqual(r['result'], 'MANUAL_RECOVERY_REQUIRED')
        self.assertIn('verify_original_routes_and_data', p.calls)

    def test_restoration_failure_alarms(self):
        for failure in ('restore_original_gateway', 'drain_connections',
                        'verify_original_routes_and_data', 'disconnect_fresh_gateway_network'):
            p = Port(failure); r = restore(p)
            self.assertEqual(r['result'], 'MANUAL_RECOVERY_REQUIRED')
            self.assertEqual(r['steps'][failure], 'FAIL')
            self.assertNotIn('do_not_export_private_error', str(r))


if __name__ == '__main__': unittest.main()
