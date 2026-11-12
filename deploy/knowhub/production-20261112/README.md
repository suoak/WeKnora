# Production configuration snapshot — 2026-11-12

This directory records the effective, non-secret KnowHub configuration after the
MCP environment selector and help-menu release. It is an audit snapshot, not a
standalone Compose deployment. Do not apply it over an arbitrary or older base.

- APP source: `dc3f11db077a2eb6aef8abf1b8e3616d3f7a3377`.
- APP image: `sha256:01e12996b9b3ce22ba56455dd415b2955c25469e389783fe1331a6c9831fece1`.
- Frontend source: `4b748624eefa55cb46090ecb839d86915aa08b8f` (production
  frontend baseline plus the changes merged in PR #132).
- Frontend image: `sha256:a1b6f5c4613fed59668e4c3ce62fd723732e65dbd4eafe891adf1a21646b988e`.
- Database remains PostgreSQL schema **3046 / dirty=false**. This source
  reconciliation does **not** authorize deployment or migration to 3047.

`runtime-manifest.json` records the observed effective Compose file order,
container identities, configuration source paths and SHA256 values. The original
Compose base and overlays contain operational secrets and remain in the
root-controlled production backup. They must not be reconstructed from an old
repository Compose file. A future release must inspect current container labels,
back up all effective files and compare the rendered configuration before apply.

Preserve the following last-overlay order:

1. The current production Compose chain, including all business overlays.
2. `app.no-docker-socket.override.yml`: sandbox disabled and complete explicit
   APP volume list, with no Docker socket. Do not restore old socket-bearing
   APP configuration, including during a frontend rollback.
3. `frontend.tls8443.override.yml`: original 80 plus `10.51.134.114:8443`, readonly
   frozen HTTP80, HTTPS8443 configuration and certificate-directory mounts.
4. `frontend.page.override.json`: current frontend image only.

The TLS override still mounts an unused legacy source-ACL file. The live
`https8443.conf` does **not** include that file: all reachable sources must
authenticate with an existing personal MCP Key. No separate Nginx rate or
concurrency limits are configured, matching the approved internal-MCP policy.
The APP's existing shared eight request slots remain unchanged.

The precise public path is `/api/v1/mcp/personal`; its sole HTTPS8443 upstream is
`http://app:8080/internal/mcp/isolated/personal`. Other paths are rejected. Port
443 stays unpublished. Formal certificates and private keys are **not** in this
directory, Git, images or release artifacts. The key remains in the root-owned
production certificate directory, mounted readonly only by frontend.

Frontend rollback is the existing root-controlled
`/root/knowhub-mcp-page-help-20261112/rollback.sh`. It uses the saved effective
Compose chain and prior image, retaining HTTP80/HTTPS8443 and APP hardening.
The original image, complete effective Compose backup and script syntax were
verified; no additional production rollback drill was executed for PR #132.

The real isolated-zone TCP/NAT and DNS acceptance remains outstanding. Do not
represent the source snapshot or controlled internal tests as that acceptance.
