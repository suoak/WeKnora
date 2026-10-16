import json
import unittest
from protocol_monitor import Monitor, Transaction, finalize
from unittest import mock
import protocol_monitor_runtime
from pathlib import Path

A = 'aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa'
C = 'cccccccc-cccc-4ccc-accc-cccccccccccc'
OTHER = 'bbbbbbbb-bbbb-4bbb-abbb-bbbbbbbbbbbb'
PRIVATE_HANDLE = 'SYNTHETIC_PRIVATE_TRANSACTION_HANDLE_NEVER_EXPORT'


def event(stage, passed=True, trace=C, category='none'):
    return dict(stage=stage, trace=trace, **{'pass': passed}, duration_ms=1, category=category)


def authorized():
    m = Monitor(100)
    t = Transaction(PRIVATE_HANDLE, 100, 700)
    m.transactions([t], 101)
    m.stage(event('authorize', trace=A))
    return m, t


def callback(m, t, auth_time=True):
    t.consumed = True
    m.transactions([t], 110)
    for stage in ('callback_state', 'token'):
        m.stage(event(stage))
    if auth_time:
        m.freshness(C, 'SIGNED_AUTH_TIME_VERIFIED')
    for stage in ('id_token', 'userinfo'):
        m.stage(event(stage))
    m.stage(event('identity_mapping', False, category='rejected'))
    m.stage(event('callback_result', False, category='rejected'))


class Adapter:
    def __init__(self):
        self.order = []
        self.prompt = True
        self.counts = (2, 2, 0, 0, 0)
        self.saved = None

    def save_evidence(self, receipt):
        self.order.append('save_evidence')
        self.saved = json.dumps(receipt)

    def restore_false(self):
        assert self.saved
        self.order.append('restore_false')
        self.prompt = False

    def verify_restored(self):
        self.order.append('verify_restored')
        assert not self.prompt and self.counts == (2, 2, 0, 0, 0)
        return 'PASS'

    def invalidate_exact(self, handle):
        self.order.append('invalidate_exact')
        assert handle == PRIVATE_HANDLE
        return 'EXACT_UNUSED_ONLY'

    def save_final(self, receipt):
        self.order.append('save_final')


class Controls(unittest.TestCase):
    def test_password_page_and_placeholders_do_not_finish_or_restore(self):
        m, _ = authorized()
        m.operator('password_page_observed')
        m.operator('【填写实际提示】')
        m.tick(300)
        self.assertFalse(m.finished)
        with self.assertRaises(RuntimeError):
            finalize(m, Adapter())

    def test_callback_collected_but_waits_for_operator(self):
        m, t = authorized()
        callback(m, t)
        self.assertFalse(m.finished)
        self.assertEqual(m.phase, 'WAITING_OPERATOR_COMPLETION')
        self.assertEqual(m.stages['protocol_only'], 'PASS')
        m.operator('authentication_complete')
        a = Adapter()
        result = finalize(m, a)
        self.assertEqual(m.reason, 'PROTOCOL_COMPLETE_IDENTITY_GATE_PENDING')
        self.assertEqual(a.order, ['save_evidence', 'restore_false', 'verify_restored', 'save_final'])
        self.assertTrue(all(s == 'PASS' for s in result['stages'].values()))
        self.assertNotIn(PRIVATE_HANDLE, json.dumps(result))

    def test_operator_completion_before_callback_keeps_waiting(self):
        m, t = authorized()
        m.operator('authentication_complete')
        self.assertFalse(m.finished)
        callback(m, t)
        self.assertTrue(m.finished)

    def test_timeout_from_transaction_not_startup(self):
        m, _ = authorized()
        m.tick(579)
        self.assertFalse(m.finished)
        m.tick(580)
        self.assertEqual(m.reason, 'INCOMPLETE_TIMEOUT')
        a = Adapter()
        finalize(m, a)
        self.assertEqual(a.order[-2:], ['invalidate_exact', 'save_final'])
        self.assertFalse(a.prompt)

    def test_no_authorization_timeout(self):
        m = Monitor(100)
        m.tick(220)
        a = Adapter()
        finalize(m, a)
        self.assertNotIn('invalidate_exact', a.order)

    def test_explicit_cancel_only_cleans_exact_bound_unused(self):
        m, _ = authorized()
        m.operator('cancel')
        a = Adapter()
        finalize(m, a)
        self.assertIn('invalidate_exact', a.order)

    def test_second_authorization_rejected(self):
        m, _ = authorized()
        m.stage(event('authorize', trace=OTHER))
        self.assertEqual(m.reason, 'CORRELATION_AMBIGUOUS')

    def test_second_transaction_rejected_even_without_log(self):
        m, t = authorized()
        m.transactions([t, Transaction('another-private-handle', 102, 702)], 103)
        self.assertEqual(m.reason, 'CORRELATION_AMBIGUOUS')

    def test_unattributed_callback_rejected(self):
        m = Monitor(100)
        m.stage(event('callback_state'))
        self.assertEqual(m.reason, 'CORRELATION_AMBIGUOUS')

    def test_different_callback_trace_rejected(self):
        m, t = authorized()
        t.consumed = True
        m.stage(event('callback_state'))
        m.stage(event('token', trace=OTHER))
        self.assertEqual(m.reason, 'CORRELATION_AMBIGUOUS')

    def test_consumed_before_binding_is_not_guessed(self):
        m = Monitor(100)
        m.transactions([Transaction(PRIVATE_HANDLE, 100, 700, consumed=True)], 102)
        self.assertEqual(m.reason, 'CORRELATION_AMBIGUOUS')

    def test_insufficient_lifetime_does_not_extend(self):
        m = Monitor(100)
        m.transactions([Transaction(PRIVATE_HANDLE, 0, 150)], 101)
        self.assertTrue(m.finished)

    def test_token_and_signature_failures_no_retry(self):
        for stage in ('token', 'id_token'):
            with self.subTest(stage=stage):
                m, t = authorized()
                t.consumed = True
                m.stage(event('callback_state'))
                if stage == 'id_token':
                    m.stage(event('token'))
                m.stage(event(stage, False, category='rejected'))
                before = len(m.events)
                m.stage(event('authorize', trace=OTHER))
                self.assertEqual(len(m.events), before)
                self.assertEqual(m.reason, 'SAFETY_FAILURE')
                a = Adapter()
                finalize(m, a)
                self.assertEqual(a.order[0:2], ['save_evidence', 'restore_false'])

    def test_userinfo_auth_time_never_fills_signed_evidence(self):
        m, t = authorized()
        m.freshness(C, 'SIGNED_AUTH_TIME_VERIFIED', source='userinfo')
        self.assertEqual(m.auth_time, 'NOT_CAPTURED')
        callback(m, t, auth_time=False)
        self.assertEqual(m.auth_time, 'NOT_CAPTURED')

    def test_absent_signed_auth_time_records_contract_requirement(self):
        m, _ = authorized()
        m.freshness(C, 'NOT_PROVIDED_CONTRACT_REQUIRED')
        self.assertEqual(m.auth_time, 'NOT_PROVIDED_CONTRACT_REQUIRED')

    def test_freshness_cannot_mix_trace(self):
        m, t = authorized()
        m.freshness(OTHER, 'SIGNED_AUTH_TIME_VERIFIED')
        t.consumed = True
        m.stage(event('callback_state'))
        self.assertEqual(m.reason, 'CORRELATION_AMBIGUOUS')

    def test_raw_extra_fields_fail_before_persistence(self):
        m, _ = authorized()
        m.stage(dict(event('token'), token='SYNTHETIC_SECRET_SHOULD_NOT_EXPORT'))
        self.assertEqual(m.reason, 'OBSERVABILITY_INCOMPLETE')
        self.assertNotIn('SYNTHETIC_SECRET', json.dumps(m.receipt()))
        self.assertEqual(m.receipt()['stages']['token'], 'NOT_CAPTURED')

    def test_duplicate_log_poll_does_not_add_second_authorize(self):
        m, _ = authorized()
        m.stage(event('authorize', trace=A))
        self.assertFalse(m.finished)

    def test_business_session_is_safety_failure(self):
        m, t = authorized()
        t.consumed = True
        m.stage(event('callback_state'))
        m.stage(event('local_session'))
        self.assertEqual(m.reason, 'UNEXPECTED_BUSINESS_SESSION')

    def test_missing_stage_is_not_capture_not_success(self):
        m, t = authorized()
        t.consumed = True
        m.stage(event('callback_state'))
        m.stage(event('userinfo'))
        self.assertEqual(m.reason, 'OBSERVABILITY_INCOMPLETE')

    def test_inert_runtime_default_never_contacts_server(self):
        with mock.patch('subprocess.run') as process, mock.patch('builtins.print'):
            self.assertEqual(protocol_monitor_runtime.main([]), 0)
            process.assert_not_called()

    def test_runtime_requires_both_authorization_and_exclusive_window(self):
        for args in (['--execute-authorized'], ['--exclusive-window-confirmed']):
            with mock.patch('subprocess.run') as process, mock.patch('builtins.print'):
                protocol_monitor_runtime.main(args)
                process.assert_not_called()

    def test_storage_failure_still_restores(self):
        m, _ = authorized()
        m.operator('cancel')
        a = Adapter()
        a.restore_false = lambda: a.order.append('restore_false')
        def fail(receipt):
            a.order.append('save_attempt')
            raise OSError('synthetic disk full')
        a.save_evidence = fail
        with self.assertRaises(OSError):
            finalize(m, a)
        self.assertEqual(a.order, ['save_attempt', 'restore_false'])

    def test_missing_freshness_event_prevents_complete_claim(self):
        m, t = authorized()
        callback(m, t, auth_time=False)
        m.operator('authentication_complete')
        self.assertEqual(m.reason, 'OBSERVABILITY_INCOMPLETE')

    def test_second_callback_even_after_first_end_wait_rejected(self):
        m, t = authorized()
        callback(m, t)
        m.stage(event('callback_state', trace=OTHER))
        self.assertEqual(m.reason, 'CORRELATION_AMBIGUOUS')

    def test_results_and_restore_do_not_modify_counts(self):
        m, t = authorized()
        callback(m, t)
        m.operator('authentication_complete')
        a = Adapter()
        baseline = a.counts
        r = finalize(m, a)
        self.assertEqual(a.counts, baseline)
        self.assertEqual(r['restore'], 'PASS')

    def test_real_adapter_restores_only_app_without_override(self):
        m, _ = authorized()
        with mock.patch.object(Path, 'read_text', return_value='{"compose_files_in_order":[]}'):
            a = protocol_monitor_runtime.PackagingAdapter(m)
        a.files = ['/safe/compose.yml', '/safe/candidate.yml', '/safe/frontend.yml', '/safe/allroles.yml', '/safe/prompt-false.yml']
        a.override = Path('/safe/once-true.yml')
        a.changed = True
        with mock.patch.object(a, 'health'), mock.patch.object(protocol_monitor_runtime, 'run', return_value=b'') as command:
            a.restore_false()
        args = command.call_args.args[0]
        self.assertNotIn(str(a.override), args)
        self.assertEqual(args[-8:], ['up', '-d', '--no-deps', '--no-build', '--pull', 'never', '--force-recreate', 'app'])
        self.assertEqual(args[-1], 'app')
        self.assertNotIn('migrate', args)
        self.assertNotIn('down', args)
        self.assertFalse(a.changed)

    def test_real_adapter_verifies_gate_counts_and_other_containers(self):
        m, _ = authorized()
        with mock.patch.object(Path, 'read_text', return_value='{"compose_files_in_order":[]}'):
            a = protocol_monitor_runtime.PackagingAdapter(m)
        a.original = {'SOURCEID_FORCE_REAUTH_PROMPT_LOGIN': 'false'}
        a.original_counts = {'users': 2, 'source_id_sessions': 0}
        a.original_ownership = [b'private-memory-only']
        a.before = {'app': {'id': 'old'}, 'frontend': {'id': 'unchanged'}}
        with mock.patch.object(a, 'flags', return_value=a.original), mock.patch.object(a, 'counts', return_value=a.original_counts), mock.patch.object(a, 'ownership', return_value=a.original_ownership), mock.patch.object(a, 'containers', return_value={'app': {'id': 'new'}, 'frontend': {'id': 'unchanged'}}), mock.patch.object(a, 'query', return_value='3027|false'), mock.patch.object(protocol_monitor_runtime, 'inspect', return_value=protocol_monitor_runtime.IMAGE):
            self.assertEqual(a.verify_restored(), 'PASS')
            with mock.patch.object(a, 'counts', return_value={'users': 3, 'source_id_sessions': 1}):
                with self.assertRaises(RuntimeError):
                    a.verify_restored()


if __name__ == '__main__':
    unittest.main(verbosity=2)
