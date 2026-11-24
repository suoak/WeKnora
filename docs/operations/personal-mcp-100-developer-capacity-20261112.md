# Personal MCP capacity for 100+ developers

This supplements `personal-mcp-concurrency-20261112.md`. Production rollout and
real-Key acceptance completed on 2026-11-12; details are recorded below.

## Decision

The original 16 tool permits are insufficient for a moderately active
100-person audience. The production rollout uses 32 tool permits and retains
four per authenticated owner, and keep bounded rejection. This is not a claim
that 100 simultaneous independent searches can all execute successfully.

| Production override under personal_mcp.concurrency | Value |
| --- | ---: |
| global_limit / user_limit | 32 / 4 |
| queue_limit / user_queue_limit, for each pool | 128 / 8 |
| wait_timeout_ms | 1500 |
| protocol_limit / protocol_user_limit | 16 / 4 |
| stream_limit / stream_user_limit | 128 / 4 |

The listener pool covers idle GET SSE connections. Every new stream must also
acquire a protocol permit for its existing live Portal permission resolution;
that permit is released before the long connection starts. Thus 128 listeners
cannot initiate 128 simultaneous authorization scans. Protocol and tool
execution retain separate bounded resources. No security check is removed.

PR #136 merged as main `686789c64ca8b10c2efca018e778c90a5bead864`.
Follow-up PR #139 raises the default per-user discovery quota to four, permits
an explicitly configured listener limit up to 256, bounds initial listener
authorization and measures the durable analytics handoff as its own stage.
Deployment followed its merge to main and exact-main build verification.

## Test method and applicability

One real APP container limited to 2 CPU / 2GiB, real PostgreSQL/ParadeDB,
100 independent active synthetic users with 100 independently generated Keys,
existing authentication and owner attribution, synthetic authorized documents
and Wiki, and a synthetic embedding service adding about seven seconds.
Half the workload uses the dedicated TLS Nginx proxy with certificate
validation; half uses the HTTP endpoint. The two routes share the scheduler.
Production was not used for these capacity tests.

These results measure admission, connection handling, protected database reads
and the existing usage pipeline against a small synthetic knowledge scope.
They do not prove throughput over the production 61-KB scope, production
embedding provider quotas or all real document sizes. The 32-slot value is an
initial measured operating point; production latency/error/DB/provider metrics
must determine subsequent adjustments. Doubling the replica count doubles
the local quotas; there is no distributed quota claim.

## Results

| Workload, 100 distinct users | Tools=16 success / 429 / other | Tools=32 success / 429 / other | Tools=32 p95 ms |
| --- | --- | --- | ---: |
| 100 searches evenly over 60s | 100 / 0 / 0 | 100 / 0 / 0 | 7181.7 |
| 100 searches evenly over 30s | 73 / 27 / 0 | 100 / 0 / 0 | 7218.1 |
| 100 mixed search/document/Wiki over 30s | 73 / 15 / 12 | 100 / 0 / 0 | 7193.5 |
| 100 searches at once | 16 / 84 / 0 | 32 / 68 / 0 | 9112.0 |

The 16-slot mixed run included nine proxy 504 and three client timeouts. It is
not described as a passing run or solely as capacity rejection. Traces show
some long authentication/usage-path time outside the tool timing; the follow-up
adds explicit durable-analytics timing so this can be distinguished from SQL
and embedding. Those extra failures were not reproduced in the 32-slot run.

The longer 32-slot run offered 600 hybrid searches at 3.33 requests/s, cycling
through the 100 authenticated owners for about three minutes. Result:
**600 successful, zero 429, zero other failures**, elapsed including drain
186.85s, p50 7131.2ms, p95 7190.1ms, completed throughput 3.21 requests/s.
APP memory remained about 225MiB in the final samples; APP CPU was about
33–47% of one core, with a 2-core cap. PostgreSQL memory was about 429MiB and
CPU about 17–25% of one core in those samples. These are samples rather than
guaranteed peaks.

With the follow-up binary and 128 listener permits, all 100 independently
authenticated TLS SSE listeners opened. While retained, the same user's three
hybrid searches succeeded (7325.6 / 7344.9 / 7346.7ms), simultaneously with
initialize and two tools/list requests (102.1 / 115.8 / 82.9ms), all HTTP200 and
valid protocol results. Closing the streams yielded an observed stream active
gauge of zero with no queued entries.

The ledger attributed 1405 invocation events to exactly 100 `web_user` owners,
with zero duplicate event keys. The 32-slot four-phase run had 401 events:
267 successful searches, 33 document reads, 33 Wiki calls and exactly 68
`concurrency_limited` refusals. This includes one preliminary sanity call.
After drain, durable usage spool active=0 and pending=0. Database inspection
found six idle connections and the inspection query active, with no lock wait.

## Limits and release gate

The configuration supports the measured distributed workload, not unrestricted
100-person bursts or arbitrary dependency slowdowns. A 100-request simultaneous
burst deliberately returns bounded 429 with Retry-After. Retain single-owner
quotas and queue limits. Do not increase all quotas to 100 simply because the
platform has 100 users. If the measured production arrival rate approaches the
32-slot service capacity, improve slow stages or scale using an explicit
distributed-admission design and database/provider budgets.

Original 3/5/10/20 concurrency, two-user fairness, cloud route mixing,
document/Wiki/search mixing, cancellation/deadline/racing release, DB pool=2,
401/403/404 and private/public scope regressions remain in the original report
and CI. No production resource exhaustion was injected.

Private raw receipts: `E:/AIData/Codex/mcp-p1-hundred-*`,
`mcp-p1-load32-*`, `mcp-p1-soak-*` and `mcp-p1-sse100-result.json`.
They contain synthetic metadata only; generated test Keys remain in private
test-host files. No real user Key, query body or document response is copied
into this report.

## Actual production rollout and acceptance

Both code PRs merged before deployment:

- https://github.com/suoak/WeKnora/pull/136 — initial concurrency isolation.
- https://github.com/suoak/WeKnora/pull/139 — discovery/listener protection.
- Deployed main: `f71b5160872c982c6d3c747f3668abe71fc95ec9`.
- Actual APP image: `sha256:9e2dc174543a188006610ffde7f18b043f02806b6f1a0c3356ba7c4baed5b8c7`.
- Running server binary SHA-256:
  `5733a3068c4a89a2626929cf2c30fdddcad299d4aac11d68a1991dd53bfab013`.
- 4169 relevant source files in the release build matched the main source
  manifest (normalized line endings); no mismatches.
- Cutover/review interval: 18:43:46–18:44:13 Asia/Shanghai. This is the rollout
  procedure duration, not a measurement of the exact user-visible outage.
- Official schema-upgrade applied 3046 -> 3047, dirty=false.
- APP healthy, automatic restart count zero. Only APP was recreated; frontend,
  PostgreSQL, Redis and DocReader container identities were retained. Nginx
  configuration was preserved and reloaded to refresh the APP upstream address.
- Existing Feishu/sync control state (excluding changing health samples) matched
  before/after. No Docker socket was mounted and AUTO_MIGRATE=false remained.
- The production concurrency values are exactly the table above, including
  stream_user_limit=4. A dedicated durable analytics spool is mounted.

The complete PostgreSQL custom-format backup is 12,678,864,560 bytes; SHA-256
`07f6787a9619ad3d30d988e459770fbfd2b5f87d2a154075ee34110db609c4b9`.
Every archive data block decoded successfully through pg_restore --file=/dev/null.
This is full archive validation, not a full database restore rehearsal. The
first interrupted task-generated backup was identified as truncated and removed
after the replacement was running; original historical backups were untouched.
The old APP image, effective private Compose and configuration remain available
for rollback under `/root/knowhub-mcp-concurrency-p1-20261112`. Binary rollback
retains the compatible additive 3047 schema and collected usage audit data.

Real personal Key acceptance through production HTTP80 and certificate-
validated HTTPS8443 passed:

| Check | HTTP80 | HTTPS8443 |
| --- | --- | --- |
| Missing/wrong Key | 401 / 401 | 401 / 401 |
| initialize / tools/list | 200 / 200, eight tools | 200 / 200, eight tools |
| Authorized KB set | Same 61 IDs as before | Same 61 IDs as before |
| Three independent keyword searches | 3/3 success, ~563ms each | 3/3 success, 390–516ms |
| Three independent hybrid searches | 3/3 complete, 1328–1422ms | 3/3 complete, 1062–1203ms |

Hybrid acceptance selected a genuinely authorized populated KB; each response
reported one searched KB and zero failed KBs. These were successful tool
results, not merely HTTP200. The 15 search invocations issued across acceptance
rounds each had exactly one successful ledger event under their server trace
IDs; no duplicate event keys were found. The durable spool drained to zero.
The authorized 61-KB-set hash was identical on both routes before and after.

A second TLS listener test used the final stream_user_limit=4: 100 distinct
owners retained 102 connections, including three from one owner representing
multiple development clients. That owner's three hybrid searches succeeded
in 7315.9 / 7335.7 / 7339.5ms, simultaneously with initialize and two tools/list
calls in 93.0 / 94.4 / 99.7ms. All returned successful protocol results.

Production observations also include two other searches timing out at the
existing 30-second deadline and one permission-denied Wiki invocation. These
were not the acceptance trace IDs. They are not hidden or claimed fixed: the
shared-slot 429 issue and measured per-user concurrency acceptance passed,
while large-scope/slow-dependency retrieval remains a separate performance
limit. Broad production scope resolution still takes seconds. The synthetic
100-user throughput is not a guarantee for every full-scope production request.

CI on the final PR head passed complete backend, format/vet/test/build,
incremental lint, Portal/RBAC/race, actual ParadeDB authenticated concurrency,
MCP accounting and both migration lineages, plus Docker production startup.
Conditional skipped checks are not represented as executed. The earlier broad
local service test's four Feishu guard failures reproduced on the unchanged
baseline and remain documented in the initial report.
