"""Read-only inventory. No secret/environment dumps or configuration writes."""
import os
import yaml

cid = run(["docker", "ps", "--filter", "publish=8080", "--format", "{{.ID}}"] ).decode().strip()
app = metadata(cid)
assert app["working_dir"] == "/home/aqyfzx/ai/knowhub-sourceid-business-test-db1b2cd05"
project = app["project"]
ids = run(["docker", "ps", "-a", "--filter", "label=com.docker.compose.project=" + project, "--format", "{{.ID}}"] ).decode().split()
containers = [metadata(x) for x in ids]
keys = ["SOURCEID_BASIC_ENCODING", "SOURCEID_ENABLE", "SOURCEID_PROTOCOL_ONLY", "SOURCEID_CONTRACT_CONFIRMED",
        "SOURCEID_AUTO_BINDING_ENABLE", "SOURCEID_EMAIL_BINDING_ENABLE", "SOURCEID_EMAIL_CONTRACT_CONFIRMED",
        "SOURCEID_SESSION_MODE", "SOURCEID_LOGOUT_ENABLE", "SOURCEID_DIAGNOSTICS_ENABLE", "OIDC_AUTH_ENABLE",
        "OIDC_AUTH_PKCE_ENABLE", "AUTO_MIGRATE", "AUTO_RECOVER_DIRTY"]
flags = {}
for key in keys:
    value = run(["docker", "exec", cid, "sh", "-c", 'printf "%s" "${' + key + '-NOT_SET}"']).decode()
    flags[key] = value if value in {"raw", "oauth_form", "true", "false", "external", "local_approved", "NOT_SET"} else "SUPPRESSED"
postgres = next(x for x in containers if x["service"] == "postgres")
def sql(query):
    return run(["docker", "exec", postgres["id"], "sh", "-c",
                'exec psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -At -c "$1"', "sh", query]).decode().strip()
schema = sql("SELECT version,dirty FROM schema_migrations")
assert re.fullmatch(r"[0-9]+\|[tf]", schema)
db_bytes = int(sql("SELECT pg_database_size(current_database())"))
counts = {}
for table in ("users", "tenants", "tenant_members", "knowledge_bases", "auth_tokens", "oidc_transactions",
              "external_identities", "source_id_sessions", "source_id_binding_reviews", "feishu_connections", "feishu_personal_imports"):
    exists = sql("SELECT to_regclass('public." + table + "') IS NOT NULL") == "t"
    counts[table] = int(sql("SELECT COUNT(*) FROM " + table)) if exists else "NOT_PRESENT"
compose = []
for name in app["compose_files"].split(","):
    path = pathlib.Path(name)
    data = yaml.safe_load(path.read_text())
    values = data.get("services", {}).get("app", {})
    compose.append({"path": str(path), "permissions": oct(path.stat().st_mode & 0o777),
                    "app_image": values.get("image"),
                    "basic_encoding_override": values.get("environment", {}).get("SOURCEID_BASIC_ENCODING", "NOT_SET")})
images = []
for line in run(["docker", "image", "ls", "--no-trunc", "--format", "{{json .}}"] ).decode().splitlines():
    item = json.loads(line)
    if any(x in item.get("Repository", "") + ":" + item.get("Tag", "") for x in ("knowhub", "sourceid", "weknora", "golang", "node", "nginx")):
        images.append({k: item.get(k) for k in ("Repository", "Tag", "ID", "Size")})
directories = []
for path in pathlib.Path("/home/aqyfzx/ai").iterdir():
    if path.is_dir() and any(x in path.name for x in ("sourceid", "knowhub")):
        selected = [str(p.relative_to(path)) for p in path.glob("*Dockerfile*")]
        selected += [str(p.relative_to(path)) for p in path.glob("Dockerfile*") if str(p.relative_to(path)) not in selected]
        directories.append({"directory": str(path), "dockerfiles": selected})
disk = os.statvfs(app["working_dir"])
print(json.dumps({"utc": datetime.datetime.now(datetime.timezone.utc).isoformat(), "app": app,
                  "containers": containers, "flags": flags, "schema": schema,
                  "database_bytes": db_bytes, "row_counts": counts, "compose_files": compose,
                  "images": images, "build_directories": directories,
                  "free_bytes": disk.f_bavail * disk.f_frsize}))
