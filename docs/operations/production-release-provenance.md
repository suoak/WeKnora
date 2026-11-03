# Paired production release identity

APP and Frontend production releases must use the same validated full source SHA.
Before switching either service, fetch current main and run
`scripts/ops/verify_release_provenance.py` against the exact approved image-inspect
snapshots. Record the expected remote main SHA and both immutable image IDs. The
gate rejects stale main, missing main ancestry, missing revision labels, mismatched
APP/Frontend source SHAs and unexpected image IDs. It never deploys or changes main.

```sh
python3 scripts/ops/verify_release_provenance.py \
  --repository . --expected-main-sha FULL_MAIN_SHA \
  --app-inspect /restricted/app-image.json \
  --frontend-inspect /restricted/frontend-image.json \
  --expected-app-image-id sha256:APP_CONFIG_DIGEST \
  --expected-frontend-image-id sha256:FRONTEND_CONFIG_DIGEST \
  --output /restricted/release-provenance.json
```

The receipt records APP and Frontend source SHA, image configuration digest,
registry digests when available, current main SHA and ancestry result. A local
Docker configuration digest is not a registry manifest digest. Engine-normalized
image IDs must also have the separate approved raw configuration/rootfs/byte
verification used by the production deployment tooling.

This gate supplements source archive, binary, frontend static-resource, migration,
permission and business verification; OCI labels alone cannot prove image bytes.
Preserve the pre/post running container identities and the acceptance evidence with
the receipt. Recheck the remote main immediately before switching and after rollout.
If main or image identity changed, stop and issue a fresh receipt.

Keep Safety incident identity, new Worker incarnation, source admission signatures,
queue snapshots and any schema compatibility checks separate from release identity.
Do not resume a protected queue or reuse a recovery signature to finish a rollout.
Retain the previous paired images and compatible rollback instructions. Never
restore an old database snapshot over new business data.

Known production-only branches and superseded PRs must be reconciled by commit
ancestry and patch inspection. Do not merge every Draft PR or overwrite main.
