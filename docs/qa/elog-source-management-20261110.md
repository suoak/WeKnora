# ELOG source management and image settings, 2026-11-10

The user authorized clearing ELOG and starting over, then requested shared
workspace source configuration, document deletion, repairs and deployment.
Private knowledge bases and cross-workspace boundaries remain enforced.

## Observed failures

1. Editing an already attempted source persisted `hold`; even a failed prior
   sync excluded the unused-source exception. Clearing that flag once did not
   change the next edit's behavior.
2. Knowledge-base access checks included deleted source tombstones. A deleted
   `revoked` source therefore blocked settings and sync diagnostics. Its source
   metadata endpoint could still report a different, active current source.
3. The deletion API admitted an asynchronous task, but the publication trigger
   rejected `parse_status=deleting` with SQLSTATE 40001. Local member deletion
   had no capability distinct from a live source publication attempt. HTTP 200
   meant task submission, not completed deletion.
4. GET knowledge-base details reused the access-only authorization projection.
   VLM and other configuration fields were absent/zero. Saving the settings
   page then wrote those defaults back. The persisted image settings and GET
   response were demonstrably different before the hotfix.
5. Independent ingestion errors also existed: image asset extraction refusal
   and repeated `latest_source_snapshot_changed` for an exported document.
   Clearing a source flag never proved those documents imported successfully.

## Implemented behavior

- Authenticated local workspace members can configure every connector, change
  KB settings and delete documents. API-key capabilities, private-owner checks
  and cross-workspace grants remain separate boundaries.
- A configuration edit cancels old execution generations and clears the old
  configuration's checkpoint. It enables a fresh sync instead of requiring an
  administrator to release `hold`. Saving a legacy held source also repairs
  that operational state without changing the selected credentials.
- KB metadata/settings remain available when an external source is unhealthy.
  Source/document serving proofs remain enforced at their output boundaries.
- A member deletion retires its exact document execution and records a
  `deleted` tombstone, distinct from source access revocation. Publication
  triggers permit removal-shaped operations only, and reject content updates,
  insertion and stale republishing. Scheduled sync skips that tombstone.
- Wiki cleanup uses an internal scope for the admitted KB and reads chunk IDs
  without loading source bodies. An unavailable source cannot prevent removal
  of its derived references. The cleanup scope grants no document body read.
- A settings GET reloads complete persisted configuration after authorization.
  Missing VLM fields preserve settings; an invalid explicit model returns an
  error instead of reporting success while disabling image processing.

## Completed production image-settings hotfix

Source: `41b92a65e2b41d9384b01d65140296bcd364d7ea`, based on production
`fd4f40f93afc85eb53c18584ff23652cefc055c3`. This first rollout contains only
the image/settings read and validation fixes; the collaboration/deletion work
is a subsequent release.

The native production build, focused race tests, vet and isolated HTTP tests
passed. The HTTP tests saved/reopened twice, retained model/language/image
instructions and zero overlap, preserved VLM on an unrelated save, and rejected
an unavailable model with HTTP 400 without overwriting the prior settings.

Production switch completed at 2026-11-10 15:22:15 Asia/Shanghai. Binary SHA256:
`3bcf6e0ed31f3cb7810cd77dd9d9cab351e6f9dacf53165b6678d613f4ab5f4b`.
The APP is healthy; database schema remains 3045/clean. Other containers and
source configuration/checkpoints were preserved. Only owned queue pause
markers were released; the prior shared sync pause was retained.

ELOG's user-selected enabled state and `deepseek-v4.1-flash` model were restored
with a private snapshot and audit record. A fresh production GET now matches
the persisted configuration. This verifies settings persistence, not a complete
image-bearing Feishu document ingestion.

Private release receipts: `E:/AIData/Codex/elog-settings-release-20261110/`.
The user-authorized ELOG reset preserved its ID and URL, removed three live
documents and their chunks, retired three old sources and their live runs,
and was verified empty through HTTP. Reset snapshots are retained privately.

## Collaboration/deletion validation

The PostgreSQL tests exercise the actual migrated publication triggers, removal
of an enrolled document, cancellation of stale execution, protection against
content writes, and sibling preservation. SQLite migration parity, role and
private/cross-workspace checks, frontend type checks and connector configuration
tests are part of the release validation. The `deleted` state requires schema
3046; downgrade refuses existing deletion tombstones instead of losing them.

The final native Linux build for `66b921d43f03d99fb3796dd46da85a690ce93f06`
passed repository race tests, handler/router/types/service/database tests,
normal-source authority tests and vet. Frontend type checks, 19 focused tests
and the production asset build passed.

An isolated HTTP environment applied the official 3045-to-3046 migration and
verified a non-creator viewer saving held source configuration (old execution
canceled), saving/reopening image settings, and single/batch/clear deletion of
three enrolled documents. Actual live document/chunk counts reached zero with
three deletion tombstones. Source deletion did not block later settings reads.
Cross-workspace source editing returned 403. Synthetic fixtures were removed.

## Completed collaboration/deletion deployment

Production APP and frontend now both identify revision
`66b921d43f03d99fb3796dd46da85a690ce93f06`. Deployment completed at
2026-11-10 16:15:16 Asia/Shanghai. Native binary SHA256:
`d7d11bc467738b145877549ad74695116809eea9ec101836155fedc414f67acc`.
The standalone official migration runner applied 3045 to 3046 while the prior
APP was stopped; the resulting schema is clean. Schema and source execution
snapshots and the prior APP/frontend images are retained for recovery.

The new APP is healthy and the frontend index hash matches the built artifact.
Source configurations and cursors were equal across the stopped-to-started
checkpoint. Database, Redis, docreader, MCP and Neo4j containers were untouched.
Only this deployment's queue pause markers were released; existing pauses
were preserved. No forced application kill was used.

Production HTTP verification returned 200 for ELOG settings, source list and
document list. The VLM enabled flag is true, the selected model remains
`f3c2beaa-3c50-432b-b3bd-41c929feb086`, and both VLM and image-processing
configuration match persisted values. ELOG currently has zero live documents
and sources following the authorized reset, so complete fresh ingestion is
not claimed by this deployment verification.

Private build, HTTP, staging, deployment and production receipts are under
`E:/AIData/Codex/elog-collaboration-20261110/`. An application rollback retains
schema 3046; its removal-only expansion preserves publication checks and must
not be downgraded after deletion tombstones exist.
