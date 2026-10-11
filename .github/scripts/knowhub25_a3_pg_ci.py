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
}
RULES = {
    "BEARER_PATTERN": re.compile(r"(?i)bearer[ \t]+(?!\[REDACTED)[A-Za-z0-9_.~/-]{8,}"),
    "MCP_KEY_PATTERN": re.compile(r"(?<![A-Za-z0-9_])mcp_[A-Za-z0-9_-]{43}(?![A-Za-z0-9_-])"),
    "CREDENTIAL_ASSIGNMENT": re.compile(r'''(?i)["']?(?:authorization|cookie|api_key|secret|token)["']?\s*[:=]\s*["']?(?!\[REDACTED)[A-Za-z0-9_.~/-]{8,}'''),
}


def main():
    directory = Path(tempfile.mkdtemp(prefix="a3-pg-", dir=os.environ["RUNNER_TEMP"]))
    directory.chmod(0o700)
    blocked = False
    for mode in ("NORMAL", "RACE"):
        raw = directory / (mode + ".jsonl")
        stderr = directory / (mode + ".stderr")
        command = ["go", "test", "-mod=readonly", "-p", "2", "-count=1", "-timeout=10m", "-json"]
        if mode == "RACE":
            command.append("-race")
        command += ["-run", "^TestA3ProcessingFencePostgres", "./internal/application/repository"]
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
                if name in EXPECTED:
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
        passed = result.returncode == 0 and rows == {name: "PASS" for name in EXPECTED} and not malformed and not data_race and scan_clear
        blocked |= not passed
        print(json.dumps({"mode": mode, "exit": result.returncode, "tests": rows,
                          "parserValid": not malformed, "dataRace": data_race, "scanClear": scan_clear,
                          "rawMetadata": findings, "status": "PASS" if passed else "BLOCKED"}, sort_keys=True))
        if not passed:
            break
    # No artifact uploads are authorized by this workflow, including on failure.
    raise SystemExit(1 if blocked else 0)


if __name__ == "__main__":
    try:
        main()
    except Exception:
        # Never serialize an exception cause or raw command output publicly.
        print(json.dumps({"status": "BLOCKED", "failureClass": "HELPER_FAILED"}))
        raise SystemExit(1) from None
