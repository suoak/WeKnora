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
