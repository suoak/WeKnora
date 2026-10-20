"""Codex translates explicit user progress into fixed enums; no free text.
Only use with the separately authorized live monitor run. No SID calls.
"""
import argparse
import json
import uuid
from pathlib import Path
from protocol_monitor_runtime import atomic_json, RELEASE


def main(argv=None):
    parser = argparse.ArgumentParser()
    parser.add_argument('--run-id', required=True)
    parser.add_argument('--action', choices=('password_page_observed', 'authentication_complete', 'cancel'), required=True)
    args = parser.parse_args(argv)
    if str(uuid.UUID(args.run_id)) != args.run_id:
        raise ValueError('run_id_invalid')
    root = RELEASE / 'private' / ('monitor-' + args.run_id)
    if root.is_symlink() or (root / 'final-result.json').exists():
        raise RuntimeError('run_not_active')
    status = json.loads((root / 'status.json').read_text())
    if status.get('run_id') != args.run_id:
        raise RuntimeError('run_mismatch')
    atomic_json(root / 'operator.json', {'run_id': args.run_id, 'action': args.action})
    print(json.dumps({'run_id': args.run_id, 'action_recorded': args.action, 'new_requests_sent': False}))


if __name__ == '__main__':
    main()
