# Source restriction status hotfix

A held or revoked source currently protects its entire knowledge base. Consequently,
an unrelated manually uploaded document can remain `pending` after its processing
delivery is denied. This change reports that current admission decision separately
from the document's historical execution state. It does not change protection scope
or claim that an affected document originated from the restricted source.

## Behavior and authorization

`GET /api/v1/knowledge-bases/:id/source-restriction` exposes only the current
`available`, `hold`, `revoked`, or `unverified` decision and reason code. It requires
a web user with an active current-space viewer role and the existing owned-KB
metadata authorization, including private-owner checks. Platform administrator
status, MCP credentials and membership of another space confer no additional access.
It returns no source IDs, credentials, filenames or content. This deliberately does
not add a new cross-space sharing metadata permission.

The query mirrors the existing KB-wide legacy restriction, including restrictions
retained for deleted sources. A metadata-only authorization context stays local to
this lookup; it never permits content reads or changes request response provenance.
An unavailable lookup does not report a false successful state. The UI polls with
space/KB and request-generation fencing and preserves a confirmed restriction on
transient refresh errors. Historical processing remains `unverified`; existing
completed/failed document states are not rewritten.

Redis and Lite use the same admission middleware for document processing and the
supported derived tasks carrying a KB/document identity. A positively confirmed
restriction stops delivery with `SkipRetry` and records a minimal admission-stop
dead letter, without invoking the legacy Knowledge failure-update callback.
Transient query errors retain retry semantics. A mid-handler legacy denial is
classified this way only after a fresh positive restriction check. Existing
server-owned fenced synchronous publication contexts retain their original path.
Chunk extraction and table-summary payloads without a KB identity retain their
existing protection and retry handling; this is not a general task-ledger rewrite.

Durable dispatch intents remain pending and unclaimed while admission is denied.
Personal recovery does not fetch content or invalidate a generation on that denial.
On a legitimate later admission, all original version, generation, nonce and grant
checks still apply. This is neither a replay authorization nor an automatic repair
of historical documents. No source, Knowledge status, permission or Key is edited.

## Validation

The focused CI workflow checks the exact PR head with disposable PostgreSQL and
Redis services. Repository tests cover both full PostgreSQL and SQLite migrations,
including retained content protection. HTTP tests cover a normal noncreator member,
private owner, private nonowner, administrator, machine caller and lookup errors.
Queue tests exercise actual Redis archival, the Lite executor, normal processing,
durable-intent preservation, and recovery generation preservation. The browser gate
mounts the actual Vue notice and action menu with synthetic state and no external
network access. Hook tests cover request races, space switches and failed refreshes.

```sh
go test -p 1 -tags sqlite_fts5 ./internal/application/repository ./internal/application/service ./internal/handler ./internal/router ./internal/middleware/asynqdl -run 'TestSourceRestriction|TestSourceAdmission|TestFeishuLegacy|TestSyncTaskExecutor|TestMiddleware' -count=1
cd frontend
npm test
npm run type-check
npm run build
node --test tests/source-restriction/ui-gate.mjs
```

Configure `FEISHU_LEGACY_TEST_DSN`, `SYNC_EXECUTION_POSTGRES_DSN` and
`SOURCE_RESTRICTION_TEST_REDIS_ADDR` only for isolated services. PostgreSQL tests
create temporary databases/schemas. The Redis test creates and deletes only its own
unique queue. Do not run older recovery fault fixtures that use `FLUSHDB` against a
shared Redis service; the focused gate does not require those fixtures.

## Release and rollback

This PR has no database migration and does not depend on PR #61's migration 3043
or the longer execution-ledger design. Its development base is main
`88e46547331acf28cf256617cdfd25ddd2c848e3`; a production release must explicitly
review the full difference from the deployed source and use an approved image SHA.
Passing this gate does not authorize merging or deployment.

Before a separately approved release: verify exact running image/source/schema,
complete CI and isolated acceptance, verify a fresh recoverable PostgreSQL backup
and the queue/storage backup policy, record existing pending/intents/dead-letter
counts, retain the current image and configuration, and define monitoring/rollback
ownership. No production backup or release is performed by this PR.

Rollback uses the previously approved application image and matching frontend;
there is no schema rollback. Admission-stop dead letters and durable pending intents
are retained. Rolling back does not restore archived deliveries or authorize replay;
the previous application can again retry held intents and show stale `pending`.
Review that operational consequence before rollback. Never mass-replay, change
source status, disable guards, or delete historical source records as a rollback.

Genuine document recovery is a separate approved action: establish origin and
current authorization, identify the exact affecting restriction, reconcile current
generation/version ownership and late deliveries, then use the supported normal
processing path. Verify parsing, chunks, embeddings and authorized retrieval per
document; merely changing `parse_status` is not recovery.
