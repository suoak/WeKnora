"""Audited CAS recovery for the explicitly authorized existing isolated pair."""
import argparse
import sys

import feishu_v2_approved_control_recovery as base
import feishu_v2_membership_recovery as previous

AUTHORIZATION = 'user:2026-10-27:accelerated-two-source-validation'


def program():
    remote = base.REMOTE
    remote = previous.replace_once(remote,
        "'ZCARD','INFO','LATENCY','SLOWLOG']",
        "'ZCARD','INFO','LATENCY','SLOWLOG','HLEN','LRANGE','HGET','EVAL']")
    remote = previous.replace_once(remote,
        "APPROVAL='user:call_085aca217ce54805be1d8372feec744b:approved_two_sources'",
        'APPROVAL=' + repr(AUTHORIZATION))
    remote = previous.replace_once(remote,
        "if SHA!=FIX:\n if OPERATION!='status':raise ValueError('recovery_mutations_bound_to_original_approved_source')",
        'if True:')
    remote = remote.replace('automatic_permit_consumer_lost', 'audit_unavailable')
    remote = remote.replace("directory=site/'approved-control-recovery-20261026'",
                            "directory=site/'accelerated-control-recovery-20261027'")
    remote = previous.replace_once(remote,
        "row['status']!='active' or row['legacy_access_state'] in ['paused','hold','revoked'] or row['auth_mode']!='user_oauth'",
        "row['status'] not in ['active','error'] or row['legacy_access_state'] not in ['hold','unmanaged','active'] or row['auth_mode']!='user_oauth'")
    old = previous.program()
    backup = old[old.index("elif OPERATION=='backup':"):old.index("elif OPERATION=='reset':")]
    # Snapshot and restore are fresh for this run, never reuse an old approval.
    backup = backup.replace('feishu_report25_restore_', 'feishu_accelerated_control_restore_')
    remote = previous.replace_once(remote, "elif OPERATION=='reset':", backup + "elif OPERATION=='reset':\n if not state.get('backup_verified'):raise ValueError('verified_backup_required')")
    remote = previous.replace_once(remote,
        "or payload.get('force_full'):\n   raise ValueError('pending_task_outside_approved_scope')",
        ":\n   raise ValueError('pending_task_outside_approved_scope')\n"
        "  if payload.get('force_full'):\n"
        "   log=payload.get('sync_log_id','')\n"
        "   if payload.get('trigger')!='approved_reconciliation' or not re.fullmatch('[a-zA-Z0-9-]{1,64}',log):raise ValueError('unapproved_full_run')\n"
        "   if sql(\"SELECT count(*) FROM sync_logs WHERE id='\"+log+\"' AND data_source_id='\"+payload['data_source_id']+\"' AND status='running' AND execution_generation=0 AND execution_claimed=false\")!=1:raise ValueError('full_run_not_fresh')")
    return remote


if __name__ == '__main__':
    parser = argparse.ArgumentParser(add_help=False)
    parser.add_argument('--operation', choices=['status','self-test','prepare','quiesce','backup','reset','restart','resume'], default='status')
    options, remaining = parser.parse_known_args()
    base.transport.REMOTE = ('OPERATION=' + repr(options.operation) + '\nRESET=' +
        repr(base.RESET.replace('automatic_permit_consumer_lost','audit_unavailable')) +
        '\nRESUME=' + repr(base.RESUME) + '\n' + program())
    sys.argv = [sys.argv[0], *remaining, '--mode', 'status']
    raise SystemExit(base.transport.main())
