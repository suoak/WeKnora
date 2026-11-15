# Bounded automatic Feishu recovery

Normal service keeps shared sync, approved Feishu sync and Wiki queues open.
The signed-health queue recovery and stop isolation are described in
[normal service](feishu-normal-service.md#recovery-and-lifecycle).

## Document snapshot recovery

When current full-source verification detects an edited document, the live
connector's fresh observation can be staged once more in the same execution.
This is enabled by the existing `KNOWHUB_FEISHU_LATEST_CONTENT=true` path;
no new credential, source permission or administrator signature is introduced.
The observation must retain the external ID, source root, URL, object type/token,
Wiki node and space, file format and a non-regressed revision. Deleted resources,
attachments and parents with externally emitted children require a complete
new traversal and cannot be adopted through this single-document path.

The retry repeats capacity, source authority, parser, assets, embedding and
atomic publication checks. It substitutes the new body/revision only after a
complete publication receipt; an existing matching receipt is also accepted.
Both the pre-parse observation and final pre-publication observation use this
same bounded recovery path. A document that changes while parsing must not
fall back to the generic whole-source admission retry. The rejected prepared
candidate is abandoned through its existing owner fence before another candidate
starts; it is never published to satisfy the cursor.
The durable SyncResult JSON records `auto_recovered` only for accepted items.
No source body, credential or private candidate is included in that counter.

Continued edits are recorded as `source_snapshot_changed`; the document remains
in the failed-document cursor, without an accepted checkpoint/publication for
the rejected version. Other documents continue. The next normal incremental
traversal retries this identity, including its full subtree when applicable.
There is no recursive retry or extra whole-source queue delivery for this case.
Permission, binding, execution, storage and unknown integrity errors retain
their existing controls. Automatic recovery does not fabricate authorization,
increase parser limits or discard emergency trips.

## Orphaned execution monitoring and visibility

The existing reconciliation worker runs every minute with a bounded operation
context. It examines heartbeats older than 30 minutes and checks a complete
native Redis delivery snapshot before finalizing an orphan under the exact
SQL generation/heartbeat fence. Live/retrying deliveries and Redis probe errors
are left intact. Progress, source permissions and successful timestamps are not
rewritten. Later normal source schedules use the retained checkpoint.

An exhausted/terminal failed run now reports `needs_attention`, rather than
claiming it is still actively retrying. A successful run with `auto_recovered`
reports that the current version was automatically revalidated. Failures that
need source-owner action remain visible in the ordinary failed-document view.

## Acceptance

Validate snapshot drift recovery, repeated drift, storage/publication refusal,
cancelled executions, changed resource bindings and external-child boundaries.
Verify that the connector preserves the failed-document cursor across restart,
continues healthy siblings and the recovery count requires a durable receipt.
Use one consolidated main deployment and verify all queues remain open, ordinary
manual/scheduled executions, published chunks/vectors and existing Wiki reads.

## Per-document Excel parser preference

For a failed source-managed Excel item whose complete row exceeds the semantic
chunk limit, an authorized source writer can use the existing datasource update
API to set `config.settings.feishu_document_parser_overrides_v1`. Its keys are
exact external IDs within that datasource, and each value accepts only
`{"xlsx_context_column_count": 1}` (integer range 1?64). Preserve the existing
source reference, resource IDs, scheduling and other configuration on update.
Credentials remain managed by the existing update service.

The setting applies only to builtin Excel parsing in the private Feishu staging
path. It repeats the requested leading context columns when splitting complete
cells; it neither truncates content nor raises semantic/resource limits. Invalid
settings, attachment items and incompatible engines/types are rejected. Source
edits use the existing authorization and execution configuration fence. No direct
knowledge metadata update or destructive reparse is used. This preference repairs
subsequent failed/unpublished candidates; an already published identical source
retains the existing duplicate-content behavior.

Validate the exact original offline first, then use one ordinary source sync and
verify its publication receipt, complete chunks and vector indexes. Resource-limit
failures of very large workbooks still require a smaller source workbook.

## Embedded worksheet column permutations

Live diagnostics found identical DOCX exports with stable source revision, title
and filename but a different order of disjoint `<col>` declarations in embedded
XLSX worksheets. The complete column attribute sets, cell data and other parts
were unchanged. This produced a false `source_snapshot_changed` refusal before
staging, despite the bounded recovery keeping healthy siblings running.

The snapshot-only DOCX v3 comparator orders disjoint column definitions by their
column number. It preserves every width, style, hidden flag, range and unknown
attribute. Overlapping definitions retain their original order because precedence
can affect the effective layout. Invalid or ambiguous column declarations are
refused. Both styled and style-less embedded workbooks use this rule. Stored
package v1/v2 identity algorithms are unchanged; a source name, authority or actual
content change still cannot publish through this comparison.

Do not infer that every snapshot failure is benign. Reproduce with bounded pairs
of authorized live exports, verify source metadata and full package differences,
and retain original packages privately. Native tests must prove the exact pair
matches through `MatchLatestFetchedSnapshot`, while width/cell/style and overlap
changes remain distinguishable. Consolidate any eventual deployment after the
current verification batch rather than restarting it just for this normalization.

## Whole-parent embedded workbook budget

The legacy source expansion path previously refused a fifth embedded workbook,
although the same five real packages individually passed the complete reader.
Use the personal exact import whole-parent budget before any native workbook
conversion: at most 64 unique parser-issued package references, at most 8 MiB
per compressed package, 32 MiB compressed total and 64 MiB expanded total. Each
ZIP entry remains limited to 8 MiB. This preserves the previous compressed total
ceiling (four times 8 MiB) and tightens the previously multiplicative expanded
allocation (four times 32 MiB). Count no longer substitutes for a parent budget.

Unknown packages, duplicate names, unsupported relationships and unmapped parser
semantic offsets still refuse the parent. No partial parent is published: all
workbooks must prove complete cell coverage and replace exactly one original
placeholder before the result changes. Literal hyperlinks remain unfetched.

## Controlled source document reparsing

Source-managed Feishu knowledge must never enter ordinary destructive reparse.
The ordinary path deletes prior indexes/chunks and writes process overrides and
parse state directly; PostgreSQL correctly rejects those writes with SQLSTATE
40001 because they do not carry the current staging attempt's capability.

The metadata-only admission recognizes persisted Feishu source bindings and
version-enrolled resources before any body load, override update or cleanup. An
authorized editor gets HTTP 409 with reason `feishu_source_sync_required`; an
unauthorized caller gets no datasource identity. Personal exact import admission
retains its own controlled recovery protocol. Incoming mixed batches and already
queued legacy batches are checked in full before any ordinary prefix is queued.

Do not replace the document action with whole-source synchronization. The user
requires a direct, per-document reparse action with its parsing configuration.
The earlier full-source UI substitution was withdrawn before deployment.

The connector now has an independently tested exact-document fetch primitive:
validate the configured root, require an unambiguous current binding, fetch only
the selected parent, and re-list/re-fetch on every snapshot check. It neither
changes configured selection nor acknowledges a traversal/deletion cursor.
`POST /datasource/:id/documents/:knowledge_id/reparse` accepts a durable request
for one selected source document. The server derives root/external identity from
persisted metadata, verifies current tenant membership and source ownership, and
wakes the normal source admission queue. Missing parser settings preserve the
document's existing settings. Explicit supported parser/chunking/VLM settings
are bound to the immutable request; unsupported enrichment settings are rejected.
Concurrent parameter edits fence both preparation and publication.

An unchanged source still receives a new staging attempt. The serving version
remains until the replacement and request completion commit together. Request
completion failure rolls publication back. A completed claim cannot force a
second publication. Three actual attempts survive execution changes and restart;
delivery-only failures use a separate bounded backoff. An independent five-second
cron sweep rechecks live authority and wakes at most 32 distinct sources without
blocking generic document recovery. Source-only runs do not advance inventory
cursors or `last_sync_at`. Request progress is projected only onto already
authorized catalog rows and never changes the serving document's parse status.

This endpoint supports enrolled Wiki/Drive sources. Personal exact imports keep
their separate controlled protocol; the endpoint does not convert them into a
datasource. These changes require merged-code CI and a consolidated release
before they can be described as available in production.

No database trigger, source permission or parser resource limit is disabled.
Do not change parse status or retry protected writes in SQL.

## Actionable parser failures and bounded retries

The pending ordinary-document change persists `metadata.parse_failure_v1` with
a safe reason code, recovery state, recommended action, actual attempt count and
queue retry budget. Transport failures and unknown parser errors retain the
existing finite queue retry budget. Only a current processing task with retries
remaining may present `retry_scheduled`. Exhausted tasks require attention.
Known encrypted containers, format mismatches, semantic row limits, blocked
addresses and exhausted parser allocations stop repeated identical attempts.
Raw parser diagnostics remain in the processing trace, not the public message.
Cancellation, deletion and replaced source files must not be revived by this
failure recorder. This does not replace the separate controlled Feishu protocols.

The blank-underline XLSX compatibility fix is PR #168. It changes only a private
parser-input copy, preserves original uploads and all shared-string text including
CR/LF, and leaves worksheet parts byte-identical. Three exact production originals
passed complete parsing with the existing header/chunk settings. Consolidate its
eventual deployment with the already merged snapshot and workbook-budget fixes.
