base = pathlib.Path("/home/aqyfzx/ai/knowhub-sourceid-phaseb-preflight-431226c3b")
data = json.loads((base / "build-status.json").read_text())
safe = {key: value for key, value in data.items() if key in {"state", "time_utc", "phase", "exit_code", "source", "compiler", "offline_dependency_baseline", "runtime_baseline"}}
logs = {}
for path in (base / "artifacts").glob("*.log"):
    text = path.read_text(errors="replace")
    logs[path.name] = {"bytes": path.stat().st_size, "test_fail": bool(re.search(r"(?m)^FAIL(?:\s|$)|^--- FAIL:", text)),
                       "go_version_observed": bool(re.search(r"go version go1\.26\.7", text))}
print(json.dumps({"status": safe, "logs": logs}))
