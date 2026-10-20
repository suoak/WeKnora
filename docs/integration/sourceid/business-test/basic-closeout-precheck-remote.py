"""Read-only packaging-host inventory. Stdout is an explicit safe JSON schema.

Container credentials exist only in this remote process; never emit a raw
inspect result, environment, log line, response body, or exception object.
"""
import base64
import datetime
import hmac
import json
import pathlib
import re
import subprocess
import urllib.parse
import urllib.request


def run(args, timeout=40):
    p = subprocess.run(args, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=timeout)
    if p.returncode:
        raise RuntimeError("command_failed")
    return p.stdout


def inspect(cid, field):
    return json.loads(run(["docker", "inspect", "--format", "{{json " + field + "}}", cid]))


def environment(cid):
    # Private IPC only. Never send this object or any derived fingerprints back.
    return dict(item.split("=", 1) for item in inspect(cid, ".Config.Env") if "=" in item)


def equivalent(env):
    whitespace = "\t\n\v\f\r \u0085\u00a0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a\u2028\u2029\u202f\u205f\u3000"
    client = env.get("OIDC_AUTH_CLIENT_ID", "").strip(whitespace)
    secret = env.get("OIDC_AUTH_CLIENT_SECRET", "").strip(whitespace)
    if client != "knowhub" or not secret:
        return "UNKNOWN"
    raw = b"Basic " + base64.b64encode((client + ":" + secret).encode("utf-8"))
    form = b"Basic " + base64.b64encode((urllib.parse.quote_plus(client, safe="") + ":" + urllib.parse.quote_plus(secret, safe="")).encode("ascii"))
    result = hmac.compare_digest(raw, form)
    raw = form = secret = None
    return "true" if result else "false"


def metadata(cid):
    labels = inspect(cid, ".Config.Labels") or {}
    state = inspect(cid, ".State")
    return {"id": inspect(cid, ".Id"), "name": inspect(cid, ".Name"),
            "image_id": inspect(cid, ".Image"), "image_reference": inspect(cid, ".Config.Image"),
            "created": inspect(cid, ".Created"), "started": state["StartedAt"],
            "running": state["Running"], "health": state.get("Health", {}).get("Status", "NOT_CONFIGURED"),
            "restart_count": inspect(cid, ".RestartCount"),
            "project": labels.get("com.docker.compose.project"), "service": labels.get("com.docker.compose.service"),
            "working_dir": labels.get("com.docker.compose.project.working_dir"),
            "compose_files": labels.get("com.docker.compose.project.config_files"),
            "ports": inspect(cid, ".NetworkSettings.Ports"),
            "mounts": [{k: v.get(k) for k in ("Type", "Name", "Source", "Destination", "RW")}
                       for v in inspect(cid, ".Mounts")]}


def safe_token_successes(cid):
    p = subprocess.run(["docker", "logs", "--tail", "5000", cid], stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=30)
    rows = []
    for line in (p.stdout + p.stderr).decode("utf-8", "replace").splitlines():
        start = line.find('{"event":')
        if start < 0:
            continue
        try:
            event = json.loads(line[start:])
        except ValueError:
            continue
        if event.get("event") != "sourceid_token_http":
            continue
        for item in event.get("http", []):
            if item.get("stage") != "token" or item.get("status") != 200:
                continue
            request = item.get("diagnostics", {}).get("request", {})
            utc = request.get("dispatch_utc", "")
            if not re.fullmatch(r"[0-9T:.+Z-]{10,40}", utc):
                utc = "NOT_CAPTURED"
            strategy = item.get("transport", {}).get("basic_strategy", "NOT_CAPTURED")
            if strategy not in ("raw_then_set_basic_auth_base64_once", "oauth_form_escape_then_set_basic_auth_base64_once"):
                strategy = "NOT_CAPTURED"
            elapsed = item.get("duration_ms")
            if not isinstance(elapsed, int) or not 0 <= elapsed <= 3600000:
                elapsed = None
            rows.append({"status": 200, "utc": utc, "duration_ms": elapsed, "basic_strategy": strategy})
    return rows[-10:]


def main():
    ids = run(["docker", "ps", "--filter", "publish=8080", "--format", "{{.ID}}"] ).decode().split()
    if len(ids) != 1:
        raise RuntimeError("app_port_not_unique")
    cid = ids[0]
    app = metadata(cid)
    if app["service"] != "app" or app["working_dir"] != "/home/aqyfzx/ai/knowhub-sourceid-business-test-db1b2cd05":
        raise RuntimeError("unexpected_project")
    env = environment(cid)
    flags = ["SOURCEID_BASIC_ENCODING", "SOURCEID_ENABLE", "SOURCEID_PROTOCOL_ONLY", "SOURCEID_CONTRACT_CONFIRMED",
             "SOURCEID_EMAIL_BINDING_ENABLE", "SOURCEID_EMAIL_CONTRACT_CONFIRMED", "SOURCEID_AUTO_BINDING_ENABLE",
             "SOURCEID_LOGOUT_ENABLE", "SOURCEID_SESSION_MODE", "SOURCEID_DIAGNOSTICS_ENABLE", "OIDC_AUTH_ENABLE", "OIDC_AUTH_PKCE_ENABLE"]
    safe_env = {key: env.get(key, "NOT_SET") for key in flags}
    for key, value in safe_env.items():
        if value not in {"NOT_SET", "true", "false", "raw", "oauth_form", "external", "local_approved", "1", "0"}:
            safe_env[key] = "UNEXPECTED_VALUE_SUPPRESSED"
    project_ids = run(["docker", "ps", "-a", "--filter", "label=com.docker.compose.project=" + app["project"], "--format", "{{.ID}}"] ).decode().split()
    project = [metadata(x) for x in project_ids]
    compose_paths = [pathlib.Path(x) for x in app["compose_files"].split(",")]
    sources = []
    try:
        import yaml
        for path in compose_paths:
            data = yaml.safe_load(path.read_text())
            service = data.get("services", {}).get("app", {})
            files = service.get("env_file", [])
            if isinstance(files, str):
                files = [files]
            sources.append({"path": str(path), "env_files": files,
                            "explicit_environment_keys": list(service.get("environment", {}).keys()) if isinstance(service.get("environment", {}), dict) else "LIST_FORMAT",
                            "image": service.get("image")})
    except ImportError:
        sources = [{"parser": "YAML_UNAVAILABLE"}]
    http = {}
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    for name, url in {"app_health": "http://127.0.0.1:8080/health", "frontend": "http://127.0.0.1/", "proxy_oidc_config": "http://127.0.0.1/api/v1/auth/oidc/config"}.items():
        try:
            with opener.open(url, timeout=10) as response:
                row = {"status": response.status}
                if name == "proxy_oidc_config":
                    body = json.load(response)
                    row.update({key: body.get(key) for key in ("success", "enabled", "sourceid_basic_encoding", "redirect_uri")})
                http[name] = row
        except Exception:
            http[name] = {"status": "FAILED"}
    print(json.dumps({"utc": datetime.datetime.now(datetime.timezone.utc).isoformat(), "app": app,
                      "project_containers": project, "flags": safe_env, "compose_sources": sources,
                      "credential_equivalence": "EQUIVALENT=" + equivalent(env),
                      "historical_successes_in_current_container": safe_token_successes(cid),
                      "http": http}))


if __name__ == "__main__":
    try:
        main()
    except Exception:
        print(json.dumps({"precheck": "FAILED", "credential_equivalence": "EQUIVALENT=UNKNOWN"}))
        raise SystemExit(2)
