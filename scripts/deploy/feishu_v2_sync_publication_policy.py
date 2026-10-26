"""Enroll only the user's explicitly authorized two-source isolated sync policy.

This calls a dedicated operator path, not a forged owner session or SQL grant.
No stop-lock, queue, source state, original approval or Knowledge is changed.
"""
import argparse
import sys

import feishu_v2_personal_export_probe as probe


def main():
    parser = argparse.ArgumentParser(add_help=False)
    parser.add_argument('--operation', choices=['inspect', 'approve'], default='inspect')
    options, remaining = parser.parse_known_args()
    if '--mode' in remaining:
        raise SystemExit('use explicit policy --operation')
    marker = "environment['FEISHU_DIAGNOSTIC_ISOLATED']='1'"
    policy = marker + "\nenvironment['FEISHU_DIAGNOSTIC_SYNC_POLICY_ACTION']=" + repr(options.operation)
    if options.operation == 'approve':
        policy += "\nenvironment['FEISHU_DIAGNOSTIC_SYNC_POLICY_APPROVAL']='user:2026-10-26:automatic-existing-source-publication'"
    probe.transport.REMOTE = probe.REMOTE.replace(marker, policy)
    sys.argv = [sys.argv[0], *remaining, '--mode', 'status']
    return probe.transport.main()


if __name__ == '__main__':
    raise SystemExit(main())
