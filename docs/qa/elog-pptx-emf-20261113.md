# ELOG PPTX embedded EMF ingestion

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
