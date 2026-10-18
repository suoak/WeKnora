"""Inert orchestration contract; no SID client, credentials or Docker globals.

The deployment adapter must implement the named operations. This controller
cannot switch anything by itself; missing approval never calls a mutation.
"""
from datetime import datetime, timezone

SCOPES = ('jit_policy', 'session_policy', 'maintenance_window', 'single_sid_transaction')


def approved(grants, now):
    if not isinstance(grants, dict) or set(grants) != set(SCOPES):
        raise ValueError('four_explicit_approvals_required')
    for name in SCOPES:
        g = grants[name]
        if not isinstance(g, dict) or set(g) != {'confirmed', 'reference', 'responsible', 'approved_at', 'valid_until'}:
            raise ValueError('approval_record_incomplete')
        if g['confirmed'] is not True:
            raise ValueError('approval_not_confirmed')
        for key in ('reference', 'responsible'):
            value = g[key]
            if not isinstance(value, str) or not value.strip() or any(x in value.lower() for x in ('synthetic', 'placeholder', 'todo')):
                raise ValueError('real_approval_required')
        try:
            start = datetime.fromisoformat(g['approved_at'])
            end = datetime.fromisoformat(g['valid_until'])
            if start.utcoffset() is None or end.utcoffset() is None or not start <= now < end:
                raise ValueError()
        except (ValueError, TypeError):
            raise ValueError('approval_time_invalid') from None


def check_target(v):
    fixed = {'host': '172.30.232.42', 'original_project': 'knowhub-company-test',
             'fresh_project': 'knowhub-jit-fresh', 'revision': '5f988be51779f1a22bd9d027b7503fe6a8749f62',
             'schema': 3030, 'dirty': False, 'fresh_empty': True,
             'original_active_transactions': 0, 'fresh_active_transactions': 0,
             'separate_db_redis_files_keys': True, 'candidate_syntax_ok': True}
    if not isinstance(v, dict) or any(v.get(k) != x for k, x in fixed.items()):
        raise ValueError('target_or_isolation_mismatch')


def restore(port):
    """Try every recovery step; one failure must never suppress gateway recovery.

    Save evidence first. Revocation adapter must use exact attributed new-session
    identifiers, never employee values or blanket deletion. Keep account/audit.
    """
    outcomes = {}
    for name in ('save_safe_evidence', 'close_fresh_gates', 'revoke_exact_new_session',
                 'restore_original_gateway', 'drain_connections',
                 'verify_original_routes_and_data', 'disconnect_fresh_gateway_network'):
        try:
            if getattr(port, name)() is not True:
                raise RuntimeError()
            outcomes[name] = 'PASS'
        except Exception:
            outcomes[name] = 'FAIL'
    return {'result': 'RESTORED' if all(v == 'PASS' for v in outcomes.values()) else 'MANUAL_RECOVERY_REQUIRED',
            'steps': outcomes}


def prepare(port, grants=None, apply=False, now=None):
    """Default only reads preflight. Never authorizes or exchanges an OIDC code."""
    check_target(port.readonly_preflight())
    if not apply:
        return {'result': 'PREPARED_NOT_SWITCHED', 'real_sid_requests': 0,
                'policy_approved': False, 'missing_approval_scopes': list(SCOPES)}
    approved(grants, now or datetime.now(timezone.utc))
    if port.verify_authority_and_approval_sources(grants) is not True:
        raise ValueError('approval_source_not_verified')
    # No configuration may change until latest backups and recovery/watchdog
    # are verified. The port must match effective policy to these grant refs.
    port.latest_paired_backup_and_recovery_check()
    try:
        port.arm_restore_watchdog()
        port.configure_fresh_approved_policy(grants)
        port.verify_fresh_effective_policy()
        port.prepare_whitelisted_observer()
        port.pause_entry_and_drain_connections()
        port.connect_fresh_gateway_network()
        port.install_candidate_and_validate()
        port.reload_gateway()
        port.verify_all_routes_without_credentials()
        port.start_single_transaction_observer()
        return {'result': 'READY_FOR_USER', 'real_sid_requests': 0,
                'automatic_login': False}
    except Exception:
        return {'result': 'PREPARATION_FAILED_NO_LOGIN', 'recovery': restore(port)}


if __name__ == '__main__':
    import json
    print(json.dumps({'result': 'NOT_EXECUTED', 'reason': 'requires_reviewed_deployment_adapter_and_four_approvals'}))
