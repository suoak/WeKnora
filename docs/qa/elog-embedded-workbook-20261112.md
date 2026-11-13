# ELOG repeated-export snapshot refusal

Production source `0612c94b-00a4-4468-8968-9b0fe7888bcd` in KB
`fe530e96-d1e0-42be-afdf-8ebf68a77cbe` repeatedly refused one document with
`latest_source_snapshot_changed`. This hotfix starts from actual production
`dc3f11db077a2eb6aef8abf1b8e3616d3f7a3377`; it introduces no schema migration.

Two authorized consecutive exports had identical source edit time, title,
binding and filename. Their only changed extracted DOCX parts were generated
`docProps/core.xml` creation time and the binary envelope of an embedded XLSX.
Every extracted file of that XLSX was byte-for-byte identical. The existing DOCX
identity normalized its outer ZIP envelope but hashed the inner ZIP as opaque
bytes, producing a false content conflict on every repeated observation.

The fix normalizes the embedded XLSX envelope, binding every filename and every
uncompressed byte, including cell XML, formulas, widths, relationships, properties
and binary assets. It does not reuse the standalone XLSX width normalization.
Malformed packages, duplicate/colliding names, traversal, CRC errors and shared
expansion-budget violations are rejected. Normalization stops after this one
embedded package level; its own binary assets remain fully byte-bound.

DOCX files containing embedded XLSX use explicit `feishu-docx-package-v2` identity.
Files without embedded XLSX retain their v1 algorithm and digest. Repository
validation admits both versioned DOCX identities only on the DOCX source path.
Old v1 identities are not silently relabeled: a differing old observation still
requires the existing fresh-source proof before replacing an unpublished or
published version. Source authority, revision checks, generation/lease fencing,
manifest publication and the final fresh observation remain enforced.

Validation on the isolated Linux test host:

- Both synthetic and actual repeated-export cases fail with the original code.
- `go test -short -race -count=1 -v ./internal/feishudocx` passes, including the
  private real-export comparison and cell/formula/layout/image/property mutations.
- Focused repository identity/snapshot race tests pass; `go vet` passes for both
  changed packages.
- Disposable PostgreSQL tests pass: `TestFeishuLatestSnapshotPostgres` (including
  v2 admission, equivalent retry and changed-cell refusal),
  `TestFeishuSourceIdentityPostgresP1`, `TestFeishuXLSXSourceIdentityPostgres`.
- The unchanged standalone XLSX large-table stress case was stopped during the
  initial broad race run and excluded with `-short`; it is not claimed as passed.
  An initial disposable PG probe ran before its network listener was ready; the
  corrected TCP-readiness probe and all three real-transaction tests passed.

Production exports and all credentials remain outside Git. Private evidence:
`E:/AIData/Codex/elog-snapshot-repair-20261112/` and the corresponding test-host
directory. Before rollout, build the native production binary, verify startup
with isolated services, retain the prior APP image/configuration, and gracefully
drain APP. Preserve schema 3046, source configuration/checkpoints, the historical
paused sync queue, and all other containers. After rollout, invoke ordinary
incremental synchronization through the authenticated UI and verify actual
publication; a passing sample comparison alone is not a completed source sync.
