# Feishu image extraction follow-up

Production image enrichment exposed a model response with empty visible content and `finish_reason=length` at the existing 5,000-token completion limit. Reasoning can consume that same limit. The request used the existing generic provider and its configured `chat_template_kwargs` thinking dialect.

For Feishu-scoped background extraction only, pass the existing per-request `Thinking=false` option. The model catalog retains responsibility for vendor dialect selection. Do not edit shared model rows, online chat settings, the completion limit, or the timeout. Providers without a supported off switch remain bounded and may still fail extraction.

Reject every scoped `length` completion, including nonempty partial OCR/captions. The existing candidate failure path retains the old published assets, chunks, vectors, and BM25 data. Non-Feishu VLM calls preserve their existing options and treatment of nonempty truncated responses.

Regression reproduction against main failed in both cases: the extraction request omitted the thinking override; nonempty truncated extraction was accepted. Tests cover the corrected scoped behavior, unchanged ordinary VLM behavior, the actual generic SDK request dialect, and denial after the shared Redis permit is revoked. Existing real PostgreSQL candidate image fault tests cover failed enrichment retaining the old published version. The integrated race job includes these VLM tests alongside image staging and atomic publication.

The first local broad parity run used `--network none`; catalog constructor DNS validation failed before mocked vendor requests. This was an isolation configuration failure, not a reason to disable SSRF. Repeat parity with working DNS, and require exact-candidate CI and controlled real extraction before further production rollout.
