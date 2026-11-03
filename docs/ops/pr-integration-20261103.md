# Production integration and PR disposition, 2026-11-03

Reviewed against main `50712d3c71990372bf9d430a5d84f83d9f4f123a`.
This record describes code disposition, not authorization to widen datasource
access or replay historical work. Production APP and Frontend must be built
from the same validated main commit and pass the existing provenance check.

| PR | Assessment | Disposition |
| --- | --- | --- |
| #73 | Four unique commits; signed shared-queue reconciliation preserves exact task bytes, source permissions and terminal outcomes. Its APP, lint, Portal and shared-queue checks passed. | Integrate its full history with the document incident fix, then close its separate entry after main contains the head. |
| #71 | Entire head `0c824d4e2` is already an ancestor of main. | Close duplicate PR; retain branch and history. |
| #70 | Entire head `c8d3733b3` is already an ancestor of main. | Close duplicate PR; retain branch and history. |
| #69 | Entire head `7cdff9fbb` is already an ancestor of main. | Close duplicate PR; retain branch and history. |
| #68 | Entire head `6042d855f` is already an ancestor of main. | Close duplicate PR; retain branch and history. |
| #66 | Entire head `dbf167131` is already an ancestor of main. | Close duplicate PR; retain branch and history. |
| #65 | Entire head `96090bb85` is already an ancestor of main. | Close duplicate PR; retain branch and history. |
| #64 | Entire head `539158a1f` is already an ancestor of main. | Close duplicate PR; retain branch and history. |
| #67 | Three unique commits add a separate sealed-document retry/quarantine protocol and frontend actions. These are absent from main and are not equivalent to administrative deletion. | Retain as draft for independent integration and acceptance; do not discard its unmerged implementation. |
| #62 | Seven unique commits add source-admission status APIs and frontend presentation. This is broader than the failing DELETE path. | Retain as draft; a complete frontend/backend acceptance is still needed before release. |
| #61 | Four unique commits add derived-write publication fencing and migration 003043. This changes schema and publication behavior independently of this incident. | Retain as draft; resolve lint/integration and renumber its migration above the already deployed 003045 before separate acceptance. |

The incident fix starts from production APP `b874d5437` and is cherry-picked
onto current main without replacing the existing mainline fixes. It separates
administrative deletion metadata from source-content publication checks,
invalidates old personal execution ownership transactionally, and bounds
PostgreSQL batch commit checks so lease renewal is not starved by repeated
per-index authorization. Existing current-owner, tenant and API-key guards remain.

The focused PostgreSQL/race workflow must run on the final integrated candidate.
It covers unpublished deletion, current membership, private-owner and tenant
isolation, stale-worker rejection, rollback and a deterministic lease-renewal
regression. A separate local control run with the production repository code
reproduced the expired-lease failure under the same bounded latency fixture.

Release must preserve database, storage, existing source policies and queued
task identities. A source-SHA change must use the existing safety pause/drain
and current signed recovery flow; it does not permit global source activation,
blind task replay, dropping queued messages, or restoring an old business DB.

A concurrent production release was detected before service cutover. Its
`789fb3a24c2d2ab6b44aa4a270c7b967b683da5a` identity handoff fix and
`4c91667d6e1ea40bd061ac12f7f8bb67eed6b06e` frontend explanation are included.
Migration 003045 retains historical principals while requiring new attempts to
use the current principal. Final migration expectations are 3045; the deployed
allowlist and all current source restrictions must be preserved. The runtime
image includes candidate migration files, and rollback uses the current hotfix
image without reverting principal history or restoring the older database.
