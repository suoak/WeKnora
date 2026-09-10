# Usage Analytics and Knowledge Governance

Usage Analytics is a system-administrator view of user-visible assistant-turn token usage and inbound MCP tool invocations. Collection starts when the migration and application version are deployed; historical messages are not backfilled.

## Coverage

The token ledger covers the final visible assistant turn for normal and fallback Knowledge QA, plus the Agent turn aggregate (existing ReAct rounds and final synthesis). It applies to web, API, IM, embed, MCP, and other channels that use those turn paths.

Phase 1 does not claim full model-cost coverage. It excludes background model calls such as history compaction, query rewriting, extraction, summaries, generated questions, tagging, graph/spreadsheet processing, session titles, memory and wiki generation, diagnostics, and other background `Chat()` calls. It also excludes separate embedding, rerank, VLM, and ASR usage, outbound MCP, pricing, currency cost, estimated tokens, historical backfill, rollups, retention, and export.

`messages.usage` remains product message data. `model_usage_events` is the canonical analytics ledger, and reports aggregate only the ledger. One persisted assistant message maps to `event_key = message:<assistant_message_id>`; the unique key makes reconnects and retries idempotent. Ledger rows have no cascading foreign keys and remain when source messages, sessions, tenants, or resources are later deleted.

Provider `total_tokens` is preserved when present. Otherwise total is input plus output. Cache read/write counters are breakdowns and are never added to total. Reasoning tokens remain zero in Phase 1 rather than being estimated.

## Attribution and privacy

Caller workspace attribution comes from the frozen caller/session tenant, not a resource execution tenant. Resource ownership is resolved by the Go backend from stored knowledge-base and knowledge records and written separately to `usage_resource_links`.

MCP records are created once around the protocol-level tool invocation, not around individual REST requests. The Python adapter reports only safe metadata: invocation key, tool/client/transport, success/error class, latency, request ID, and bounded resource IDs. It never reports tool arguments, query text, results, document content, authorization headers, API keys, or OAuth tokens. The Go backend derives principal, caller tenant, and API-key ID from authenticated context.

Shared-gateway requests cannot reliably identify the real external caller. They are stored as `principal_type = system/unknown` with a null caller tenant and appear as “Shared gateway / Unattributed”. A failed analytics report is logged but never changes an otherwise successful chat turn or MCP tool result.

## System administrator API

All report endpoints below are system-admin-only and accept `from`, `to`, `tenant_id`, and `interval` (`hour`, `day`, `week`, or `month`). List endpoints additionally accept `page`, `page_size`, and a documented UI sort value. The default range is the last 30 days.

- `GET /api/v1/system/admin/usage/overview`
- `GET /api/v1/system/admin/usage/tenants`
- `GET /api/v1/system/admin/usage/timeseries`
- `GET /api/v1/system/admin/usage/models`
- `GET /api/v1/system/admin/usage/mcp`
- `GET /api/v1/system/admin/usage/knowledge-bases`

The authenticated MCP adapter sends one best-effort report to `POST /api/v1/usage/mcp-events`. This endpoint does not accept caller identity or resource-owner tenant fields in its JSON DTO.

## Knowledge usage governance

Governance reuses the existing ledgers and resource links; it does not add a second analytics store. An active space has a model event attributed to its tenant or an attributed MCP event in the requested window. An active knowledge base has at least one linked model or MCP event in the window. One event-to-knowledge-base link counts as one access, so total accesses are model accesses plus MCP accesses.

Status is deliberately rule-based: activity in 30 days is `active`; activity in 90 days but not 30 is `low_activity`; no activity in 90 days is `inactive` only after collection history covers 90 days; no event at all is `never_used` only after that same coverage; otherwise the status is `insufficient_data`. Zero-usage spaces and knowledge bases remain visible because reports start from their master tables and left join database-side aggregates.

Cross-space reuse requires a non-null caller tenant different from the linked resource tenant. Unattributed MCP calls are excluded from internal and cross-space attribution. MCP adoption counts only attributed inbound calls. Platform penetration uses current active, non-deleted spaces as its denominator; adoption among active spaces uses spaces active by ledger activity.

`collecting_since` is a write-once internal system setting (`usage.analytics.collecting_since`) initialized when this application version first starts. The migration runner records only schema version and dirty state, not an applied timestamp, so the first business event is not used as a misleading proxy. The setting is retained across upgrades and excluded from the editable System Settings API.

The governance extensions remain under `/api/v1/system/admin/usage`: overview KPIs and attention hints, space status/adoption/reuse filters, `knowledge-bases?group_by=knowledge_base`, MCP grouping by tool/client/knowledge base, and allowlisted access time-series dimensions. Existing knowledge-base caller grouping remains the default for compatibility.

## Query plans and storage

Aggregations execute in PostgreSQL or SQLite, including zero-usage master-table joins, cross-space ranking, adoption, and top-knowledge-base queries. CI executes seven representative `EXPLAIN` plans: overview at 30 and 90 days, spaces at 30 days, knowledge-base governance at 90 days, cross-space ranking, MCP adoption, and top knowledge bases. Existing time, tenant, event, and resource-link indexes are reused; this feature adds no migration or speculative compound index. Plan output is retained in the Portal Final Gate job log for review against production-scale observations.

This governance phase does not add pricing, cost estimates, provider multi-meter accounting, backfill, rollups, retention, exports, alerts, automatic remediation, or tenant-admin analytics.
