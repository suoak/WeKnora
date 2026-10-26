# Historical Wiki regeneration and model snapshot conflicts

The operator command previews a single owning tenant and knowledge base. Apply
requires the exact preview snapshot, all source documents completed, no durable
pending operations, active verified source authority, and supported input bounds
(2048 documents, 1000 active Wiki pages). It never certifies existing page text.

Use the same Redis and PostgreSQL instance as the running worker. Pass credentials
only through environment variables, never command arguments or checked-in files:

```sh
wiki-regenerate -tenant 10013 -kb KNOWLEDGE_BASE_UUID
wiki-regenerate -tenant 10013 -kb KNOWLEDGE_BASE_UUID -apply -snapshot PREVIEW_SNAPSHOT
```

`WIKI_REGEN_POSTGRES_DSN`, `REDIS_ADDR`, `REDIS_PASSWORD` and `REDIS_DB` are read
from the operator process environment. Preview returns only counts and a digest.
Apply preserves all old page bodies and revisions by soft-deleting and renaming
their slugs in one transaction, then seeds a Wiki slot and durable operation for
each current source document. It preserves source files, chunks, embeddings and
document identity. The model sees current source inputs, not old Wiki text.
The existing worker generates, verifies and publishes new pages; normal task
inspection exposes progress. If Redis enqueue fails after commit, durable ops
remain available for housekeeping to re-arm. Reapplying the old preview cannot
duplicate operations. Back up the database before applying; archived pages are
not a substitute for a full backup. Restoring old slugs while generation runs
is unsafe: stop the scoped generation first and preserve its new output.

Proof-backed ingest, finalize and operator reset share a token-owned renewable
per-KB lease. Loss of ownership cancels detached writes as well as model work.
Cross-page incoming links are derived metadata. A separate server-only snapshot
allows those links to merge without discarding model output, while keeping the
original full ETag for public callers and rejecting changes to body, sources,
status, hierarchy, revision, outgoing links or ownership. Backlink updates lock
the current row and change only the incoming-link set. No source proof, mutation
trigger, or public optimistic concurrency check is disabled.

Deploy the new APP before using this command: older workers do not participate
in the shared generation lease. This change requires no schema migration.
