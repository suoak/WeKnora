# Incident-bound operations client

Read [the runtime runbook](../../docs/operations/feishu-incident-recovery.md)
before using `feishu_recovery.py`. Run secret-bearing operations on the managed
Unix operations host with verified owner-only files. Windows secret reads fail
closed because Windows ACL attestation is not implemented. HTTPS or an
authenticated SSH tunnel to loopback is required. Redirects and automatic retries
are forbidden; lost acknowledgments require status/audit reconciliation.

The API must already be installed through a separately approved deployment.
These are templates, **not a production execution approval**. Replace
`APPROVED_RUNTIME_SHA` with the reviewed exact 40-character runtime commit;
the incident's old production SHA is not the new runtime SHA.

```sh
python3 scripts/ops/feishu_recovery.py plan \
  --namespace feishu-prod-080f7bb6a01f \
  --trip feishu_headers_io_failure --incident 1793268000119-0 \
  --runtime-sha APPROVED_RUNTIME_SHA --expected-pending 6 \
  --source 5c7bc873-0ba3-43c7-90a0-32c23e6bd529 \
  --base-url https://APPROVED_HOST --session-token-file /restricted/web-session \
  --output /restricted/new-plan.json
```

A separate approver reviews all four evidence categories. Manifest paths are
relative to the manifest. Each artifact needs an actual SHA256, and the manifest
needs a real reviewer and `review_outcome: accepted`. A synthetic test manifest
must never be used for production.

```json
{
  "reviewer": "REVIEWER_ID",
  "review_outcome": "accepted",
  "artifacts": [
    {"kind":"backup_restore","file":"backup-restore.json","sha256":"ACTUAL_HASH"},
    {"kind":"isolation_safety","file":"isolation.json","sha256":"ACTUAL_HASH"},
    {"kind":"source_authorization","file":"source-acl.json","sha256":"ACTUAL_HASH"},
    {"kind":"runtime_identity","file":"runtime.json","sha256":"ACTUAL_HASH"}
  ]
}
```

Use the same incident identity flags for each operation. Offline `approve`
additionally requires `--plan-file`, `--operation reset` (then separately
`canary`), `--reference`, `--approval-key-file`, `--evidence-manifest`,
`--valid-seconds` (1..900) and `--output`. It makes no API request. `execute`
requires the saved `--plan-file` and independently issued `--approval-file`,
plus the API URL/session file. It never signs an approval on behalf of an
executor. Plans and approvals are created exclusively and never overwritten.

After approved reset, inspect status and verify fresh controller/consumer
health before executing the separately approved canary. Neither operation
resumes the original queue. Status does not authorize another execution.

Tests: `python3 -m unittest discover -s scripts/ops -p test_feishu_recovery.py`.
The isolated Go suite additionally covers real Redis CAS and service claims.
