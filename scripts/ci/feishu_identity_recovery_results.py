"""Require real isolated PG/Asynq events; report unavailable live OAuth separately."""
import json
import sys
from pathlib import Path

required = {
    "TestFeishuPersonalRecoveryPostgres",
    "TestFeishuPersonalRecoveryAsynqPostgres",
    "TestFeishuPersonalRecoveryAsynqPostgres/redis_restart",
    "TestFeishuPersonalRecoveryAsynqPostgres/app_process_exit",
    "TestFeishuPersonalRecoveryConcurrentPreparePostgres",
    "TestFeishuPersonalRecoveryCommitAuthorizationPostgres",
    "TestFeishuPersonalRecoveryGrantRefreshPostgres",
    "TestMVPFirstCreateIdempotencyPostgres",
    "TestMVPFirstCreateRedisRecoveryUsesOriginalKnowledge",
    "TestMVPPublicParsePGVectorBM25AndWikiHandoff",
    "TestApplicationGrantPGVectorBM25AndWikiHandoff",
    "TestApplicationGrantConcurrentSameResourceAndChangedRevision",
    "TestRC3LegacyFreezePostgresAsynq",
}
allowed_live_skips = {"TestApplicationGrantRealDocxFirstCreate"}
events = [json.loads(line) for line in Path(sys.argv[1]).read_text(encoding="utf-8").splitlines()
          if line.startswith("{")]
passed = {e["Test"] for e in events if e.get("Action") == "pass" and e.get("Test")}
skipped = {e["Test"] for e in events if e.get("Action") == "skip" and e.get("Test")}
failures = sum(e.get("Action") == "fail" for e in events)
print(json.dumps({"required_not_passed": sorted(required - passed), "skipped_tests": sorted(skipped),
                  "unexpected_skips": sorted(skipped - allowed_live_skips), "failure_events": failures}))
sys.exit(bool(required - passed or skipped - allowed_live_skips or failures))
