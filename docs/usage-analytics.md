# Usage Analytics (Phase 1)

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
