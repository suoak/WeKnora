# ELOG branch-document export repair

Production baseline: merged main 6062b270c4, schema 3047. The prior repairs
published the original three failed documents. Correcting the gateway thinking
dialect also allowed three subsequent OCR failures to recover. The complete
source scan then revealed two remaining documents: Anhui branch and Shandong
CCB. Their private double exports reproduce these additional cases.

Both embedded workbooks contain valid direct string cells (`t="str"`) without
formulas. Accept that type in the existing bounded complete-cell reader;
formula/dependency rejection, coordinates, cell limits and complete coverage stay
enforced. The source tables contain nine and twelve cells, respectively.

Shandong exports also reallocate border and cell-format indexes and interchange
identical fill definitions. Compare fresh DOCX snapshots by sorting the complete
border/fill/cell-format tables and remapping all supported references. Keep index
zero fixed for implicit defaults. Preserve the entire table, its multiplicity,
and each cell's resolved style. Reject invalid indexes, ambiguous tables and
unsupported reference locations. Cells, formulas, widths, actual style properties,
relationships and binary assets remain bound. Stored DOCX v1/v2 identities are
unchanged; the separate fresh snapshot algorithm advances to v2.

Validation uses bounded synthetic mutation tests, complete package race tests,
private real double exports and native workbook/image admission tests on isolated
Linux. Private document bytes and credentials are excluded from Git. Merge into
current main before building the merged native binary and schema-3047 startup
smoke. Recheck live APP identity and preserve existing configuration/checkpoints
when deploying. A completed source sync is required before incident closure.
