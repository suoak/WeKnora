"""Bounded Feishu regression using the unchanged continuous safety contracts."""
import json
from pathlib import Path
import sys
import knowhub25_continuous_ci as D

PACKAGES = ["internal/datasource/connector/feishu/" + name for name in ["core", "wiki", "drive"]]
WIKI_REQUIRED = [
    "TestFetchDocxWithBlocks_AttachmentDownloadFailure",
    "TestFetchDocxWithBlocks_ImageDownloadFailure",
    "TestFetchDocxWithBlocks_WhitelistedTinyAttachmentNotPromoted",
    "TestGolden_RichDocxAllCapabilities",
]

def check_workflow():
    raw = (D.QA.parents[1] / "workflows/knowhub25-feishu-log-safety.yml").read_text()
    branch = "integration/knowhub25-feishu-doc-log-safety-20261012"
    if raw.count(branch) != 2 or "contents: read" not in raw or "persist-credentials: true" in raw or "secrets." in raw:
        raise ValueError("WORKFLOW_FAILED")
    if "steps.review.outcome == 'success'" not in raw or "path: continuous-public/summary.json" not in raw:
        raise ValueError("UPLOAD_FAILED")

def run(runner):
    for package in PACKAGES:
        index = D.M["packages"].index(package) + 1
        required = set(D.M["required"].get(package, []))
        if runner.side == "CANDIDATE":
            required.update(D.M["candidate_required"].get(package, []))
            if package.endswith("/wiki"):
                required.update(WIKI_REQUIRED)
        cc, _, _ = runner.execute("GO", ["go", "test", "-mod=readonly", "-p", "2", "-c", "-o", str(runner.root / f"pkg{index}.test"), "./" + package], "GO_COMPILE_LOG")
        if cc == 0:
            command = ["go", "test", "-mod=readonly", "-p", "2", "-count=1", "-json"]
            if runner.mode == "RACE":
                command.append("-race")
            code, out, err = runner.execute("GO", command + ["./" + package], "GO_TEST_JSONL")
            parsed = D.C.parse(out.read_text(), cc, code, sorted(required), D.M["approved_skips"], err.read_text())
        else:
            parsed = D.C.parse("", cc, None, sorted(required))
        runner.record("GO", parsed, f"PKG_{index:03d}", cc)

if __name__ == "__main__":
    try:
        if sys.argv[1:] == ["--self-test"]:
            D.selftest()
            check_workflow()
            assert all(name in D.M["diagnostic_test_ids"] for name in WIKI_REQUIRED)
            print(json.dumps({"feishuWorkflow": "PASS", "packagesPerSideMode": 3, "requiredWikiCases": 4}))
        elif sys.argv[1:] == ["--review"]:
            D.validate_summary(json.loads(Path("continuous-public/summary.json").read_text()))
        else:
            check_workflow()
            if len(sys.argv) != 3 or sys.argv[2] not in {"NORMAL", "RACE"}:
                raise ValueError("HELPER_FAILED")
            runner = D.Runner(sys.argv[1], sys.argv[2])
            try:
                run(runner)
            except Exception:
                runner.failure = "HELPER_FAILED"
            sys.exit(runner.finish())
    except Exception:
        # Fail closed: no unchecked raw output or exception text becomes public.
        sys.exit(1)
