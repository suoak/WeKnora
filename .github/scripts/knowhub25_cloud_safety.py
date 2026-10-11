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
BASES = {'ARCHIVE_AND_BUILD_CONTEXT', 'SOURCE_BOUND_MASK', 'INSUFFICIENT_EVIDENCE',
         'BUILD_BLOCK_ABSENT', 'BUILD_MAPPING_UNBOUND', 'ARCHIVE_UNVERIFIED', 'BUILD_WORK_UNRESOLVED', 'UNSUPPORTED_MATCH_SHAPE', 'VERIFIED_VET_IMPORT_MAP', 'VERIFIED_VET_ARCHIVE', 'VERIFIED_VET_FACT_PATH',
         'UNPROVEN_JSON_MAPPING', 'UNPROVEN_JSON_SCALAR', 'UNPROVEN_GO_COMMAND', 'UNPROVEN_SHELL_COMMAND',
         'UNPROVEN_VET_IMPORT_MAP', 'UNPROVEN_VET_ARCHIVE', 'UNPROVEN_VET_FACT_PATH'}

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

def vet_proofs(text, root, goroot):
    """Prove complete Go 1.26.7 vet objects against retained private files."""
    keys = {'ID', 'Compiler', 'Dir', 'ImportPath', 'GoFiles', 'NonGoFiles', 'IgnoredFiles',
            'ModulePath', 'ModuleVersion', 'ImportMap', 'PackageFile', 'Standard', 'PackageVetx',
            'VetxOnly', 'VetxOutput', 'Stdout', 'GoVersion', 'FixArchive', 'SucceedOnTypecheckFailure'}
    maps = {'ImportMap', 'PackageFile', 'Standard', 'PackageVetx'}
    def unique(pairs):
        value = {}
        for key, item in pairs:
            if key in value:
                raise ValueError('DUPLICATE_FIELD')
            value[key] = item
        return value
    def package(value):
        return isinstance(value, str) and re.fullmatch(r'[A-Za-z0-9._/-]+', value) and not any(p in {'', '.', '..'} for p in value.split('/'))
    lines = text.splitlines()
    proofs, unproven, work = {}, {}, None
    for index, line in enumerate(lines):
        marker = re.fullmatch(r'WORK=(\S+)', line)
        if marker:
            try:
                candidate = pathlib.Path(marker[1]).resolve(strict=True)
                work = candidate if candidate.is_dir() and candidate.is_relative_to(root.resolve()) else None
            except (OSError, ValueError):
                work = None
        header = re.fullmatch(r"cat >(\S+/b[0-9]+/vet\.cfg) << 'EOF' # internal", line)
        if not header:
            continue
        try:
            name = header[1]
            if name.startswith('$WORK/'):
                if work is None or not re.fullmatch(r'\$WORK/b[0-9]+/vet\.cfg', name):
                    continue
                name = str(work / name[6:])
            config_path = pathlib.Path(name).resolve(strict=True)
            if not config_path.is_relative_to(root.resolve()):
                continue
            end = lines.index('EOF', index + 1)
            body = '\n'.join(lines[index + 1:end]) + '\n'
            if config_path.read_bytes() != body.encode('utf-8'):
                continue
            config = json.loads(body, object_pairs_hook=unique)
            if not isinstance(config, dict) or set(config) != keys or config['Compiler'] != 'gc':
                continue
            if any(type(config[k]) is not bool for k in ['VetxOnly', 'SucceedOnTypecheckFailure']):
                continue
            if any(not isinstance(config[k], str) for k in keys - maps - {'GoFiles', 'NonGoFiles', 'IgnoredFiles', 'VetxOnly', 'SucceedOnTypecheckFailure'}):
                continue
            if any(config[k] is not None and (not isinstance(config[k], list) or any(not isinstance(v, str) for v in config[k])) for k in ['GoFiles', 'NonGoFiles', 'IgnoredFiles']):
                continue
            if any(not isinstance(config[k], dict) or any(not package(v) for v in config[k]) for k in maps):
                continue
            if any(not isinstance(v, str) for k in maps - {'Standard'} for v in config[k].values()) or any(type(v) is not bool for v in config['Standard'].values()):
                continue
            field = None
            for offset in range(index + 1, end):
                field_match = re.fullmatch(r'\t"(ImportMap|PackageFile|PackageVetx|Standard)": \{', lines[offset])
                if field_match:
                    field = field_match[1]
                    continue
                if re.fullmatch(r'\t\}[,]?', lines[offset]):
                    field = None
                    continue
                entry = re.fullmatch(r'\t\t("(?:[^"\\]|\\.)+"): ("(?:[^"\\]|\\.)*")[,]?', lines[offset])
                if not field or not entry:
                    continue
                key, value = json.loads(entry[1]), json.loads(entry[2])
                if config[field].get(key) != value:
                    continue
                if field in {'ImportMap', 'PackageFile', 'PackageVetx'}:
                    unproven[offset] = {'ImportMap': 'UNPROVEN_VET_IMPORT_MAP',
                                        'PackageFile': 'UNPROVEN_VET_ARCHIVE',
                                        'PackageVetx': 'UNPROVEN_VET_FACT_PATH'}[field]
                if field == 'ImportMap' and key == value and config['Standard'].get(key) is True and goroot is not None:
                    directory = (goroot / 'src' / key).resolve(strict=True)
                    if directory.is_relative_to((goroot / 'src').resolve()) and directory.is_dir() and any(directory.glob('*.go')):
                        proofs[offset] = 'VERIFIED_VET_IMPORT_MAP'
                elif field == 'PackageFile' and archive_is_safe(value, root):
                    proofs[offset] = 'VERIFIED_VET_ARCHIVE'
                elif field == 'PackageVetx':
                    fact = pathlib.Path(value).resolve(strict=True)
                    if fact.is_relative_to(root.resolve()) and fact.is_file() and (fact.name.endswith('.vetx') or re.fullmatch(r'[0-9a-f]{64}-d', fact.name)):
                        proofs[offset] = 'VERIFIED_VET_FACT_PATH'
        except (OSError, ValueError, TypeError, KeyError):
            continue
    return proofs, unproven

def classify(text, role, root, masks, goroot=None):
    """Classify every exact frozen-rule match; never suppress a match."""
    rows = []
    importcfg_lines = set()
    work = None
    def resolve_work(value):
        if value.startswith('$WORK/'):
            if work is None or not re.fullmatch(r'\$WORK/b[0-9]+/(?:importcfg(?:\.link)?|_pkg_\.a)', value):
                return None
            return str(work / value[6:])
        return value
    vetted, unproven_vet = vet_proofs(text, root, goroot) if role == 'GO_COMPILE_LOG' else ({}, {})
    for line_number, line in enumerate(text.splitlines()):
        marker = re.fullmatch(r'WORK=(\S+)', line)
        if marker and role == 'GO_COMPILE_LOG':
            try:
                candidate = pathlib.Path(marker[1]).resolve(strict=True)
                work = candidate if candidate.is_dir() and candidate.is_relative_to(root.resolve()) else None
            except (OSError, ValueError):
                work = None
        header = re.fullmatch(r"cat >(\S+/b[0-9]+/importcfg(?:\.link)?) << 'EOF' # internal", line)
        if line.startswith('cat '):
            importcfg_lines = set()
        if role == 'GO_COMPILE_LOG' and header:
            try:
                resolved = resolve_work(header[1])
                config_path = pathlib.Path(resolved).resolve(strict=True) if resolved else None
                if config_path is not None and config_path.is_relative_to(root.resolve()):
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
            basis = 'INSUFFICIENT_EVIDENCE'
            # Only a complete import package/archive relationship within a verified -x
            # importcfg block and an actual private archive can be proved safe.
            mapping = re.fullmatch(r'packagefile ([A-Za-z0-9._/-]+)=(\S+)', line)
            if rule == 'CREDENTIAL_ASSIGNMENT' and role == 'GO_COMPILE_LOG':
                if len(matches) != 1 or not mapping:
                    basis = 'UNSUPPORTED_MATCH_SHAPE'
                    if re.fullmatch(r'\s*"(?:[^"\\]|\\.)+": "(?:[^"\\]|\\.)*",?', line):
                        basis = 'UNPROVEN_JSON_MAPPING' if line.startswith('\t\t') else 'UNPROVEN_JSON_SCALAR'
                    elif re.match(r'\S+/(?:compile|link|vet)(?: |$)', line):
                        basis = 'UNPROVEN_GO_COMMAND'
                    elif re.match(r'(?:cp|mv|mkdir|cd|cat) ', line):
                        basis = 'UNPROVEN_SHELL_COMMAND'
                else:
                    resolved = resolve_work(mapping[2])
                    canonical = 'packagefile ' + mapping[1] + '=' + resolved if resolved else None
                    basis = 'BUILD_WORK_UNRESOLVED' if not resolved else 'BUILD_BLOCK_ABSENT' if not importcfg_lines else 'BUILD_MAPPING_UNBOUND' if line not in importcfg_lines and canonical not in importcfg_lines else 'ARCHIVE_UNVERIFIED' if not archive_is_safe(resolved, root) else 'ARCHIVE_AND_BUILD_CONTEXT'
                    if basis == 'ARCHIVE_AND_BUILD_CONTEXT':
                        category = 'CONFIRMED_NON_CREDENTIAL_BUILD_METADATA'
                if len(matches) == 1 and line_number in vetted:
                    category, basis = 'CONFIRMED_NON_CREDENTIAL_BUILD_METADATA', vetted[line_number]
                elif line_number in unproven_vet:
                    basis = unproven_vet[line_number]
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
            rows[-1]['basis'] = basis if category != 'MASKED_CREDENTIAL_RELATED' else rows[-1]['basis']
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
            if set(hit) != {'ruleId', 'count', 'classification', 'basis'} or hit['ruleId'] not in {r for r, _ in RULES} or type(hit['count']) is not int or hit['count'] < 1 or hit['classification'] not in CATEGORIES or hit['basis'] not in BASES:
                raise ValueError('SCHEMA_FAILED')
    expected = M.get('expected_executions', 8)
    allowed = {(s, m, p) for s in ['BASELINE', 'CANDIDATE']
               for p, modes in [('PKG_001', ['COMPILE']), ('PKG_008', ['COMPILE']),
                                ('PKG_017', [] if M.get('execution_scope') == 'COMPILE_ONLY' else ['NORMAL', 'RACE'])]
               for m in modes}
    if expected != len(allowed):
        raise ValueError('SCHEMA_FAILED')
    if len(value['executions']) > expected:
        raise ValueError('SCHEMA_FAILED')
    keys = {(r['side'], r['mode'], r['packageId']) for r in value['executions']}
    if len(keys) != len(value['executions']):
        raise ValueError('SCHEMA_FAILED')
    if not keys <= allowed:
        raise ValueError('SCHEMA_FAILED')
    file_keys = [(r['side'], r['mode'], r['packageId']) for r in value['files']]
    if any(file_keys.count(k) != 2 for k in set(file_keys)) or not set(file_keys) <= keys:
        raise ValueError('SCHEMA_FAILED')
    if any(r['role'] != ('GO_COMPILE_LOG' if r['mode'] == 'COMPILE' else 'GO_TEST_JSONL') for r in value['files']):
        raise ValueError('SCHEMA_FAILED')
    if value['status'] == 'ATTRIBUTION_CLEAR' and (len(keys) != expected or len(value['files']) != expected * 2 or any(r['exit'] != 0 for r in value['executions']) or any(h['classification'] != 'CONFIRMED_NON_CREDENTIAL_BUILD_METADATA' for r in value['files'] for h in r['findings'])):
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
        archive_dir = root / 'b002'
        archive_dir.mkdir()
        work_archive = archive_dir / '_pkg_.a'
        work_archive.write_bytes(b'!<arch>\n')
        work_build = 'packagefile example.test/compiler/token=' + pathlib.Path(os.path.relpath(work_archive)).as_posix()
        config.write_text(work_build + '\n')
        work_context = 'WORK=' + str(root.resolve()) + "\ncat >$WORK/b001/importcfg << 'EOF' # internal\n"
        assert scan(work_context + work_build)[0]['classification'] == 'CONFIRMED_NON_CREDENTIAL_BUILD_METADATA'
        assert scan(work_context + work_build + ' token=unknown_value')[0]['classification'] == 'UNKNOWN'
        assert scan("WORK=/outside\ncat >$WORK/b001/importcfg << 'EOF' # internal\n" + work_build)[0]['classification'] == 'UNKNOWN'
        work_archive.write_bytes(b'synthetic_nonarchive')
        assert scan(work_context + work_build)[0]['basis'] == 'ARCHIVE_UNVERIFIED'
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
        # Complete retained vet objects only; malformed, mixed and outside files
        # retain every frozen-rule hit and cannot inherit a metadata proof.
        goroot = root / 'go'
        (goroot / 'src/go/token').mkdir(parents=True)
        (goroot / 'src/go/token/token.go').write_text('package token\n')
        vet = {k: '' for k in ['ID', 'Compiler', 'Dir', 'ImportPath', 'ModulePath',
               'ModuleVersion', 'VetxOutput', 'Stdout', 'GoVersion', 'FixArchive']}
        vet.update(Compiler='gc', GoFiles=[], NonGoFiles=None, IgnoredFiles=None,
                   ImportMap={'go/token': 'go/token'}, PackageFile={},
                   Standard={'go/token': True}, PackageVetx={}, VetxOnly=False,
                   SucceedOnTypecheckFailure=False)
        vet_path = root / 'b001/vet.cfg'
        def vet_scan(value, suffix='', retain=True):
            body = json.dumps(value, indent='\t') + '\n'
            if retain:
                vet_path.write_bytes(body.encode())
            text = 'WORK=' + str(root.resolve()) + "\ncat >$WORK/b001/vet.cfg << 'EOF' # internal\n" + body + 'EOF\n' + suffix
            return classify(text, 'GO_COMPILE_LOG', root, [], goroot)
        assert vet_scan(vet)[0]['basis'] == 'VERIFIED_VET_IMPORT_MAP'
        mixed_vet = vet_scan(vet, 'token=unknown_value')
        assert any(r['classification'] == 'UNKNOWN' for r in mixed_vet)
        assert sum(r['count'] for r in mixed_vet) == len(ASSIGN.findall(
            json.dumps(vet, indent='\t') + '\ntoken=unknown_value'))
        invalid = dict(vet, Standard={'go/token': False})
        assert vet_scan(invalid)[0]['classification'] == 'UNKNOWN'
        vet_path.write_bytes(b'{}\n')
        assert vet_scan(vet, retain=False)[0]['classification'] == 'UNKNOWN'
        unknown = dict(vet, token='unknown_value')
        assert all(r['classification'] == 'UNKNOWN' for r in vet_scan(unknown))
        outside = dict(vet, ImportMap={}, PackageFile={'go/token': '/outside/archive'})
        assert vet_scan(outside)[0]['classification'] == 'UNKNOWN'
        archive.write_bytes(b'!<arch>\n')
        archived = dict(vet, ImportMap={}, PackageFile={'go/token': pathlib.Path(os.path.relpath(archive)).as_posix()})
        assert vet_scan(archived)[0]['basis'] == 'VERIFIED_VET_ARCHIVE'
        archive.write_bytes(b'not_an_archive')
        assert vet_scan(archived)[0]['classification'] == 'UNKNOWN'
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
    print(json.dumps({'selfTests': 31, 'status': 'PASS', 'frozenScannerEquivalence': 'PASS'}))

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
        toolroot = subprocess.run(['go', 'env', 'GOROOT'], capture_output=True, env=env)
        if toolroot.returncode:
            raise ValueError('ENVIRONMENT_FAILED')
        goroot = pathlib.Path(toolroot.stdout.decode().strip()).resolve(strict=True)
        for side in ['BASELINE', 'CANDIDATE']:
            source = pathlib.Path(side.lower())
            identity(source, side)
            fixture_source = git(source, 'show', 'HEAD:internal/datasource/connector/yuque/client_test.go').decode()
            values = re.findall(r'(?:APIToken:\s*|rawToken\s*:=\s*)"([^"\n]+)"', fixture_source)
            masks = {v[:6] + '...' + v[-4:] for v in values if len(v) >= 12}
            for package, package_id, modes in [('internal/datasource/connector/yuque', 'PKG_017', ['NORMAL', 'RACE']), ('internal/middleware', 'PKG_001', ['COMPILE']), ('internal/router', 'PKG_008', ['COMPILE'])]:
                for mode in modes:
                    if M.get('execution_scope') == 'COMPILE_ONLY' and mode != 'COMPILE':
                        continue
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
                        findings = classify(raw.decode('utf-8', 'strict'), 'GO_COMPILE_LOG' if mode == 'COMPILE' else 'GO_TEST_JSONL', root, masks, goroot)
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
