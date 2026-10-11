"""Continuous isolated regression runner. Public output is fixed metadata only."""
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import time
import urllib.request

QA = Path(__file__).resolve().parents[1] / 'qa/continuous'
M = json.loads((QA / 'manifest.json').read_text())
spec = importlib.util.spec_from_file_location('continuous_contracts', QA / 'contracts.py')
C = importlib.util.module_from_spec(spec)
spec.loader.exec_module(C)
PHASES = {'GO', 'SHUFFLE', 'UNITTEST', 'PYTEST', 'GATEWAY', 'NOTION_OVERLAY',
          'PY_DEPENDENCIES', 'FRONT_DEPENDENCIES', 'FRONT_UNIT', 'FRONT_TYPE',
          'FRONT_QA_BUILD', 'FRONT_RELEASE_BUILD', 'FRONT_QA_E2E', 'FRONT_RELEASE_E2E', 'FRONT_BROWSER'}
MODES = {'NORMAL', 'RACE', 'SHUFFLE', 'PYTHON', 'FRONTEND', 'OVERLAY_NORMAL', 'OVERLAY_RACE', 'EXTRA_NORMAL', 'EXTRA_RACE'}
ROLES = {'GO_TEST_JSONL', 'GO_COMPILE_LOG', 'PY_JUNIT_XML', 'PYTHON_OUTPUT', 'GATEWAY_OUTPUT', 'FRONTEND_OUTPUT', 'FRONTEND_JUNIT_XML'}
FAILURES = {'NONE', 'IDENTITY_FAILED', 'DEPENDENCY_FAILED', 'COMPILE_FAILED', 'TEST_FAILED',
            'PARSER_FAILED', 'DATA_RACE', 'RAW_SCAN_HIT', 'EVIDENCE_INCOMPLETE', 'HELPER_FAILED'}

def sha(raw):
    return hashlib.sha256(raw).hexdigest()

def git(root, *args):
    p = subprocess.run(['git', '-C', str(root), *args], capture_output=True)
    if p.returncode:
        raise ValueError('IDENTITY_FAILED')
    return p.stdout

def check_manifest():
    if len(M['packages']) != 23 or len(set(M['packages'])) != 23 or len(M['race']) != 21 or len(set(M['race'])) != 21 or not set(M['race']) <= set(M['packages']):
        raise ValueError('MANIFEST_FAILED')
    if M['matrix']['total'] != 98:
        raise ValueError('MANIFEST_FAILED')
    if len(M['extra_packages']) != 5 or len(set(M['extra_packages'])) != 5 or set(M['extra_packages']) & set(M['packages']):
        raise ValueError('MANIFEST_FAILED')
    if not M['candidate_products'] or any(not re.fullmatch('[0-9a-f]{40}', v['blob']) or not re.fullmatch('[0-9a-f]{64}', v['sha256']) for v in M['candidate_products'].values()):
        raise ValueError('MANIFEST_FAILED')
    if not M['notion_overlay_required'] or not isinstance(M['approved_skips'], dict):
        raise ValueError('MANIFEST_FAILED')
    for row in M['fixtures']:
        if sha((QA / row['path']).read_bytes()) != row['sha256']:
            raise ValueError('IDENTITY_FAILED')
    for group in ['required', 'candidate_required']:
        if not isinstance(M[group], dict) or not set(M[group]) <= set(M['packages']):
            raise ValueError('MANIFEST_FAILED')
        if any(not isinstance(v, list) or any(not isinstance(x, str) or not x.startswith('Test') for x in v) or len(v) != len(set(v)) for v in M[group].values()):
            raise ValueError('MANIFEST_FAILED')

def validate_summary(value):
    if set(value) != {'schema', 'scope', 'side', 'mode', 'sourceSHA', 'status', 'failureClass', 'rows', 'rawFiles', 'productionGates'}:
        raise ValueError('SCHEMA_FAILED')
    if value['schema'] != 2 or value['scope'] != 'CONTINUOUS_REGRESSION_ONLY' or value['side'] not in {'BASELINE', 'CANDIDATE'} or value['mode'] not in MODES or value['status'] not in {'PASS', 'BLOCKED'} or value['failureClass'] not in FAILURES or not re.fullmatch('[0-9a-f]{40}', value['sourceSHA']):
        raise ValueError('SCHEMA_FAILED')
    if value['productionGates'] != {'postgresSchema3023': 'NOT_EVALUATED', 'tokenGovernance': 'NOT_EVALUATED', 'release': 'NOT_EVALUATED'}:
        raise ValueError('SCHEMA_FAILED')
    for row in value['rows']:
        if set(row) != {'phase', 'packageId', 'status', 'compileExit', 'testExit', 'run', 'passed', 'failed', 'skipped', 'requiredPassCount', 'requiredMissingCount', 'a5NotPresentCount', 'dataRace'} or row['phase'] not in PHASES or row['status'] not in {'PASS', 'FAIL', 'NOT_EXECUTED'} or row['packageId'] not in {'NOT_APPLICABLE'} | {f'PKG_{i+1:03d}' for i in range(28)}:
            raise ValueError('SCHEMA_FAILED')
        for key in ['compileExit', 'testExit', 'run', 'passed', 'failed', 'skipped', 'requiredPassCount', 'requiredMissingCount', 'a5NotPresentCount']:
            if row[key] is not None and (type(row[key]) is not int or (key not in {'compileExit', 'testExit'} and row[key] < 0)):
                raise ValueError('SCHEMA_FAILED')
        if type(row['dataRace']) is not bool:
            raise ValueError('SCHEMA_FAILED')
        if row['status'] == 'PASS' and (row['testExit'] != 0 or row['failed'] or row['requiredMissingCount'] or row['dataRace']):
            raise ValueError('SCHEMA_FAILED')
    for row in value['rawFiles']:
        if set(row) != {'role', 'logicalId', 'sha256', 'scanStatus', 'findings'} or row['role'] not in ROLES or not re.fullmatch('RAW_[0-9]{4}', row['logicalId']) or not re.fullmatch('[0-9a-f]{64}', row['sha256']) or row['scanStatus'] not in {'CLEAR', 'HIT', 'NOT_VERIFIED'}:
            raise ValueError('SCHEMA_FAILED')
        for hit in row['findings']:
            if set(hit) != {'ruleId', 'count'} or hit['ruleId'] not in {'CREDENTIAL_ASSIGNMENT', 'BEARER_PATTERN', 'MCP_KEY_PATTERN'} | {f'SYNTHETIC_{i+1:03d}' for i in range(len(M['canaries']))} or type(hit['count']) is not int or hit['count'] <= 0:
                raise ValueError('SCHEMA_FAILED')
        if row['scanStatus'] == 'CLEAR' and row['findings']:
            raise ValueError('SCHEMA_FAILED')
    if value['status'] == 'PASS' and (not value['rows'] or value['failureClass'] != 'NONE' or any(r['status'] != 'PASS' for r in value['rows']) or any(r['scanStatus'] != 'CLEAR' for r in value['rawFiles'])):
        raise ValueError('SCHEMA_FAILED')
    return value

class Runner:
    def __init__(self, side, mode):
        os.umask(0o077)
        check_manifest()
        self.side, self.mode = side, mode
        self.source = Path(side.lower()).resolve()
        self.head = git(self.source, 'rev-parse', 'HEAD').decode().strip()
        expected = M['baseline'] if side == 'BASELINE' else os.environ['GITHUB_SHA']
        if self.head != expected or git(self.source, 'status', '--porcelain'):
            raise ValueError('IDENTITY_FAILED')
        if side == 'CANDIDATE':
            if git(self.source, 'merge-base', '--is-ancestor', M['baseline'], self.head):
                raise ValueError('IDENTITY_FAILED')
            expected_products = M['candidate_products']
            changed = set(git(self.source, 'diff', '--name-only', M['baseline'], self.head).decode().splitlines())
            if {p for p in changed if not p.startswith('.github/')} != set(expected_products):
                raise ValueError('IDENTITY_FAILED')
            if {p for p in changed if p.startswith('.github/')} != set(M['ci_file_paths']):
                raise ValueError('IDENTITY_FAILED')
            for path, identity in expected_products.items():
                if git(self.source, 'rev-parse', self.head + ':' + path).decode().strip() != identity['blob'] or sha((self.source / path).read_bytes()) != identity['sha256']:
                    raise ValueError('IDENTITY_FAILED')
        for path, blob in M['protected'].items():
            expected_blob = M['candidate_products'].get(path, {}).get('blob', blob) if side == 'CANDIDATE' else blob
            if git(self.source, 'rev-parse', self.head + ':' + path).decode().strip() != expected_blob:
                raise ValueError('IDENTITY_FAILED')
        for path in ['go.mod', 'go.sum', 'frontend/package.json', 'frontend/package-lock.json']:
            if git(self.source, 'show', M['baseline'] + ':' + path) != git(self.source, 'show', self.head + ':' + path):
                raise ValueError('IDENTITY_FAILED')
        self.root = Path(os.environ['RUNNER_TEMP']) / f'knowhub25-private-{side}-{mode}'
        self.root.mkdir(mode=0o700, exist_ok=False)
        self.public = Path('continuous-public')
        self.public.mkdir(exist_ok=False)
        self.env = dict(os.environ, CGO_ENABLED='1', GOTOOLCHAIN='local', GOFLAGS='-mod=readonly',
                        PYTHONDONTWRITEBYTECODE='1', LOG_FORMAT='',
                        GOCACHE=str(self.root / 'go-cache'), GOMODCACHE=str(self.root / 'go-modules'),
                        GOTMPDIR=str(self.root / 'go-temp'), PLAYWRIGHT_BROWSERS_PATH=str(self.root / 'browsers'))
        for key in ['GOCACHE', 'GOMODCACHE', 'GOTMPDIR', 'PLAYWRIGHT_BROWSERS_PATH']:
            Path(self.env[key]).mkdir(mode=0o700)
        self.rows, self.raw = [], []
        self.failure = 'NONE'

    def execute(self, phase, command, role, cwd=None, env=None):
        ident = len(self.raw) + 1
        paths = [self.root / f'{ident:04d}.stdout', self.root / f'{ident:04d}.stderr']
        self.raw.extend([(path, role) for path in paths])
        with paths[0].open('wb') as out, paths[1].open('wb') as err:
            p = subprocess.run(command, cwd=cwd or self.source, env=env or self.env, stdout=out, stderr=err, timeout=7200)
        return p.returncode, paths[0], paths[1]

    def record(self, phase, record, package='NOT_APPLICABLE', compile_exit=None, a5_missing=0):
        row = dict(phase=phase, packageId=package, status='PASS' if record.get('gatePass') else 'FAIL',
                   compileExit=compile_exit, testExit=record.get('testExit', record.get('exit')),
                   run=record.get('run'), passed=record.get('passed'), failed=len(record.get('fail', [])) + len(record.get('error', [])),
                   skipped=len(record.get('skip', [])), requiredPassCount=len(record.get('requiredPassed', [])),
                   requiredMissingCount=len(record.get('missing', [])), a5NotPresentCount=a5_missing,
                   dataRace=bool(record.get('dataRace', False)))
        self.rows.append(row)
        if not record.get('gatePass'):
            self.failure = 'DATA_RACE' if row['dataRace'] else 'COMPILE_FAILED' if compile_exit not in {None, 0} else 'PARSER_FAILED' if record.get('validationErrors') or record.get('validJUnit') is False or record.get('validLifecycle') is False else 'TEST_FAILED'

    def simple(self, phase, command, role, cwd=None, env=None):
        code, out, err = self.execute(phase, command, role, cwd, env)
        self.record(phase, {'gatePass': code == 0, 'exit': code, 'fail': [], 'skip': [], 'error': []})
        return code

    def go(self):
        packages = M['extra_packages'] if self.mode.startswith('EXTRA_') else M['packages'] if self.mode == 'NORMAL' else M['race']
        agent_pass = False
        for package in packages:
            index = (M['extra_packages'].index(package) + 24) if self.mode.startswith('EXTRA_') else M['packages'].index(package) + 1
            required = set(M['required'].get(package, []))
            if self.side == 'CANDIDATE':
                required.update(M['candidate_required'].get(package, []))
                required.update(M['extra_required'].get(package, []))
            tags = ['-tags', 'sqlite_fts5'] if package.endswith('/retriever/sqlite') else []
            cc = 0
            if self.mode in {'NORMAL', 'EXTRA_NORMAL'}:
                cc, _, _ = self.execute('GO', ['go', 'test', '-mod=readonly', '-p', '2', '-c', '-o', str(self.root / f'pkg{index}.test')] + tags + ['./' + package], 'GO_COMPILE_LOG')
            if cc == 0:
                command = ['go', 'test', '-mod=readonly', '-p', '2', '-count=1', '-json'] + tags + (['-race'] if self.mode in {'RACE', 'EXTRA_RACE'} else []) + ['./' + package]
                code, out, err = self.execute('GO', command, 'GO_TEST_JSONL')
                parsed = C.parse(out.read_text(), cc, code, sorted(required), M['approved_skips'], err.read_text())
            else:
                parsed = C.parse('', cc, None, sorted(required))
            definitions = {name for path in (self.source / package).glob('*_test.go') for name in re.findall(r'^func (Test[A-Za-z0-9_]*)\(', path.read_text(), re.MULTILINE)}
            not_present = sum(name.split('/')[0] not in definitions for name in M['candidate_required'].get(package, [])) if self.side == 'BASELINE' else 0
            self.record('GO', parsed, f'PKG_{index:03d}', cc, not_present)
            if package == 'internal/agent':
                agent_pass = bool(parsed['gatePass'])
        if self.mode == 'NORMAL':
            if not agent_pass:
                self.rows.append(dict(phase='SHUFFLE', packageId='PKG_011', status='NOT_EXECUTED', compileExit=None, testExit=None,
                                      run=None, passed=None, failed=None, skipped=None, requiredPassCount=None,
                                      requiredMissingCount=None, a5NotPresentCount=0, dataRace=False))
                return
            code, out, err = self.execute('SHUFFLE', ['go', 'test', '-mod=readonly', '-p', '2', '-count=1', '-json', '-shuffle=on', './internal/agent'], 'GO_TEST_JSONL')
            required = set(M['required'].get('internal/agent', []))
            if self.side == 'CANDIDATE':
                required.update(M['candidate_required'].get('internal/agent', []))
            self.record('SHUFFLE', C.parse(out.read_text(), 0, code, sorted(required), M['approved_skips'], err.read_text()), 'PKG_011', 0)

    def python(self):
        if self.simple('PY_DEPENDENCIES', [sys.executable, '-m', 'pip', 'install', '--require-hashes', '-r', str(QA / 'python-linux.lock')], 'PYTHON_OUTPUT'):
            raise ValueError('DEPENDENCY_FAILED')
        env = dict(self.env, PYTHONPATH=str(self.source / 'mcp-server'))
        path = self.root / 'unittest.json'
        code, _, _ = self.execute('UNITTEST', [sys.executable, '-B', str(QA / 'contracts.py'), '--unittest-run', str(self.source / 'mcp-server'), 'test_*.py', str(path)], 'PYTHON_OUTPUT', env=env)
        self.raw.append((path, 'PYTHON_OUTPUT'))
        self.record('UNITTEST', C.parse_unittest(path, code))
        path = self.root / 'pytest.xml'
        code, _, _ = self.execute('PYTEST', [sys.executable, '-m', 'pytest', '-q', 'mcp-server/tests', '--junitxml=' + str(path)], 'PYTHON_OUTPUT', env=env)
        self.raw.append((path, 'PY_JUNIT_XML'))
        self.record('PYTEST', C.parse_pytest(path, code))
        target = self.source / 'internal/middleware/user_mcp_lifecycle_http_test.go'
        if target.exists():
            raise ValueError('IDENTITY_FAILED')
        shutil.copyfile(QA / 'personal_mcp_bridge_test.go', target)
        try:
            binary = self.root / 'gateway.test'
            cc, _, _ = self.execute('GATEWAY', ['go', 'test', '-mod=readonly', '-p', '2', '-c', '-o', str(binary), './internal/middleware'], 'GO_COMPILE_LOG')
            path = self.root / 'gateway.json'
            code = None
            if cc == 0:
                gatewayenv = dict(env, PERSONAL_MCP_APP_TEST_BINARY=str(binary))
                code, _, _ = self.execute('GATEWAY', [sys.executable, '-B', str(QA / 'contracts.py'), '--unittest-run', str(QA), 'test_personal_mcp_lifecycle_http.py', str(path)], 'GATEWAY_OUTPUT', env=gatewayenv)
                self.raw.append((path, 'GATEWAY_OUTPUT'))
            parsed = C.parse_unittest(path, code)
            parsed['gatePass'] = cc == 0 and parsed['gatePass'] and parsed['run'] == parsed['passed'] == 4
            self.record('GATEWAY', parsed, compile_exit=cc)
        finally:
            target.unlink()
        if self.side == 'CANDIDATE':
            target = self.source / 'internal/datasource/connector/notion/a4_ci_nested_http_test.go'
            if target.exists():
                raise ValueError('IDENTITY_FAILED')
            shutil.copyfile(QA / 'notion_nested_http_test.go', target)
            try:
                for race in [False, True]:
                    code, out, err = self.execute('NOTION_OVERLAY', ['go', 'test', '-mod=readonly', '-p', '2', '-count=1', '-json'] + (['-race'] if race else []) + ['./internal/datasource/connector/notion'], 'GO_TEST_JSONL')
                    required = set(M['required']['internal/datasource/connector/notion']) | set(M['candidate_required'].get('internal/datasource/connector/notion', [])) | set(M['notion_overlay_required'])
                    self.record('NOTION_OVERLAY', C.parse(out.read_text(), 0, code, sorted(required), M['approved_skips'], err.read_text()), 'PKG_016', 0)
            finally:
                target.unlink()

    def serve_e2e(self, build, phase, expected, extra):
        frontend = self.source / 'frontend'
        output = self.root / phase
        output.mkdir(mode=0o700)
        log = self.root / (phase + '-server.log')
        with log.open('wb') as stream:
            server = subprocess.Popen(['node', 'tests/agent-editor/stable-server.mjs', str(build)], cwd=frontend, env=self.env, stdout=stream, stderr=stream)
            self.raw.append((log, 'FRONTEND_OUTPUT'))
            try:
                deadline = time.monotonic() + 10
                while True:
                    try:
                        with urllib.request.urlopen('http://127.0.0.1:4187/index.html', timeout=1) as response:
                            if response.status == 200:
                                break
                    except OSError:
                        pass
                    if server.poll() is not None or time.monotonic() > deadline:
                        raise ValueError('HELPER_FAILED')
                    time.sleep(0.05)
                try:
                    urllib.request.urlopen('http://127.0.0.1:4187/api/v1/agents', timeout=1)
                except urllib.error.HTTPError as error:
                    if error.code != 503:
                        raise ValueError('HELPER_FAILED')
                else:
                    raise ValueError('HELPER_FAILED')
                xml = output / 'junit.xml'
                env = dict(self.env, CONTINUOUS_E2E_DIR=str(output), CONTINUOUS_JUNIT=str(xml))
                code, _, _ = self.execute(phase, ['node', 'node_modules/@playwright/test/cli.js', 'test', '--config', 'tests/agent-editor/continuous-linux.config.ts'] + extra, 'FRONTEND_OUTPUT', cwd=frontend, env=env)
                self.raw.append((xml, 'FRONTEND_JUNIT_XML'))
                parsed = C.parse_pytest(xml, code)
                parsed['gatePass'] = parsed['gatePass'] and parsed['run'] == expected
                self.record(phase, parsed)
            finally:
                server.terminate()
                try:
                    server.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    server.kill()
                    server.wait(timeout=10)

    def frontend(self):
        frontend = self.source / 'frontend'
        if self.simple('FRONT_DEPENDENCIES', ['npm', 'ci', '--no-audit', '--no-fund'], 'FRONTEND_OUTPUT', cwd=frontend):
            raise ValueError('DEPENDENCY_FAILED')
        if self.simple('FRONT_BROWSER', ['node', 'node_modules/@playwright/test/cli.js', 'install', 'chromium'], 'FRONTEND_OUTPUT', cwd=frontend):
            raise ValueError('DEPENDENCY_FAILED')
        browser_script = 'process.stdout.write(require("playwright").chromium.executablePath())'
        code, browser_path, _ = self.execute('FRONT_BROWSER', ['node', '-e', browser_script], 'FRONTEND_OUTPUT', cwd=frontend)
        browser = Path(browser_path.read_text().strip())
        if code or not browser.is_file() or not browser.is_relative_to(Path(self.env['PLAYWRIGHT_BROWSERS_PATH'])):
            raise ValueError('DEPENDENCY_FAILED')
        self.env.update(BROWSERSKILL_TEST_CHROMIUM=str(browser), BROWSERSKILL_TEST_PLAYWRIGHT=str(frontend / 'node_modules/playwright/index.mjs'))
        xml = self.root / 'front-unit.xml'
        code, _, _ = self.execute('FRONT_UNIT', ['npm', 'test', '--', '--test-concurrency=1', '--test-reporter=junit', '--test-reporter-destination=' + str(xml)], 'FRONTEND_OUTPUT', cwd=frontend)
        self.raw.append((xml, 'FRONTEND_JUNIT_XML'))
        self.record('FRONT_UNIT', C.parse_pytest(xml, code))
        self.simple('FRONT_TYPE', ['node', 'node_modules/vue-tsc/bin/vue-tsc.js', '--build'], 'FRONTEND_OUTPUT', cwd=frontend)
        target = frontend / 'tests/agent-editor/continuous-linux.config.ts'
        if target.exists():
            raise ValueError('IDENTITY_FAILED')
        shutil.copyfile(QA / 'playwright-linux.config.ts', target)
        try:
            qa_build = self.root / 'qa-dist'
            env = dict(self.env, RC24_QA_BUILD='1', RC24_BUILD_DIR=str(qa_build), VITE_IS_DOCKER='true')
            if not self.simple('FRONT_QA_BUILD', ['node', 'node_modules/vite/bin/vite.js', 'build', '--mode', 'rc24-qa', '--config', 'tests/agent-editor/frozen-build.config.ts'], 'FRONTEND_OUTPUT', cwd=frontend, env=env):
                self.serve_e2e(qa_build, 'FRONT_QA_E2E', 39, [])
            release_build = self.root / 'release-dist'
            if not self.simple('FRONT_RELEASE_BUILD', ['node', 'node_modules/vite/bin/vite.js', 'build', '--outDir', str(release_build), '--emptyOutDir', 'false'], 'FRONTEND_OUTPUT', cwd=frontend):
                self.serve_e2e(release_build, 'FRONT_RELEASE_E2E', 25, ['close-cycle.spec.ts', 'navigation.spec.ts', 'editor.spec.ts', 'knowledge-gate.spec.ts', 'mcp-scope.spec.ts', '--grep-invert', 'quick answer exposes retrieval'])
        finally:
            target.unlink()

    def finish(self):
        files = []
        for index, (path, role) in enumerate(self.raw):
            try:
                raw = path.read_bytes()
                findings = C.scan_findings(raw.decode('utf-8', 'strict'))
                state = 'HIT' if findings else 'CLEAR'
            except (OSError, UnicodeError):
                raw, findings, state = b'', [], 'NOT_VERIFIED'
            files.append(dict(role=role, logicalId=f'RAW_{index+1:04d}', sha256=sha(raw), scanStatus=state, findings=findings))
        if any(r['scanStatus'] != 'CLEAR' for r in files):
            self.failure = 'RAW_SCAN_HIT' if any(r['scanStatus'] == 'HIT' for r in files) else 'EVIDENCE_INCOMPLETE'
        value = dict(schema=2, scope='CONTINUOUS_REGRESSION_ONLY', side=self.side, mode=self.mode, sourceSHA=self.head,
                     status='PASS' if self.failure == 'NONE' and self.rows and all(r['status'] == 'PASS' for r in self.rows) else 'BLOCKED',
                     failureClass=self.failure, rows=self.rows, rawFiles=files,
                     productionGates=dict(postgresSchema3023='NOT_EVALUATED', tokenGovernance='NOT_EVALUATED', release='NOT_EVALUATED'))
        (self.public / 'summary.json').write_text(json.dumps(validate_summary(value), indent=2) + '\n')
        return 0 if value['status'] == 'PASS' else 1

def selftest():
    os.umask(0o077)
    check_manifest()
    go = C.parser_selftest()
    junit = C.junit_selftest()
    unittest = C.unittest_selftest()
    sample = dict(schema=2, scope='CONTINUOUS_REGRESSION_ONLY', side='CANDIDATE', mode='NORMAL', sourceSHA='a' * 40,
                  status='BLOCKED', failureClass='EVIDENCE_INCOMPLETE', rows=[], rawFiles=[],
                  productionGates=dict(postgresSchema3023='NOT_EVALUATED', tokenGovernance='NOT_EVALUATED', release='NOT_EVALUATED'))
    validate_summary(sample)
    for patch in [{'rawCause': 'not-public'}, {'status': 'PASS'}, {'failureClass': 'unknown-freetext'}]:
        try:
            validate_summary(dict(sample, **patch))
        except ValueError:
            pass
        else:
            raise ValueError('SCHEMA_FAILED')
    for name in ['packages', 'candidate_required', 'notion_overlay_required', 'candidate_products']:
        old = M.pop(name)
        try:
            try:
                check_manifest()
            except (ValueError, KeyError):
                pass
            else:
                raise ValueError('MANIFEST_FAILED')
        finally:
            M[name] = old
    for value in ['token=synthetic_fixture_value', 'Bearer synthetic_fixture_value', M['canaries'][0]]:
        if not C.scan_findings(value):
            raise ValueError('SCAN_FAILED')
    workflow = (QA.parents[1] / 'workflows/knowhub25-continuous.yml').read_text()
    if workflow.count('integration/knowhub25-continuous-20261011') != 2 or 'contents: read' not in workflow or 'persist-credentials: true' in workflow or 'secrets.' in workflow or 'pull_request_target' in workflow:
        raise ValueError('WORKFLOW_FAILED')
    if 'steps.review.outcome == \'success\'' not in workflow or 'path: continuous-public/summary.json' not in workflow or 'path: ${{ runner.temp }}' in workflow:
        raise ValueError('UPLOAD_FAILED')
    print(json.dumps({'goParserCases': len(go), 'junitCases': len(junit), 'unittestCases': len(unittest), 'safeSchemaNegativeCases': 3,
                      'manifestNegativeCases': 4, 'rawScanPositiveCases': 3, 'workflowFixedGuard': 'PASS', 'matrix': 98,
                      'additionalPackageRows': 20, 'status': 'PASS'}))

if __name__ == '__main__':
    try:
        if sys.argv[1:] == ['--self-test']:
            selftest()
        elif sys.argv[1:] == ['--review']:
            validate_summary(json.loads(Path('continuous-public/summary.json').read_text()))
        else:
            runner = Runner(sys.argv[1], sys.argv[2])
            try:
                if runner.mode in {'NORMAL', 'RACE', 'EXTRA_NORMAL', 'EXTRA_RACE'}:
                    runner.go()
                elif runner.mode == 'PYTHON':
                    runner.python()
                elif runner.mode == 'FRONTEND':
                    runner.frontend()
                else:
                    raise ValueError('HELPER_FAILED')
            except Exception:
                runner.failure = 'HELPER_FAILED'
            sys.exit(runner.finish())
    except Exception:
        # Initialization failures have no reviewed raw evidence or test counts.
        if len(sys.argv) == 3 and sys.argv[1] in {'BASELINE', 'CANDIDATE'} and sys.argv[2] in MODES:
            value = dict(schema=2, scope='CONTINUOUS_REGRESSION_ONLY', side=sys.argv[1], mode=sys.argv[2],
                         sourceSHA=os.environ.get('GITHUB_SHA', ''), status='BLOCKED',
                         failureClass='IDENTITY_FAILED', rows=[], rawFiles=[],
                         productionGates=dict(postgresSchema3023='NOT_EVALUATED', tokenGovernance='NOT_EVALUATED', release='NOT_EVALUATED'))
            validate_summary(value)
            output = Path('continuous-public')
            output.mkdir(exist_ok=True)
            (output / 'summary.json').write_text(json.dumps(value, indent=2)+'\n', encoding='utf-8')
        sys.exit(1)
