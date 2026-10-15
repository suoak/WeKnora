"""Create isolated candidate build; never changes the default Compose project."""
import hashlib
import shutil
import tarfile
import os

os.umask(0o077)
target = pathlib.Path("/home/aqyfzx/ai/knowhub-sourceid-phaseb-preflight-431226c3b")
old = pathlib.Path("/home/aqyfzx/ai/knowhub-sourceid-token-build-2fcbcaaed")
manifest = json.loads((target / "source-manifest.json").read_text())
assert manifest["sha"] == "431226c3b1bd5338ec06c1bd0520e637d5fed398"
assert hashlib.sha256((target / "source.tar.gz").read_bytes()).hexdigest() == manifest["archive_sha256"]
source = target / "source"
source.mkdir(mode=0o700, exist_ok=False)
with tarfile.open(str(target / "source.tar.gz")) as archive:
    for member in archive.getmembers():
        (source / member.name).resolve().relative_to(source.resolve())
        if member.issym() or member.islnk():
            (source / member.name).parent.joinpath(member.linkname).resolve().relative_to(source.resolve())
    archive.extractall(str(source))
for rel in ("third_party/anydoc-go/lib", "third_party/duckdb"):
    origin = old / "source" / rel
    if origin.exists():
        shutil.copytree(str(origin), str(source / rel), dirs_exist_ok=True)
(target / "artifacts").mkdir(mode=0o700)
for name in ("Dockerfile.app", "Dockerfile.frontend", "build-app.sh", "build-frontend.sh"):
    text = (old / name).read_text().replace("2fcbcaaedfa75fb955bd44dd7fe464ded64eec7c", manifest["sha"]).replace("2fcbcaaed", "431226c3b")
    (target / name).write_text(text)
(target / ".dockerignore").write_text("private\nsource.tar.gz\ncache\ntools\ntoolchain\nartifacts/*.log\nsource/frontend/node_modules\nsource/frontend/dist\nsource/WeKnora\n")
free = int(next(x.split()[1] for x in pathlib.Path("/proc/meminfo").read_text().splitlines() if x.startswith("MemAvailable:"))) * 1024
assert free >= 12 * 1024**3
worker = (old / "build-worker.py").read_text().replace(str(old), str(target)).replace("2fcbcaaed", "431226c3b")
worker = worker.replace("knowhub-sourceid-business/", "knowhub-sourceid-phaseb/")
builder_id = run(["docker", "image", "inspect", "--format", "{{.Id}}", "knowhub-build/golang:1.26-bookworm-ustc"]).decode().strip()
worker = worker.replace("'knowhub-build/golang:1.26-bookworm-ustc'", repr(builder_id))
worker = worker.replace("['docker','run','--rm'", "['docker','run','--cpus','2','--memory','8g','--pids-limit','1024','--rm'")
worker = worker.replace("['docker','build','--network'", "['docker','build','--pull=false','--network'")
(target / "build-worker.py").write_text(worker)
with (target / "worker.log").open("wb") as log:
    process = subprocess.Popen(["python3", str(target / "build-worker.py")], cwd=str(target), stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
print(json.dumps({"build_started": True, "pid": process.pid, "directory": str(target), "source_sha": manifest["sha"],
                  "archive_sha256": manifest["archive_sha256"], "builder_image_id": builder_id,
                  "compiler_baseline": "go1.26.7", "runtime_baseline": "sourceid-8ae91bc44", "candidate_image_namespace": "knowhub-sourceid-phaseb",
                  "build_limits": {"cpus": 2, "memory_bytes": 8 * 1024**3}, "default_services_modified": False}))
