# MCP invocation accounting

Baseline: origin/main `1e9d8ae3f8e3f546e71883a8a7cadf4892afc291`.
The existing working directory was an older SourceID branch with unrelated
uncommitted work. Implementation uses an independent worktree.

## Audit

Both Go entry points lacked inbound tool ledger recording. Endpoint activity
only touched `last_used_at`. Personal MCP also rejects requests during HTTP
admission, before the tool guard. Python already wrapped tool registration but
reported synchronously once, omitted `kb_id`, and conflated submitted scope
with usage. UsageAnalytics already supplied append-only events, unique event
keys, transactional resource links, caller context and admin-only aggregation.

Upstream Tencent MCP uses the same tool middleware/filter mechanism, but has
no equivalent inbound UsageAnalytics integration to adopt. Its middleware is
the integration point reused here; the business tool implementations and
authorization decisions are preserved.

## Counting contract

- One authenticated `tools/call` produces one final inbound event. Personal
  internal retries and fan-out do not create additional events.
- `IsError`, Go errors, panic, cancellation, deadline, permission/admission
  refusal and endpoint rate limits produce unsuccessful events. Raw errors,
  questions, arguments, answers, document content and credentials are absent.
- initialize, listing and authentication failures are not tool events; their
  HTTP/protocol monitoring remains separate. Unknown tool names are reduced
  to `unknown` to avoid retaining arbitrary submitted text as a tool name.
- Identity, caller space and numeric key ID come from authentication context.
  Endpoint calls retain their endpoint machine principal and endpoint ID;
  shared endpoints cannot establish the employee behind the shared token.
- Client presets are never read. Available MCP session negotiation is labeled
  `negotiated`; Python adapter negotiation is labeled `adapter_negotiated`.
  Unavailable client information is `unknown`, including stateless transports
  which do not retain initialize information across requests.
- `knowledge_base_target` links describe authorized search execution scope;
  `knowledge_base_hit` describes returned matches. Existing `knowledge_base`
  links describe used resources, preserving current governance queries.
  `knowledge` links count successful explicit document reads. Search result
  document references and list results are not document reads. Wiki neighbors
  in link-routing metadata are not counted as hits.
- Dynamic personal targets are observed after live execution authorization;
  the key's full authorization scope is never substituted for actual scope.
- Python IDs are normalized from `kb_id` aliases; search targets come from
  successful backend search requests, and hits from returned result records.

## Delivery and recovery

Set `MCP_USAGE_SPOOL_DIR` to a persistent, private directory, with one writer
per replica and separate directories for Go and Python. The default is
`data/mcp-usage-spool`. Both adapters retain bounded metadata files (10,000
including dead letters), fsync before handoff, and retry delivery at most five
times. Failures and overflow emit `[mcp-usage]` / `MCP usage` error logs and
must be included in deployment log alerts. No business result depends on a
successful database write.

Admitted invocations are stored as `.active`; finalization produces `.json`.
On process restart unfinished admissions become `interrupted`, never guessed
successful. Their duration is unknown (zero in the raw record), and they are
excluded from average latency. Final events retain their original timestamp.
Five failed deliveries leave `.dead` files for operator investigation/replay;
they are retained and count against capacity. For Go, after fixing delivery,
stop the collector, rename only reviewed `<digest>.json.dead` files back to
`<digest>.json`, then restart. For Python use `<digest>.dead` to `<digest>.json`.
Replay preserves event keys; database uniqueness prevents double counting.

Python credentials are never persisted in the spool. Configured gateway
credentials are re-registered on startup. Pending passthrough-user events
after restart wait for the same authenticated credential to return; they
cannot be delivered using another user's key. A retired credential can leave
pending metadata requiring operational reconciliation. This limitation must
not be presented as full historical coverage.

## Schema and rollback

Migration `003046_mcp_usage_coverage` adds event entry point, endpoint ID and
client provenance, expands resource-type validation, and records a separate
write-once Go collection epoch. Old rows remain `legacy` / `unknown`.
PostgreSQL changes are additive. SQLite recreates the constrained link table
inside the migration transaction, preserving every row and ID and recreating
its indexes. The migration governance manifest is updated.

Rollback deploys the previous application with the additive schema retained.
The down migration deliberately retains columns, links, events and epochs.
Do not force migrations below this version to remove analytics data. Back up
the database and both spool directories before deployment.

## Existing admin API

`GET /api/v1/system/admin/usage/mcp` keeps time, space and direction filters.
It additionally accepts `entry_point=go_personal|go_endpoint|python|legacy`
and `tool_name`. Additional `group_by` values are `summary`, `entry_point`,
`error`, `space`, `event`, and `resource`; tool/client/knowledge-base groups
remain compatible. Summary and entry/space groups expose distinct employee
principals and active credentials; distinct counts across groups must not be
summed. Event grouping exposes safe identity and result metadata. Resource
grouping exposes target/hit/read dimensions and resource ownership.

Use explicit RFC3339 `from` and `to` for today, 7-day or 30-day windows.
Responses include `mcp_server_collecting_since` and
`historical_mcp_coverage=partial_before_server_collection`. Earlier missing
Go events are not backfilled or represented as zero use. The UI exposes the
entry filter, today range and the new MCP collection epoch.

## Validation

Linux related-package tests and a race-enabled run passed, including
PostgreSQL transactional migration/replay and SQLite aggregates. Python
statistics-specific tests passed. Full Python testing on Windows encountered
an existing symlink privilege requirement; Linux validation is required.
CI and runtime acceptance receipts are recorded separately; this document
does not claim a deployment has occurred.
