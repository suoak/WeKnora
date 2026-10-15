import os

app = metadata(run(["docker", "ps", "--filter", "publish=8080", "--format", "{{.ID}}"] ).decode().strip())
assert app["working_dir"] == "/home/aqyfzx/ai/knowhub-sourceid-business-test-db1b2cd05"
files = [pathlib.Path(x) for x in app["compose_files"].split(",")]
assert len(files) == 2
pin = files[1]
directory = pin.parent
directory.relative_to(pathlib.Path(app["working_dir"]) / "private/basic-encoding-backups")
rollback = directory / "rollback.sh"
run(["sh", "-n", str(rollback)])
env = environment(app["id"])
assert env["SOURCEID_BASIC_ENCODING"] == "oauth_form"
assert env["AUTO_MIGRATE"] == "false"
assert env["SOURCEID_PROTOCOL_ONLY"] == "true" and env["SOURCEID_CONTRACT_CONFIRMED"] == "false"
assert env["SOURCEID_AUTO_BINDING_ENABLE"] == "false"
perms = {str(x): oct(x.stat().st_mode & 0o777) for x in (directory, directory / "compose.yml.before", rollback, pin, files[0])}
assert perms[str(directory)] == "0o700" and perms[str(rollback)] == "0o700"
assert all(perms[str(x)] == "0o600" for x in (directory / "compose.yml.before", pin, files[0]))
opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
checks = {}
for path in ("http://127.0.0.1/", "http://127.0.0.1:8080/health", "http://127.0.0.1/api/v1/auth/oidc/config", "http://127.0.0.1:8080/api/v1/auth/oidc/config"):
    with opener.open(path, timeout=10) as response:
        assert response.status == 200
        row = {"status": response.status}
        if path.endswith("/config"):
            body = json.load(response)
            assert body.get("sourceid_basic_encoding") == "oauth_form"
            row["sourceid_basic_encoding"] = body["sourceid_basic_encoding"]
        checks[path] = row
print(json.dumps({"final_app_id": app["id"], "image_id": app["image_id"], "health": app["health"],
                  "credential_equivalence": "EQUIVALENT=" + equivalent(env), "configuration": "oauth_form",
                  "rollback_script_syntax": "PASS", "rollback_executed": False, "protected_permissions": perms,
                  "http": checks, "utc": datetime.datetime.now(datetime.timezone.utc).isoformat()}))
