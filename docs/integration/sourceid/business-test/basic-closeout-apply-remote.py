"""One-key configuration transaction, using shared precheck primitives.

EXPECTED and LOCAL_FIXTURE are supplied over encrypted SSH stdin by the
controller, never placed in a command line or persisted on the host.
"""
import copy
import fcntl
import os
import signal
import time
import urllib.error
import yaml

BASE = pathlib.Path("/home/aqyfzx/ai/knowhub-sourceid-business-test-db1b2cd05")
COMPOSE = BASE / "compose.yml"
PROJECT = "knowhub-sourceid-business"
opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))


def http(path, method="GET", body=None, token=None):
    headers = {"Content-Type": "application/json"}
    if token:
        headers["Authorization"] = "Bearer " + token
    request = urllib.request.Request("http://127.0.0.1" + path, method=method,
                                     data=json.dumps(body).encode() if body is not None else None, headers=headers)
    try:
        response = opener.open(request, timeout=12)
    except urllib.error.HTTPError as error:
        response = error
    with response:
        raw = response.read(1048576)
        try:
            data = json.loads(raw)
        except ValueError:
            data = {}
        return response.status, data


def local_acceptance():
    token = None
    rows = {}
    try:
        status, body = http("/api/v1/auth/login", "POST", LOCAL_FIXTURE)
        assert status == 200 and body.get("success") is True and isinstance(body.get("token"), str)
        token = body["token"]
        rows["local_login"] = status
        for path in ("/api/v1/auth/me", "/api/v1/knowledge-bases", "/api/v1/agents", "/api/v1/models",
                     "/api/v1/mcp-endpoints", "/api/v1/mcp-endpoints/tools", "/api/v1/me/feishu"):
            status, data = http(path, token=token)
            rows[path] = status
            assert status == 200
        return rows
    finally:
        if token:
            rows["local_logout"] = http("/api/v1/auth/logout", "POST", token=token)[0]
            rows["local_token_revoked"] = http("/api/v1/auth/me", token=token)[0] == 401
        token = None


def schema_version(containers):
    postgres = [x for x in containers if x["service"] == "postgres"]
    assert len(postgres) == 1
    # Local socket authentication, no password argument or environment dump.
    value = run(["docker", "exec", postgres[0]["id"], "sh", "-c",
                 'exec psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -At -c "SELECT version, dirty FROM schema_migrations;"']).decode().strip()
    assert re.fullmatch(r"[0-9]+\|[tf]", value)
    return value


def project_snapshot():
    ids = run(["docker", "ps", "-a", "--filter", "label=com.docker.compose.project=" + PROJECT, "--format", "{{.ID}}"] ).decode().split()
    return [metadata(x) for x in ids]


def current_app():
    ids = run(["docker", "ps", "-a", "--filter", "label=com.docker.compose.project=" + PROJECT,
               "--filter", "label=com.docker.compose.service=app", "--format", "{{.ID}}"] ).decode().split()
    assert len(ids) == 1
    return metadata(ids[0])


def wait_healthy(image):
    for _ in range(60):
        try:
            app = current_app()
            if app["running"] and app["image_id"] == image and app["health"] == "healthy":
                with opener.open("http://127.0.0.1:8080/health", timeout=3) as response:
                    if response.status == 200:
                        return app
        except Exception:
            pass
        time.sleep(2)
    raise RuntimeError("health_timeout")


def atomic_write(path, data):
    pending = path.with_name(path.name + ".basic-closeout-pending")
    fd = os.open(str(pending), os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, "wb") as file:
        file.write(data)
        file.flush()
        os.fsync(file.fileno())
    os.replace(str(pending), str(path))


def main():
    os.umask(0o077)
    result = {"utc": datetime.datetime.now(datetime.timezone.utc).isoformat(), "target": "172.30.232.42",
              "switch_completed": False, "rolled_back": False, "real_sid_login": "NOT_EXECUTED",
              "real_sid_token_exchange": "NOT_EXECUTED"}
    changed = False
    backup = pin = before_bytes = before_env = before_app = None
    lock = (BASE / "private/basic-encoding-closeout.lock").open("a")
    fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
    try:
        before_app = current_app()
        assert before_app["id"] == EXPECTED["app"]["id"]
        assert before_app["image_id"] == EXPECTED["app"]["image_id"]
        assert before_app["image_reference"] == "knowhub-sourceid-business/app:2fcbcaaed"
        assert before_app["compose_files"] == str(COMPOSE)
        assert before_app["working_dir"] == str(BASE)
        before_env = environment(before_app["id"])
        assert before_env.get("SOURCEID_BASIC_ENCODING") == "raw"
        assert before_env.get("AUTO_MIGRATE") == "false" and before_env.get("AUTO_RECOVER_DIRTY") == "false"
        result["credential_equivalence"] = "EQUIVALENT=" + equivalent(before_env)
        assert result["credential_equivalence"] == "EQUIVALENT=true"
        successes = safe_token_successes(before_app["id"])
        assert successes
        result["same_container_historical_token_200_count"] = len(successes)
        before_project = project_snapshot()
        result["schema_before"] = schema_version(before_project)
        assert result["schema_before"] == "3026|f"
        result["baseline_local_acceptance"] = local_acceptance()
        assert result["baseline_local_acceptance"]["local_logout"] == 200
        assert result["baseline_local_acceptance"]["local_token_revoked"]

        before_bytes = COMPOSE.read_bytes()
        document = yaml.safe_load(before_bytes)
        assert document["services"]["app"]["environment"]["SOURCEID_BASIC_ENCODING"] == "raw"
        # No SID credential is copied into the backup/override or command line.
        assert not {"OIDC_AUTH_CLIENT_ID", "OIDC_AUTH_CLIENT_SECRET"}.intersection(document["services"]["app"]["environment"])
        sources = {}
        for name in document["services"]["app"]["env_file"]:
            path = (BASE / name).resolve()
            path.relative_to(BASE)
            for line in path.read_text().splitlines():
                match = re.fullmatch(r"\s*(OIDC_AUTH_CLIENT_ID|OIDC_AUTH_CLIENT_SECRET)\s*=\s*(.*?)\s*", line)
                if match:
                    value = match[2]
                    if value[:1] in ("'", '"'):
                        assert len(value) >= 2 and value[-1] == value[0]
                        value = value[1:-1]
                    assert not any(c in value for c in ("$", "\\", "\n", "\r"))
                    sources[match[1]] = value
        assert all(sources.get(k) == before_env.get(k) for k in ("OIDC_AUTH_CLIENT_ID", "OIDC_AUTH_CLIENT_SECRET"))
        result["source_credential_matches_running"] = True
        assert equivalent(sources) == "true"

        pattern = r"(?m)^([ \t]+SOURCEID_BASIC_ENCODING:[ \t]*)(['\"]?)raw\2([ \t]*(?:#[^\r\n]*)?)(\r?)$"
        after_text, count = re.subn(pattern, lambda m: m[1] + m[2] + "oauth_form" + m[2] + m[3] + m[4], before_bytes.decode("utf-8"))
        assert count == 1
        expected_document = copy.deepcopy(document)
        expected_document["services"]["app"]["environment"]["SOURCEID_BASIC_ENCODING"] = "oauth_form"
        assert yaml.safe_load(after_text) == expected_document

        backup_dir = BASE / "private/basic-encoding-backups" / datetime.datetime.now(datetime.timezone.utc).strftime("%Y%m%dT%H%M%SZ")
        backup_dir.mkdir(mode=0o700, parents=True, exist_ok=False)
        backup = backup_dir / "compose.yml.before"
        backup.write_bytes(before_bytes)
        backup.chmod(0o600)
        pin = backup_dir / "image-pin.yml"
        pin.write_text("services:\n  app:\n    image: " + before_app["image_id"] + "\n    pull_policy: never\n")
        pin.chmod(0o600)
        compose_args = ["docker", "compose", "--project-name", PROJECT, "--project-directory", str(BASE),
                        "-f", str(COMPOSE), "-f", str(pin), "up", "-d", "--no-deps", "--no-build", "--pull", "never", "--force-recreate", "app"]
        import shlex
        rollback = backup_dir / "rollback.sh"
        rollback.write_text("#!/bin/sh\nset -eu\numask 077\ninstall -m 600 " + shlex.quote(str(backup)) + " " + shlex.quote(str(COMPOSE)) + "\n" + shlex.join(compose_args) + " >/dev/null 2>&1\n")
        rollback.chmod(0o700)
        result["backup_directory"] = str(backup_dir)
        result["rollback_script"] = str(rollback)
        result["image_pin_override"] = str(pin)
        result["configuration_semantic_changed_keys"] = ["services.app.environment.SOURCEID_BASIC_ENCODING"]
        assert COMPOSE.read_bytes() == before_bytes
        atomic_write(COMPOSE, after_text.encode("utf-8"))
        changed = True
        run(compose_args, timeout=180)
        after_app = wait_healthy(before_app["image_id"])
        after_env = environment(after_app["id"])
        wanted_env = dict(before_env, SOURCEID_BASIC_ENCODING="oauth_form")
        assert after_env == wanted_env
        result["only_basic_encoding_environment_changed"] = True
        result["credential_equivalence_after"] = "EQUIVALENT=" + equivalent(after_env)
        assert result["credential_equivalence_after"] == "EQUIVALENT=true"
        result["app_before"] = before_app
        result["app_after"] = after_app
        result["fixed_image_id_unchanged"] = after_app["image_id"] == before_app["image_id"]
        after_project = project_snapshot()
        old_others = {x["service"]: x for x in before_project if x["service"] != "app"}
        new_others = {x["service"]: x for x in after_project if x["service"] != "app"}
        assert old_others == new_others
        result["non_app_containers_unchanged"] = True
        result["non_app_container_ids"] = {name: value["id"] for name, value in new_others.items()}
        assert sorted(before_app["mounts"], key=lambda x: x["Destination"]) == sorted(after_app["mounts"], key=lambda x: x["Destination"])
        assert before_app["ports"] == after_app["ports"]
        result["app_volumes_and_ports_unchanged"] = True
        result["schema_after"] = schema_version(after_project)
        assert result["schema_after"] == result["schema_before"]
        status, config = http("/api/v1/auth/oidc/config")
        assert status == 200 and config.get("sourceid_basic_encoding") == "oauth_form"
        assert config.get("redirect_uri") == EXPECTED["http"]["proxy_oidc_config"]["redirect_uri"]
        assert config.get("enabled") == EXPECTED["http"]["proxy_oidc_config"]["enabled"]
        result["oidc_config"] = {key: config.get(key) for key in ("success", "enabled", "sourceid_basic_encoding", "redirect_uri")}
        with opener.open("http://127.0.0.1/", timeout=10) as response:
            assert response.status == 200
            result["frontend_80_status"] = response.status
        result["app_8080_health_status"] = 200
        result["post_switch_local_acceptance"] = local_acceptance()
        assert result["post_switch_local_acceptance"]["local_logout"] == 200
        assert result["post_switch_local_acceptance"]["local_token_revoked"]
        result["preserved_flags"] = {key: after_env.get(key, "NOT_SET") for key in EXPECTED["flags"] if key != "SOURCEID_BASIC_ENCODING"}
        result["switch_completed"] = True
        result["rollback_ready"] = backup.is_file() and pin.is_file() and rollback.is_file()
    except BaseException:
        result["failure"] = "SAFE_PRECHECK_OR_ACCEPTANCE_FAILURE"
        if changed and backup and before_app:
            try:
                atomic_write(COMPOSE, backup.read_bytes())
                run(compose_args, timeout=180)
                restored = wait_healthy(before_app["image_id"])
                assert environment(restored["id"]) == before_env
                result["rolled_back"] = True
                result["restored_app_id"] = restored["id"]
            except BaseException:
                result["rollback_failure"] = "MANUAL_RECOVERY_REQUIRED"
    finally:
        fcntl.flock(lock, fcntl.LOCK_UN)
        lock.close()
        print(json.dumps(result))
    return 0 if result["switch_completed"] else 2


if __name__ == "__main__":
    raise SystemExit(main())
