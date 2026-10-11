"""Bounded B-contract CI; raw output never published."""
import hashlib
import json
import os
import re
import subprocess
import sys
from pathlib import Path

PACKAGES = ['internal/application/repository/retriever/milvus']
BASELINE = 'a6403708915680ccad310b0fd64506597b879804'
# Exact pre-existing exception approved in A4/A5 manifest (source Run 38056265554).
# Necessary B tests are never eligible; skipped is reported separately from passed.
APPROVED_SKIP = ('github.com/Tencent/WeKnora/internal/agent/tools',
                 'TestSkillPythonPackageRecoveryInstallsWithoutPip/uv')
APPROVED_SKIP_REASON = 'skill_runtime_guard_test.go:53: uv is required'
REQUIRED = {'internal/application/repository/retriever/milvus': ['TestCopyIndicesCopiesEveryMappedChunkWhenResultOrderDrifts', 'TestCopyIndicesIgnoresSourceChunksOutsideTheMapping']}
CANARIES = json.loads((Path(__file__).with_name('knowhub25_milvus_copy_canaries.json')).read_text())
RULES = [(f'SYNTHETIC_{i + 1:03}', re.compile(re.escape(v))) for i, v in enumerate(CANARIES)]
RULES += [
    ('BEARER_PATTERN', re.compile(r'(?i)bearer[ \t]+(?!\[REDACTED)[A-Za-z0-9_.~/-]{8,}')),
    ('MCP_KEY_PATTERN', re.compile(r'(?<![A-Za-z0-9_])mcp_[A-Za-z0-9_-]{43}(?![A-Za-z0-9_-])')),
    ('CREDENTIAL_ASSIGNMENT', re.compile(r'''(?i)[\"']?(?:authorization|cookie|api_key|secret|token)[\"']?\s*[:=]\s*[\"']?(?!\[REDACTED)[A-Za-z0-9_.~/-]{8,}''')),
]


def parse(data, exit_code, required=()):
    states = {}
    outputs, packages = {}, {}
    package_status = []
    for line in data.decode('utf-8', errors='strict').splitlines():
        event = json.loads(line)
        name, action = event.get('Test'), event.get('Action')
        if name:
            packages[name] = event.get('Package')
        if name and action == 'output':
            outputs[name] = outputs.get(name, '') + event.get('Output', '')
        if name and action in {'run', 'pass', 'fail', 'skip'}:
            states.setdefault(name, []).append(action)
        elif not name and action in {'pass', 'fail'}:
            package_status.append(action)
    valid = exit_code == 0 and package_status == ['pass'] and bool(states)
    counts = dict(run=0, passed=0, failed=0, skipped=0)
    for name, seq in states.items():
        approved_skip = (packages.get(name), name) == APPROVED_SKIP and APPROVED_SKIP_REASON in outputs.get(name, '')
        valid &= len(seq) == 2 and seq[0] == 'run' and (seq[1] in {'pass', 'fail'} or
                  (seq[1] == 'skip' and approved_skip and not any(name == r or name.startswith(r + '/') for r in required)))
        counts['run'] += seq.count('run')
        counts['passed'] += seq.count('pass')
        counts['failed'] += seq.count('fail')
        counts['skipped'] += seq.count('skip')
    valid &= all(states.get(name) == ['run', 'pass'] for name in required)
    valid &= counts['failed'] == 0
    return valid, counts


def self_test():
    good = b'{"Test":"fixed","Action":"run"}\n{"Test":"fixed","Action":"pass"}\n{"Action":"pass"}\n'
    assert parse(good, 0)[0]
    assert not parse(good, 1)[0]
    assert not parse(good.replace(b'"Test":"fixed","Action":"pass"', b'"Test":"fixed","Action":"fail"'), 0)[0]
    assert not parse(good.replace(b'"pass"', b'"skip"', 1), 0)[0]
    assert not parse(good.splitlines()[0], 0)[0]
    assert not parse(good + good, 0)[0]
    approved = ('{"Package":"%s","Test":"%s","Action":"run"}\n'
                '{"Package":"%s","Test":"%s","Action":"output","Output":"%s"}\n'
                '{"Package":"%s","Test":"%s","Action":"skip"}\n{"Action":"pass"}\n') % (
                *APPROVED_SKIP, *APPROVED_SKIP, APPROVED_SKIP_REASON, *APPROVED_SKIP)
    assert parse(approved.encode(), 0)[0]
    assert not parse(approved.replace(APPROVED_SKIP_REASON, 'wrong').encode(), 0)[0]
    assert not parse(approved.encode(), 0, [APPROVED_SKIP[1]])[0]
    assert not parse(good, 0, ['missing'])[0]
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
        print('MILVUS_COPY_CONTRACT_INFRASTRUCTURE_SELF_TEST_PASS')
        return
    assert subprocess.check_output(['git', 'merge-base', BASELINE, 'HEAD']).decode().strip() == BASELINE
    expected_source = {'internal/application/repository/retriever/milvus/repository.go': '57a8aa8dbaf43ea89cb307a6277351133fe5340c', 'internal/application/repository/retriever/milvus/copy_test.go': 'bf247b9b0b7acee78fd0e296b1461825603222b8'}
    approved = set(expected_source) | {'.github/scripts/knowhub25_milvus_copy_canaries.json', '.github/workflows/knowhub25-milvus-copy.yml', '.github/scripts/knowhub25_milvus_copy_contracts.py'}
    assert set(subprocess.check_output(['git', 'diff', '--name-only', BASELINE, 'HEAD']).decode().splitlines()) == approved
    for path, blob in expected_source.items():
        assert subprocess.check_output(['git', 'rev-parse', 'HEAD:' + path]).decode().strip() == blob
    assert subprocess.check_output(['go', 'env', 'GOOS']).decode().strip() == 'linux'
    assert subprocess.check_output(['go', 'version']).decode().strip() == 'go version go1.26.7 linux/amd64'
    raw = Path(os.environ['RUNNER_TEMP']) / 'knowhub25-milvus-copy-raw'
    raw.mkdir(mode=0o700)
    safe = Path(os.environ['RUNNER_TEMP']) / 'knowhub25-milvus-copy-safe'
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
            status, counts = parse(path.read_bytes(), proc.returncode, REQUIRED[package])
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
            results.append(dict(packageId=f'MILVUS_COPY_PKG_{i:03}', mode=mode, exit=proc.returncode,
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
    except Exception:
        if '--self-test' in sys.argv:
            raise
        print('MILVUS_COPY_CONTRACT_VALIDATION_BLOCKED')
        sys.exit(1)
