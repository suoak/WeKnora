"""Check required test events, not just the Go process exit code."""
import json
import sys
from pathlib import Path

required = {
    "TestFeishuV2PostgresSharedAdmissionRequiresRevisionApproval",
    "TestFeishuV2PostgresBatchConcurrentIdempotencyAndAliasRecovery",
    "TestFeishuV2PostgresSourceReadAndCommitRevocation",
    "TestFeishuV2PostgresSourceHandoffIsAtomicAndFencesOldWorker",
}
failed = False
for filename in sys.argv[1:]:
    events = []
    for line in Path(filename).read_text(encoding="utf-8").splitlines():
        try:
            events.append(json.loads(line))
        except ValueError:
            continue
    passed = {e.get("Test") for e in events if e.get("Action") == "pass"}
    skipped = {e.get("Test") for e in events if e.get("Action") == "skip"}
    failures = [e for e in events if e.get("Action") == "fail"]
    missing = required - passed
    print(json.dumps({"file": filename, "passed_tests": len(passed),
                      "skipped_tests": sorted(t for t in skipped if t),
                      "required_not_passed": sorted(missing), "failure_events": len(failures)}))
    failed |= bool(missing or required & skipped or failures)
sys.exit(1 if failed or len(sys.argv) < 2 else 0)
