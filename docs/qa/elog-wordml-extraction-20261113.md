# ELOG Word 2003 XML and bounded image extraction

Production baseline: merged main e9a19fb5a, schema 3047. The PPTX is now
published with 61 chunks/vectors; the remaining technical failures include a
Word 2003 XML file named .doc and an image OCR response exceeding 5000 tokens.

For scoped Feishu .doc requests with the exact Word 2003 XML root, use the
existing private DocReader and its installed python-docx to build a DOCX from
the closed paragraph/run model, including literal list labels and hyperlinks.
Refuse unsupported body structures, active/external dependencies, ambiguous
image references and malformed or oversized input. Compare the converted
package's ordered text/image events, complete image bytes and hyperlink targets
against the source. Run splitting and inserted layout spaces are normalized;
every non-whitespace character, image order and anchor paragraph remains bound. Native DOCX
parsing and the ordinary candidate image checks still run after conversion.
The original fetched XML remains the source snapshot. No source permission,
publication or checkpoint guard is relaxed. Conversion payloads stay inside
the private RPC and are removed before normal result materialization.

Dense scoped VLM extraction gets one complete retry with a 20000-token ceiling
after a 5000-token length finish. Discard the partial first response, retain
the existing overall timeout and disabled reasoning, and still refuse a
truncated retry. Ordinary interactive VLM requests retain their existing behavior.

Validation covers source dependencies, missing/changed text and images,
hyperlink coverage, scoped routing and binding errors, VLM retry bounds and
ordinary request compatibility. Private real documents and model responses
remain outside Git. Complete real WordML conversion/native candidate tests
and production-baseline startup verification must pass before deployment.

The independent IM retrieval failure is not an empty ELOG library: its
synthetic system user fails the personal published-viewer membership check.
This change does not grant that identity access or change IM authorization.
