# Feishu V2 isolated packaging helpers

These helpers target only the authorized packaging host `172.30.232.42` as
`aqyfzx`. They reject unknown SSH host keys and read credentials through stdin.
They do not discover credentials from other environments. Reports omit secrets.
Python requires Paramiko; the PowerShell wrapper decrypts a caller-specified
current-user DPAPI file in memory and streams the result to a selected helper.

The workflow is deliberately staged:

1. `feishu_v2_inventory.py`: read-only container, image, port and disk inventory.
   Timeouts are recorded as incomplete probes, never as successful checks.
2. `feishu_v2_prepare_build.py`: archive the exact committed SHA with LF text,
   upload into a fresh private directory under
   `/mnt/sata0/knowhub-feishu-v2-builds`, verify SHA-256, then safely extract regular
   files and directories. It does not package local uncommitted files or secrets.
3. Download the same SHA's `feishu-v2-native-library-<SHA>` artifact from the
   authenticated GitHub Actions run. Its qualification covers the native build
   and cargo audit only; it is **not** evidence that the complete run passed.
4. `feishu_v2_build_candidate.py`: validate the artifact's source/toolchain/file
   fingerprints and archive members, then compile in the verified Debian builder
   with two CPUs and 4 GiB memory. It imports only modules named by this source's
   `go.sum` from the existing read-only module cache; module checksums remain
   enforced. The application and its databases are never mounted. Repeat calls
   poll the same build. A new `--attempt` preserves failed build artifacts.
5. `feishu_v2_runtime_image.py`: require successful compilation, verify binary
   digest and build identity, overlay the current binary/config/scripts/migrations
   onto the pinned local Debian runtime, then check shared-library linkage in an
   offline container. It creates an image, without starting the application.
6. Download `feishu-v2-frontend-image-<SHA>` from the passing frontend job. It uses
   the repository Dockerfile's pinned Node/nginx bases and records its source SHA,
   image ID and archive digest.
7. `feishu_v2_prepare_test_site.py`: verify/load that frontend image and write a
   private Compose plan with new PostgreSQL, Redis, files, staging and network.
   Two application processes share only these new resources. Preview ports bind
   to loopback. This helper starts no services and leaves the HTTP OAuth exception
   disabled. The `preparation.json` explicitly records migration/restore checks
   as `NOT_EXECUTED`.

The candidate's complete CI must pass before application deployment. The next
deployment steps still require actual empty-database backup, migration, restore
into a separate disposable database, worker/health/privacy/MCP smoke checks and
entry rollback verification. Preparing a Compose file is not a deployment PASS.
None of these helpers switches the existing 80/8080 entry, merges main, starts
production services, deletes old images, clears source cursors or resumes held
sources. Keep existing application data and routes until isolated validation is
complete.

`feishu_v2_gateway_probe.py` sends one synthetic embedding request only with
`--execute`, to the fixed authorized gateway/model; its default is offline.
`feishu_v2_remote_gateway_probe.py` runs the same request in a one-shot verified
container without application mounts. `--network-only` sends credential-free
HEAD probes with TLS verification and no redirects. Neither probe attests real
document ingestion, OAuth, search quality or provider quota availability.

Example invocation (the DPAPI path and SHA are supplied by the operator):

```powershell
./scripts/deploy/Invoke-FeishuV2Host.ps1 `
  -CredentialPath <existing-ssh-dpapi-file> `
  -Script feishu_v2_prepare_build.py `
  -ScriptArguments @('--source-root', <worktree>, '--sha', <full-sha>)
```

Keep local manifests outside the repository. PowerShell `Tee-Object` can produce
UTF-16 JSON; manifest readers handle UTF-16 BOM and UTF-8. Never pass passwords,
application secrets, access tokens or model keys in process arguments.

For an existing isolated site, use `feishu_v2_upgrade_site.py` with the original
site manifest and the new candidate manifest. `prepare` verifies all five exact
SHA gates and image provenance; `apply` stops both workers, takes a private
backup and restores into a new database before changing images. `finish` checks
the migrated application and starts both workers and the restricted entry.
`rollback` restores the retained Compose/environment files without deleting
business data. This path preserves test users, OAuth connections, Redis, files,
encryption keys and cursors. Keep `preparation.json` as the original project
identity; the private upgrade state records the new deployed source.

The build helper can reuse an explicitly supplied previous successful isolated
builder's cache with `--reuse-cache-manifest` and `--reuse-cache-attempt`.
It checks the source manifest, stopped container, successful exit, pinned builder
image, compile-only label, owned volume and absence of running cache consumers.
Go module checksums and current native-library/source fingerprints are still
verified; this never mounts application storage or application credentials.

After an upgrade, startup and synthetic privacy probes accept the original
`--manifest` plus `--candidate-manifest`. `--retry-stalled-synthetic` reparses only
the helper's own stored canary once for that candidate; it never resets a real
Feishu resource. The privacy probe exercises real gateway vectors, BM25 and
hybrid retrieval, plus short-lived synthetic personal MCP credentials. It does
not attest real Feishu OAuth or business documents.
