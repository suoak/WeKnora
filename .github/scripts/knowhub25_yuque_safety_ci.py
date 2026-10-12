"""Bounded Yuque regression. Raw output stays private to the runner."""
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys

PACKAGE = './internal/datasource/connector/yuque'
POLICY = json.loads((Path(__file__).resolve().parents[1]/'qa/yuque-safety.json').read_bytes())
REQUIRED_TESTS = set(POLICY['requiredTests'])
RULES = {
    'BEARER_PATTERN': re.compile(r'(?i)bearer[ \t]+(?!\[REDACTED)[A-Za-z0-9_.~/-]{8,}'),
    'MCP_KEY_PATTERN': re.compile(r'(?<![A-Za-z0-9_])mcp_[A-Za-z0-9_-]{43}(?![A-Za-z0-9_-])'),
    'CREDENTIAL_ASSIGNMENT': re.compile(r'''(?i)["']?(?:authorization|cookie|api_key|secret|token)["']?\s*[:=]\s*["']?(?!\[REDACTED)[A-Za-z0-9_.~/-]{8,}'''),
}
RULES.update({'SYNTHETIC_%03d' % (i+1): re.compile(re.escape(value))
              for i, value in enumerate(POLICY['canaries'])})


def parse(data, exit_code):
    result = dict(run=0, passed=0, failed=0, skipped=0, exit=exit_code, state='BLOCKED')
    starts, terminals, package_states = set(), {}, []
    valid = exit_code == 0 and b'DATA RACE' not in data
    try:
        for line in data.decode('utf-8', errors='strict').splitlines():
            event = json.loads(line)
            if not isinstance(event, dict):
                raise ValueError
            action = event.get('Action')
            if event.get('Package') != 'github.com/Tencent/WeKnora/'+PACKAGE[2:]:
                # Exactly one package is invoked; never accept cross-package events.
                raise ValueError
            test = event.get('Test')
            if action == 'run':
                if not test or test in starts:
                    raise ValueError
                starts.add(test)
            if action in ('pass', 'fail', 'skip'):
                if test:
                    if test not in starts or test in terminals:
                        raise ValueError
                    terminals[test] = action
                else:
                    package_states.append(action)
        valid = valid and starts == set(terminals) and package_states == ['pass'] and bool(starts)
        valid = valid and all(terminals.get(test) == 'pass' for test in REQUIRED_TESTS)
        result.update(run=len(starts), passed=sum(v == 'pass' for v in terminals.values()),
                      failed=sum(v == 'fail' for v in terminals.values()), skipped=sum(v == 'skip' for v in terminals.values()))
        valid = valid and result['failed'] == result['skipped'] == 0
    except (ValueError, UnicodeError, TypeError):
        valid = False
    result['state'] = 'PASS' if valid else 'BLOCKED'
    return result


def findings(data):
    text = data.decode('utf-8', errors='strict')
    return [{'ruleId': rule, 'count': len(pattern.findall(text))}
            for rule, pattern in RULES.items() if pattern.search(text)]


def self_test():
    # Source identity compares dependencies with a fixed historical baseline.
    # A depth-one checkout cannot satisfy that contract.
    workflow = (Path(__file__).resolve().parents[1] / 'workflows/knowhub25-yuque-log-safety.yml').read_text()
    assert re.search(r'(?m)^\s+fetch-depth:\s+0\s*$', workflow)
    pkg = 'github.com/Tencent/WeKnora/internal/datasource/connector/yuque'
    events = []
    for test in sorted(REQUIRED_TESTS):
        events.extend([{'Action':'run','Package':pkg,'Test':test}, {'Action':'pass','Package':pkg,'Test':test}])
    events.append({'Action':'pass','Package':pkg})
    data = ('\n'.join(json.dumps(e) for e in events) + '\n').encode()
    assert parse(data, 0)['state'] == 'PASS'
    for bad in (data + b'not-json\n', data.splitlines()[0] + b'\n', data.replace(b'"pass"', b'"skip"'), data + data):
        assert parse(bad, 0)['state'] == 'BLOCKED'
    assert parse(data, 1)['state'] == 'BLOCKED'
    assert findings(b'token=synthetic-secret-fixture')
    assert findings(b'Bearer synthetic-secret-fixture')
    assert not findings(b'[Yuque] client configured')
    # Mixed harmless output never neutralizes a credential-shaped match.
    assert findings(b'[Yuque] client configured\ntoken=synthetic-secret-fixture')
    assert all(findings(value.encode()) for value in POLICY['canaries'])


def run():
    os.umask(0o077)
    root = Path(os.environ['RUNNER_TEMP']) / 'knowhub25-yuque-private'
    root.mkdir(mode=0o700, exist_ok=False)
    public = Path(os.environ['RUNNER_TEMP']) / 'knowhub25-yuque-reviewed'
    public.mkdir(mode=0o700, exist_ok=False)
    summary = {'schemaVersion': 1, 'sourceSHA': os.environ['GITHUB_SHA'], 'packageId': 'PKG_017',
               'rows': [], 'rawUploadPermitted': False, 'gatePass': False, 'failureClass': 'UNKNOWN'}
    try:
        self_test()
        assert subprocess.check_output(['git', 'rev-parse', 'HEAD']).decode().strip() == os.environ['GITHUB_SHA']
        assert subprocess.check_output(['git', 'rev-parse', '--is-shallow-repository']).decode().strip() == 'false'
        assert not subprocess.check_output(['git', 'status', '--porcelain'])
        for name, blob in POLICY['sourceBlobs'].items():
            assert subprocess.check_output(['git', 'rev-parse', 'HEAD:' + name]).decode().strip() == blob
        for name in ['go.mod', 'go.sum']:
            assert subprocess.check_output(['git', 'show', POLICY['baseline'] + ':' + name]) == subprocess.check_output(['git', 'show', 'HEAD:' + name])
        assert subprocess.check_output(['go', 'version']).decode().strip().startswith('go version go1.26.7 linux/amd64')
        assert os.environ.get('CGO_ENABLED') == '1' and os.environ.get('GOTOOLCHAIN') == 'local'
        for mode in ('NORMAL', 'RACE'):
            command = ['go', 'test', '-mod=readonly', '-p', '2', '-count=1', '-json']
            if mode == 'RACE':
                command.append('-race')
            command.append(PACKAGE)
            output, errors = root/(mode+'.jsonl'), root/(mode+'.stderr.log')
            with output.open('wb') as out, errors.open('wb') as err:
                completed = subprocess.run(command, stdout=out, stderr=err, timeout=600)
            row = dict(mode=mode, **parse(output.read_bytes(), completed.returncode))
            row['files'] = []
            for role, path in [('GO_TEST_JSONL', output), ('GO_TEST_STDERR', errors)]:
                data = path.read_bytes()
                hits = findings(data)
                row['files'].append({'role': role, 'sha256': hashlib.sha256(data).hexdigest(), 'findings': hits})
                if hits or b'DATA RACE' in data:
                    row['state'] = 'BLOCKED'
            summary['rows'].append(row)
        summary['gatePass'] = len(summary['rows']) == 2 and all(r['state'] == 'PASS' for r in summary['rows'])
        summary['failureClass'] = 'NONE' if summary['gatePass'] else 'TEST_OR_RAW_SCAN_BLOCKED'
    except Exception:
        summary['failureClass'] = 'HELPER_FAILED'
    # Only fixed keys/enums, numeric counts and full-file/source hashes are emitted.
    assert summary['failureClass'] in ('UNKNOWN','NONE','TEST_OR_RAW_SCAN_BLOCKED','HELPER_FAILED')
    assert re.fullmatch('[0-9a-f]{40}', summary['sourceSHA'])
    (public/'summary.json').write_text(json.dumps(summary, indent=2)+'\n', encoding='utf-8')
    return 0 if summary['gatePass'] else 1


if __name__ == '__main__':
    if sys.argv[1:] == ['--self-test']:
        self_test()
        print('YUQUE_SAFETY_SELF_TEST_PASS')
    else:
        sys.exit(run())
