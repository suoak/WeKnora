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
