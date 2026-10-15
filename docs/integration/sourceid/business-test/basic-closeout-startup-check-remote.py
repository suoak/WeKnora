import datetime
import json
import pathlib
import re

# Shared read-only primitives are supplied by the SSH controller in memory.
ids = run(["docker", "ps", "--filter", "publish=8080", "--format", "{{.ID}}"] ).decode().split()
assert len(ids) == 1
cid = ids[0]
meta = metadata(cid)
assert meta["working_dir"] == "/home/aqyfzx/ai/knowhub-sourceid-business-test-db1b2cd05"
env = environment(cid)
import yaml
paths = [pathlib.Path(x) for x in meta["compose_files"].split(",")]
assert len(paths) == 1
data = yaml.safe_load(paths[0].read_text())
app = data["services"]["app"]
settings = app.get("environment", {})
assert isinstance(settings, dict)
mode = settings.get("SOURCEID_BASIC_ENCODING")
assert mode == "raw"
assert not {"OIDC_AUTH_CLIENT_ID", "OIDC_AUTH_CLIENT_SECRET"}.intersection(settings)
files = app["env_file"]
sources = {}
stats = []
for item in files:
    path = (paths[0].parent / item).resolve()
    path.relative_to(paths[0].parent.resolve())
    stats.append({"path": str(path), "mode": oct(path.stat().st_mode & 0o777),
                  "modified_utc": datetime.datetime.fromtimestamp(path.stat().st_mtime, datetime.timezone.utc).isoformat()})
    for line in path.read_text().splitlines():
        match = re.fullmatch(r"\s*(OIDC_AUTH_CLIENT_ID|OIDC_AUTH_CLIENT_SECRET)\s*=\s*(.*?)\s*", line)
        if not match:
            continue
        value = match[2]
        if value[:1] in ("'", '"'):
            assert len(value) >= 2 and value[-1] == value[0]
            value = value[1:-1]
        # Fail closed for syntax requiring interpolation/unescaping. No values
        # or character analysis are emitted; this subset matches safe fixtures.
        assert not any(c in value for c in ("$", "\\", "\n", "\r"))
        sources[match[1]] = value
source_matches = all(sources.get(k) == env.get(k) for k in ("OIDC_AUTH_CLIENT_ID", "OIDC_AUTH_CLIENT_SECRET"))
image_labels = json.loads(run(["docker", "image", "inspect", "--format", "{{json .Config.Labels}}", meta["image_id"]])) or {}
revision = image_labels.get("org.opencontainers.image.revision", "NOT_CAPTURED")
if not re.fullmatch(r"[a-f0-9]{7,40}", revision):
    revision = "NOT_CAPTURED"
print(json.dumps({"migration_startup_flags": {k: env.get(k, "NOT_SET") if env.get(k, "NOT_SET") in ("true", "false", "0", "1", "NOT_SET") else "SUPPRESSED" for k in ("AUTO_MIGRATE", "AUTO_RECOVER_DIRTY")},
                  "image_revision": revision, "basic_source": {"file": str(paths[0]), "location": "services.app.environment.SOURCEID_BASIC_ENCODING", "value": mode},
                  "credential_source_matches_running_container": source_matches,
                  "credential_equivalence": "EQUIVALENT=" + equivalent(sources),
                  "source_file_metadata": stats,
                  "compose_file_mode": oct(paths[0].stat().st_mode & 0o777)}))
