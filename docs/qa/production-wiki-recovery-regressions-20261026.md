# Wiki generation recovery regressions

Production incident: generation could stop before MAP on an unproved historical
index, and a browser GET could create another unproved default index during
recovery. A large successfully generated batch also ran out of the ten-second
claim-cleanup budget while publishing pages, leaving durable operations unsettled.

This change makes the HTTP directory view read-only when the index is absent.
It returns an empty introduction and continues listing existing pages. Persistent
index creation remains in the generation path. It also gives normal publication
and post-processing a batch-sized, bounded context separate from emergency claim
cleanup, and reports an early returned error as failed in batch statistics.

No legacy page is certified by this change. Existing unproved content still
requires verified regeneration; source closure checks, page proofs, version
checks and mutation triggers remain in force. The production data reconciliation
was a separate operation and is not a database migration in this PR.

Regression tests cover repeated directory reads without writes, existing intro
preservation and database errors, cancellation and deadline bounds, and an actual
publication operation lasting beyond the old ten-second timeout. The dedicated
PR/main workflow also runs existing Wiki/provenance tests with race detection.

Follow-up work remains for a general historical-page regeneration command and
cross-page backlink changes conflicting with in-flight dependency snapshots.
Do not disable provenance or weaken ETags to work around those cases. Production
images must be built from the merged source, with required CI checks and the
image revision recorded, rather than relying on uncommitted hotfix files.
