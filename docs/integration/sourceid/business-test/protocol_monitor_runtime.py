"""Gated packaging-host adapter for protocol_monitor; inert unless authorized.
Deploy source files/filter beside this script only after separate execution
authorization. No request is ever sent to SID or the authorize endpoint here.
"""
import argparse
import contextlib
import datetime
import importlib.util
import json
import os
from pathlib import Path
import re
import subprocess
import time

from protocol_monitor import Monitor, Transaction, finalize, KNOWN, CATEGORIES

LIVE = Path('/home/aqyfzx/ai/knowhub-sourceid-business-test-db1b2cd05')
RELEASE = Path('/home/aqyfzx/ai/knowhub-prompt-login-479416c90')
APP = 'knowhub-sourceid-business-app-1'
PG = 'knowhub-sourceid-business-postgres-1'
IMAGE = 'sha256:85f9d6c052298e7f52e7d7c4533e131ce19326d461521378e90c6df3b01fa49c'
FLAG_KEYS = ('SOURCEID_FORCE_REAUTH_PROMPT_LOGIN', 'SOURCEID_PROTOCOL_ONLY', 'SOURCEID_CONTRACT_CONFIRMED', 'SOURCEID_EMAIL_BINDING_ENABLE', 'SOURCEID_EMAIL_CONTRACT_CONFIRMED', 'SOURCEID_AUTO_BINDING_ENABLE', 'SOURCEID_AUTHORIZATION_RECHECK_CONFIRMED', 'SOURCEID_SESSION_MODE', 'SOURCEID_LOCAL_REFRESH_ENABLE', 'SOURCEID_BASIC_ENCODING', 'OIDC_AUTH_ENABLE', 'SOURCEID_DIAGNOSTICS_ENABLE', 'AUTO_MIGRATE')


def run(args, data=None, timeout=120):
    p = subprocess.run(args, input=data, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=timeout)
    if p.returncode:
        raise RuntimeError('controlled_command_failed')
    return p.stdout


def inspect(name, field):
    return json.loads(run(['docker', 'inspect', '--format', '{{json ' + field + '}}', name]))


def atomic_json(path, value):
    raw = json.dumps(value, indent=2).encode()
    if len(raw) > 1048576:
        raise RuntimeError('receipt_budget_exceeded')
    temp = path.with_suffix('.tmp')
    with temp.open('wb') as f:
        f.write(raw)
        f.flush()
        os.fsync(f.fileno())
    temp.chmod(0o600)
    temp.replace(path)


class PackagingAdapter:
    def __init__(self, monitor):
        self.monitor = monitor
        self.root = RELEASE / 'private' / ('monitor-' + monitor.run_id)
        self.files = json.loads((RELEASE / 'app-deployment-result.json').read_text())['compose_files_in_order']
        self.override = self.root / 'once.yml'
        self.started = datetime.datetime.now(datetime.timezone.utc).isoformat()
        self.changed = False
        self.safe_events = []
        self.event_ids = set()
        self.bytes = 0
        self.operator_file_seen = None
        self.exporter = None

    def query(self, q, mutate=False):
        prefix = "SET log_statement='none';SET log_min_error_statement='panic';SET log_parameter_max_length_on_error=0;"
        query = prefix + ('BEGIN;' if mutate else 'BEGIN READ ONLY;') + q + (';COMMIT;' if mutate else ';ROLLBACK;')
        return run(['docker', 'exec', '-i', PG, 'sh', '-c', 'exec psql -X -U "$POSTGRES_USER" -d "$POSTGRES_DB" -qAt -v ON_ERROR_STOP=1'], query.encode(), 20).decode().strip()

    def transactions(self):
        # Handles are opaque existing DB keys, private to this process. Never
        # emit them or original Result/nonce/verifier into a receipt.
        q = """SELECT COALESCE(json_agg(s),'[]') FROM (
        SELECT key,EXTRACT(EPOCH FROM expires_at) AS expires,consumed,
        CASE WHEN NOT consumed AND nonce<>'' AND result LIKE '{"version":1,"prompt":"login",%'
        THEN EXTRACT(EPOCH FROM (result::jsonb->>'started')::timestamptz) ELSE NULL END AS created,
        CASE WHEN NOT consumed AND nonce<>'' AND result LIKE '{"version":1,"prompt":"login",%' THEN TRUE ELSE FALSE END AS prompt
        FROM oidc_transactions ORDER BY expires_at LIMIT 10001) s"""
        rows = json.loads(self.query(q))
        if len(rows) > 10000:
            raise RuntimeError('transaction_snapshot_budget_exceeded')
        return [Transaction(r['key'], float(r['created']) if r['created'] is not None else float(r['expires']) - 600, float(r['expires']), r['consumed'], r['prompt']) for r in rows]

    def flags(self):
        out = {}
        for k in FLAG_KEYS:
            v = run(['docker', 'exec', APP, 'sh', '-c', 'printf "%s" "${' + k + '-NOT_SET}"']).decode()
            if v not in ('true', 'false', 'oauth_form', 'NOT_SET', 'external'):
                raise RuntimeError('flag_contract_changed')
            out[k] = v
        return out

    def counts(self):
        tables = ('users', 'tenant_members', 'external_identities', 'source_id_binding_reviews', 'source_id_sessions', 'source_id_local_sessions')
        return {t: int(self.query('SELECT COUNT(*) FROM ' + t)) for t in tables}

    def ownership(self):
        # Digests never leave process memory, including in error receipts.
        return [self.query("SELECT COALESCE(md5(string_agg(row_to_json(s)::text,'')),'') FROM (SELECT " + cols + ' FROM ' + table + ' ORDER BY id) s') for table, cols in [('users', 'id,tenant_id,email,is_active,is_system_admin,can_access_all_tenants'), ('tenant_members', 'id,user_id,tenant_id,role,status,deleted_at')]]

    def containers(self):
        ids = run(['docker', 'ps', '-a', '--filter', 'label=com.docker.compose.project=knowhub-sourceid-business', '--format', '{{.ID}}']).decode().split()
        return {inspect(i, '.Config.Labels').get('com.docker.compose.service'): {'id': inspect(i, '.Id'), 'image': inspect(i, '.Image'), 'started': inspect(i, '.State.StartedAt'), 'running': inspect(i, '.State.Running'), 'mounts': inspect(i, '.Mounts')} for i in ids}

    def compose(self, override=False):
        args = ['docker', 'compose', '--project-name', 'knowhub-sourceid-business', '--project-directory', str(LIVE)]
        for f in self.files + ([str(self.override)] if override else []):
            args += ['-f', f]
        return args

    def health(self):
        for _ in range(90):
            if inspect(APP, '.Image') == IMAGE and inspect(APP, '.State.Health.Status') == 'healthy':
                return
            time.sleep(1)
        raise RuntimeError('app_health_timeout')

    def prepare(self):
        if inspect(APP, '.Image') != IMAGE or inspect(APP, '.Config.Labels').get('com.docker.compose.project.working_dir') != str(LIVE):
            raise RuntimeError('target_mismatch')
        self.before = self.containers()
        if inspect(APP, '.Config.Labels').get('com.docker.compose.project.config_files', '').split(',') != self.files:
            raise RuntimeError('compose_order_changed')
        self.original = self.flags()
        self.original_counts = self.counts()
        self.original_ownership = self.ownership()
        if self.query("SELECT version::text||'|'||dirty::text FROM schema_migrations") != '3027|false':
            raise RuntimeError('schema_mismatch')
        initial_transactions = self.transactions()
        if any(not tx.consumed and tx.expires > time.time() for tx in initial_transactions):
            raise RuntimeError('active_transaction_exists')
        initial_handles = {tx.handle for tx in initial_transactions}
        required = {'SOURCEID_FORCE_REAUTH_PROMPT_LOGIN': 'false', 'SOURCEID_PROTOCOL_ONLY': 'true', 'SOURCEID_CONTRACT_CONFIRMED': 'false', 'SOURCEID_EMAIL_BINDING_ENABLE': 'false', 'SOURCEID_AUTO_BINDING_ENABLE': 'false', 'SOURCEID_AUTHORIZATION_RECHECK_CONFIRMED': 'false', 'SOURCEID_BASIC_ENCODING': 'oauth_form', 'SOURCEID_SESSION_MODE': 'NOT_SET', 'OIDC_AUTH_ENABLE': 'true', 'SOURCEID_DIAGNOSTICS_ENABLE': 'true', 'AUTO_MIGRATE': 'false'}
        if any(self.original[k] != v for k, v in required.items()):
            raise RuntimeError('gate_mismatch')
        self.root.mkdir(mode=0o700)
        atomic_json(self.root / 'before-flags.json', self.original)
        atomic_json(self.root / 'before-containers.json', self.before)
        atomic_json(self.root / 'compose-order.json', {'files': self.files, 'image': IMAGE})
        # Exporter/filter package is copied as reviewed source, no credentials.
        path = Path(__file__).with_name('export_token_diagnostics.py')
        spec = importlib.util.spec_from_file_location('safe_monitor_export', path)
        self.exporter = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(self.exporter)
        self.override.write_text('services:\n  app:\n    environment:\n      SOURCEID_FORCE_REAUTH_PROMPT_LOGIN: "true"\n')
        self.override.chmod(0o600)
        self.changed = True  # Covers partial failure of container recreation.
        run(self.compose(True) + ['up', '-d', '--no-deps', '--no-build', '--pull', 'never', '--force-recreate', 'app'])
        self.health()
        if self.flags() != dict(self.original, SOURCEID_FORCE_REAUTH_PROMPT_LOGIN='true'):
            raise RuntimeError('enabled_flags_mismatch')
        self.monitor.baseline = initial_handles
        # Prevent unexplained activity during restart from being hidden.
        if any(tx.handle not in initial_handles for tx in self.transactions()):
            raise RuntimeError('activity_during_prepare')
        self.monitor.ready_deadline = time.time() + 120
        atomic_json(self.root / 'status.json', {'run_id': self.monitor.run_id, 'phase': 'READY', 'prompt': True, 'ready_deadline': self.monitor.ready_deadline})

    def poll(self):
        # Read source logs before DB snapshot so callback consumption observed
        # between polls is reflected before processing callback events.
        # Docker logs normally emits application stderr on stderr: capture both
        # without allowing a raw subprocess exception to escape.
        p = subprocess.run(['docker', 'logs', '--since', self.started, '--tail', '3000', APP], stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=15)
        if p.returncode or len(p.stdout) > 2097152 or len(p.stdout.splitlines()) >= 3000:
            self.monitor.end('OBSERVABILITY_INCOMPLETE')
            return
        self.monitor.transactions(self.transactions(), time.time())
        for line in p.stdout.decode('utf-8', 'replace').splitlines():
            if len(line) > 65536:
                self.monitor.end('OBSERVABILITY_INCOMPLETE')
                break
            event = self.exporter.extract(line)
            if event is not None:
                encoded = json.dumps(event, sort_keys=True)
                if encoded not in self.event_ids:
                    self.event_ids.add(encoded)
                    self.bytes += len(encoded.encode())
                    if len(self.safe_events) >= 256 or self.bytes > 786432:
                        self.monitor.end('OBSERVABILITY_INCOMPLETE')
                        break
                    self.safe_events.append(event)
                    if event['event'] == 'sourceid_reauthentication':
                        self.monitor.freshness(event['trace'], event['evidence'])
                    elif event['event'] in ('sourceid_token_request', 'sourceid_token_send', 'sourceid_token_http'):
                        if self.monitor.callback_trace != event['trace']:
                            self.monitor.end('CORRELATION_AMBIGUOUS')
                        if event['event'] == 'sourceid_token_send' and event['transport']['observed_write_attempts'] > 1:
                            self.monitor.end('UNEXPECTED_TOKEN_RETRY')
                continue
            offset = line.find('{"event"')
            if offset < 0:
                continue
            try:
                item = json.JSONDecoder().raw_decode(line[offset:])[0]
            except (TypeError, ValueError):
                continue
            if not isinstance(item, dict):
                continue
            if item.get('event') == 'sourceid_diagnostics_limit':
                self.monitor.end('OBSERVABILITY_INCOMPLETE')
            elif item.get('event') == 'sourceid_stage':
                if item.get('stage') not in KNOWN or item.get('category') not in CATEGORIES:
                    self.monitor.end('OBSERVABILITY_INCOMPLETE')
                    break
                self.monitor.stage({k: item.get(k) for k in ('trace', 'stage', 'pass', 'duration_ms', 'category')})
        signal = self.root / 'operator.json'
        if signal.exists():
            if signal.stat().st_size > 1024:
                self.monitor.end('OBSERVABILITY_INCOMPLETE')
            else:
                data = json.loads(signal.read_text())
                if set(data) == {'run_id', 'action'} and data['run_id'] == self.monitor.run_id:
                    self.monitor.operator(data['action'])
        self.monitor.tick(time.time())
        self.save_evidence(self.monitor.receipt())

    def save_evidence(self, receipt):
        atomic_json(self.root / 'pre-restore-evidence.json', dict(receipt, http_events=self.safe_events))

    def restore_false(self):
        if self.changed:
            run(self.compose(False) + ['up', '-d', '--no-deps', '--no-build', '--pull', 'never', '--force-recreate', 'app'])
            self.health()
            self.changed = False

    def verify_restored(self):
        if self.flags() != self.original or self.counts() != self.original_counts or self.ownership() != self.original_ownership:
            raise RuntimeError('restore_or_identity_baseline_changed')
        after = self.containers()
        if any(after[k] != v for k, v in self.before.items() if k != 'app'):
            raise RuntimeError('non_app_container_changed')
        if inspect(APP, '.Image') != IMAGE or self.query("SELECT version::text||'|'||dirty::text FROM schema_migrations") != '3027|false':
            raise RuntimeError('restore_version_changed')
        return 'PASS'

    def invalidate_exact(self, handle):
        # Strict existing internal handle format, no unknown transaction writes.
        if not re.fullmatch(r'[a-f0-9]{64}', handle) or self.monitor.bound is None or self.monitor.bound.handle != handle:
            raise RuntimeError('cleanup_handle_invalid')
        tx = self.monitor.bound
        rows = self.transactions()
        current = next((r for r in rows if r.handle == handle), None)
        if current is None or current.consumed:
            return 'NO_UNUSED_BOUND_TRANSACTION'
        if current.expires != tx.expires:
            raise RuntimeError('cleanup_transaction_changed')
        q = "UPDATE oidc_transactions SET consumed=TRUE,result='',verifier='' WHERE key='" + handle + "' AND NOT consumed AND result LIKE '{\"version\":1,\"prompt\":\"login\",%'"
        self.query(q, mutate=True)
        # Exact unique key and server-owned prompt metadata only; never bulk
        # invalidate by time window or touch another user's transaction.
        if any(r.handle == handle and not r.consumed for r in self.transactions()):
            raise RuntimeError('cleanup_not_confirmed')
        return 'EXACT_UNUSED_TRANSACTION_INVALIDATED'

    def save_final(self, receipt):
        atomic_json(self.root / 'final-result.json', dict(receipt, http_events=self.safe_events))


def main(argv=None):
    parser = argparse.ArgumentParser()
    parser.add_argument('--execute-authorized', action='store_true')
    parser.add_argument('--exclusive-window-confirmed', action='store_true')
    args = parser.parse_args(argv)
    if not args.execute_authorized or not args.exclusive_window_confirmed:
        print(json.dumps({'result': 'NOT_EXECUTED', 'requires_separate_real_test_authorization': True, 'exclusive_window_required': True}))
        return 0
    import fcntl
    import signal
    def interrupted(signum, frame):
        raise KeyboardInterrupt()
    signal.signal(signal.SIGTERM, interrupted)
    signal.signal(signal.SIGINT, interrupted)
    os.umask(0o077)
    # One local monitor only; does not exclude outside browser traffic.
    with (RELEASE / 'private/protocol-monitor.lock').open('a') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        monitor = Monitor(time.time())
        adapter = PackagingAdapter(monitor)
        try:
            adapter.prepare()
            while not monitor.finished:
                adapter.poll()
                time.sleep(0.25)
        except (Exception, KeyboardInterrupt):
            monitor.end('CONTROLLED_FAILURE_NO_RETRY')
        finally:
            if adapter.root.exists():
                try:
                    # Capture safe logs once before restore even on cancellation.
                    # Existing evidence is already saved on every completed poll.
                    finalize(monitor, adapter)
                except Exception:
                    # Evidence/storage failure must not prevent restoring false.
                    with contextlib.suppress(Exception):
                        adapter.restore_false()
                    atomic_json(adapter.root / 'recovery-status.json', {'run_id': monitor.run_id, 'result': 'RECOVERY_REQUIRES_REVIEW', 'reason': monitor.reason})
            print(json.dumps({'run_id': monitor.run_id, 'reason': monitor.reason, 'monitor_finished': monitor.finished, 'new_sid_requests_by_monitor': False}))
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
