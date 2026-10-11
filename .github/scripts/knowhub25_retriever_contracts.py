"""Bounded B-contract CI; raw output never published."""
import hashlib
import json
import os
import re
import subprocess
import sys
from pathlib import Path

PACKAGES = ['internal/application/repository/retriever/sqlite']
BASELINE = 'a6403708915680ccad310b0fd64506597b879804'
CANARIES = json.loads((Path(__file__).with_name('knowhub25_retriever_canaries.json')).read_text())
RULES = [(f'SYNTHETIC_{i + 1:03}', re.compile(re.escape(v))) for i, v in enumerate(CANARIES)]
RULES += [
    ('BEARER_PATTERN', re.compile(r'(?i)bearer[ \t]+(?!\[REDACTED)[A-Za-z0-9_.~/-]{8,}')),
    ('MCP_KEY_PATTERN', re.compile(r'(?<![A-Za-z0-9_])mcp_[A-Za-z0-9_-]{43}(?![A-Za-z0-9_-])')),
    ('CREDENTIAL_ASSIGNMENT', re.compile(r'''(?i)[\"']?(?:authorization|cookie|api_key|secret|token)[\"']?\s*[:=]\s*[\"']?(?!\[REDACTED)[A-Za-z0-9_.~/-]{8,}''')),
]


def parse(data, exit_code):
    states = {}
    package_status = []
    for line in data.decode('utf-8', errors='strict').splitlines():
        event = json.loads(line)
        name, action = event.get('Test'), event.get('Action')
        if name and action in {'run', 'pass', 'fail', 'skip'}:
            states.setdefault(name, []).append(action)
        elif not name and action in {'pass', 'fail'}:
            package_status.append(action)
    valid = exit_code == 0 and package_status == ['pass'] and bool(states)
    counts = dict(run=0, passed=0, failed=0, skipped=0)
    for seq in states.values():
        valid &= len(seq) == 2 and seq[0] == 'run' and seq[1] in {'pass', 'fail'}
        counts['run'] += seq.count('run')
        counts['passed'] += seq.count('pass')
        counts['failed'] += seq.count('fail')
        counts['skipped'] += seq.count('skip')
    return valid, counts


def self_test():
    good = b'{"Test":"fixed","Action":"run"}\n{"Test":"fixed","Action":"pass"}\n{"Action":"pass"}\n'
    assert parse(good, 0)[0]
    assert not parse(good, 1)[0]
    assert not parse(good.replace(b'"pass"', b'"skip"', 1), 0)[0]
    assert not parse(good.splitlines()[0], 0)[0]
    assert not parse(good + good, 0)[0]
    try:
        parse(good + b'broken', 0)
    except (ValueError, UnicodeError):
        pass
    else:
        raise AssertionError('Corrupt JSON accepted')
    assert any(p.search('token=SYNTHETIC_CREDENTIAL_VALUE') for _, p in RULES)


def main():
    self_test()
    if '--self-test' in sys.argv:
        print('RETRIEVER_CONTRACT_INFRASTRUCTURE_SELF_TEST_PASS')
        return
    assert subprocess.check_output(['git', 'rev-parse', 'HEAD^']).decode().strip() == BASELINE
    assert subprocess.check_output(['go', 'env', 'GOOS']).decode().strip() == 'linux'
    assert subprocess.check_output(['go', 'version']).decode().strip() == 'go version go1.26.7 linux/amd64'
    raw = Path(os.environ['RUNNER_TEMP']) / 'knowhub25-retriever-raw'
    raw.mkdir(mode=0o700)
    safe = Path(os.environ['RUNNER_TEMP']) / 'knowhub25-retriever-safe'
    safe.mkdir(mode=0o700)
    results, ok = [], True
    for i, package in enumerate(PACKAGES, 1):
        for mode in ['NORMAL', 'RACE']:
            path = raw / f'{i}-{mode}.jsonl'
            errpath = raw / f'{i}-{mode}.stderr'
            command = ['go', 'test', '-mod=readonly', '-p', '2', '-count=1', '-json']
            if mode == 'RACE':
                command += ['-race']
            command += ['./' + package]
            with path.open('wb') as out, errpath.open('wb') as err:
                proc = subprocess.run(command, stdout=out, stderr=err, timeout=600)
            status, counts = parse(path.read_bytes(), proc.returncode)
            findings = []
            race = False
            for file in [path, errpath]:
                content = file.read_bytes()
                text = content.decode('utf-8', errors='strict')
                race |= 'WARNING: DATA RACE' in text
                findings.append(dict(sha256=hashlib.sha256(content).hexdigest(),
                                     hits=[dict(ruleId=r, count=len(p.findall(text)))
                                           for r, p in RULES if p.search(text)]))
            clear = not any(x['hits'] for x in findings)
            ok &= status and clear and not race
            results.append(dict(packageId=f'RETRIEVER_PKG_{i:03}', mode=mode, exit=proc.returncode,
                                counts=counts, testPass=status, rawScanClear=clear,
                                dataRace=race, rawEvidence=findings))
    summary = dict(schema=1, candidate=subprocess.check_output(['git', 'rev-parse', 'HEAD']).decode().strip(),
                   baseline=BASELINE, rows=results, gatePass=ok, rawArtifactPublished=False)
    # Fixed schema only: no test names, paths, error messages, output excerpts, or raw values.
    (safe / 'summary.json').write_text(json.dumps(summary, indent=2), encoding='utf-8')
    sys.exit(0 if ok else 1)


if __name__ == '__main__':
    try:
        main()
    except BaseException:
        if '--self-test' in sys.argv:
            raise
        print('RETRIEVER_CONTRACT_VALIDATION_BLOCKED')
        sys.exit(1)
