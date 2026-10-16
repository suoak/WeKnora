"""Pure control state machine. No network, credentials or application mutation.
Only safe events enter this module. Transaction handles stay in memory.
"""
import json
import uuid
from dataclasses import dataclass, field

STAGES = ('authorize', 'callback', 'token', 'id_token', 'nonce', 'userinfo', 'protocol_only')
KNOWN = {'authorize', 'callback_state', 'token', 'id_token', 'userinfo', 'identity_mapping', 'local_session', 'callback_result'}
CATEGORIES = {'none', 'rejected', 'token_http_401', 'nonce_mismatch', 'enrollment_required', 'identity_conflict', 'account_disabled', 'identity_fields_missing', 'subject_mismatch', 'id_token_verification'}
EVIDENCE = {'NOT_PROVIDED_CONTRACT_REQUIRED', 'INVALID_OR_STALE', 'SIGNED_AUTH_TIME_VERIFIED'}


@dataclass(repr=False)
class Transaction:
    handle: str = field(repr=False)
    created: float
    expires: float
    consumed: bool = False
    prompt_login: bool = True


class Monitor:
    def __init__(self, now, baseline_handles=(), wait_seconds=480, ready_seconds=120):
        if not 1 <= wait_seconds <= 480:
            raise ValueError('wait_window_invalid')
        self.run_id = str(uuid.uuid4())
        self.ready_deadline = now + ready_seconds
        self.wait_seconds = wait_seconds
        self.baseline = set(baseline_handles)
        self.seen_new = set()
        self.bound = None
        self.deadline = None
        self.authorize_trace = None
        self.callback_trace = None
        self.seen_events = set()
        self.events = []
        self.bytes = 0
        self.phase = 'READY'
        self.finished = False
        self.reason = None
        self.operator_complete = False
        self.callback_finished = False
        self.auth_time = 'NOT_CAPTURED'
        self.freshness_trace = None
        self.stages = {s: 'NOT_EXECUTED' for s in STAGES}
        self.write_attempts = 0

    def end(self, reason):
        self.finished = True
        self.reason = reason
        self.phase = 'FINISHED'

    def transactions(self, rows, now):
        if self.finished:
            return
        for tx in rows:
            if tx.handle in self.baseline:
                continue
            self.seen_new.add(tx.handle)
            if len(self.seen_new) > 1:
                self.end('CORRELATION_AMBIGUOUS')
                return
            if self.bound is None:
                # A consumed row first observed after a fast callback has lost
                # request metadata. Do not guess it belonged to this run.
                if tx.consumed or not tx.prompt_login or tx.created > now or tx.expires - now < self.wait_seconds:
                    self.end('CORRELATION_AMBIGUOUS')
                    return
                self.bound = tx
                self.deadline = min(tx.created + self.wait_seconds, tx.expires)
                self.phase = 'WAITING_EMPLOYEE_AUTHENTICATION'
            elif tx.handle == self.bound.handle:
                self.bound.consumed = tx.consumed
        if self.deadline is not None and now >= self.deadline:
            self.end('INCOMPLETE_TIMEOUT')

    def stage(self, event):
        if self.finished:
            return
        if set(event) != {'trace', 'stage', 'pass', 'duration_ms', 'category'} or event['stage'] not in KNOWN or event['category'] not in CATEGORIES or type(event['pass']) is not bool or type(event['duration_ms']) is not int or event['duration_ms'] < 0:
            self.end('OBSERVABILITY_INCOMPLETE')
            return
        try:
            uuid.UUID(event['trace'])
        except (ValueError, TypeError):
            self.end('OBSERVABILITY_INCOMPLETE')
            return
        identity = json.dumps(event, sort_keys=True)
        if identity in self.seen_events:
            return
        self.seen_events.add(identity)
        self.bytes += len(identity.encode())
        if len(self.events) >= 256 or self.bytes > 1048576:
            self.end('OBSERVABILITY_INCOMPLETE')
            return
        self.events.append(dict(event))
        name = event['stage']
        trace = event['trace']
        if name == 'authorize':
            if self.authorize_trace is not None:
                self.end('CORRELATION_AMBIGUOUS')
                return
            self.authorize_trace = trace
            self.stages['authorize'] = 'PASS' if event['pass'] else 'FAIL'
            if not event['pass']:
                self.end('SAFETY_FAILURE')
            return
        if self.bound is None or not self.bound.consumed or self.authorize_trace is None:
            self.end('CORRELATION_AMBIGUOUS')
            return
        if self.callback_trace is not None and trace != self.callback_trace:
            self.end('CORRELATION_AMBIGUOUS')
            return
        self.callback_trace = trace
        if self.freshness_trace not in (None, trace):
            self.end('CORRELATION_AMBIGUOUS')
            return
        if name == 'callback_state':
            self.stages['callback'] = 'PASS' if event['pass'] else 'FAIL'
        elif name in ('token', 'id_token', 'userinfo'):
            previous = {'token': 'callback', 'id_token': 'token', 'userinfo': 'id_token'}[name]
            if self.stages[previous] != 'PASS':
                self.end('OBSERVABILITY_INCOMPLETE')
                return
            self.stages[name] = 'PASS' if event['pass'] else 'FAIL'
            if name == 'id_token' and event['pass']:
                # Deployed 479416c90 emits id_token PASS after strict nonce.
                self.stages['nonce'] = 'PASS'
        elif name == 'local_session':
            self.end('UNEXPECTED_BUSINESS_SESSION')
            return
        elif name == 'identity_mapping':
            if not event['pass'] and self.stages['userinfo'] == 'PASS' and event['category'] == 'rejected':
                self.stages['protocol_only'] = 'PASS'
            else:
                self.stages['protocol_only'] = 'FAIL'
                self.end('SAFETY_FAILURE')
            return
        elif name == 'callback_result':
            if event['pass'] or self.stages['protocol_only'] != 'PASS':
                self.end('SAFETY_FAILURE')
                return
            self.callback_finished = True
            self.phase = 'WAITING_OPERATOR_COMPLETION'
            if self.operator_complete:
                self.end('OBSERVABILITY_INCOMPLETE' if self.auth_time == 'NOT_CAPTURED' else 'PROTOCOL_COMPLETE_IDENTITY_GATE_PENDING')
            return
        if not event['pass']:
            if event['category'] == 'nonce_mismatch':
                self.stages['nonce'] = 'FAIL'
            self.end('SAFETY_FAILURE')

    def freshness(self, trace, value, source='signed_id_token'):
        if self.finished:
            return
        if source != 'signed_id_token':
            return  # UserInfo cannot supply signed auth_time evidence.
        try:
            uuid.UUID(trace)
        except (ValueError, TypeError):
            self.end('OBSERVABILITY_INCOMPLETE')
            return
        if value not in EVIDENCE or self.callback_trace not in (None, trace) or self.freshness_trace not in (None, trace):
            self.end('CORRELATION_AMBIGUOUS')
            return
        self.freshness_trace = trace
        self.auth_time = value
        if value == 'INVALID_OR_STALE':
            self.end('SAFETY_FAILURE')

    def operator(self, action):
        if self.finished:
            return
        if action == 'password_page_observed':
            return  # Explicitly does not end, restore or invalidate.
        if action == 'authentication_complete':
            self.operator_complete = True
            if self.callback_finished:
                self.end('OBSERVABILITY_INCOMPLETE' if self.auth_time == 'NOT_CAPTURED' else 'PROTOCOL_COMPLETE_IDENTITY_GATE_PENDING')
        elif action == 'cancel':
            self.end('CANCELLED_INCOMPLETE')
        else:
            # Ambiguous prose/placeholder cannot authorize termination.
            return

    def tick(self, now):
        if self.finished:
            return
        if now >= (self.deadline if self.deadline is not None else self.ready_deadline):
            self.end('INCOMPLETE_TIMEOUT')

    def receipt(self):
        result = dict(self.stages)
        if self.reason == 'OBSERVABILITY_INCOMPLETE':
            result = {k: ('NOT_CAPTURED' if v == 'NOT_EXECUTED' else v) for k, v in result.items()}
        return {'run_id': self.run_id, 'phase': self.phase, 'reason': self.reason, 'finished': self.finished,
                'stages': result, 'authorize_trace': self.authorize_trace, 'callback_trace': self.callback_trace,
                'auth_time': self.auth_time, 'operator_complete': self.operator_complete,
                'transaction_bound': self.bound is not None, 'wait_seconds': self.wait_seconds,
                'events': self.events, 'raw_logs_saved': False,
                'correlation_scope': 'one new transaction and one callback trace; not native shared authorize trace'}


def finalize(monitor, adapter):
    """Ordering contract: save safe evidence before any container restoration."""
    if not monitor.finished:
        raise RuntimeError('cannot_finalize_waiting_authentication')
    receipt = monitor.receipt()
    try:
        adapter.save_evidence(receipt)
    finally:
        # If storage fails, restoration still wins; caller must mark evidence
        # incomplete rather than claim success or leave the feature enabled.
        adapter.restore_false()
    # Account/session baselines must be checked by the real adapter.
    receipt['restore'] = adapter.verify_restored()
    if monitor.bound is not None and not monitor.bound.consumed:
        receipt['transaction_cleanup'] = adapter.invalidate_exact(monitor.bound.handle)
    adapter.save_final(receipt)
    return receipt
