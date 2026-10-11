"""Bounded attribution. Raw logs never leave the private runner directory."""
import ast
import hashlib
import json
import os
import pathlib
import re
import subprocess
import sys
import tempfile

HERE = pathlib.Path(__file__).resolve().parents[1]
M = json.loads((HERE / 'qa/cloud-safety/manifest.json').read_text())
ASSIGN = re.compile(r'(?i)[\"\']?(?:authorization|cookie|api_key|secret|token)[\"\']?\s*[:=]\s*[\"\']?(?!\[REDACTED)[A-Za-z0-9_.~/-]{8,}')
RULES = [(f'SYNTHETIC_{i+1:03d}', re.compile(re.escape(v))) for i, v in enumerate(M['canaries'])] + [
    ('BEARER_PATTERN', re.compile(r'(?i)bearer[ \t]+(?!\[REDACTED)[A-Za-z0-9_.~/-]{8,}')),
    ('MCP_KEY_PATTERN', re.compile(r'(?<![A-Za-z0-9_])mcp_[A-Za-z0-9_-]{43}(?![A-Za-z0-9_-])')),
    ('CREDENTIAL_ASSIGNMENT', ASSIGN)]
CATEGORIES = {'CONFIRMED_NON_CREDENTIAL_BUILD_METADATA', 'MASKED_CREDENTIAL_RELATED', 'UNKNOWN'}

def digest(value):
    return hashlib.sha256(value).hexdigest()

def git(source, *args):
    p = subprocess.run(['git', '-C', str(source), *args], capture_output=True)
    if p.returncode:
        raise ValueError('IDENTITY_FAILED')
    return p.stdout

def identity(source, side):
    if git(source, 'rev-parse', 'HEAD').decode().strip() != M[side]:
        raise ValueError('IDENTITY_FAILED')
    if git(source, 'status', '--porcelain', '--untracked-files=all'):
        raise ValueError('IDENTITY_FAILED')
    if side == 'CANDIDATE':
        for row in M['source']:
            value = git(source, 'show', 'HEAD:' + row['path'])
            blob = git(source, 'rev-parse', 'HEAD:' + row['path']).decode().strip()
            if blob != row['blob'] or digest(value) != row['sha256']:
                raise ValueError('IDENTITY_FAILED')
    for row in M['yuque_source'][side]:
        if digest(git(source, 'show', 'HEAD:' + row['path'])) != row['sha256']:
            raise ValueError('IDENTITY_FAILED')

def archive_is_safe(value, root):
    try:
        path = pathlib.Path(value).resolve(strict=True)
        if not path.is_relative_to(root.resolve()):
            return False
        with path.open('rb') as stream:
            return stream.read(8) == b'!<arch>\n'
    except (OSError, ValueError):
        return False

def classify(text, role, root, masks):
    """Classify every exact frozen-rule match; never suppress a match."""
    rows = []
    importcfg_lines = set()
    for line in text.splitlines():
        header = re.fullmatch(r"cat >(\S+/b[0-9]+/importcfg(?:\.link)?) << 'EOF' # internal", line)
        if line.startswith('cat '):
            importcfg_lines = set()
        if role == 'GO_COMPILE_LOG' and header:
            try:
                config_path = pathlib.Path(header[1]).resolve(strict=True)
                if config_path.is_relative_to(root.resolve()):
                    importcfg_lines = set(config_path.read_text(encoding='utf-8').splitlines())
            except (OSError, ValueError):
                pass
        elif line == 'EOF':
            importcfg_lines = set()
        for rule, pattern in RULES:
            matches = list(pattern.finditer(line))
            if not matches:
                continue
            category = 'UNKNOWN'
            # Only a complete go/token build relationship within a verified -x
            # importcfg block and an actual private archive can be proved safe.
            mapping = re.fullmatch(r'packagefile go/token=(\S+)', line)
            if rule == 'CREDENTIAL_ASSIGNMENT' and len(matches) == 1 and line in importcfg_lines and mapping and archive_is_safe(mapping[1], root):
                category = 'CONFIRMED_NON_CREDENTIAL_BUILD_METADATA'
            if rule == 'CREDENTIAL_ASSIGNMENT' and role == 'GO_TEST_JSONL':
                try:
                    event = json.loads(line)
                    output = event.get('Output', '')
                    if len(matches) == 1 and event.get('Action') == 'output' and isinstance(output, str) and any('token=' + mask + ' base=' in output for mask in masks):
                        category = 'MASKED_CREDENTIAL_RELATED'
                except (ValueError, TypeError):
                    pass
            rows.append({'ruleId': rule, 'count': len(matches), 'classification': category,
                         'basis': 'ARCHIVE_AND_BUILD_CONTEXT' if category == 'CONFIRMED_NON_CREDENTIAL_BUILD_METADATA' else 'SOURCE_BOUND_MASK' if category == 'MASKED_CREDENTIAL_RELATED' else 'INSUFFICIENT_EVIDENCE'})
    # Frozen \s includes real newlines. A whole-byte match that spans lines
    # has no complete single-line proof and must be retained as UNKNOWN.
    for rule, pattern in RULES:
        difference = len(pattern.findall(text)) - sum(row['count'] for row in rows if row['ruleId'] == rule)
        if difference < 0:
            raise ValueError('SCANNER_CONTRACT_FAILED')
        if difference:
            rows.append({'ruleId': rule, 'count': difference, 'classification': 'UNKNOWN', 'basis': 'INSUFFICIENT_EVIDENCE'})
    return rows

def safe_summary(value):
    if set(value) != {'schema', 'status', 'historicalStatus', 'executions', 'files'} or value['schema'] != 1:
        raise ValueError('SCHEMA_FAILED')
    if value['status'] not in {'ATTRIBUTION_CLEAR', 'BLOCKED', 'DIAGNOSTIC_FAILED'} or value['historicalStatus'] != 'UNKNOWN':
        raise ValueError('SCHEMA_FAILED')
    for row in value['executions']:
        if set(row) != {'side', 'mode', 'packageId', 'exit', 'status'} or row['side'] not in {'BASELINE', 'CANDIDATE'} or row['mode'] not in {'NORMAL', 'RACE', 'COMPILE'} or row['packageId'] not in {'PKG_001', 'PKG_008', 'PKG_017'} or type(row['exit']) is not int or row['status'] not in {'PASS', 'FAIL'}:
            raise ValueError('SCHEMA_FAILED')
    for row in value['files']:
        if set(row) != {'role', 'side', 'mode', 'packageId', 'sha256', 'findings'} or row['role'] not in {'GO_COMPILE_LOG', 'GO_TEST_JSONL'} or not re.fullmatch('[0-9a-f]{64}', row['sha256']):
            raise ValueError('SCHEMA_FAILED')
        if row['side'] not in {'BASELINE', 'CANDIDATE'} or row['mode'] not in {'NORMAL', 'RACE', 'COMPILE'} or row['packageId'] not in {'PKG_001', 'PKG_008', 'PKG_017'}:
            raise ValueError('SCHEMA_FAILED')
        for hit in row['findings']:
            if set(hit) != {'ruleId', 'count', 'classification', 'basis'} or hit['ruleId'] not in {r for r, _ in RULES} or type(hit['count']) is not int or hit['count'] < 1 or hit['classification'] not in CATEGORIES or hit['basis'] not in {'ARCHIVE_AND_BUILD_CONTEXT', 'SOURCE_BOUND_MASK', 'INSUFFICIENT_EVIDENCE'}:
                raise ValueError('SCHEMA_FAILED')
    if len(value['executions']) > 8:
        raise ValueError('SCHEMA_FAILED')
    keys = {(r['side'], r['mode'], r['packageId']) for r in value['executions']}
    if len(keys) != len(value['executions']):
        raise ValueError('SCHEMA_FAILED')
    if value['status'] == 'ATTRIBUTION_CLEAR' and (len(keys) != 8 or len(value['files']) != 16 or any(r['exit'] != 0 for r in value['executions']) or any(h['classification'] != 'CONFIRMED_NON_CREDENTIAL_BUILD_METADATA' for r in value['files'] for h in r['findings'])):
        raise ValueError('SCHEMA_FAILED')
    return value

def self_test():
    tree = ast.parse((HERE / 'scripts/knowhub25_a5_b2_ci.py').read_text())
    function = next(node for node in tree.body if isinstance(node, ast.FunctionDef) and node.name == 'scan_findings')
    scope = {'re': re, 'M': M}
    exec(compile(ast.Module(body=[function], type_ignores=[]), '<frozen scanner>', 'exec'), scope)
    for sample in ['token=unknown_value', 'token=\nunknown_value', 'Bearer unknown_value', 'token=123456789', 'packagefile go/token=/tmp/synthetic/archive', 'token=[REDACTED]'] + M['canaries']:
        exact = [{'ruleId': rule, 'count': len(pattern.findall(sample))} for rule, pattern in RULES if pattern.search(sample)]
        assert exact == scope['scan_findings'](sample)
    with tempfile.TemporaryDirectory(dir='.') as directory:
        root = pathlib.Path(directory)
        archive = root / '_pkg_.a'
        archive.write_bytes(b'!<arch>\n')
        build = 'packagefile go/token=' + pathlib.Path(os.path.relpath(archive)).as_posix()
        config = root / 'b001/importcfg'
        config.parent.mkdir()
        config.write_text(build + '\n')
        context = "cat >" + pathlib.Path(os.path.relpath(config)).as_posix() + " << 'EOF' # internal\n"
        def scan(text, role='GO_COMPILE_LOG'):
            return classify(text, role, root, ['abcdef...1234'])
        assert scan(context + build)[0]['classification'] == 'CONFIRMED_NON_CREDENTIAL_BUILD_METADATA'
        config.write_text('packagefile other/safe=invalid\n')
        assert scan(context + build)[0]['classification'] == 'UNKNOWN'
        config.write_text(build + '\n')
        assert scan("cat >/outside/b001/importcfg << 'EOF' # internal\n" + build)[0]['classification'] == 'UNKNOWN'
        assert scan(context + build + ' token=unknown_value')[0]['classification'] == 'UNKNOWN'
        for text in [build, context + build + ' token=unknown_value', context + 'packagefile go/token=/outside/archive', context + build + '\nEOF\n' + build, 'token=123456789', 'token=unknown_value']:
            assert any(r['classification'] == 'UNKNOWN' for r in scan(text))
        masked = json.dumps({'Action': 'output', 'Output': '[Yuque] client configured token=abcdef...1234 base=loopback'})
        assert scan(masked, 'GO_TEST_JSONL')[0]['classification'] == 'MASKED_CREDENTIAL_RELATED'
        mixed = masked + '\n' + json.dumps({'Action': 'output', 'Output': 'token=unknown_value'})
        assert any(r['classification'] == 'UNKNOWN' for r in scan(mixed, 'GO_TEST_JSONL'))
        across_lines = scan('token=\nunknown_value')
        assert across_lines == [{'ruleId': 'CREDENTIAL_ASSIGNMENT', 'count': 1, 'classification': 'UNKNOWN', 'basis': 'INSUFFICIENT_EVIDENCE'}]
        assert scan('Bearer unknown_value')[0]['classification'] == 'UNKNOWN'
        assert scan('{broken token=unknown_value', 'GO_TEST_JSONL')[0]['classification'] == 'UNKNOWN'
        value = {'schema': 1, 'status': 'BLOCKED', 'historicalStatus': 'UNKNOWN', 'executions': [], 'files': []}
        safe_summary(value)
        try:
            safe_summary(dict(value, raw='sensitive'))
        except ValueError:
            pass
        else:
            raise AssertionError('SCHEMA_FAILED')
        try:
            safe_summary(dict(value, status='ATTRIBUTION_CLEAR'))
        except ValueError:
            pass
        else:
            raise AssertionError('SCHEMA_FAILED')
    print(json.dumps({'selfTests': 18, 'status': 'PASS', 'frozenScannerEquivalence': 'PASS'}))

def run():
    if sys.platform != 'linux':
        raise ValueError('ENVIRONMENT_FAILED')
    os.umask(0o077)
    root = pathlib.Path(os.environ['RUNNER_TEMP']) / 'knowhub25-cloud-safety-private'
    root.mkdir(mode=0o700, exist_ok=False)
    public = pathlib.Path('cloud-safety-public')
    public.mkdir(exist_ok=False)
    result = {'schema': 1, 'status': 'DIAGNOSTIC_FAILED', 'historicalStatus': 'UNKNOWN', 'executions': [], 'files': []}
    try:
        env = dict(os.environ, CGO_ENABLED='1', GOTOOLCHAIN='local', GOFLAGS='-mod=readonly', GOTMPDIR=str(root / 'tmp'), GOCACHE=str(root / 'cache'), GOMODCACHE=str(root / 'modules'))
        for key in ['GOTMPDIR', 'GOCACHE', 'GOMODCACHE']:
            pathlib.Path(env[key]).mkdir(mode=0o700)
        version = subprocess.run(['go', 'version'], capture_output=True, env=env)
        if version.returncode or version.stdout.strip() != b'go version go1.26.7 linux/amd64':
            raise ValueError('ENVIRONMENT_FAILED')
        for side in ['BASELINE', 'CANDIDATE']:
            source = pathlib.Path(side.lower())
            identity(source, side)
            fixture_source = git(source, 'show', 'HEAD:internal/datasource/connector/yuque/client_test.go').decode()
            values = re.findall(r'(?:APIToken:\s*|rawToken\s*:=\s*)"([^"\n]+)"', fixture_source)
            masks = {v[:6] + '...' + v[-4:] for v in values if len(v) >= 12}
            for package, package_id, modes in [('internal/datasource/connector/yuque', 'PKG_017', ['NORMAL', 'RACE']), ('internal/middleware', 'PKG_001', ['COMPILE']), ('internal/router', 'PKG_008', ['COMPILE'])]:
                for mode in modes:
                    ident = f'{side}-{package_id}-{mode}'
                    command = ['go', 'test', '-mod=readonly', '-p', '2']
                    command += ['-c', '-x', '-work', '-o', str(root / (ident + '.test'))] if mode == 'COMPILE' else ['-count=1', '-json'] + (['-race'] if mode == 'RACE' else [])
                    command += ['./' + package]
                    outputs = [root / (ident + '.stdout'), root / (ident + '.stderr')]
                    with outputs[0].open('wb') as out, outputs[1].open('wb') as err:
                        process = subprocess.run(command, cwd=source, env=env, stdout=out, stderr=err, timeout=900)
                    result['executions'].append({'side': side, 'mode': mode, 'packageId': package_id, 'exit': process.returncode, 'status': 'PASS' if process.returncode == 0 else 'FAIL'})
                    for path in outputs:
                        raw = path.read_bytes()
                        findings = classify(raw.decode('utf-8', 'strict'), 'GO_COMPILE_LOG' if mode == 'COMPILE' else 'GO_TEST_JSONL', root, masks)
                        result['files'].append({'role': 'GO_COMPILE_LOG' if mode == 'COMPILE' else 'GO_TEST_JSONL', 'side': side, 'mode': mode, 'packageId': package_id, 'sha256': digest(raw), 'findings': findings})
        blocked = any(r['exit'] != 0 for r in result['executions']) or any(h['classification'] != 'CONFIRMED_NON_CREDENTIAL_BUILD_METADATA' for r in result['files'] for h in r['findings'])
        result['status'] = 'BLOCKED' if blocked else 'ATTRIBUTION_CLEAR'
    except Exception:
        result['status'] = 'DIAGNOSTIC_FAILED'
    (public / 'summary.json').write_text(json.dumps(safe_summary(result), indent=2) + '\n')
    return 0 if result['status'] == 'ATTRIBUTION_CLEAR' else 1

if __name__ == '__main__':
    if sys.argv[1:] == ['--self-test']:
        self_test()
    else:
        try:
            sys.exit(run())
        except Exception:
            # No exception text is ever published.
            sys.exit(1)
