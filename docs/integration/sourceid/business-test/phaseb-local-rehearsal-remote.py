"""Backup current test DB; migrate/start/rollback only an isolated clone.

Uses shared safe Docker inventory primitives. No real SID credentials loaded.
"""
import hashlib
import os
import shutil
import socket
import time
import urllib.error
import yaml

BASE = pathlib.Path("/home/aqyfzx/ai/knowhub-sourceid-phaseb-preflight-431226c3b")
LIVE = pathlib.Path("/home/aqyfzx/ai/knowhub-sourceid-business-test-db1b2cd05")
SHA = "431226c3b1bd5338ec06c1bd0520e637d5fed398"
PREFIX = "sourceid-phaseb-rehearsal-431226c3b"
NETWORK = PREFIX + "-net"
names = {key: PREFIX + "-" + key for key in ("postgres", "redis", "app", "frontend", "oldapp")}
opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))


def execute(args, input=None, timeout=180, output=None):
    p = subprocess.run(args, input=input, stdout=output if output is not None else subprocess.PIPE,
                       stderr=subprocess.PIPE, timeout=timeout)
    if p.returncode:
        error = p.stderr.decode("utf-8", "replace").lower()
        category = "unknown"
        for fragment, label in (("does not exist", "missing_object"), ("permission denied", "permission_denied"),
                                ("could not translate host", "dns"), ("connection refused", "connection_refused"),
                                ("executable file not found", "missing_executable"), ("no such file", "missing_file")):
            if fragment in error:
                category = label
                break
        raise RuntimeError("isolated_command_failed_" + category)
    return p.stdout


def sql(query):
    return execute(["docker", "exec", "-i", names["postgres"], "psql", "-U", "sourceid_rehearsal", "-d", "knowhub_clone",
                    "-v", "ON_ERROR_STOP=1", "-At"], input=query.encode()).decode().strip()


def wait_pg():
    for _ in range(45):
        # Official postgres init temporarily opens only a Unix socket, then
        # restarts. Wait for final TCP readiness, not that transient server.
        p = subprocess.run(["docker", "exec", names["postgres"], "pg_isready", "-h", "127.0.0.1", "-U", "sourceid_rehearsal", "-d", "knowhub_clone"],
                           stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        if p.returncode == 0:
            return
        time.sleep(2)
    raise RuntimeError("clone_pg_timeout")


def request(port, path, method="GET", payload=None, token=None):
    headers = {"Content-Type": "application/json"}
    if token:
        headers["Authorization"] = "Bearer " + token
    target, target_port = {18197: (names["app"], 8080), 18196: (names["frontend"], 80), 18198: (names["oldapp"], 8080)}[port]
    networks = inspect(target, ".NetworkSettings.Networks")
    assert set(networks) == {NETWORK}
    address = networks[NETWORK]["IPAddress"]
    q = urllib.request.Request("http://" + address + ":" + str(target_port) + path, method=method,
                               data=json.dumps(payload).encode() if payload is not None else None, headers=headers)
    try:
        r = opener.open(q, timeout=12)
    except urllib.error.HTTPError as error:
        r = error
    with r:
        body = r.read(1048576)
        try:
            data = json.loads(body)
        except ValueError:
            data = {}
        return r.status, data


def wait_http(port):
    deadline = time.monotonic() + 90
    while time.monotonic() < deadline:
        try:
            if request(port, "/health")[0] == 200:
                return
        except Exception:
            pass
        time.sleep(2)
    raise RuntimeError("clone_app_timeout")


def start_app(name, image, port, envfile):
    execute(["docker", "run", "-d", "--name", name, "--network", NETWORK, "--cpus", "2", "--memory", "4g",
             "--log-driver", "none", "--env-file", str(envfile), image])
    wait_http(port)


def validate_business(port, expected_commit):
    result = {}
    status, data = request(port, "/api/v1/auth/oidc/config")
    assert status == 200 and data.get("enabled") is False and data.get("sourceid_basic_encoding") == "oauth_form"
    result["oidc_config"] = {"status": status, "enabled": False, "sourceid_basic_encoding": "oauth_form"}
    token = None
    try:
        status, data = request(port, "/api/v1/auth/login", "POST", LOCAL_FIXTURE)
        assert status == 200 and data.get("success") is True
        token = data["token"]
        result["local_login"] = status
        for path in ("/api/v1/auth/me", "/api/v1/knowledge-bases", "/api/v1/agents", "/api/v1/mcp-endpoints", "/api/v1/me/feishu"):
            status, data = request(port, path, token=token)
            assert status == 200
            result[path] = status
        status, data = request(port, "/api/v1/system/info", token=token)
        result["system_info_status"] = status
        if status == 200:
            info = data.get("data", data)
            commit = info.get("commit_id", "NOT_CAPTURED")
            assert commit in (expected_commit, expected_commit[:9])
            result["binary_commit_id"] = commit
        else:
            raise RuntimeError("binary_version_not_verified")
    finally:
        if token:
            result["local_logout"] = request(port, "/api/v1/auth/logout", "POST", token=token)[0]
        token = None
    return result


def clone_counts():
    return {table: int(sql("SELECT COUNT(*) FROM " + table)) for table in
            ("users", "tenants", "tenant_members", "knowledge_bases", "feishu_connections", "feishu_personal_imports")}


def main():
    os.umask(0o077)
    result = {"target_sha": SHA, "live_version_changed": False, "live_database_migrated": False,
              "real_sid_login": "NOT_EXECUTED", "result": "FAILED"}
    created = []
    net_created = False
    artifacts = BASE / "artifacts"
    private = BASE / "private-retry7"
    private.mkdir(mode=0o700, exist_ok=False)
    try:
        state = json.loads((BASE / "build-status.json").read_text())
        assert state["state"] == "COMPLETE"
        current_id = run(["docker", "ps", "--filter", "publish=8080", "--format", "{{.ID}}"] ).decode().strip()
        before = metadata(current_id)
        assert before["working_dir"] == str(LIVE)
        assert before["image_id"] == "sha256:93f5c831b6e061832d71aa19fe74835c78e16b637d77e7ce18a4283e9ed62daa"
        before_ids = run(["docker", "ps", "-a", "--filter", "label=com.docker.compose.project=" + before["project"], "--format", "{{.ID}}"] ).decode().split()
        before_all = [metadata(x) for x in before_ids]
        live_pg = next(x for x in before_all if x["service"] == "postgres")
        live_schema = run(["docker", "exec", live_pg["id"], "sh", "-c", 'exec psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -At -c "SELECT version,dirty FROM schema_migrations"']).decode().strip()
        assert live_schema == "3026|f"
        result["live_schema_before"] = live_schema
        for port in (18196, 18197, 18198):
            s = socket.socket()
            try:
                s.bind(("127.0.0.1", port))
            finally:
                s.close()
        backup = private / "backups"
        backup.mkdir(mode=0o700)
        dump = backup / "test-db-3026.dump"
        with dump.open("wb") as file:
            execute(["docker", "exec", live_pg["id"], "sh", "-c", 'exec pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fc --no-owner --no-acl'], output=file)
        dump.chmod(0o600)
        result["database_backup"] = {"path": str(dump), "bytes": dump.stat().st_size, "mode": "0600"}
        configuration_files = [pathlib.Path(p) for p in before["compose_files"].split(",")]
        doc = yaml.safe_load(configuration_files[0].read_text())
        configuration_files += [LIVE / p for p in doc["services"]["app"]["env_file"]]
        files = []
        for index, path in enumerate(configuration_files):
            target = backup / (str(index) + "-" + path.name)
            shutil.copyfile(str(path), str(target))
            target.chmod(0o600)
            files.append({"source": str(path), "backup": str(target), "mode": "0600"})
        result["protected_configuration_backups"] = files
        images = {}
        for kind in ("app", "frontend"):
            ref = "knowhub-sourceid-phaseb/" + kind + ":431226c3b"
            iid = run(["docker", "image", "inspect", "--format", "{{.Id}}", ref]).decode().strip()
            labels = json.loads(run(["docker", "image", "inspect", "--format", "{{json .Config.Labels}}", iid]))
            assert labels["org.opencontainers.image.revision"] == SHA
            images[kind] = {"reference": ref, "id": iid, "revision": SHA}
        result["candidate_images"] = images
        execute(["docker", "network", "create", "--internal", NETWORK])
        net_created = True
        assert json.loads(run(["docker", "network", "inspect", "--format", "{{json .Internal}}", NETWORK])) is True
        execute(["docker", "run", "-d", "--name", names["postgres"], "--network", NETWORK, "--network-alias", "rehearsal-pg",
                 "--cpus", "2", "--memory", "3g", "--log-driver", "none", "--tmpfs", "/var/lib/postgresql/data:rw,size=2147483648",
                 "-e", "POSTGRES_HOST_AUTH_METHOD=trust", "-e", "POSTGRES_USER=sourceid_rehearsal", "-e", "POSTGRES_DB=knowhub_clone", live_pg["image_id"]])
        created.append(names["postgres"])
        wait_pg()
        with dump.open("rb") as file:
            p = subprocess.run(["docker", "exec", "-i", names["postgres"], "pg_restore", "-U", "sourceid_rehearsal", "-d", "knowhub_clone",
                                "--clean", "--if-exists", "--no-owner", "--no-acl", "--exit-on-error"], stdin=file, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=180)
            if p.returncode:
                failure = p.stderr.decode("utf-8", "replace").lower()
                result["restore_failure_markers"] = {word: word in failure for word in
                    ("role", "does not exist", "extension", "pg_search", "vector", "already exists", "access method", "permission", "connection", "relation", "schema", "collation")}
                # Object names only, never data values or raw SQL/error text.
                objects = re.findall(r'(?:role|extension|schema|access method) "([a-z_][a-z0-9_]{0,62})"', failure)
                result["restore_failure_objects"] = sorted(set(objects))
                raise RuntimeError("clone_restore_failed")
        assert sql("SELECT version,dirty FROM schema_migrations") == "3026|f"
        baseline = clone_counts()
        result["clone_restored_baseline_counts"] = baseline
        # Never exercise cloned SID/local credentials or carry background work.
        sql("TRUNCATE auth_tokens,oidc_transactions; UPDATE users SET is_active=FALSE WHERE email <> '" + LOCAL_FIXTURE["email"].replace("'", "''") + "';")
        redis_image = next(x["image_id"] for x in before_all if x["service"] == "redis")
        execute(["docker", "run", "-d", "--name", names["redis"], "--network", NETWORK, "--network-alias", "rehearsal-redis",
                 "--memory", "256m", "--log-driver", "none", redis_image, "redis-server", "--save", "", "--appendonly", "no"])
        created.append(names["redis"])
        migration_cmd = ["docker", "run", "--rm", "--network", NETWORK, "--log-driver", "none", "--entrypoint", "sh", images["app"]["id"],
                         "-c", "exec migrate -path /app/migrations/versioned -database postgres://sourceid_rehearsal@rehearsal-pg:5432/knowhub_clone?sslmode=disable "]
        execute(migration_cmd[:-1] + [migration_cmd[-1] + "up 1"])
        assert sql("SELECT version,dirty FROM schema_migrations") == "3027|f"
        assert clone_counts() == baseline
        result["clone_migration_3026_to_3027"] = "PASS"
        assert sql("SELECT to_regclass('public.feishu_connections') IS NOT NULL AND to_regclass('public.source_id_binding_reviews') IS NOT NULL") == "t"
        # Synthetic per-clone token/session fixture proves down revokes derived
        # tokens before deleting metadata. It is not a signed JWT/login test.
        fixture_sql = """INSERT INTO source_id_sessions(id,issuer,session_index_hash,user_id,revoked,expires_at,created_at)
          SELECT '00000000-0000-4000-8000-000000000027','https://synthetic.invalid','',id,FALSE,NOW()+INTERVAL '10 minutes',NOW() FROM users LIMIT 1;
          INSERT INTO source_id_local_sessions(id,binding_id,subject,policy_ref,admission_policy_ref,admission_valid_until,last_seen_at,idle_seconds,access_seconds,refresh_enabled,admission_source,admission_checked_at,recheck_supported)
          VALUES ('00000000-0000-4000-8000-000000000027','synthetic-binding','synthetic-sub','synthetic-policy','synthetic-admission',NOW()+INTERVAL '10 minutes',NOW(),600,600,FALSE,'authorization',NOW(),FALSE);
          INSERT INTO auth_tokens(id,session_id,user_id,token,token_type,expires_at,is_revoked,created_at,updated_at)
          SELECT '00000000-0000-4000-8000-000000000028','00000000-0000-4000-8000-000000000027',user_id,'SYNTHETIC_CLONE_TOKEN_NOT_JWT','access_token',NOW()+INTERVAL '10 minutes',FALSE,NOW(),NOW() FROM source_id_sessions WHERE id='00000000-0000-4000-8000-000000000027';"""
        sql(fixture_sql)
        revoke_sql = "BEGIN; UPDATE auth_tokens SET is_revoked=TRUE WHERE session_id IN (SELECT id FROM source_id_local_sessions); UPDATE source_id_sessions SET revoked=TRUE WHERE id IN (SELECT id FROM source_id_local_sessions); COMMIT;"
        sql(revoke_sql)
        assert sql("SELECT is_revoked FROM auth_tokens WHERE id='00000000-0000-4000-8000-000000000028'") == "t"
        assert sql("SELECT revoked FROM source_id_sessions WHERE id='00000000-0000-4000-8000-000000000027'") == "t"
        sql("UPDATE auth_tokens SET is_revoked=FALSE; UPDATE source_id_sessions SET revoked=FALSE;")
        execute(migration_cmd[:-1] + [migration_cmd[-1] + "down 1"])
        assert sql("SELECT version,dirty FROM schema_migrations") == "3026|f"
        assert sql("SELECT is_revoked FROM auth_tokens WHERE id='00000000-0000-4000-8000-000000000028'") == "t"
        assert sql("SELECT revoked FROM source_id_sessions WHERE id='00000000-0000-4000-8000-000000000027'") == "t"
        assert clone_counts() == baseline
        result["explicit_session_revoke_on_clone"] = "PASS"
        result["clone_down_3027_revokes_tokens_sessions"] = "PASS"
        execute(migration_cmd[:-1] + [migration_cmd[-1] + "up 1"])
        assert sql("SELECT version,dirty FROM schema_migrations") == "3027|f"
        envfile = private / "rehearsal.env"
        env = {"DB_DRIVER": "postgres", "DB_HOST": "rehearsal-pg", "DB_PORT": "5432", "DB_USER": "sourceid_rehearsal", "DB_PASSWORD": "SyntheticCloneOnly", "DB_NAME": "knowhub_clone",
               "REDIS_ADDR": "rehearsal-redis:6379", "RETRIEVE_DRIVER": "postgres", "AUTO_MIGRATE": "false", "AUTO_RECOVER_DIRTY": "false",
               "JWT_SECRET": "SyntheticCloneSigningKeyNeverUsedOutsideRehearsal", "OIDC_AUTH_ENABLE": "false", "SOURCEID_ENABLE": "true", "SOURCEID_BASIC_ENCODING": "oauth_form",
               "SOURCEID_PROTOCOL_ONLY": "true", "SOURCEID_CONTRACT_CONFIRMED": "false", "SOURCEID_AUTO_BINDING_ENABLE": "false", "SOURCEID_EMAIL_BINDING_ENABLE": "false",
               "SOURCEID_LOGOUT_ENABLE": "false", "SOURCEID_DIAGNOSTICS_ENABLE": "false", "SOURCEID_SESSION_MODE": "external", "NEO4J_ENABLE": "false",
               "LANGFUSE_ENABLED": "false", "LLM_DEBUG_LOG": "false", "GIN_MODE": "release", "LOG_LEVEL": "error", "LOG_PATH": "/tmp/rehearsal-logs",
               "WEKNORA_SANDBOX_DOCKER_ENABLED": "false", "WEKNORA_BOOTSTRAP_SYSTEM_ADMIN_EMAIL": "", "STORAGE_TYPE": "local", "LOCAL_STORAGE_BASE_DIR": "/data/files",
               "ASYNQ_WORKER_CONCURRENCY": "1"}
        envfile.write_text("\n".join(key + "=" + value for key, value in env.items()) + "\n")
        envfile.chmod(0o600)
        # Register names before startup so failed health checks still clean up.
        created.append(names["app"])
        start_app(names["app"], images["app"]["id"], 18197, envfile)
        result["candidate_app_acceptance"] = validate_business(18197, SHA)
        execute(["docker", "run", "-d", "--name", names["frontend"], "--network", NETWORK, "--log-driver", "none", "--memory", "256m",
                 "-e", "APP_HOST=" + names["app"], images["frontend"]["id"]])
        created.append(names["frontend"])
        for _ in range(30):
            try:
                if request(18196, "/")[0] == 200:
                    break
            except Exception:
                pass
            time.sleep(1)
        assert request(18196, "/")[0] == 200
        assert request(18196, "/api/v1/auth/oidc/config")[1].get("sourceid_basic_encoding") == "oauth_form"
        result["candidate_frontend_and_api_proxy"] = "PASS"
        # Old application rollback tested with additive 3027 still present.
        execute(["docker", "stop", names["frontend"], names["app"]])
        created.append(names["oldapp"])
        start_app(names["oldapp"], before["image_id"], 18198, envfile)
        result["old_app_with_3027_rollback_acceptance"] = validate_business(18198, "2fcbcaaedfa75fb955bd44dd7fe464ded64eec7c")
        execute(["docker", "stop", names["oldapp"]])
        execute(migration_cmd[:-1] + [migration_cmd[-1] + "down 1"])
        assert sql("SELECT version,dirty FROM schema_migrations") == "3026|f"
        assert clone_counts() == baseline
        result["clone_final_schema"] = "3026|f"
        result["core_row_counts_preserved"] = True
        after_ids = run(["docker", "ps", "-a", "--filter", "label=com.docker.compose.project=" + before["project"], "--format", "{{.ID}}"] ).decode().split()
        after_all = [metadata(x) for x in after_ids]
        assert sorted(before_all, key=lambda x: x["id"]) == sorted(after_all, key=lambda x: x["id"])
        result["default_containers_unchanged"] = True
        result["live_schema_after"] = run(["docker", "exec", live_pg["id"], "sh", "-c", 'exec psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -At -c "SELECT version,dirty FROM schema_migrations"']).decode().strip()
        assert result["live_schema_after"] == "3026|f"
        result["result"] = "PASS"
    except Exception as error:
        result["failure_category"] = type(error).__name__
        if isinstance(error, RuntimeError) and re.fullmatch(r"isolated_command_failed_[a-z_]+", str(error)):
            result["safe_failure_class"] = str(error)
        import traceback
        result["failure_lines"] = [frame.lineno for frame in traceback.extract_tb(error.__traceback__) if frame.filename == "<stdin>"]
    finally:
        cleanup = {}
        for name in reversed(created):
            p = subprocess.run(["docker", "rm", "-f", name], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            cleanup[name] = p.returncode == 0
        if net_created:
            p = subprocess.run(["docker", "network", "rm", NETWORK], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            cleanup[NETWORK] = p.returncode == 0
        result["isolated_cleanup"] = cleanup
        result["completed_utc"] = datetime.datetime.now(datetime.timezone.utc).isoformat()
        (BASE / "rehearsal-result.json").write_text(json.dumps(result, indent=2) + "\n")
        print(json.dumps(result))
    return 0 if result["result"] == "PASS" else 2


if __name__ == "__main__":
    raise SystemExit(main())
