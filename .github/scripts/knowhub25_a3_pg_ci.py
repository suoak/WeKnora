"""Bounded PG foundation verification; raw output stays on the ephemeral runner."""
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile
import sys

PUBLIC_ROWS = []
SERVICE_EXPECTED = {
    "TestA3ProcessingFencePostgresAtomicPendingAdmission",
 'TestA3ProcessingAttemptCleanupPostgres',
 'TestA3ProcessingAttemptCleanupPostgres/actual_processChunks_preserves_new_generation',
 'TestA3ProcessingExecutionPostgresLifecycle',
 'TestA3ProcessingExecutionPostgresSupersession',
 'TestA3ProcessingExecutionPostgresParentCancellation',
 'TestA3ProcessingExecutionPostgresDurableCleanup',
}


def emit(value):
    """Publish fixed metadata only; retain no raw output or exception strings."""
    allowed = {'REPOSITORY', 'SERVICE', 'SERVICE_FULL', 'NORMAL', 'RACE', 'RUN',
               'PASS', 'FAIL', 'SKIP', 'BLOCKED', 'UNKNOWN', 'UNKNOWN_TEST_ID',
               'GO_TEST_JSONL', 'GO_TEST_STDERR', 'HELPER_FAILED'} | set(RULES) | EXPECTED | SERVICE_EXPECTED
    allowed |= {'TestA3ProcessingAttemptCleanupPostgres',
                'TestA3ProcessingAttemptCleanupPostgres/actual_processChunks_preserves_new_generation'}
    for source in Path('internal/application/service').glob('*_test.go'):
        allowed.update(re.findall(r'(?m)^func\s+(Test[A-Za-z0-9_]+)\(t \*testing\.T\)', source.read_text(encoding='utf-8')))
    fields = {'package', 'mode', 'exit', 'run', 'pass', 'fail', 'skip', 'tests',
              'failedTestIDs', 'parserValid', 'dataRace', 'scanClear', 'rawMetadata',
              'safeHitTests', 'status', 'role', 'sha256', 'rules', 'testID',
              'ruleID', 'count', 'classification', 'failureClass'} | set(RULES) | EXPECTED | SERVICE_EXPECTED
    fields |= {'TestA3ProcessingAttemptCleanupPostgres',
               'TestA3ProcessingAttemptCleanupPostgres/actual_processChunks_preserves_new_generation'}
    def inspect(item):
        if item is None or type(item) in (bool, int): return
        if isinstance(item, str):
            if item not in allowed and not re.fullmatch('[0-9a-f]{64}', item):
                raise ValueError('PUBLIC_SCHEMA_FAILED')
        elif isinstance(item, list):
            for child in item: inspect(child)
        elif isinstance(item, dict):
            if not set(item) <= fields: raise ValueError('PUBLIC_SCHEMA_FAILED')
            for child in item.values(): inspect(child)
        else: raise ValueError('PUBLIC_SCHEMA_FAILED')
    inspect(value)
    payload = json.dumps(value, sort_keys=True)
    if any(rule.search(payload) for rule in RULES.values()):
        raise ValueError('PUBLIC_SCAN_FAILED')
    PUBLIC_ROWS.append(value)
    target = Path(os.environ['RUNNER_TEMP']) / 'knowhub25-a3-public'
    target.mkdir(mode=0o700, exist_ok=True)
    (target / 'summary.json').write_text(json.dumps({'schema': 1, 'rows': PUBLIC_ROWS}, indent=2), encoding='utf-8')
    print(payload)

EXPECTED = {
    "TestA3ProcessingFencePostgresConcurrentGeneration",
    "TestA3ProcessingFencePostgresChunkOwnershipAndRollback",
    "TestA3ProcessingFencePostgresCancelDeleteAndLease",
    "TestA3ProcessingFencePostgresMigrationSafety",
    "TestA3ProcessingFencePostgresQueueClaimAndRecovery",
    "TestA3ProcessingFencePostgresKnowledgeWriteGuard",
}
RULES = {
    "BEARER_PATTERN": re.compile(r"(?i)bearer[ \t]+(?!\[REDACTED)[A-Za-z0-9_.~/-]{8,}"),
    "MCP_KEY_PATTERN": re.compile(r"(?<![A-Za-z0-9_])mcp_[A-Za-z0-9_-]{43}(?![A-Za-z0-9_-])"),
    "CREDENTIAL_ASSIGNMENT": re.compile(r'''(?i)["']?(?:authorization|cookie|api_key|secret|token)["']?\s*[:=]\s*["']?(?!\[REDACTED)[A-Za-z0-9_.~/-]{8,}'''),
}

# Fixed synthetic identifiers copied byte-for-byte from the frozen A5 manifest.
FROZEN_CANARIES = [
    "A5_ASSEMBLER_BEARER_CANARY",
    "A5_ASSEMBLER_PROVIDER_CANARY",
    "A5_ASSEMBLER_SQL_CANARY",
    "A5_ASSEMBLER_TOKEN_CANARY",
    "A5_R2_BEARER_CANARY",
    "A5_R2_PROVIDER_CANARY",
    "A5_R2_SQL_CANARY",
    "A5_R2_TOKEN_CANARY",
    "A5_R2_UNWRAP_CANARY",
    "A5_SYNTHETIC_BEARER_CANARY",
    "A5_SYNTHETIC_HTTP_PROVIDER_CANARY",
    "A5_SYNTHETIC_PROVIDER_CANARY",
    "A5_SYNTHETIC_SQL_CANARY",
    "A5_SYNTHETIC_TOKEN_CANARY",
    "Authorization: Bearer SYNTHETIC",
    "B2_MESSAGE_CANARY",
    "B2_OVERFLOW_CANARY",
    "B2_PROVIDER_CANARY",
    "B2_SQL_CANARY",
    "B2_TOKEN_CANARY",
    "SECRET-CANARY",
    "SQL-CANARY",
    "SQL_CANARY",
    "SYNTHETIC_LABEL_SECRET",
    "SYNTHETIC_TOKEN_SQL_CANARY",
    "TOKEN-CANARY",
    "TOKEN_CANARY",
    "TOKEN_SQL_BEARER_CANARY",
    "WIKI_LINT_SQL_TOKEN_CANARY",
    "WIKI_PROVIDER_TOKEN_CANARY",
    "WIKI_SQL_SECRET_CANARY",
    "a23-isolated-password",
    "a23-isolated-signing-key-for-tests-only",
    "other-principal-token",
    "other-secret",
    "registered-secret",
    "synthetic-access",
    "synthetic-access-token",
    "synthetic-client-secret",
    "synthetic-foreign-only",
    "synthetic-parent-secret-canary",
    "synthetic-password",
    "synthetic-refresh",
    "synthetic-refresh-token",
    "synthetic-revoke-a",
    "synthetic-revoke-b",
    "synthetic-revoke-other",
    "LOGIN_TOKEN_SQL_CANARY",
    "SYNTHETIC_LOGIN_CREDENTIAL",
    "SYNTHETIC_LOGIN_BEARER",
    "synthetic-secret",
    "tenant-a-token",
    "tenant-b-token"
]
RULES.update({f"SYNTHETIC_CANARY_{index:03d}": re.compile(re.escape(value)) for index, value in enumerate(FROZEN_CANARIES, 1)})



def full_service(directory):
    blocked = False
    allowed_tests = set()
    for path in Path("internal/application/service").glob("*_test.go"):
        allowed_tests.update(re.findall(r"(?m)^func\s+(Test[A-Za-z0-9_]+)\(t \*testing\.T\)", path.read_text(encoding="utf-8")))
    # Exact historical exceptions copied from the accepted A4 R2 gate manifest.
    allowed_skip = {"TestSyncServicePostgres", "TestRealParserSegmentsAcceptance", "TestCreateStore_DifferentEndpointSameIndex_Allowed"}
    for mode in ("NORMAL", "RACE"):
        raw = directory / ("SERVICE_FULL-" + mode + ".jsonl")
        stderr = directory / ("SERVICE_FULL-" + mode + ".stderr")
        command = ["go", "test", "-mod=readonly", "-p", "2", "-count=1", "-timeout=10m", "-json"]
        if mode == "RACE": command.append("-race")
        command.append("./internal/application/service")
        with raw.open("wb") as out, stderr.open("wb") as err:
            result = subprocess.run(command, stdout=out, stderr=err, check=False)
        rows = {}; invalid = False; package_pass = False; race = False; findings = []
        for path, role in ((raw,"GO_TEST_JSONL"),(stderr,"GO_TEST_STDERR")):
            path.chmod(0o600)
            data = path.read_bytes(); text = data.decode("utf-8",errors="strict")
            race |= "WARNING: DATA RACE" in text
            findings.append({"role":role,"sha256":hashlib.sha256(data).hexdigest(),"rules":{key:len(regex.findall(text)) for key,regex in RULES.items()}})
        try:
            for line in raw.read_text(encoding="utf-8").splitlines():
                event = json.loads(line); name = event.get("Test"); action = event.get("Action")
                if name:
                    if action == "run":
                        if name in rows: invalid = True
                        rows[name] = "RUN"
                    elif action in ("pass","fail","skip"):
                        if rows.get(name) != "RUN": invalid = True
                        rows[name] = action.upper()
                elif action == "pass": package_pass = True
                elif action == "fail": invalid = True
        except (ValueError,TypeError,UnicodeError): invalid = True
        safe_hits = []
        for line in raw.read_text(encoding="utf-8").splitlines():
            counts = {key:len(regex.findall(line)) for key,regex in RULES.items()}
            if not any(counts.values()): continue
            try: event = json.loads(line)
            except (ValueError,TypeError): event = {}
            test_name = event.get("Test", "").split("/")[0]
            # Never expose arbitrary subtest text, Output, path or match text.
            public_id = test_name if test_name in allowed_tests else "UNKNOWN_TEST_ID"
            for rule,count in counts.items():
                if count: safe_hits.append({"role":"GO_TEST_JSONL","testID":public_id,"ruleID":rule,"count":count,"classification":"UNKNOWN"})
        skipped = {key for key,value in rows.items() if value == "SKIP"}
        scan_clear = all(not count for row in findings for count in row["rules"].values())
        passed = result.returncode == 0 and bool(rows) and package_pass and not invalid and not race and scan_clear and skipped <= allowed_skip and all(value in ("PASS","SKIP") for value in rows.values()) and rows.get("TestA3ProcessingAttemptCleanupPostgres") == "PASS" and rows.get("TestLoginTokenIssueFailureLogIsControlled") == "PASS"
        emit({"package":"SERVICE_FULL","mode":mode,"exit":result.returncode,"run":len(rows),"pass":sum(value=="PASS" for value in rows.values()),"fail":sum(value=="FAIL" for value in rows.values()),"failedTestIDs":sorted({key.split("/")[0] for key,value in rows.items() if value=="FAIL" and key.split("/")[0] in allowed_tests}),"skip":len(skipped),"parserValid":not invalid,"dataRace":race,"scanClear":scan_clear,"rawMetadata":findings,"safeHitTests":safe_hits,"status":"PASS" if passed else "BLOCKED"})
        blocked |= not passed
        if not passed: break
    return not blocked


def main():
    directory = Path(tempfile.mkdtemp(prefix="a3-pg-", dir=os.environ["RUNNER_TEMP"]))
    directory.chmod(0o700)
    blocked = False
    matrix = [(package, mode) for package in ("REPOSITORY", "SERVICE") for mode in ("NORMAL", "RACE")]
    for package, mode in matrix:
        expected = EXPECTED if package == "REPOSITORY" else SERVICE_EXPECTED
        path = "./internal/application/repository" if package == "REPOSITORY" else "./internal/application/service"
        pattern = "^TestA3ProcessingFencePostgres" if package == "REPOSITORY" else "^TestA3Processing(AttemptCleanupPostgres|ExecutionPostgres)"
        raw = directory / (package + "-" + mode + ".jsonl")
        stderr = directory / (package + "-" + mode + ".stderr")
        command = ["go", "test", "-mod=readonly", "-p", "2", "-count=1", "-timeout=10m", "-json"]
        if mode == "RACE":
            command.append("-race")
        command += ["-run", pattern, path]
        with raw.open("wb") as out, stderr.open("wb") as err:
            result = subprocess.run(command, stdout=out, stderr=err, check=False)
        raw.chmod(0o600)
        stderr.chmod(0o600)
        rows = {}
        malformed = False
        data_race = False
        findings = []
        for path, role in ((raw, "GO_TEST_JSONL"), (stderr, "GO_TEST_STDERR")):
            data = path.read_bytes()
            text = data.decode("utf-8", errors="strict")
            data_race |= "WARNING: DATA RACE" in text
            counts = {name: len(pattern.findall(text)) for name, pattern in RULES.items()}
            findings.append({"role": role, "sha256": hashlib.sha256(data).hexdigest(), "rules": counts})
        try:
            for line in raw.read_text(encoding="utf-8").splitlines():
                event = json.loads(line)
                name = event.get("Test")
                action = event.get("Action")
                if name in expected:
                    if action == "run":
                        if name in rows:
                            malformed = True
                        rows[name] = "RUN"
                    elif action in ("pass", "skip", "fail"):
                        if rows.get(name) != "RUN":
                            malformed = True
                        rows[name] = action.upper()
                elif name:
                    malformed = True
        except (ValueError, UnicodeError, TypeError):
            malformed = True
        scan_clear = all(not count for row in findings for count in row["rules"].values())
        passed = result.returncode == 0 and rows == {name: "PASS" for name in expected} and not malformed and not data_race and scan_clear
        blocked |= not passed
        emit({"package": package, "mode": mode, "exit": result.returncode, "tests": rows,
                          "parserValid": not malformed, "dataRace": data_race, "scanClear": scan_clear,
                          "rawMetadata": findings, "status": "PASS" if passed else "BLOCKED"})
        if not passed:
            break
    if not blocked:
        blocked = not full_service(directory)
    # No artifact uploads are authorized by this workflow, including on failure.
    raise SystemExit(1 if blocked else 0)


def metadata_self_test():
    previous = os.environ.get('RUNNER_TEMP')
    try:
        with tempfile.TemporaryDirectory() as directory:
            os.environ['RUNNER_TEMP'] = directory
            emit({'status': 'BLOCKED', 'failureClass': 'HELPER_FAILED'})
            for bad in [{'raw': 'unknown'}, {'status': 'token=unknown_secret'},
                        {'status': 'BLOCKED', 'cause': 'unknown'}]:
                try: emit(bad)
                except ValueError: pass
                else: raise AssertionError('FAIL_OPEN')
            assert len(json.loads((Path(directory) / 'knowhub25-a3-public/summary.json').read_text())['rows']) == 1
    finally:
        PUBLIC_ROWS.clear()
        if previous is None: os.environ.pop('RUNNER_TEMP', None)
        else: os.environ['RUNNER_TEMP'] = previous
    print('SAFE_METADATA_SELFTEST_4_PASS')


if __name__ == "__main__":
    try:
        if '--self-test' in sys.argv: metadata_self_test()
        else: main()
    except Exception:
        # Never serialize an exception cause or raw command output publicly.
        emit({"status": "BLOCKED", "failureClass": "HELPER_FAILED"})
        raise SystemExit(1) from None
