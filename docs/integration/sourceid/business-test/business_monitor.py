"""Single-business-transaction collector. No authentication or config changes.

Reuses reviewed event vocabularies/filter, not the old protocol-only worker.
Opaque transaction/session/user handles are private to adapters, never receipts.
"""
import json
import math
import uuid

from protocol_monitor import KNOWN, CATEGORIES, Transaction
from export_token_diagnostics import extract

REVISION = '5f988be51779f1a22bd9d027b7503fe6a8749f62'
PROJECT = 'knowhub-company-test'
IMAGE = 'sha256:0994762cd32f4289dc397757f9123bafd3cc913b407c05927c7ee064b32ade2b'
SCHEMA = 3030
STAGES = ('authorize', 'callback', 'token', 'token_http', 'id_token', 'nonce', 'userinfo',
          'identity_mapping', 'local_session', 'callback_result', 'portal')
COMMON = {'auth_me_200', 'portal_loaded', 'no_refresh', 'single_bound_session',
          'session_access_bounded', 'knowledge_access_correct', 'logout_revoked',
          'session_user_matches', 'access_session_matches'}
EXISTING = {'original_user_id', 'roles_unchanged', 'memberships_unchanged',
            'acl_unchanged', 'no_new_user'}
JIT = {'one_new_user', 'single_identity_record', 'no_admin_flags',
       'no_memberships', 'no_default_knowledge_access', 'no_historical_collision'}


def valid_uuid(value):
    try:
        return isinstance(value, str) and str(uuid.UUID(value)) == value
    except (ValueError, TypeError):
        return False


class BusinessMonitor:
    def __init__(self, case, now, baseline=(), wait_seconds=480):
        if case not in ('existing', 'jit') or type(wait_seconds) is not int or not 1 <= wait_seconds <= 480:
            raise ValueError('invalid_monitor_contract')
        self.case = case
        self.run_id = str(uuid.uuid4())
        self.baseline = set(baseline)
        self.seen = set()
        self.bound = None
        self.deadline = now + 120
        self.wait_seconds = wait_seconds
        self.authorize_trace = self.callback_trace = None
        self.events = []
        self.http_events = []
        self.dedup = set()
        self.byte_count = 0
        self.raw_bytes = 0
        self.stages = {s: 'NOT_EXECUTED' for s in STAGES}
        self.probes = None
        self.operator_complete = False
        self.finished = False
        self.reason = None
        self.correlation_failure = None
        self.unassociated_stage_counts = {}

    def retain_batch_evidence(self, lines):
        """Keep bounded, classified stage counts before DB correlation can stop.

        Counts are observations only: never assign them to the tested callback
        or turn a stage into PASS. Discard values outside the closed vocabulary.
        """
        for raw in lines[:256]:
            if not isinstance(raw, str) or len(raw) > 65536:
                continue
            offset = raw.find('{"event"')
            if offset < 0:
                continue
            try:
                item = json.JSONDecoder().raw_decode(raw[offset:])[0]
            except (ValueError, TypeError):
                continue
            if not isinstance(item, dict) or item.get('event') != 'sourceid_stage':
                continue
            stage, passed, category = item.get('stage'), item.get('pass'), item.get('category')
            duration = item.get('duration_ms')
            if (not valid_uuid(item.get('trace')) or not isinstance(stage, str) or stage not in KNOWN or
                    type(passed) is not bool or not isinstance(category, str) or category not in CATEGORIES or
                    type(duration) is not int or not 0 <= duration <= 600000):
                continue
            key = stage + (':pass' if passed else ':fail')
            self.unassociated_stage_counts[key] = min(256, self.unassociated_stage_counts.get(key, 0) + 1)

    def end(self, reason):
        if not self.finished:
            self.finished = True
            self.reason = reason

    def tick(self, now):
        if not self.finished and now >= self.deadline:
            self.end('INCOMPLETE_TIMEOUT')

    def transactions(self, rows, now):
        if self.finished:
            return
        for tx in rows:
            if tx.handle in self.baseline:
                continue
            if not isinstance(tx, Transaction) or not tx.handle or tx.prompt_login is not False:
                self.correlation_failure = 'TRANSACTION_METADATA_INVALID'
                self.end('CORRELATION_AMBIGUOUS')
                return
            self.seen.add(tx.handle)
            if len(self.seen) != 1:
                self.correlation_failure = 'MULTIPLE_NEW_AUTHORIZATIONS'
                self.end('CORRELATION_AMBIGUOUS')
                return
            if self.bound is None:
                if tx.consumed:
                    self.correlation_failure = 'FIRST_OBSERVATION_ALREADY_CONSUMED'
                    self.end('OBSERVABILITY_INCOMPLETE')
                    return
                if not all(math.isfinite(v) for v in (tx.created, tx.expires)) or tx.created > now or tx.expires <= now:
                    self.correlation_failure = 'TRANSACTION_TIME_INVALID'
                    self.end('CORRELATION_AMBIGUOUS')
                    return
                self.bound = tx
                self.deadline = min(now + self.wait_seconds, tx.expires)
            elif tx.handle == self.bound.handle:
                if tx.expires != self.bound.expires or self.bound.consumed and not tx.consumed:
                    self.correlation_failure = 'TRANSACTION_CHANGED'
                    self.end('CORRELATION_AMBIGUOUS')
                    return
                self.bound.consumed = tx.consumed
        self.tick(now)

    def _append(self, value, dest):
        encoded = json.dumps(value, sort_keys=True)
        if encoded in self.dedup:
            return False
        self.dedup.add(encoded)
        self.byte_count += len(encoded.encode())
        if len(self.events) + len(self.http_events) >= 256 or self.byte_count > 786432:
            self.end('OBSERVABILITY_INCOMPLETE')
            return False
        dest.append(value)
        return True

    def line(self, raw):
        """Raw line stays in this call only; never copy arbitrary log fields."""
        if self.finished:
            return
        self.raw_bytes += len(raw.encode())
        if len(raw) > 65536 or self.raw_bytes > 10 * 1024 * 1024:
            self.end('OBSERVABILITY_INCOMPLETE')
            return
        safe = extract(raw)
        if safe is not None:
            if self.callback_trace is None or safe['trace'] != self.callback_trace:
                self.end('CORRELATION_AMBIGUOUS')
                return
            if safe['event'] == 'sourceid_token_send' and safe['transport']['observed_write_attempts'] > 1:
                self.end('UNEXPECTED_TOKEN_RETRY')
                return
            added = self._append(safe, self.http_events)
            if added and safe['event'] == 'sourceid_token_http':
                rows = [row for row in safe['http'] if row['stage'] == 'token']
                summaries = [e for e in self.http_events if e['event'] == 'sourceid_token_http']
                if len(rows) != 1 or len(summaries) != 1:
                    self.end('CORRELATION_AMBIGUOUS')
                    return
                row = rows[0]
                if row['status'] != 200:
                    self.stages['token_http'] = self.stages['token'] = 'FAIL'
                    self.end('SAFETY_FAILURE')
                    return
                transport = row.get('transport')
                if transport is not None:
                    if transport['observed_write_attempts'] != 1 or transport['retry_observed']:
                        self.stages['token_http'] = 'FAIL'
                        self.end('UNEXPECTED_TOKEN_RETRY')
                        return
                    self.stages['token_http'] = 'PASS'
            self._complete()
            return
        offset = raw.find('{"event"')
        if offset < 0:
            return
        try:
            item = json.JSONDecoder().raw_decode(raw[offset:])[0]
        except (ValueError, TypeError):
            return
        if not isinstance(item, dict):
            return
        if item.get('event') == 'sourceid_diagnostics_limit':
            self.end('OBSERVABILITY_INCOMPLETE')
        elif item.get('event') == 'sourceid_stage':
            # Additional fields are not retained, even plausible credentials.
            self.stage({k: item.get(k) for k in ('trace', 'stage', 'pass', 'duration_ms', 'category')})

    def stage(self, e):
        if self.finished:
            return
        if (set(e) != {'trace', 'stage', 'pass', 'duration_ms', 'category'} or
                not valid_uuid(e['trace']) or e['stage'] not in KNOWN or e['category'] not in CATEGORIES or
                type(e['pass']) is not bool or type(e['duration_ms']) is not int or not 0 <= e['duration_ms'] <= 600000):
            self.end('OBSERVABILITY_INCOMPLETE')
            return
        if not self._append(dict(e), self.events):
            return
        name = e['stage']
        if name == 'authorize':
            if self.authorize_trace is not None:
                self.end('CORRELATION_AMBIGUOUS')
                return
            self.authorize_trace = e['trace']
        else:
            if self.bound is None or not self.bound.consumed or self.authorize_trace is None:
                self.end('CORRELATION_AMBIGUOUS')
                return
            if self.callback_trace not in (None, e['trace']):
                self.end('CORRELATION_AMBIGUOUS')
                return
            self.callback_trace = e['trace']
        name = 'callback' if name == 'callback_state' else name
        previous = {'callback': 'authorize', 'token': 'callback', 'id_token': 'token',
                    'userinfo': 'id_token', 'identity_mapping': 'userinfo',
                    'local_session': 'identity_mapping', 'callback_result': 'local_session'}.get(name)
        if previous and self.stages[previous] != 'PASS':
            self.end('OBSERVABILITY_INCOMPLETE')
            return
        self.stages[name] = 'PASS' if e['pass'] else 'FAIL'
        if name == 'id_token' and e['pass']:
            # Exact f40 code emits PASS only after its strict nonce check.
            self.stages['nonce'] = 'PASS'
        if not e['pass']:
            if e['category'] == 'nonce_mismatch':
                self.stages['nonce'] = 'FAIL'
            self.end('SAFETY_FAILURE')
        self._complete()

    def business_probes(self, trace, values):
        if self.finished:
            return
        keys = COMMON | (EXISTING if self.case == 'existing' else JIT)
        if trace != self.callback_trace or self.stages['callback_result'] != 'PASS':
            self.end('CORRELATION_AMBIGUOUS')
            return
        if set(values) != keys or any(type(v) is not bool for v in values.values()):
            self.end('OBSERVABILITY_INCOMPLETE')
            return
        self.probes = dict(values)
        self.stages['portal'] = 'PASS' if values['portal_loaded'] else 'FAIL'
        if not all(values.values()):
            self.end('BUSINESS_INVARIANT_FAILURE')
        self._complete()

    def operator(self, action):
        if action == 'cancel':
            self.end('CANCELLED_INCOMPLETE')
        elif action == 'authentication_complete':
            self.operator_complete = True
            self._complete()
        # Password page, arbitrary prose and placeholders cannot finish a run.

    def _complete(self):
        if not self.finished and self.operator_complete and self.probes is not None and all(self.probes.values()) and all(v == 'PASS' for v in self.stages.values()):
            self.end('BUSINESS_ACCEPTANCE_PASS')

    def receipt(self):
        stages = dict(self.stages)
        if self.stages['callback_result'] == 'PASS' and self.probes is None:
            stages['portal'] = 'NOT_CAPTURED'
        if self.stages['token'] == 'PASS' and self.stages['token_http'] == 'NOT_EXECUTED':
            stages['token_http'] = 'NOT_CAPTURED'
        return {'run_id': self.run_id, 'case': self.case, 'finished': self.finished,
                'result': 'PASS' if self.reason == 'BUSINESS_ACCEPTANCE_PASS' else
                          ('INCOMPLETE' if self.reason in (None, 'INCOMPLETE_TIMEOUT', 'CANCELLED_INCOMPLETE', 'OBSERVABILITY_INCOMPLETE') else 'FAIL'),
                'reason': self.reason, 'stages': stages, 'business_probes': self.probes,
                'authorize_trace': self.authorize_trace, 'callback_trace': self.callback_trace,
                'transaction_bound': self.bound is not None, 'wait_seconds': self.wait_seconds,
                'prompt_login_required': False, 'auth_time_required': False,
                'events': self.events, 'http_events': self.http_events, 'raw_logs_saved': False,
                'correlation_scope': 'exclusive single new transaction + one callback trace; no native shared authorize trace',
                'observation_mode': 'SERVER_METADATA_AND_USER_PORTAL_CONFIRMATION' if getattr(self,'observation_mode',None)=='SERVER_METADATA_AND_USER_PORTAL_CONFIRMATION' else 'PROTECTED_EXTERNAL_OBSERVER',
                'acceptance_scope':'A_positive' if getattr(self,'acceptance_scope',None)=='A_positive' else 'full',
                'cross_space_negative':'NOT_EXECUTED' if getattr(self,'acceptance_scope',None)=='A_positive' else ('PASS' if self.probes and self.probes.get('knowledge_access_correct') else 'NOT_CAPTURED'),
                'link_policy_verification':'PASS' if getattr(self,'link_policy_verification',None)=='PASS' else 'NOT_CAPTURED',
                'control_failure':getattr(self,'control_failure',None),
                'correlation_failure':self.correlation_failure,
                'unassociated_stage_counts':dict(self.unassociated_stage_counts)}


def finish(m, adapter):
    """Always save before restore. Revoke only an unambiguous exact session."""
    if not m.finished:
        raise RuntimeError('still_waiting_for_authentication')
    result = m.receipt()
    failures = []
    for name, action in (
        ('evidence_save', lambda: adapter.save_evidence(result)),
        ('restore_gates', adapter.restore_gates),
        ('targeted_session_revocation', lambda: adapter.revoke_bound_session(m)),
        ('unused_transaction_cleanup', lambda: adapter.cleanup_transaction(m)),
        ('restore_verification', adapter.verify_restored),
    ):
        try:
            result[name] = action()
        except Exception:
            result[name] = 'FAIL'
            failures.append(name)
    if failures:
        result['result'] = 'FAIL'
        result['recovery'] = 'REQUIRES_REVIEW'
        result['observed_reason'] = result['reason']
        if result['reason'] == 'BUSINESS_ACCEPTANCE_PASS':
            result['reason'] = 'RECOVERY_OR_EVIDENCE_FAILURE'
    adapter.save_final(result)
    return result
