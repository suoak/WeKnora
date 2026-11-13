# Signed retirement of an expired Feishu worker

`feishu_worker_retirement.py` is an SSH operator tool, independent from the APP.
It does not deploy, resume a queue, clear a stop, issue a Permit or update SQL.
Its v2 signing domain is `knowhub_expired_worker_retirement_v2`.

Use only after explicit incident-specific production approval. Generate a fresh
`plan`, sign it on the separate approved signing host, then `execute` it within
60 seconds. Each execution retires exactly one named, proven-dead worker.
An existing approval must never be reused after membership or task changes.
Do not put signing keys, database credentials or Redis passwords in Git or
command arguments. The APP must retain its no-Docker-socket configuration.

The tool requires:

- Two to 64 matching worker/lease records, one current live maintenance consumer
  and expired leases for all other retained members. The target container is
  absent or stopped; current Docker identity, runtime source and read-only public
  signing-key mount must match.
- A latched incident, no Permit, a paused shared sync queue, zero active sync
  claims/publication leases, and no running/retry/scheduled/archived shared work.
- Zero to 64 pending shared tasks whose exact identities and binary fields are
  signed and compared in Redis. Their count is not hard-coded to one old incident.
- Any retained recovery state must be expired and in `awaiting_review`; it is
  compared and preserved. Running or unexpired recovery cannot be retired.
  A separately reviewed `--allow-expired-frozen` plan can also retain an expired
  version-1 `frozen` record belonging exactly to the dead target's registered
  fingerprint/incarnation, with empty run ID and generation zero. The flag is
  signed; an existing approval cannot acquire it. Current-worker, running,
  admitted, started or unexpired recovery remains rejected. The recovery bytes,
  trip, Permit and shared queues are preserved; Reset and Canary still need
  their own new approvals after retirement.
- Other independent worker pools, such as Wiki, may remain active. Every current
  maintenance/sync consumer must be idle; any live consumer of an expired member
  rejects retirement.

The signature covers the entire bounded snapshot. Execution rechecks all member
identities, stale-member leases, current live lease, incident, recovery record,
queue/task bytes and Asynq consumer records. It writes audit before removing only
the target's worker/lease/lifecycle entries and increasing membership revision.
Any changed proof rejects the operation. Generate a new signed plan rather than
retrying automatically. RecoveryManager reset and Canary require their own fresh
approval after retirement; this tool never creates that authorization.

Tests use a newly owned disposable Redis and synthetic keys only. Never point
the tests at an existing Redis: the fixture uses `FLUSHDB`. The CI workflow
creates a fresh service for this purpose. Local safe execution should isolate
the test Redis in its own network namespace with no production credentials.

Run with Python and `cryptography`:

```text
python -m unittest discover -s scripts/ops -p test_feishu_worker_retirement.py -v
```

Both `KNOWHUB_OWNED_RETIREMENT_REDIS=1` and an explicit
`KNOWHUB_RETIREMENT_TEST_REDIS` for that disposable service are required.
