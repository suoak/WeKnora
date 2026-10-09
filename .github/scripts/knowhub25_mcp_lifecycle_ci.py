"""Draft: isolated whole-package characterization, not a remediation PASS gate."""
import hashlib
import json
import os
import platform
import subprocess
import sys
from pathlib import Path

BASELINE = 'ac7dbe5e2607af5ab4e74c062137e0cad48d0e50'
PATCH_SHA256 = '6d99f4278b605a2069fb77ced7696892b76f7571eed31d85b92d2e6f2fc319bf'
BLOBS = {
    'internal/middleware/mcp_token_lifecycle_http_test.go': 'cee5689b7b01a20d1f14be2ec3ed625e7e167f49',
    'internal/middleware/mcp_token_stale_write_security_test.go': '1ead383a540ae3d0f5c20ceeb755ae57d923fce9',
}
CI_FILES = {'.github/workflows/knowhub25-mcp-lifecycle-linux.yml', '.github/scripts/knowhub25_mcp_lifecycle_ci.py'}
PACKAGES = ['internal/middleware', 'internal/mcpserver', 'internal/application/service',
            'internal/application/repository', 'internal/application/access', 'internal/types',
            'internal/handler', 'internal/router', 'internal/mcp', 'internal/agent/tools']
RACE = ['internal/mcp', 'internal/mcpserver', 'internal/middleware']
SECURITY_REQUIRED = {'internal/mcp': ['TestBuiltinOAuthTenantConnectionIsolation', 'TestBuiltinOAuthWireScope', 'TestMCPClientDisconnectsWhenTheSessionIsGone', 'TestMCPHTTPAuthErrorsAreNotRetried', 'TestMCPManagerReconnectsWithoutReplayingToolCall', 'TestMCPSessionErrorClassification', 'TestMCPStateless404DoesNotResetConnection', 'TestManagerRealWaiterDeadlineDoesNotCancelInitialization', 'TestOAuthAuthorizationFailClosedStateAndRecovery', 'TestOAuthAuthorizationFailClosedStateAndRecovery/invalid-key', 'TestOAuthAuthorizationFailClosedStateAndRecovery/missing-key', 'TestOAuthAuthorizationFailClosedStateAndRecovery/public', 'TestOAuthAuthorizationFailClosedStateAndRecovery/sql-error', 'TestOAuthCallDoesNotRetryMoreThanOnce', 'TestOAuthCallRefreshesAndRetriesResource401Once', 'TestOAuthCallbackInvalidatesOnlyAuthenticatedStateScope', 'TestOAuthDiscoveryKeepsRedirectPolicyAndExistingPublicClient', 'TestOAuthDiscoveryKeepsRedirectPolicyAndExistingPublicClient/cross-origin', 'TestOAuthDiscoveryKeepsRedirectPolicyAndExistingPublicClient/same-origin', 'TestOAuthDiscoveryRejectsInvalidMetadataBeforeSideEffects', 'TestOAuthDiscoveryRejectsInvalidMetadataBeforeSideEffects/cancel', 'TestOAuthDiscoveryRejectsInvalidMetadataBeforeSideEffects/deadline', 'TestOAuthDiscoveryRejectsInvalidMetadataBeforeSideEffects/empty', 'TestOAuthDiscoveryRejectsInvalidMetadataBeforeSideEffects/empty-object', 'TestOAuthDiscoveryRejectsInvalidMetadataBeforeSideEffects/http-400', 'TestOAuthDiscoveryRejectsInvalidMetadataBeforeSideEffects/http-401', 'TestOAuthDiscoveryRejectsInvalidMetadataBeforeSideEffects/http-403', 'TestOAuthDiscoveryRejectsInvalidMetadataBeforeSideEffects/http-500', 'TestOAuthDiscoveryRejectsInvalidMetadataBeforeSideEffects/interrupted', 'TestOAuthDiscoveryRejectsInvalidMetadataBeforeSideEffects/malformed', 'TestOAuthDiscoveryRejectsInvalidMetadataBeforeSideEffects/missing-authorization', 'TestOAuthDiscoveryRejectsInvalidMetadataBeforeSideEffects/missing-registration', 'TestOAuthDiscoveryRejectsInvalidMetadataBeforeSideEffects/missing-token', 'TestOAuthDiscoveryRejectsInvalidMetadataBeforeSideEffects/null', 'TestOAuthDiscoveryRejectsInvalidMetadataBeforeSideEffects/unsafe-endpoint', 'TestOAuthDiscoveryUsesConsumerCacheAndGuardsRebuiltHandlers', 'TestOAuthMaliciousProviderErrorsAndLogsAreRedacted', 'TestOAuthMaliciousProviderErrorsAndLogsAreRedacted/discovery', 'TestOAuthMaliciousProviderErrorsAndLogsAreRedacted/exchange', 'TestOAuthMaliciousProviderErrorsAndLogsAreRedacted/registration', 'TestOAuthManagerClientStorageFailureIsNotAuthorizationSuccess', 'TestOAuthManagerKeepsTheRegisteredClientSecret', 'TestOAuthManagerKeepsTheRegisteredClientSecret/confidential', 'TestOAuthManagerKeepsTheRegisteredClientSecret/public', 'TestOAuthRefreshBadDiscoveryPreservesToken', 'TestOAuthRefreshRedactionPreservesClassification', 'TestOAuthRuntimeDeletesPermanentlyInvalidRefreshToken', 'TestOAuthRuntimePreservesTokenOnTemporaryRefreshFailure', 'TestOAuthRuntimeRefreshesExpiredToken', 'TestOAuthRuntimeSerializesRotatingRefreshToken', 'TestOAuthTokenStoreErrorsAreSafeAndScoped', 'TestOAuthTokenStoreErrorsAreSafeAndScoped/cancel-read', 'TestOAuthTokenStoreErrorsAreSafeAndScoped/cancel-write', 'TestOAuthTokenStoreErrorsAreSafeAndScoped/managed-read', 'TestOAuthTokenStoreErrorsAreSafeAndScoped/read', 'TestOAuthTokenStoreErrorsAreSafeAndScoped/write'], 'internal/types': [], 'internal/application/service': [], 'internal/handler': ['TestOAuthAPIAndAccessLogsDoNotLeakProviderOrSQL', 'TestOAuthAPIAndAccessLogsDoNotLeakProviderOrSQL/callback', 'TestOAuthAPIAndAccessLogsDoNotLeakProviderOrSQL/provider', 'TestOAuthAPIAndAccessLogsDoNotLeakProviderOrSQL/revoke', 'TestOAuthAPIAndAccessLogsDoNotLeakProviderOrSQL/sql', 'TestOAuthAPIAndAccessLogsDoNotLeakProviderOrSQL/status', 'TestOAuthRevokeSuccessClearsOnlyAuthenticatedScope'], 'internal/application/repository': ['TestMCPOAuthClientFailClosedSQL', 'TestMCPOAuthClientFailClosedSQL/entropy', 'TestMCPOAuthClientFailClosedSQL/forged-prefix', 'TestMCPOAuthClientFailClosedSQL/invalid', 'TestMCPOAuthClientFailClosedSQL/missing', 'TestMCPOAuthClientFailClosedSQL/public', 'TestMCPOAuthClientFailClosedSQL/valid', 'TestMCPOAuthRepositorySQLLogsAreRedacted'], 'internal/application/access': [], 'internal/middleware': ['TestOAuthProviderErrorQueryRedaction'], 'internal/agent/tools': []}
CHARACTERIZATION = {'TestMCPEndpointLifecycleHTTPCharacterization/' + case for case in
                    ['creator-demoted', 'creator-removed', 'creator-disabled', 'share-revoked',
                     'cross-tenant', 'admin-revokes', 'admin-rotates']}
REQUIRED = CHARACTERIZATION | {'TestMCPEndpointStaleConfigWriteCannotRestoreRotatedKey',
                               'TestMCPEndpointStaleWriteCannotResurrectDeletedEndpoint'}

def sha(data):
    return hashlib.sha256(data).hexdigest()

def git(root, *args):
    return subprocess.check_output(['git', '-C', str(root), *args])

def save(path, value):
    path.write_text(json.dumps(value, indent=2) + '\n')

def require(ok, message):
    if not ok:
        raise RuntimeError(message)

def parse(log, compile_exit, test_exit, required=()):
    events = []
    for line in log.splitlines():
        try:
            events.append(json.loads(line))
        except ValueError:
            pass
    names = {action: {e['Test'] for e in events if e.get('Action') == action and 'Test' in e}
             for action in ['run', 'pass', 'fail', 'skip']}
    missing = set(required) - names['pass']
    package_pass = any(e.get('Action') == 'pass' and 'Test' not in e for e in events)
    ok = compile_exit == 0 and test_exit == 0 and package_pass and bool(names['run']) and not names['fail'] and not missing
    return {'compileExit': compile_exit, 'testExit': test_exit, 'gatePass': ok,
            'status': 'PASS' if ok else 'COMPILE_FAILED_NOT_EXECUTED' if compile_exit else 'BLOCKED',
            'run': len(names['run']), 'topLevel': sum('/' not in n for n in names['run']),
            'pass': len(names['pass']), 'fail': sorted(names['fail']), 'skip': sorted(names['skip']),
            'requiredMissing': sorted(missing), 'requiredPassed': sorted(set(required) & names['pass'])}

def execute(root, command, log):
    with log.open('wb') as output:
        result = subprocess.run(command, cwd=root, stdout=output, stderr=subprocess.STDOUT)
    return result.returncode

def main():
    workspace = Path(os.environ['GITHUB_WORKSPACE'])
    evidence = workspace / 'evidence'
    evidence.mkdir(exist_ok=True)
    result = {'purpose': 'risk characterization, not product remediation', 'gate': 'BLOCKED', 'results': {}}
    try:
        require(platform.system() == 'Linux' and platform.machine() == 'x86_64', 'Runner architecture mismatch')
        require(os.environ.get('CGO_ENABLED') == '1' and os.environ.get('GOTOOLCHAIN') == 'local', 'CGO/toolchain mismatch')
        for key in ['DATABASE_URL', 'DB_DSN', 'POSTGRES_DSN', 'SYSTEM_AES_KEY']:
            require(not os.environ.get(key), 'Unexpected credential environment: ' + key)
        version = subprocess.check_output(['go', 'version']).decode()
        require(version.strip() == 'go version go1.26.7 linux/amd64', 'Go version mismatch')
        save(evidence / 'environment.json', {'go': version,
             'goEnv': subprocess.check_output(['go', 'env', 'GOOS', 'GOARCH', 'CGO_ENABLED', 'CC', 'GOTOOLCHAIN', 'GOMODCACHE']).decode(),
             'gcc': subprocess.check_output(['gcc', '--version']).decode()})
        candidate, baseline = workspace / 'candidate', workspace / 'baseline'
        candidate_sha = git(candidate, 'rev-parse', 'HEAD').decode().strip()
        require(candidate_sha == os.environ['GITHUB_SHA'], 'Candidate SHA mismatch')
        require(git(candidate, 'rev-parse', 'HEAD^').decode().strip() == BASELINE, 'Candidate parent mismatch')
        require(git(baseline, 'rev-parse', 'HEAD').decode().strip() == BASELINE, 'Baseline mismatch')
        paths = set(git(candidate, 'diff', '--name-only', BASELINE, 'HEAD').decode().splitlines())
        require(paths == set(BLOBS) | CI_FILES, 'Candidate whitelist mismatch')
        for name, blob in BLOBS.items():
            require(git(candidate, 'rev-parse', 'HEAD:' + name).decode().strip() == blob, 'Source blob mismatch: ' + name)
        # Keep Git path ordering identical to the frozen scratch-index manifest.
        patch = git(candidate, 'diff', '--binary', '--full-index', BASELINE, 'HEAD', '--', *sorted(BLOBS))
        require(sha(patch) == PATCH_SHA256, 'Source patch mismatch')
        result.update(candidate=candidate_sha, baseline=BASELINE, tree=git(candidate, 'rev-parse', 'HEAD^{tree}').decode().strip(), patchSHA256=sha(patch))
        dependencies = []
        for label, root in [('baseline', baseline), ('candidate', candidate)]:
            require(not git(root, 'status', '--porcelain'), label + ' checkout dirty')
            locked = {p: sha((root / p).read_bytes()) for p in ['go.mod', 'go.sum']}
            dependencies.append(locked)
            result['results'][label] = {}
            for package in PACKAGES:
                folder = evidence / label / package.replace('/', '_')
                folder.mkdir(parents=True)
                compile_code = execute(root, ['go', 'test', '-mod=readonly', '-p', '2', '-c', '-x', '-o', str(folder / 'package.test'), './' + package], folder / 'compile.log')
                test_code = None
                if compile_code == 0:
                    test_code = execute(root, ['go', 'test', '-mod=readonly', '-p', '2', '-count=1', '-json', './' + package], folder / 'test.jsonl')
                log = (folder / 'test.jsonl').read_text(errors='replace') if test_code is not None else ''
                required = set(SECURITY_REQUIRED.get(package, ())) | (REQUIRED if label == 'candidate' and package == 'internal/middleware' else set())
                record = parse(log, compile_code, test_code, required)
                result['results'][label][package] = record
                save(folder / 'result.json', record)
                save(evidence / 'comparison.json', result)
            for package in RACE:
                folder = evidence / label / ('race_' + package.replace('/', '_'))
                folder.mkdir()
                code = execute(root, ['go', 'test', '-mod=readonly', '-p', '2', '-count=1', '-race', '-json', './' + package], folder / 'test.jsonl')
                required = set(SECURITY_REQUIRED.get(package, ())) | (REQUIRED if label == 'candidate' and package == 'internal/middleware' else set())
                record = parse((folder / 'test.jsonl').read_text(errors='replace'), None, code, required)
                # Race go test compiles and tests in one invocation; no standalone race compile claim.
                record['gatePass'] = code == 0 and record['run'] > 0 and not record['fail'] and not record['requiredMissing'] and any(
                    json.loads(line).get('Action') == 'pass' and 'Test' not in json.loads(line)
                    for line in (folder / 'test.jsonl').read_text().splitlines() if line.startswith('{'))
                record['status'] = 'PASS' if record['gatePass'] else 'BLOCKED'
                result['results'][label]['race:' + package] = record
                save(folder / 'result.json', record)
                save(evidence / 'comparison.json', result)
            require(not git(root, 'status', '--porcelain'), label + ' source mutated')
            require(locked == {p: sha((root / p).read_bytes()) for p in locked}, 'Dependencies mutated')
        require(dependencies[0] == dependencies[1], 'Dependency drift')
        verify = execute(candidate, ['go', 'mod', 'verify'], evidence / 'module-verify.log')
        require(verify == 0, 'Module cache integrity failed')
        # Bearer values are generated at runtime, never intentional credential canaries in logs.
        # Fail conservatively if any full endpoint bearer-shaped value is printed.
        import re
        leaks = []
        for log in evidence.rglob('*'):
            if log.suffix in {'.log', '.jsonl', '.txt'}:
                content = log.read_text(errors='replace')
                pattern = r'(?<![A-Za-z0-9_])mcp_[A-Za-z0-9_-]{43}(?![A-Za-z0-9_-])'
                if re.search(pattern, content):
                    leaks.append(str(log.relative_to(evidence)))
                    # Preserve the leakage finding, but never publish a usable synthetic bearer.
                    log.write_text(re.sub(pattern, '[REDACTED_ENDPOINT_BEARER]', content))
        result['possibleCredentialLeakFiles'] = leaks
        ok = not leaks and all(r['gatePass'] for values in result['results'].values() for r in values.values())
        result['gate'] = 'LINUX_CHARACTERIZATION_COMPLETED' if ok else 'BLOCKED'
        result['remediationGate'] = 'NOT_IMPLEMENTED'
        result['securityRegressionManualReviewRequired'] = True
        return 0 if ok else 1
    except Exception as error:
        result['error'] = str(error)
        return 1
    finally:
        # Also sanitize on identity/test/infrastructure exceptions before artifact upload.
        import re
        for log in evidence.rglob('*'):
            if log.suffix in {'.log', '.jsonl', '.txt'}:
                content = log.read_text(errors='replace')
                pattern = r'(?<![A-Za-z0-9_])mcp_[A-Za-z0-9_-]{43}(?![A-Za-z0-9_-])'
                if re.search(pattern, content):
                    name = str(log.relative_to(evidence))
                    if name not in result.setdefault('possibleCredentialLeakFiles', []):
                        result['possibleCredentialLeakFiles'].append(name)
                    log.write_text(re.sub(pattern, '[REDACTED_ENDPOINT_BEARER]', content))
                    result['gate'] = 'BLOCKED'
        save(evidence / 'comparison.json', result)
        save(evidence / 'artifact-sha256.json', {str(p.relative_to(evidence)): sha(p.read_bytes())
             for p in evidence.rglob('*') if p.is_file() and p.suffix in {'.json', '.jsonl', '.log', '.txt'} and p.name != 'artifact-sha256.json'})

if __name__ == '__main__':
    sys.exit(main())
