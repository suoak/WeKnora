# ELOG PPTX images and XLSX export snapshots

Production baseline: merged main 43c2b7cf22, schema 3047. The two branch
documents now publish with 70/60 chunks and vectors. A later source attachment,
RG-eLog introduction.pptx, contains two EMF images among its raster media.

Keep the original PPTX and every image placement as the source. On scoped
Feishu PPTX parsing only, send parser-issued EMF bytes to the existing private
DocReader service, rasterize with its installed LibreOffice renderer, validate
the complete PNG, and then use the unchanged candidate image admission and
publication guards. Unknown inputs, incomplete records, missing rendering or
invalid PNG output refuse the candidate. Native unknown-format asset names must
match the serializer's deterministic .bin links; host MIME extension guesses
previously detached EMF bytes from their placements.

The EMF reader bounds input bytes, declared dimensions and record count, walks
every complete record through EOF, uses an isolated LibreOffice profile and the
existing process-group timeout executor, then validates output PNG bytes,
dimensions and integrity. The APP validates the returned image again.
Use the documented [PNG export pixel parameters](https://help.libreoffice.org/latest/en-US/text/shared/guide/graphic_export_params.html)
for a bounded higher-resolution canvas. Remove only its surrounding white page,
preserving every rendered mark with a margin; empty or edge-clipped output fails.
No source file is edited and no image failure becomes a silent text-only import.

Validation includes synthetic malformed/truncated input and raster tests,
parser routing, native scoped parsing of the private real PPTX and complete
candidate image admission, and real rendering of both EMFs in an isolated
network-disabled container. Private document bytes and credentials are excluded
from Git. Merge main first, build its native APP, and stage the three Python
source changes on the exact current DocReader image without changing its
dependencies/configuration. Drain tasks before replacing DocReader and APP;
preserve source checkpoints and retain both previous images for rollback.

The separate Zhejiang branch export returns Feishu HTTP403/code1069902 and
requires resource permission correction by its owner/administrator.

The security/gateway worksheet also produced different hyperlink relationship
IDs and declaration/link order on consecutive exports of the same revision.
Fresh XLSX snapshot v2 binds complete hyperlink targets, modes and attributes
while comparing declarations and uniquely placed links without exporter order.
Other relationship IDs remain unchanged. Unknown uses of a remapped ID,
dangling links and duplicate placements fail closed. Persisted package v1
identities retain their existing behavior. Real consecutive exports compare
equal; changed targets, placements, labels and tooltips still change the digest.
Cell values, formulas, styles, assets and source authority/revision fences stay
covered by the existing package and repository tests.

Go 1.26.11 CI rejects LLVM metadata in Rust static archives before tests run.
Prepare the copied Linux archive by removing its .llvm* sections with objcopy,
retaining native code/symbols; tool failures stop the build. This follows the
reported [upstream build issue](https://github.com/Tencent/WeKnora/issues/5399).
Do not downgrade Go or disable its cgo validation. Production rendering and
native parsing still use the existing validated dependencies.
