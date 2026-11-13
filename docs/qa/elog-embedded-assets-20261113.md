# ELOG embedded workbook ingestion repair

Production baseline: main ab8bb762ab2ca4c3a8582cfc9d8aae44697b4630,
schema 3047. The first snapshot repair removes embedded XLSX ZIP-envelope
variability, but the manual sync still fails for two DOCX documents.

## Findings and behavior

The DOCX reader emits an embedded XLSX as an OLE image reference. Image
admission correctly refuses its non-image MIME type. Expand only parser-issued
OLE bytes using the existing closed workbook reader, prove complete worksheet
cell coverage and a unique placeholder, append all worksheet sections and image
assets, then run normal image admission. Unknown dependencies and ambiguous
placements remain refused, without partially changing the parsed result.

The original manual contains seven PNGs and one workbook resource. The workbook
has 12 cells and one PNG: complete expansion yields eight real images. A previous
native-reader-only check counted eight resources but did not prove image admission.

Repeated authorized exports also reorder the unique declarations in
`[Content_Types].xml`, in both standalone and embedded XLSX. Compare fresh
snapshots with a separate versioned digest that sorts declarations while binding
their complete names and MIME values. Duplicate or unsupported declarations fail.
Keep persisted XLSX v1 and DOCX v1/v2 identity algorithms unchanged. Preserve
source revision, authority, title, filename, execution generation, lease and
atomic publication checks. Embedded widths and every other embedded part remain
byte-for-byte bound. Standalone XLSX retains its existing width normalization.

## Validation

Isolated Linux testing uses no production writes. It covers race tests for package
comparison; real double-export fixtures; native workbook parsing and candidate
image resolution; changed cells/formulas/assets and malformed declarations;
repository snapshot binding and revision checks; and vet of the changed packages.
Real fixtures and credentials are private and are not committed.

Merge into the current main before deployment. Recheck both main and live APP,
build the merged source, run the isolated schema-3047 startup smoke, then replace
only the APP while preserving the live environment and unrelated services.
Do not mark the incident resolved until a normal source sync publishes its
documents and advances its accepted checkpoint.
