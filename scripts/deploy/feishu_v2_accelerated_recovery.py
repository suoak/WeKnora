"""Audited CAS recovery for the explicitly authorized existing isolated pair."""
import argparse
import sys

import feishu_v2_approved_control_recovery as base
import feishu_v2_membership_recovery as previous

AUTHORIZATION = 'user:2026-10-27:accelerated-two-source-validation'


def program(incident='audit_unavailable'):
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
    remote = remote.replace('automatic_permit_consumer_lost', incident)
    remote = remote.replace("directory=site/'approved-control-recovery-20261026'",
                            "directory=site/"+repr('accelerated-control-recovery-20261027' + ('-embedding' if incident=='embedding_http_400' else '')))
    terminal = previous.program()
    terminal = terminal[terminal.index('def terminal_pending_ids():'):terminal.index('def quiet():')]
    terminal = terminal.replace("or payload.get('force_full') or",
        "or (payload.get('force_full') and payload.get('trigger')!='approved_reconciliation') or")
    remote = previous.replace_once(remote, 'def quiet():', terminal+'\ndef quiet():')
    remote = remote.replace('return not any(queue_counts().values())',
        "counts=queue_counts()\n if any(counts[k] for k in ['active','scheduled','retry']):return False\n terminal_pending_ids()\n return True")
    remote = remote.replace("if any(queue_counts().values()):raise ValueError('queue_not_quiescent')",
        "if not quiet():raise ValueError('queue_not_quiescent')")
    remote = remote.replace('cfg=configuration();topology();rows=sources()',
        "cfg=configuration()\n if any(v['running'] for v in cfg.values()):topology()\n elif not quiet():raise ValueError('stopped_topology_not_quiescent')\n rows=sources()")
    remote = remote.replace("redis('EVAL',RESET,len(keys()),*keys(),state['plan_id'],revision,APPROVAL,FIX)",
        "redis('EVAL',RESET,len(keys()),*keys(),state['plan_id'],revision,APPROVAL,FIX,*terminal_pending_ids())")
    remote = previous.replace_once(remote,
        "row['status']!='active' or row['legacy_access_state'] in ['paused','hold','revoked'] or row['auth_mode']!='user_oauth'",
        "row['status'] not in ['active','error'] or row['legacy_access_state'] not in ['hold','unmanaged','active'] or row['auth_mode']!='user_oauth'")
    old = previous.program()
    backup = old[old.index("elif OPERATION=='backup':"):old.index("elif OPERATION=='reset':")]
    # Snapshot and restore are fresh for this run, never reuse an old approval.
    backup = backup.replace('feishu_report25_restore_', 'feishu_accelerated_control_restore_')
    remote = previous.replace_once(remote, "elif OPERATION=='reset':", backup +
        "elif OPERATION=='reset':\n if not state.get('backup_verified'):raise ValueError('verified_backup_required')\n"
        " backup=directory/('before-'+state['plan_id']+'.dump')\n"
        " if backup.is_symlink() or hashlib.sha256(backup.read_bytes()).hexdigest()!=state['backup_sha256']:raise ValueError('recovery_backup_changed')")
    remote = previous.replace_once(remote,
        "or payload.get('force_full'):\n   raise ValueError('pending_task_outside_approved_scope')",
        ":\n   raise ValueError('pending_task_outside_approved_scope')\n"
        "  if payload.get('force_full'):\n"
        "   log=payload.get('sync_log_id','')\n"
        "   if payload.get('trigger')!='approved_reconciliation' or not re.fullmatch('[a-zA-Z0-9-]{1,64}',log):raise ValueError('unapproved_full_run')\n"
        "   if sql(\"SELECT count(*) FROM sync_logs WHERE id='\"+log+\"' AND data_source_id='\"+payload['data_source_id']+\"' AND execution_claimed=false AND ((status='running' AND execution_generation=0) OR status IN ('success','failed','canceled'))\")!=1:raise ValueError('full_run_not_fresh')")
    return remote


if __name__ == '__main__':
    parser = argparse.ArgumentParser(add_help=False)
    parser.add_argument('--operation', choices=['status','self-test','prepare','quiesce','backup','reset','restart','resume'], default='status')
    parser.add_argument('--incident',choices=['audit_unavailable','embedding_http_400'],default='audit_unavailable')
    options, remaining = parser.parse_known_args()
    base.transport.REMOTE = ('OPERATION=' + repr(options.operation) + '\nRESET=' +
        repr(previous.reset_program().replace(previous.STOP,options.incident)) +
        '\nRESUME=' + repr(base.RESUME) + '\n' + program(options.incident))
    sys.argv = [sys.argv[0], *remaining, '--mode', 'status']
    raise SystemExit(base.transport.main())
