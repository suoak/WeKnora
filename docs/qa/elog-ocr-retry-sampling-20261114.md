# Escape repetitive OCR after a truncated first attempt

The structured retry restored the Chongqing document, now published with 27
chunks and vectors. Two other finite terminal screenshots still repeat until
20,000 tokens at the model's temperature 0.1. Explicit stop instructions and
run-length output did not resolve these images. Transcoding is not indicated.

Raise sampling to at least 1.0 only for the existing structured OCR retry.
Successful first attempts, captions, interactive calls and unsupported protocols
retain their temperature. Preserve a higher operator-configured temperature.
The existing deadline, single full retry, disabled thinking, JSON validation,
output cap and refusal to publish truncated results remain enforced.

Both failing original images completed twice at temperature 1.0: the single
1577x598 screenshot took 5.94/7.39 seconds; the 1181x678 ping screenshot took
6.25/5.99 seconds. All four calls stopped and returned valid lines arrays.
OCR line grouping and recognition vary between calls; these observations do not
establish exact transcription. Original assets remain authoritative and unchanged.
No diagnostic response was published.

Regression checks cover initial/online/unsupported sampling preservation and
the actual initial/retry wire temperatures. Required CI and a merged-main build
with schema-3047 startup smoke precede APP-only deployment. No migrations,
shared model configuration or DocReader changes are included.
