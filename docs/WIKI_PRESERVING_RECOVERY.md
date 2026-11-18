# Preserve visible Wiki pages during verified regeneration

Use this bounded operator recovery when current source documents independently
verify, but historical page provenance refers to an earlier source version.
The regular regeneration command remains available. This mode does not delete
historical pages before their replacements exist.

## Preview and apply

Run the CLI with the deployed application's PostgreSQL/Redis configuration and
the exact owning tenant and KB. The command takes the same KB lock as workers.

```sh
wiki-regenerate -preserve-existing-pages -tenant TENANT -kb KB
wiki-regenerate -preserve-existing-pages -tenant TENANT -kb KB -apply -snapshot PREVIEW_SNAPSHOT
```

Preview validates the complete current source closure, page mutation triggers
and original unclaimed ingest operations. Apply rejects a changed snapshot,
preserves existing task identities and queues each remaining verified source.
It changes no document parse state, connector configuration or sync cursor.
The operator marker is internal SQL state, not a request header or source grant.

Generation reads only pages whose current provenance verifies. Old page bodies
are excluded from model-facing queries, including metadata projections. A new
page with the same slug atomically replaces the captured old row only after all
normal source and Wiki mutation fences pass. The page ID stays stable, its
version advances, and its previous content is retained as revision history.
Failure or concurrent human/source edits roll back the replacement.

## Finish

```sh
wiki-regenerate -finish-preserving-pages -tenant TENANT -kb KB
wiki-regenerate -finish-preserving-pages -tenant TENANT -kb KB -apply -snapshot FINISH_SNAPSHOT
```

Finish is refused while ingest/finalize work remains, while any current nonempty
source lacks a published verified summary, or when an unverified historical page
changed after recovery began. Only unchanged obsolete pages captured in the
original plan may then be archived. Their original rows and bodies remain in
storage; generated pages retain their current provenance. The scoped recovery
marker becomes completed and ordinary strict generation resumes.

Always retain the preview, off-host backups and apply receipts. A rejected plan
does not authorize direct proof edits, marking work successful or deleting the
old queue. Re-preview after actual inputs change; do not override a live claim.
