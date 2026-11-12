# Personal MCP concurrency investigation and release

2026-11-12, Asia/Shanghai. This report distinguishes production observations from
isolated synthetic load tests. No credentials, queries, document bodies or peer
addresses are included. Release/production acceptance remains pending below.

## Production evidence before changes

- Main investigated: `6dedb66ce93151a7f97e7e7051871b14d91f85eb`.
- One running APP replica: source `dc3f11db077a2eb6aef8abf1b8e3616d3f7a3377`,
  image `sha256:01e12996b9b3ce22ba56455dd415b2955c25469e389783fe1331a6c9831fece1`.
- Running `/app/WeKnora` SHA-256 matches the retained release binary:
  `ea7946b95c50cb634a2175020ad33a6a5d604a1ea1c2f53b35573399389fe178`.
  Constructor machine code loads 8 immediately before `runtime.makechan`.
  Production and main both use eight slots; there is no evidence of a
  single-concurrency setting.
- The channel belongs to the singleton PersonalServer, not a user. HTTP80 and
  TLS8443 both reach it. It covers GET notification streams, initialize,
  tools/list, tools/call, Portal resolution, retrieval and serialization.
  Admission immediately returns 429 when full; there is no queue or user quota.
- Auth middleware runs before the channel. Portal resolution/revalidation runs
  after admission. Both public publication and private Key scope checks remain
  required; the channel is not an authorization mechanism.
- The pinned mcp-go v0.52.0 GET handler flushes HTTP200 and holds its SSE
  connection until cancellation. The APP bounds it to 30 seconds. Thus an idle
  notification listener consumes a search permit for about 30 seconds.
- Retained frontend logs (container started about four hours before sampling):
  GET200=2010, POST200=755, POST202=82, POST429=171, GET429=25,
  GET499=10, POST499=6, POST401=3, POST400=2. Among POST200/202/429,
  171/1008=16.96% were 429; this is an HTTP request rate, not a tool event rate.
- Retained APP logs requested over 24h (bounded by its actual eight-hour uptime):
  3835 HTTP200, 208 HTTP429, 22 HTTP403, 112 HTTP202, 16 HTTP401, 3 HTTP400.
  2916 HTTP200 took at least 29000ms; maximum duration 30140ms.
- A bounded real-Key production reproduction at 14:49:22 observed three
  independent keyword searches: 429 at 329ms, 429 at 16ms, and HTTP200 at
  30016ms. The last response did not parse as a successful tool result; it is
  deliberately not counted as search success. Busy refusals matched the APP's
  exact `personal MCP busy; retry later` message and `Retry-After: 1`.
- Effective Nginx configuration contains no limit_req/limit_conn. HTTP80
  personal proxy timeouts are 120s; TLS8443 connect/send/read are 5/30/35s;
  both disable upstream replay. Worker connections=1024. These are not the
  source of that exact busy response. External upstream gateway configuration
  is not available; it cannot be declared fully audited.
- APP/PG were healthy; sampled PG had eight idle client connections, one active
  inspection query and max_connections=100. APP has no Docker memory/CPU cap;
  host has eight CPUs and about 13GiB available RAM. A momentary resource sample
  cannot exclude historical contention. Production data volume has about 13GiB
  available (98% used); avoid staging artifacts/backups there unnecessarily.

## Root cause and evidence limits

The confirmed limiting component is the APP's eight-slot, per-instance,
immediate-rejection channel shared by both personal endpoints. Long-lived GET
SSE connections compete with actual searches and handshake requests; one user
can occupy all slots. A synthetic authenticated PostgreSQL reproduction on the
unchanged main handler opened eight GET listeners and then observed **0/3**
independent searches succeed, plus tools/list=429. Slow calls from one owner
also made another owner's search and discovery return 429.

These observations explain why three newly submitted requests can encounter
less than three free permits without the configured capacity being one. Old
logs do not correlate every occupying connection with the exact production
incident, so the number/owners of streams at that instant remain unproven.

The deployed binary predates main's Go MCP UsageAnalytics collection and the
production database lacks entry_point/client_source columns. Existing ledger
rows cover the older Python adapter (878 total; recent client names empty),
not personal MCP refusals. Tool/client-specific personal 429 frequency cannot
be reconstructed truthfully. PersonalPerformanceTrace was also sent through a
payload-private logger and silently discarded; no production auth/Resolve/SQL/
Embedding stage baseline is available before this fix. HTTP200 stream duration
must not be confused with retrieval latency. No untrusted client-provided user
ID or User-Agent is treated as authenticated attribution.

Upstream Tencent/WeKnora `cbdcebabf` was fetched and inspected: it has the
workspace MCP server and endpoint rate limiter, but no KnowHub personal.go.
Its endpoint frequency limit is not this personal concurrency channel. The
Python MCP adapter is a separate process and does not own this channel.

## Implementation and configuration

All limits are per APP instance and the same pool objects serve both routes.

| Setting under personal_mcp.concurrency | Default |
| --- | ---: |
| global_limit / user_limit (all tools) | 16 / 4 |
| queue_limit / user_queue_limit (per pool) | 64 / 8 |
| wait_timeout_ms | 1500 |
| protocol_limit / protocol_user_limit | 8 / 4 |
| stream_limit / stream_user_limit (SSE GET) | 8 / 2 |

Tools, short protocol operations and GET listeners have separate bounded pools.
All still perform existing authentication and Portal checks. There are at most
32 admitted HTTP operations across the three classes with these defaults;
maximum queued entries are 192 across all pools. Auth work precedes scheduling.
Zero values use defaults; invalid/negative/overlarge settings fail closed by
disabling personal MCP rather than removing protection.

The queue grants eligible requests in arrival order and skips users already at
their execution quota; a waiting request never reserves a global permit.
Per-user queue caps prevent one owner from filling the waiting room. All keys
and workspace selections of a server-authenticated web_user share one quota.
Client headers cannot select that owner. Cancellation, deadline, racing handoff
and repeated release are accounted under one mutex; empty user counters and
queue references are removed. There is no background worker or unbounded task
fan-out. Cross-KB search stays serial: raising its fan-out before resolving the
SSE issue would multiply DB/model pressure without a measured need.

Capacity refusals use HTTP429, `X-KnowHub-Error-Code: concurrency_limited`,
`Cache-Control: private, no-store`, and Retry-After rounded up from the wait
interval (default 2 seconds). Ordinary frequency limits remain rate_limited.
No authorization scope, public publication rule, private document guard,
isolated output boundary, database fence, or audit is disabled.

Anonymous PersonalMCPPerf records now contain class, wait_ms, outcome, global
and current authenticated user's load, cumulative capacity refusals and
after-release gauges. They use an explicit server-owned projection outside the
payload-private context, without logging principal IDs, inputs or responses.
The existing invocation ledger keeps its single final event and stable event
key across queuing and internal retries; TraceID correlates stage/load metrics
to its already authenticated attribution. Tool latency/success/error stays in
the existing ledger. No cross-user cache is introduced.

## Isolated verification

Dedicated loopback-only PostgreSQL/ParadeDB, synthetic users/keys/documents,
real HTTP/auth/retrieval, synthetic 20ms embedding delay, 3 CPU / 6GiB test
container limit. Before/after use the same fixture and database engine. These
small burst tests are not a production throughput forecast.

| Concurrent searches | Old success / 429 | New success / 429 | Old p95 ms | New p95 ms |
| --- | --- | --- | ---: | ---: |
| 3 | 3 / 0 | 3 / 0 | 189.1 | 182.3 |
| 5 | 5 / 0 | 5 / 0 | 306.1 | 360.5 |
| 10 | 8 / 2 | 10 / 0 | 295.1 | 573.3 |
| 20 | 8 / 12 | 12 / 8 | 542.4 | 640.0 |

Old/new successful throughput for n=20: 14.60 / 18.17 calls/s. New latency
includes useful bounded queuing; rejected calls make old percentiles look
artificially faster. In the SSE saturation reproduction, old 0/3 -> new 3/3
searches succeed, discovery 429 -> 200; new owner SSE cap admits 2/8 listeners
and boundedly rejects excess listeners. With slow owner A, owner B 429 -> 200
at 147.2ms. Mixed search/document/Wiki across both routes passed with only two
DB connections. Client cancellation/timeout and subsequent successful search
passed. Queue exhaustion/deadline/racing cancel-grant/repeated release and
authenticated-identity spoofing tests passed. Burst ledger assertions verify
one unique event per call and exactly one concurrency_limited per HTTP429.

Core, authenticated HTTP tests, admission/usage race tests, middleware/access/
router regressions passed. The broad application/service run failed
TestFeishuLegacyRealParserEmbeddingPostgres (four expected-guard failures);
the unchanged-main comparison reproduced the same four failures (exit 1,
57.69s including build). This is an inherited failure and the broad suite is
not claimed as passing. Relevant middleware/access/router tests did pass.
The focused concurrency CI job now uses real ParadeDB; accounting CI includes
admission race tests. Multi-replica cluster quotas are not implemented or
claimed: production has one APP replica. Increasing replicas multiplies these
limits; distributed admission must be designed before scaling out.

Private raw receipts and scripts: `E:/AIData/Codex/mcp-p1-*`; isolated execution
directory `/home/aqyfzx/ai/mcp-concurrency-p1-20261112`. Only numeric summaries
are included here. Production resource depletion was not injected.

## Release and rollback gate (pending)

PR: https://github.com/suoak/WeKnora/pull/136. CI links, actual deployed SHA and real-Key acceptance will be appended
after completion. Main includes reconciliation migration 3047 while production
is clean 3046; this is a required release dependency, not a new P1 migration.
Do not deploy main against 3046 and claim complete accounting. Before switching:

1. Verify CI, exact candidate binary/image provenance, native parser archive,
   available resources, unchanged protected worker controls and no Docker socket.
2. Retain the current APP image and effective private Compose/config; prepare
   rollback to that exact image/config. Take/verify a full PostgreSQL backup
   outside the near-full data volume and snapshot usage/permissions/worker state.
3. Apply the official 3046 -> 3047 migration with the schema-upgrade tool after
   verifying both historical 3046 lineages in isolation. Preserve AUTO_MIGRATE=false.
   3047 additions are compatible with the retained old APP on binary rollback;
   do not automatically drop newly collected usage on rollback.
4. Mount a durable dedicated MCP_USAGE_SPOOL_DIR (per replica) so accounting
   survives APP recreation, and apply the reviewed bounded concurrency values.
5. Recreate only APP through the established Compose chain. Verify image/SHA,
   healthy startup, schema/dirty state, database and worker controls, permissions,
   and unchanged authorized KB-set hash with the same real personal Key.
6. Execute three simultaneous independent searches through both cloud routes;
   require three actual successful tool results, not merely HTTP200. Check
   Retry-After under safe bounded overload and unique ledger events. Keep old
   images/config and backup until post-release observation is complete.
