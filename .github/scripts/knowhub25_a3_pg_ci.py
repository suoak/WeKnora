"""Bounded PG foundation verification; raw output stays on the ephemeral runner."""
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile

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


def full_service(directory):
    blocked = False
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
        skipped = {key for key,value in rows.items() if value == "SKIP"}
        scan_clear = all(not count for row in findings for count in row["rules"].values())
        passed = result.returncode == 0 and bool(rows) and package_pass and not invalid and not race and scan_clear and skipped <= allowed_skip and all(value in ("PASS","SKIP") for value in rows.values()) and rows.get("TestA3ProcessingAttemptCleanupPostgres") == "PASS"
        print(json.dumps({"package":"SERVICE_FULL","mode":mode,"exit":result.returncode,"run":len(rows),"pass":sum(value=="PASS" for value in rows.values()),"fail":sum(value=="FAIL" for value in rows.values()),"skip":len(skipped),"parserValid":not invalid,"dataRace":race,"scanClear":scan_clear,"rawMetadata":findings,"status":"PASS" if passed else "BLOCKED"},sort_keys=True))
        blocked |= not passed
        if not passed: break
    return not blocked


def main():
    directory = Path(tempfile.mkdtemp(prefix="a3-pg-", dir=os.environ["RUNNER_TEMP"]))
    directory.chmod(0o700)
    blocked = False
    matrix = [(package, mode) for package in ("REPOSITORY", "SERVICE") for mode in ("NORMAL", "RACE")]
    for package, mode in matrix:
        expected = EXPECTED if package == "REPOSITORY" else {"TestA3ProcessingAttemptCleanupPostgres"}
        path = "./internal/application/repository" if package == "REPOSITORY" else "./internal/application/service"
        pattern = "^TestA3ProcessingFencePostgres" if package == "REPOSITORY" else "^TestA3ProcessingAttemptCleanupPostgres$"
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
        print(json.dumps({"package": package, "mode": mode, "exit": result.returncode, "tests": rows,
                          "parserValid": not malformed, "dataRace": data_race, "scanClear": scan_clear,
                          "rawMetadata": findings, "status": "PASS" if passed else "BLOCKED"}, sort_keys=True))
        if not passed:
            break
    if not blocked:
        blocked = not full_service(directory)
    # No artifact uploads are authorized by this workflow, including on failure.
    raise SystemExit(1 if blocked else 0)


if __name__ == "__main__":
    try:
        main()
    except Exception:
        # Never serialize an exception cause or raw command output publicly.
        print(json.dumps({"status": "BLOCKED", "failureClass": "HELPER_FAILED"}))
        raise SystemExit(1) from None
