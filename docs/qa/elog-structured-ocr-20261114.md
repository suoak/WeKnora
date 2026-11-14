# ELOG screenshot OCR continuation regression

The production model repeatedly continues terminal prompts from a finite screenshot.
The same line occurs 491 times before the 20,000-token output is truncated. The
source has visibly repeated prompts, so deduplicating output would discard evidence.
Changing WebP to a pixel-identical PNG and adding stop instructions did not help.

Use a closed JSON lines object for the existing complete retry when a Feishu OCR
request is truncated and its resolved Chat Completions protocol supports JSON mode.
Successful first attempts, image descriptions, interactive calls and unsupported
protocols keep their current behavior. Both attempts share the existing deadline;
the retry retains the 20,000-token cap and disabled thinking.

Validate the complete object before returning any transcription. Reject missing,
null, duplicate or extra fields, non-string/null entries, malformed or trailing
objects, invalid UTF-8, excessive output, and unfinished completions. Join lines
in order without deduplicating or rewriting source assets. An explicit empty
array represents an image without text.

Production-image diagnostics: the JSON request completed at 5,000 tokens in
13.28 seconds (3,010 tokens, 50 lines, 274 date entries). At the retry's actual
20,000-token cap it completed in 15.71 seconds (2,995 tokens). These are model
transcriptions, not a guarantee of character-perfect OCR; the original image
remains the source. No partial OCR was published by the diagnostics.

Validation includes the actual wire format and unchanged image bytes, scope and
capability gating, successful-first-attempt compatibility, repeated-line
preservation, strict object decoding, and bounded truncated/filtered retries.
Run the VLM and image-processing race regressions and required CI before merge.
Build from the merged main revision and verify schema-3047 startup before APP-only
deployment. There is no migration, model configuration change or DocReader change.
