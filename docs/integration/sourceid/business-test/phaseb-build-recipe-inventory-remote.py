base = pathlib.Path("/home/aqyfzx/ai/knowhub-sourceid-token-build-2fcbcaaed")
recipes = {}
for name in ("Dockerfile.app", "Dockerfile.frontend", "build.sh"):
    path = base / name
    if path.exists():
        text = path.read_text()
        if re.search(r"(?i)(client_secret|authorization:|password\s*=|https?://[^\s/]+:[^\s/]+@)", text):
            recipes[name] = "SUPPRESSED"
        else:
            recipes[name] = text
files = [x.name for x in base.iterdir()]
status = {}
path = base / "build-status.json"
if path.exists():
    data = json.loads(path.read_text())
    for key, value in data.items():
        if key in {"sha", "status", "app_image", "frontend_image", "go_version", "node_version", "app_image_id", "frontend_image_id", "build_container", "build_container_id"}:
            status[key] = value
builder_containers = []
for line in run(["docker", "ps", "-a", "--format", "{{json .}}"] ).decode().splitlines():
    item = json.loads(line)
    if any(x in item.get("Names", "") for x in ("build", "builder")):
        builder_containers.append({key: item.get(key) for key in ("ID", "Image", "Names", "Status")})
print(json.dumps({"recipes": recipes, "files": files, "status": status, "builder_containers": builder_containers}))
