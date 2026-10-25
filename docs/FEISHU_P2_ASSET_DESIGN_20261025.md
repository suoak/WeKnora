# Feishu candidate image publication

Baseline: ccd27c2fc1d6d4c2df232d5f5b0042d048a723f7.

The ordinary ImageResolver stores extracted images with SaveBytes; ordinary
multimodal jobs append OCR/caption chunks after the document is visible. Neither
operation is suitable for the synchronous versioned Feishu cursor boundary.

The Feishu path will reuse the resolver and image action policy, with an
attempt-scoped file-service adapter and synchronous, bounded enrichment. It must
not call the ordinary public chunk writer, enqueue image jobs, or bind images to
the stable knowledge ID before publication. Upload failures cannot be swallowed.

Candidate files are stored under tenant/attempt, deduplicated by content within
that attempt, read back and hashed, and recorded in the hashed manifest. Existing
file authorization denies unpublished and superseded attempt directories.
Publication validates tenant, backend, attempt ownership and all image/chunk
references, then commits resource bindings, chunks, vector/BM25 rows and active
attempt in one PostgreSQL transaction. Uploads are not part of that transaction.
Old files are retained on every failure and on replacement. Parser spans must be
remapped exactly when image references change; ambiguous transformations fail
instead of silently discarding semantic boundaries or source locators.

The existing caption/observation/OCR action policy and text-parent graph are
preserved. Image children point only to text chunks in the same manifest. A
failed requested image action fails the candidate, rather than publishing an
incomplete version. All heavy stages renew only a currently owned lease.

Cleanup defaults to inventory only: retain current/active attempts, referenced
resources, backups, and rollback images. Do not automatically delete candidate
files. Any future physical reclamation needs a verified, bounded object list and
a repeated reference check. Capacity admission must reject affected staging
writes while leaving old publications and unrelated online traffic available.

The local-storage admission default is 2 GiB free plus three times the incoming
file size, checked before candidate creation/uploads and before publication.
`FEISHU_STAGING_MIN_FREE_BYTES` may raise the floor; invalid/zero values fail
closed. The check is admission, not a cluster-exclusive disk reservation. Every
30 seconds the APP records an expiring, metadata-only storage sample and emits
state transitions to existing Redis audit and application logs. No external
paging integration is invented. Existing parser temporary-file context managers
already unlink on write/flush/close failure; this change does not sweep another
process's temporary files. `storage_inventory.py` and `asset_inventory.sql` are
read-only review tools, deliberately not automatic candidate/backup cleaners.

Feishu-scoped raw API model calls (including synchronous candidate VLM) use the
same fail-closed egress transport and shared cluster RPM/RPS/in-flight budget.
VLM image token consumption is not claimed as measured embedding TPM. Normal
online/unscoped model requests delegate unchanged to their existing transport.
Preserve the configured VLM/attribute/OCR policy; a builtin model remains usable
under the existing `(tenant_id = tenant OR is_builtin)` authorization rule.

Delivery: build/export on the packaging host. Load compressed verified archives
directly, never retain a reconstructed uncompressed image tar in production.
After independent replica hash verification, exact cache identity/reference
checks and loaded rollback-image verification, duplicate delivery caches can be
retired. Retain the authoritative packaging archive, provenance receipts, live
and rollback images, source snapshots and database/file backups. Configure APP
log rotation on the next APP recreation; do not restart unrelated services just
to change their logging settings.

Required validation: real parser + local files + PostgreSQL/ParadeDB, duplicate
images, exact span remapping, OCR/upload/readback/embedding failures, mid-batch
failure, expired owner, concurrent publisher, hold/revocation, pre-publication
file denial, superseded file denial, vector/BM25/gallery consistency and race.

Native parser prerequisite: CI identified RUSTSEC-2026-0367/0368/0369 in
the existing ZIP 8.6.0 dependency. Keep anydoc 0.2.4 and its C ABI, pin its
materialized ZIP dependency to 9.0.0, and build with the committed Cargo.lock.
Apply the pin to cached patched-anydoc trees too. Rebuild the native static
archive and verify its lockfile, build-script and archive hashes before linking;
an old archive must not be reused with the updated lockfile. Run the native
parser tests and cargo audit, without suppressing these advisories. The native
build script now requires Python 3 in addition to its existing Rust toolchain.
