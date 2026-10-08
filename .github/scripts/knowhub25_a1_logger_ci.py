"""Isolated same-runner comparison. Never pushes, migrates, deploys or publishes."""
import hashlib
import json
import os
import platform
import subprocess
import sys
import time
from pathlib import Path

BASELINE = "1cbde4617aceab31b8928feb705599e2d65c7158"
UPSTREAM = "6057a81017375a8d680cd32afca1c2ff03b0b17c"
PATCH_SHA256 = "3fde5b86121f58ff70f69453ab38f17c2caedcb8906a9daf3f9802717b0f379d"
PATCH_ID = "1a4963e4d73418a762e549f477cc14f38ee98a47"
SOURCE_BLOBS = {
    "internal/middleware/logger.go": "a65ef0f98d55e9a3fb0fd6b316d7a85c1bbb6014",
    "internal/middleware/logger_test.go": "79fe6d814e3c2190ee0f2b173c70cf5391cdb7aa",
}
RECIPE = {
    ".github/workflows/knowhub25-a1-logger-linux.yml",
    ".github/scripts/knowhub25_a1_logger_ci.py",
    ".github/scripts/knowhub25_a1_logger_ci_test.py",
}
LOGGER_TESTS = {
    "TestReadRequestBodyKeepsShortBodyIntact",
    "TestReadRequestBodyStaysInsideItsLogBudget",
    "TestReadRequestBodyLeavesRouteBodyCapDeciding",
    "TestReadRequestBodySkipsNonTextBodies",
}
PACKAGE = "github.com/Tencent/WeKnora/internal/middleware"


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def git(root, *args):
    return subprocess.check_output(["git", *args], cwd=root)


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def write_json(path, value):
    path.write_text(json.dumps(value, indent=2) + "\n", encoding="utf-8")


def classify(compile_code, test_code, events, candidate=False):
    runs = {e["Test"] for e in events if e.get("Package") == PACKAGE and e.get("Action") == "run" and "Test" in e}
    outcomes = {action: {e["Test"] for e in events if e.get("Package") == PACKAGE and e.get("Action") == action and "Test" in e}
                for action in ("pass", "fail", "skip")}
    package_pass = any(e.get("Package") == PACKAGE and e.get("Action") == "pass" and "Test" not in e for e in events)
    passed = (compile_code == 0 and test_code == 0 and package_pass and bool(runs) and bool(outcomes["pass"])
              and not outcomes["fail"] and runs == set.union(*outcomes.values())
              and (not candidate or LOGGER_TESTS <= outcomes["pass"]))
    if compile_code != 0:
        status = "BUILD_FAILED_TESTS_NOT_EXECUTED"
    elif not runs:
        status = "NO_TEST_EXECUTION_EVIDENCE"
    else:
        status = "PASS" if passed else "TEST_OR_EXECUTION_FAILURE"
    return {"status": status, "compileExit": compile_code, "testExit": test_code,
            "executedIncludingSubtests": len(runs), "executedTopLevel": sum("/" not in name for name in runs),
            "pass": len(outcomes["pass"]), "fail": len(outcomes["fail"]), "skip": len(outcomes["skip"]),
            "loggerRegressionPass": sorted(LOGGER_TESTS & outcomes["pass"]), "passedTests": sorted(outcomes["pass"])}


def execute(command, cwd, destination, commands, stderr=None):
    started = time.monotonic()
    with destination.open("wb") as output:
        if stderr:
            with stderr.open("wb") as errors:
                result = subprocess.run(command, cwd=cwd, stdout=output, stderr=errors)
        else:
            result = subprocess.run(command, cwd=cwd, stdout=output, stderr=subprocess.STDOUT)
    commands.append({"command": command, "cwd": str(cwd), "log": str(destination),
                     "exit": result.returncode, "elapsedSeconds": time.monotonic() - started})
    return result.returncode


def comparison_gate(results, verify_code):
    valid = {label: result["status"] == "PASS" and result["sourceUnchanged"] and result["moduleListExit"] == 0
             for label, result in results.items()}
    both_pass = valid["baseline"] and valid["candidate"] and verify_code == 0
    classification = ("BOTH_PASSED" if both_pass else "BASELINE_OR_ENVIRONMENT_FAILURE"
                      if not valid["baseline"] or verify_code != 0 else "CANDIDATE_OR_INTEGRITY_FAILURE")
    return both_pass, classification


def runner_agent(evidence, commands):
    # Read only executable paths, never process arguments/environ containing tokens.
    pid = os.getppid()
    for _ in range(12):
        try:
            executable = Path(os.readlink(f"/proc/{pid}/exe"))
            if executable.name in ("Runner.Worker", "Runner.Listener"):
                listener = executable.with_name("Runner.Listener")
                if listener.is_file():
                    code = execute([str(listener), "--version"], evidence, evidence / "runner-agent-version.txt", commands)
                    if code == 0:
                        return {"version": (evidence / "runner-agent-version.txt").read_text().strip(), "path": str(listener)}
            status = Path(f"/proc/{pid}/status").read_text()
            pid = int(next(line.split()[1] for line in status.splitlines() if line.startswith("PPid:")))
            if pid == 0:
                break
        except (OSError, StopIteration, ValueError):
            break
    return {"version": "UNKNOWN", "source": "Retrieve actual version from GitHub Actions Set up job log"}


def compare(workspace, evidence, report):
    baseline, candidate = workspace / "baseline", workspace / "candidate"
    candidate_sha = os.environ["CANDIDATE_SHA"]
    require(os.environ["BASELINE_SHA"] == BASELINE and os.environ["UPSTREAM_SHA"] == UPSTREAM, "Unexpected baseline/upstream")
    require(platform.system() == "Linux" and platform.machine() == "x86_64", "Linux x86_64 runner required")
    for root, expected in ((baseline, BASELINE), (candidate, candidate_sha)):
        require(git(root, "rev-parse", "HEAD").decode().strip() == expected, "Checkout SHA mismatch")
        require(not git(root, "status", "--porcelain"), "Checkout not clean")
    changed = set(git(candidate, "diff", "--name-only", BASELINE, "HEAD").decode().splitlines())
    require(changed == set(SOURCE_BLOBS) | RECIPE, "Unapproved candidate file changes")
    patch = git(candidate, "diff", "--no-ext-diff", "--no-textconv", "--no-renames", "--full-index", "--binary",
                "--src-prefix=a/", "--dst-prefix=b/", BASELINE, "HEAD", "--", *SOURCE_BLOBS)
    require(hashlib.sha256(patch).hexdigest() == PATCH_SHA256, "Integration patch mismatch")
    (evidence / "candidate-logger.patch").write_bytes(patch)
    subprocess.run(["git", "fetch", "--no-tags", "--depth=2", "https://github.com/Tencent/WeKnora.git", UPSTREAM], cwd=candidate, check=True)
    upstream_patch = git(candidate, "show", "--format=", "--binary", UPSTREAM)
    (evidence / "upstream-logger.patch").write_bytes(upstream_patch)
    patch_ids = [subprocess.check_output(["git", "patch-id", "--stable"], input=p).decode().split()[0]
                 for p in (patch, upstream_patch)]
    require(patch_ids == [PATCH_ID, PATCH_ID], "Upstream patch equivalence mismatch")
    for name, blob in SOURCE_BLOBS.items():
        require(git(candidate, "rev-parse", "HEAD:" + name).decode().strip() == blob, "Candidate source blob mismatch")
        require(git(candidate, "rev-parse", UPSTREAM + ":" + name).decode().strip() == blob, "Upstream source blob mismatch")
    report["identity"] = {"baseline": BASELINE, "candidate": candidate_sha, "upstream": UPSTREAM,
                          "patchSha256": PATCH_SHA256, "patchId": PATCH_ID, "sourceBlobs": SOURCE_BLOBS,
                          "changedFiles": sorted(changed)}
    commands = report["commands"]
    for name, command in (("go-version", ["go", "version"]), ("gcc-version", ["gcc", "--version"]),
                          ("gxx-version", ["g++", "--version"]), ("uname", ["uname", "-a"])):
        require(execute(command, baseline, evidence / (name + ".txt"), commands) == 0, name + " failed")
    fields = ["GOVERSION", "GOOS", "GOARCH", "CGO_ENABLED", "GOTOOLCHAIN", "CC", "CXX", "CGO_CFLAGS",
              "CGO_CPPFLAGS", "CGO_CXXFLAGS", "CGO_LDFLAGS", "GOMODCACHE", "GOCACHE", "GOPROXY", "GOSUMDB", "GOFLAGS"]
    require(execute(["go", "env", "-json", *fields], baseline, evidence / "go-environment.json", commands) == 0, "go env failed")
    go_env = json.loads((evidence / "go-environment.json").read_text())
    for field, expected in {"GOVERSION": "go1.26.7", "GOOS": "linux", "GOARCH": "amd64", "CGO_ENABLED": "1", "GOTOOLCHAIN": "local", "GOFLAGS": ""}.items():
        require(go_env[field] == expected, "Unexpected " + field)
    require(os.environ["GO_VERSION"] == "1.26.7", "Unexpected setup-go version")
    report["environment"] = {"go": go_env, "platform": platform.platform(),
                             "runnerAgent": runner_agent(evidence, commands),
                             "runner": {key: os.environ.get(key) for key in ("RUNNER_OS", "RUNNER_ARCH", "ImageOS", "ImageVersion", "GITHUB_RUN_ID", "GITHUB_RUN_ATTEMPT")}}
    report["results"] = {}
    for label, root in (("baseline", baseline), ("candidate", candidate)):
        folder = evidence / label
        folder.mkdir()
        locked = {name: sha(root / name) for name in ("go.mod", "go.sum")}
        (folder / "source-sha.txt").write_text(git(root, "rev-parse", "HEAD").decode(), encoding="utf-8")
        compile_code = execute(["go", "test", "-mod=readonly", "-p", "2", "-c", "-x", "-o", str(folder / "middleware.test"), "./internal/middleware"], root, folder / "compile.log", commands)
        events, test_code = [], None
        if compile_code == 0:
            execute(["file", str(folder / "middleware.test")], root, folder / "elf.txt", commands)
            execute(["ldd", str(folder / "middleware.test")], root, folder / "linkage.txt", commands)
            test_code = execute(["go", "test", "-mod=readonly", "-p", "2", "-count=1", "-json", "./internal/middleware"], root, folder / "test.jsonl", commands, folder / "test.stderr.log")
            for line in (folder / "test.jsonl").read_text(encoding="utf-8").splitlines():
                try:
                    events.append(json.loads(line))
                except json.JSONDecodeError:
                    continue
        result = classify(compile_code, test_code, events, candidate=label == "candidate")
        result["moduleIdentity"] = locked
        result["moduleListExit"] = execute(["go", "list", "-mod=readonly", "-m", "-json", "all"], root, folder / "modules.json", commands)
        result["sourceUnchanged"] = not git(root, "status", "--porcelain") and locked == {name: sha(root / name) for name in locked}
        report["results"][label] = result
        write_json(folder / "result.json", result)
        write_json(evidence / "comparison.json", report)
    verify_code = execute(["go", "mod", "verify"], baseline, evidence / "module-cache-verification.txt", commands)
    report["moduleCacheVerifyExit"] = verify_code
    results = report["results"]
    require(results["baseline"]["moduleIdentity"] == results["candidate"]["moduleIdentity"], "Dependency version drift")
    both_pass, classification = comparison_gate(results, verify_code)
    report["gate"] = "A1.1_LINUX_CI_PASS" if both_pass else "BLOCKED"
    report["classification"] = classification
    return 0 if both_pass else 1


def main():
    workspace = Path(os.environ["GITHUB_WORKSPACE"])
    evidence = workspace / "evidence"
    evidence.mkdir(exist_ok=True)
    report = {"gate": "BLOCKED", "commands": [], "scope": "A1.1 Linux CI only; not A1/KnowHub25 release or offline build approval"}
    try:
        code = compare(workspace, evidence, report)
    except Exception as error:
        report["error"] = str(error)
        code = 1
    write_json(evidence / "comparison.json", report)
    summary = json.dumps(report, indent=2)
    print(summary)
    if os.environ.get("GITHUB_STEP_SUMMARY"):
        with Path(os.environ["GITHUB_STEP_SUMMARY"]).open("a", encoding="utf-8") as output:
            output.write("## A1.1 comparison\n\n```json\n" + summary + "\n```\n")
    return code


if __name__ == "__main__":
    sys.exit(main())
