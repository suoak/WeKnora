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
FIXED_RUNTIME = '2607fbec7b7ee63ee66d83fb0e199f02945074fb'


def reset_program():
    script = prior.RESET.replace('automatic_permit_consumer_lost', STOP)
    script = replace_once(script,
        "if redis.call('LLEN',KEYS[9])~=0 or redis.call('LLEN',KEYS[10])~=0 or",
        "if redis.call('LLEN',KEYS[9])~=0 or")
    return replace_once(script,
        "if redis.call('EXISTS',KEYS[13])~=0 then return redis.error_reply('RECOVERY_ALREADY_RESET') end",
        "local pending=redis.call('LRANGE',KEYS[10],0,-1)\n"
        "if #pending~=#ARGV-4 then return redis.error_reply('PENDING_CHANGED') end\n"
        "for i,id in ipairs(pending) do if id~=ARGV[4+i] then return redis.error_reply('PENDING_CHANGED') end end\n"
        "if redis.call('EXISTS',KEYS[13])~=0 then return redis.error_reply('RECOVERY_ALREADY_RESET') end")


def replace_once(value, before, after):
    if value.count(before) != 1:
        raise ValueError('recovery_protocol_anchor_changed')
    return value.replace(before, after, 1)


def program():
    # The audit transport deliberately stays read-only. Opt into only the
    # additional commands this separate, explicitly approved protocol needs.
    remote = replace_once(prior.REMOTE,
        "'ZCARD','INFO','LATENCY','SLOWLOG']",
        "'ZCARD','INFO','LATENCY','SLOWLOG','HLEN','LRANGE','HGET','EVAL']")
    remote = replace_once(remote,
        "APPROVAL='user:call_085aca217ce54805be1d8372feec744b:approved_two_sources'",
        'APPROVAL=' + repr(APPROVAL))
    remote = replace_once(remote, "directory=site/'approved-control-recovery-20261026'",
        "directory=site/'approved-membership-recovery-report25'")
    # Retain archive and image provenance checks for the selected fixed source.
    remote = replace_once(remote,
        "if SHA!=FIX:\n if OPERATION!='status':raise ValueError('recovery_mutations_bound_to_original_approved_source')",
        'if True:')
    remote = remote.replace('automatic_permit_consumer_lost', STOP)
    remote = replace_once(remote, 'def quiet():', r'''
def terminal_pending_ids():
 ids=redis('LRANGE',queue+'pending',0,-1)
 if len(ids)>10:raise ValueError('unexpected_pending_volume')
 allowed={row['id'] for row in (state['sources'] if state else sources())}
 for task_id in ids:
  raw=redis('HGET',queue+'t:'+task_id.decode(),'msg')
  if not raw:raise ValueError('pending_task_receipt_missing')
  msg=fields(raw);payload=json.loads(msg.get(2,[b'{}'])[0])
  source=payload.get('data_source_id','');log=payload.get('sync_log_id','')
  if msg.get(1,[b''])[0]!=b'datasource:sync' or source not in allowed or payload.get('force_full') or not re.fullmatch('[a-zA-Z0-9-]{1,64}',log) or not re.fullmatch('[a-zA-Z0-9-]{1,64}',source):
   raise ValueError('pending_task_outside_approved_terminal_scope')
  if sql("SELECT count(*) FROM sync_logs WHERE id='"+log+"' AND data_source_id='"+source+"' AND status IN ('success','failed','canceled') AND execution_claimed=false")!=1:
   raise ValueError('pending_execution_not_terminal')
 return [value.decode() for value in ids]

def quiet():''')
    remote = replace_once(remote, 'return not any(queue_counts().values())',
        "counts=queue_counts()\n if any(counts[k] for k in ['active','scheduled','retry']):return False\n terminal_pending_ids()\n return True")
    remote = replace_once(remote, "if any(queue_counts().values()):raise ValueError('queue_not_quiescent')",
        "counts=queue_counts()\n if any(counts[k] for k in ['active','scheduled','retry']):raise ValueError('queue_not_quiescent')\n terminal_pending_ids()")
    remote = replace_once(remote,
        "redis('EVAL',RESET,len(keys()),*keys(),state['plan_id'],revision,APPROVAL,FIX)",
        "redis('EVAL',RESET,len(keys()),*keys(),state['plan_id'],revision,APPROVAL,FIX,*terminal_pending_ids())")
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
    remote = replace_once(remote,
        "if test_redis('GET',kk[2]) or test_redis('GET','untouched-budget')!='127':raise ValueError('resume_damaged_budget_or_pause')",
        "if test_redis('GET',kk[2]) or test_redis('GET','untouched-budget')!='127':raise ValueError('resume_damaged_budget_or_pause')\n"
        "  test_redis('SET',kk[0],"+repr(STOP)+");test_redis('SET',kk[2],'feishu_safety_stop')\n"
        "  test_redis('DEL',kk[1],kk[3],kk[4],kk[5],kk[6],kk[12])\n"
        "  test_redis('RPUSH',kk[9],'terminal-a','terminal-b')\n"
        "  check(RESET,aa+['wrong','terminal-b'],'PENDING_CHANGED')\n"
        "  check(RESET,aa+['terminal-a','terminal-b'],'1')\n"
        "  if test_redis('LRANGE',kk[9],0,-1)!='terminal-a\\nterminal-b' or test_redis('GET',kk[2])!='feishu_safety_stop':raise ValueError('terminal_queue_not_preserved')")
    remote = replace_once(remote, 'connection.close();print(json.dumps(result))', r'''
pending_receipts=[]
for task_id in redis('LRANGE',queue+'pending',0,9):
 raw=redis('HGET',queue+'t:'+task_id.decode(),'msg')
 receipt={'task_hash':mask(task_id.decode()),'payload_available':bool(raw)}
 if raw:
  message=fields(raw);payload=json.loads(message.get(2,[b'{}'])[0])
  source=payload.get('data_source_id','');log=payload.get('sync_log_id','')
  receipt.update(task_type='datasource:sync' if message.get(1,[b''])[0]==b'datasource:sync' else 'other',
   source_hash=mask(source),log_hash=mask(log),force_full=bool(payload.get('force_full')))
  if re.fullmatch('[a-zA-Z0-9-]{1,64}',log) and re.fullmatch('[a-zA-Z0-9-]{1,64}',source):
   receipt['execution']=sql("SELECT row_to_json(q) FROM (SELECT status,execution_claimed,execution_generation FROM sync_logs WHERE id='"+log+"' AND data_source_id='"+source+"') q")
 pending_receipts.append(receipt)
result['pending_task_receipts']=pending_receipts
connection.close();print(json.dumps(result))
''')
    return ("if OPERATION not in ['status','self-test'] and SHA!=" + repr(FIXED_RUNTIME) +
        ":raise ValueError('approved_fixed_runtime_required')\n" + remote)


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
        repr(reset_program()) + '\nRESUME=' +
        repr(prior.RESUME) + '\nRECONCILIATION_LOG_HASH=' + repr(options.reconciliation_log_hash) + '\n' + program())
    sys.argv = [sys.argv[0], *remaining, '--mode', 'status']
    return prior.transport.main()


if __name__ == '__main__':
    raise SystemExit(main())
