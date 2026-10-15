base = pathlib.Path("/home/aqyfzx/ai/knowhub-sourceid-token-build-2fcbcaaed")
out = {}
for name in ("build-app.sh", "build-frontend.sh", "build-worker.py"):
    text = (base / name).read_text()
    assert not re.search(r"(?i)(client_secret|authorization:|password\s*=|https?://[^\s/]+:[^\s/]+@)", text)
    out[name] = text
print(json.dumps(out))
