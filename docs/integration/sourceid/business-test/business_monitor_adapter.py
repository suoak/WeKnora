"""Company-test business adapter and bounded orchestration.

The port owns protected DB/browser IPC and approved APP-only configuration.
No protocol-only runtime import, credentials in receipts, or role split.
"""
import json
import traceback
from pathlib import Path

from business_monitor import BusinessMonitor, PROJECT, IMAGE, REVISION, SCHEMA, finish

CLOSED = {'OIDC_AUTH_ENABLE': 'false', 'SOURCEID_PROTOCOL_ONLY': 'true',
          'SOURCEID_CONTRACT_CONFIRMED': 'false', 'SOURCEID_EMAIL_BINDING_ENABLE': 'false',
          'SOURCEID_AUTO_BINDING_ENABLE': 'false', 'SOURCEID_SESSION_MODE': 'external',
          'SOURCEID_FORCE_REAUTH_PROMPT_LOGIN': 'false', 'SOURCEID_LOCAL_REFRESH_ENABLE': 'false',
          'SOURCEID_BASIC_ENCODING': 'oauth_form', 'AUTO_MIGRATE': 'false',
          'SOURCEID_JIT_ENABLE': 'false','SOURCEID_ACCOUNT_LINK_MODE':'trusted_ownership',
          'SOURCEID_VERIFIED_EMAIL_POLICY_CONFIRMED':'false'}


class PilotBusinessAdapter:
    def __init__(self, port, case, approved=False, exclusive=False):
        self.port = port
        self.case = case
        self.approved = approved
        self.exclusive = exclusive
        self.changed = False
        self.before = None
        self.bound_session = None  # Random internal handle; never serialized.
        self.recovery = None
        self.monitor = None
        self.correlation_changed = False

    def prepare(self, now):
        if not self.approved or not self.exclusive:
            raise RuntimeError('separate_authorization_and_exclusive_window_required')
        v = self.port.inventory()
        if (v['project'] != PROJECT or v['image'] != IMAGE or v['revision'] != REVISION or
                v['schema'] != SCHEMA or v['dirty'] is not False or not v['healthy']):
            raise RuntimeError('fixed_target_mismatch')
        if any(v['flags'].get(k) != value for k, value in getattr(self.port, 'closed_baseline', CLOSED).items()):
            raise RuntimeError('closed_gate_baseline_mismatch')
        self.before = self.port.snapshot()
        self.before_flags = dict(v['flags'])
        rows = self.port.transactions()
        if any(not t.consumed and t.expires > now for t in rows):
            raise RuntimeError('active_authorization_transaction_exists')
        m = BusinessMonitor(self.case, now, (t.handle for t in rows))
        self.monitor = m
        m.acceptance_scope=getattr(self.port,'acceptance_scope','full')
        self.port.save_private_baseline(self.before)  # In-memory only by contract.
        self.changed = True  # Recovery also covers partially failed enable.
        self.port.enable_approved(self.case)
        enabled = self.port.inventory()
        if enabled['project'] != PROJECT or enabled['image'] != IMAGE or enabled['schema'] != SCHEMA or enabled['dirty'] or not enabled['healthy']:
            raise RuntimeError('enabled_target_changed')
        flags = enabled['flags']
        if getattr(self.port,'env',{}).get('SOURCEID_ACCOUNT_LINK_MODE')=='verified_email_policy' and (flags.get('SOURCEID_ACCOUNT_LINK_MODE')!='verified_email_policy' or flags.get('SOURCEID_VERIFIED_EMAIL_POLICY_CONFIRMED')!='true'):
            raise RuntimeError('verified_email_mode_not_loaded')
        needed = {'OIDC_AUTH_ENABLE':'true', 'SOURCEID_PROTOCOL_ONLY':'false',
                  'SOURCEID_CONTRACT_CONFIRMED':'true', 'SOURCEID_EMAIL_BINDING_ENABLE':'true',
                  'SOURCEID_AUTO_BINDING_ENABLE':'true', 'SOURCEID_SESSION_MODE':'local_approved',
                  'SOURCEID_FORCE_REAUTH_PROMPT_LOGIN':'false', 'SOURCEID_LOCAL_REFRESH_ENABLE':'false',
                  'SOURCEID_BASIC_ENCODING':'oauth_form', 'SOURCEID_ACCOUNT_SCOPE':'enterprise_all',
                  'SOURCEID_ADMISSION_MODE':'oidc_sso_approved', 'SOURCEID_DIAGNOSTICS_ENABLE':'true',
                  'AUTO_MIGRATE':'false', 'SOURCEID_JIT_ENABLE':'true' if self.case=='jit' else 'false'}
        if any(flags.get(k) != value for k, value in needed.items()):
            raise RuntimeError('effective_business_gate_mismatch')
        if self.port.other_services() != self.before['other_services']:
            raise RuntimeError('non_app_service_changed')
        # Start stream BEFORE READY. Unexpected activity during restart is fatal.
        self.port.start_stream()
        if any(t.handle not in m.baseline for t in self.port.transactions()):
            raise RuntimeError('activity_during_preparation')
        return m

    def poll(self, m, now):
        # Capture stream first, then consumption snapshot, then process stages.
        lines = self.port.drain_lines()
        m.retain_batch_evidence(lines)
        m.transactions(self.port.transactions(), now)
        for line in lines:
            m.line(line)
            if m.finished:
                break
        if hasattr(self.port,'observe_business_lines'):
            self.port.observe_business_lines(lines,m)
        if not m.finished and m.stages['callback_result'] == 'PASS':
            private = self.port.session_candidate(self.before)
            if private is not None:
                if not self.port.candidate_belongs_to_run(private, m, self.before, self.case):
                    m.end('CORRELATION_AMBIGUOUS')
                else:
                    self.bound_session = private
                    probes = self.port.business_probes(self.before, private, self.case, m.run_id, m.callback_trace)
                    if probes is not None:
                        observed_trace, values = probes
                        m.business_probes(observed_trace, values)
                        if getattr(self.port,'server_observer',None) is not None:
                            m.observation_mode='SERVER_METADATA_AND_USER_PORTAL_CONFIRMATION'
                        m.link_policy_verification=getattr(self.port,'link_policy_status','NOT_CAPTURED')
        action = self.port.operator_action(m.run_id)
        if action is not None:
            m.operator(action)
        m.tick(now)
        self.save_evidence(m.receipt())

    def save_evidence(self, receipt):
        raw = json.dumps(receipt)
        if len(raw.encode()) > 1048576:
            raise RuntimeError('evidence_limit')
        self.port.save_safe('before-restore', receipt)
        return 'PASS'

    def restore_gates(self):
        observation_failed = False
        try:
            if self.monitor is not None:
                allowed = self.monitor.baseline | ({self.monitor.bound.handle} if self.monitor.bound else set())
                self.correlation_changed = any(t.handle not in allowed for t in self.port.transactions())
        except Exception:
            observation_failed = True
        finally:
            if self.changed:
                self.port.restore_original_app_only()
                self.changed = False
        if observation_failed:
            raise RuntimeError('restore_correlation_snapshot_unavailable')
        if self.correlation_changed:
            raise RuntimeError('late_parallel_authorization_detected')
        return 'PASS'

    def revoke_bound_session(self, m):
        # Re-read DB after restoring; an ambiguous stream never licenses bulk
        # revocation of sessions potentially belonging to other operators.
        candidates = self.port.new_session_candidates(self.before)
        if not candidates:
            return 'NO_NEW_ENTERPRISE_SESSION'
        if self.correlation_changed or m.reason == 'CORRELATION_AMBIGUOUS' or len(candidates) != 1 or m.bound is None or not m.bound.consumed or m.callback_trace is None:
            raise RuntimeError('targeted_revocation_ambiguous')
        candidate = candidates[0]
        if not self.port.candidate_belongs_to_run(candidate, m, self.before, self.case):
            raise RuntimeError('targeted_revocation_unattributable')
        self.port.revoke_exact_session_and_access(candidate)
        if not self.port.revocation_verified(candidate):
            raise RuntimeError('targeted_revocation_not_confirmed')
        return 'EXACT_SESSION_AND_DERIVED_ACCESS_REVOKED'

    def cleanup_transaction(self, m):
        if m.bound is None or m.bound.consumed:
            return 'NO_UNUSED_BOUND_TRANSACTION'
        return self.port.invalidate_exact_unused(m.bound)

    def verify_restored(self):
        after = self.port.inventory()
        if self.changed or after['image'] != IMAGE or after['schema'] != SCHEMA or after['dirty'] or not after['healthy']:
            raise RuntimeError('restore_failed')
        if any(after['flags'].get(k) != value for k, value in getattr(self.port, 'closed_baseline', CLOSED).items()):
            raise RuntimeError('restore_gates_failed')
        if after['flags'] != self.before_flags:
            raise RuntimeError('approved_override_not_fully_removed')
        if self.port.other_services() != self.before['other_services']:
            raise RuntimeError('non_app_service_changed')
        if not self.port.permissions_preserved(self.before, self.case):
            raise RuntimeError('permissions_or_user_invariants_changed')
        return 'PASS'

    def save_final(self, result):
        self.port.save_safe('final', result)


def run(port, case, approved=False, exclusive=False):
    """No real endpoint request is made here; only observe the user's one run."""
    adapter = PilotBusinessAdapter(port, case, approved, exclusive)
    m = None
    try:
        m = adapter.prepare(port.now())
        port.ready(m.run_id)
        while not m.finished:
            adapter.poll(m, port.now())
            if not m.finished:
                port.pause()
    except (Exception, KeyboardInterrupt) as error:
        if m is None:
            m = BusinessMonitor(case, port.now())
        m.control_failure={'class':type(error).__name__, 'sites':[
            {'module':Path(frame.filename).name,'line':frame.lineno}
            for frame in traceback.extract_tb(error.__traceback__)
            if Path(frame.filename).name in ('business_monitor_adapter.py','business_monitor_pilot.py','company_gateway_observer.py')][-5:]}
        m.end('CONTROLLED_FAILURE_NO_RETRY')
    finally:
        try:
            if adapter.changed or adapter.before is not None:
                result = finish(m, adapter)
            else:
                result = m.receipt()
                port.save_safe('final', result)
        finally:
            port.close_stream()
    return result
