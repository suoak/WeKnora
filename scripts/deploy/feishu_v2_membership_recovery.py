"""Report25 recovery of the second incident; never reuses report19 approval.

Separate approval and state, exact source-state and pending-task preconditions.
No operation changes source hold, cursor, budget, whitelist or namespace.
"""
import argparse
import re
import sys

import feishu_v2_approved_control_recovery as prior

APPROVAL = 'user:report25:approved-membership-and-held-source-reconciliation'
STOP = 'automatic_membership_probe_context_deadline'


def replace_once(value, before, after):
    if value.count(before) != 1:
        raise ValueError('recovery_protocol_anchor_changed')
    return value.replace(before, after, 1)


def program():
    remote = replace_once(prior.REMOTE,
        "APPROVAL='user:call_085aca217ce54805be1d8372feec744b:approved_two_sources'",
        'APPROVAL=' + repr(APPROVAL))
    remote = replace_once(remote, "directory=site/'approved-control-recovery-20261026'",
        "directory=site/'approved-membership-recovery-report25'")
    # Retain archive and image provenance checks for the selected fixed source.
    remote = replace_once(remote,
        "if SHA!=FIX:\n if OPERATION!='status':raise ValueError('recovery_mutations_bound_to_original_approved_source')",
        'if True:')
    remote = remote.replace('automatic_permit_consumer_lost', STOP)
    remote = replace_once(remote,
        "row['status']!='active' or row['legacy_access_state'] in ['paused','hold','revoked'] or row['auth_mode']!='user_oauth'",
        "row['status'] not in ['active','error'] or row['auth_mode']!='user_oauth' or "
        "(mask(row['id'])=='6d1698a6e0d9' and row['legacy_access_state']!='unmanaged') or "
        "(mask(row['id'])=='1dcf458346ba' and row['legacy_access_state']!='hold')")
    remote = replace_once(remote,
        "or payload.get('force_full'):\n   raise ValueError('pending_task_outside_approved_scope')",
        ":\n   raise ValueError('pending_task_outside_approved_scope')\n"
        "  if payload.get('force_full'):\n"
        "   log=payload.get('sync_log_id','')\n"
        "   if (mask(payload.get('data_source_id'))!='1dcf458346ba' or "
        "payload.get('trigger')!='approved_reconciliation' or mask(log)!=RECONCILIATION_LOG_HASH or "
        "not re.fullmatch('[a-zA-Z0-9-]{1,64}',log)):\n"
        "    raise ValueError('unapproved_full_reconciliation_task')\n"
        "   if sql(\"SELECT count(*) FROM sync_logs WHERE id='\"+log+\"' AND status='running' "
        "AND execution_generation=0 AND execution_claimed=false\")!=1:\n"
        "    raise ValueError('approved_reconciliation_run_not_pending')\n"
        "   reconciliation_seen=True")
    remote = replace_once(remote, "allowed={v['id'] for v in state['sources']}\n",
        "allowed={v['id'] for v in state['sources']}\n reconciliation_seen=False\n")
    remote = replace_once(remote, "return [v.decode() for v in ids]",
        "if not reconciliation_seen:raise ValueError('approved_reconciliation_must_be_queued')\n return [v.decode() for v in ids]")
    return remote


def main():
    parser = argparse.ArgumentParser(description=__doc__, add_help=False)
    parser.add_argument('--operation', choices=['status', 'self-test', 'prepare', 'quiesce', 'reset', 'restart', 'resume'], default='status')
    parser.add_argument('--approval-reference')
    parser.add_argument('--reconciliation-log-hash', default='')
    options, remaining = parser.parse_known_args()
    if '--mode' in remaining:
        raise SystemExit('use the explicit report25 operation')
    if options.operation not in ['status', 'self-test'] and options.approval_reference != APPROVAL:
        raise SystemExit('new explicit report25 approval required')
    if options.operation == 'resume' and not re.fullmatch('[a-f0-9]{12}', options.reconciliation_log_hash):
        raise SystemExit('approved reconciliation receipt required')
    prior.transport.REMOTE = ('OPERATION=' + repr(options.operation) + '\nRESET=' +
        repr(prior.RESET.replace('automatic_permit_consumer_lost', STOP)) + '\nRESUME=' +
        repr(prior.RESUME) + '\nRECONCILIATION_LOG_HASH=' + repr(options.reconciliation_log_hash) + '\n' + program())
    sys.argv = [sys.argv[0], *remaining, '--mode', 'status']
    return prior.transport.main()


if __name__ == '__main__':
    raise SystemExit(main())
