"""Review draft only; no CI commit or push authorized. Whole-package SQLite/module regression, not PostgreSQL or frontend acceptance."""
import hashlib
import json
import os
import platform
import subprocess
import sys
from pathlib import Path

BASELINE = '620080ddf5961a254b9a90b173d7ec8036fcae9b'
PARENT = '620080ddf5961a254b9a90b173d7ec8036fcae9b'
PATCH_SHA256 = '41a37a232c056c49a2e5acc1ace2db1360f689e332b8213b27826746b32e0361'
BLOBS = {'frontend/src/views/integrations/McpServerIntegrationSettings.vue': 'dfc9cdfcf3ae9c574f70dfa9808ec42709f3456c', 'frontend/src/views/integrations/mcpKnowledgeBaseOptions.ts': 'abf0d4f41d87770297060e2789df900d0493b5b8', 'frontend/src/views/integrations/mcpKnowledgeBaseOptions.test.ts': '8e344795e962d657673a32fed1da9a48e23f2354', 'frontend/src/i18n/locales/zh-CN.ts': '08728c2b7aaa3f344fe1d9a865589ebe26c36d22', 'frontend/src/i18n/locales/en-US.ts': 'af3986ab3f02867fa231a737f5530ccddd29849d', 'frontend/src/i18n/locales/ja-JP.ts': '8a1281d6808f19b46ef1203eabea1cc2f4a48358', 'frontend/src/i18n/locales/ko-KR.ts': '982d5ba1816cfb29d92ecc27ae90a990a1f08a6d', 'frontend/src/i18n/locales/ru-RU.ts': 'c9f0e6d2d7feb0d77a3b76421a504c63f72b7fb6', 'frontend/tests/agent-editor/final-gate.config.ts': 'e72ff6719e3545423e9cc4ddbc7ac93f79259cb3', 'frontend/tests/agent-editor/run-gate.mjs': 'bc280a5a5d3bb3d048de13a95c49cf38e5466fbb', 'frontend/tests/agent-editor/mcp-scope.spec.ts': 'dcbd1c9cb2e9b72ca8f4182ddd5f243325096d84', 'internal/mcpserver/shared_scope_revalidation_test.go': 'b9e481a32c05a1d628915f924bc00a6b96199968', 'internal/application/service/mcp_endpoint_shared_revalidation_test.go': '8edcd03702cb31765aa0ad79443e46673593190b'}
CI_FILES = {'.github/scripts/knowhub25_gh_a23_combined_ci.py', '.github/workflows/knowhub25-gh-a23-combined-linux.yml'}
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

FIX_REQUIRED = {'internal/middleware': ['TestMCPEndpointHTTPConditionalConfigInterleavings', 'TestMCPEndpointRotationConditionalInterleavings', 'TestMCPEndpointHTTPZeroValuesAndToolsPolicy', 'TestMCPEndpointDeleteAfterCommittedRotation', 'TestMCPEndpointHTTPStorageErrorDoesNotPublishToken', 'TestMCPEndpointHTTPConditionalConfigInterleavings/config', 'TestMCPEndpointHTTPConditionalConfigInterleavings/disable', 'TestMCPEndpointHTTPConditionalConfigInterleavings/scope', 'TestMCPEndpointHTTPConditionalConfigInterleavings/tools', 'TestMCPEndpointHTTPConditionalConfigInterleavings/rotate', 'TestMCPEndpointHTTPConditionalConfigInterleavings/delete', 'TestMCPEndpointRotationConditionalInterleavings/config', 'TestMCPEndpointRotationConditionalInterleavings/rotate', 'TestMCPEndpointRotationConditionalInterleavings/delete'], 'internal/application/repository': ['TestMCPEndpointSQLWhitelistCASAndTouch', 'TestMCPEndpointMissingAndForeignConditionalWrites', 'TestMCPEndpointExplicitTransactionRollback', 'TestMCPEndpointPostgresConditionalWriteFailures', 'TestMCPEndpointPostgresConditionalWriteFailures/config/zero-rows', 'TestMCPEndpointPostgresConditionalWriteFailures/config/execute', 'TestMCPEndpointPostgresConditionalWriteFailures/config/commit', 'TestMCPEndpointPostgresConditionalWriteFailures/rotate/zero-rows', 'TestMCPEndpointPostgresConditionalWriteFailures/rotate/execute', 'TestMCPEndpointPostgresConditionalWriteFailures/rotate/commit', 'TestMCPEndpointPostgresConditionalWriteFailures/delete/zero-rows', 'TestMCPEndpointPostgresConditionalWriteFailures/delete/execute', 'TestMCPEndpointPostgresConditionalWriteFailures/delete/commit']}

A23_REQUIRED = {'internal/application/service': ['TestMCPEndpointSharedSaveRevalidation', 'TestMCPEndpointCreatorDowngradeDoesNotRevokeMachineToken', 'TestMCPEndpointSharedSaveRevocationInterleavings', 'TestMCPEndpointSharedSaveRevocationInterleavings/before-check', 'TestMCPEndpointSharedSaveRevocationInterleavings/during-check', 'TestMCPEndpointSharedSaveRevocationInterleavings/after-check-before-commit'], 'internal/mcpserver': ['TestMCPEndpointLiveSharedScopeRevalidation', 'TestA23MCPEndpointManagementHTTPPermissions', 'TestA23SharedRevocationAfterCheckBeforeCommitMCPHTTP']}
def requirements(label, package):
    result = set(SECURITY_REQUIRED.get(package, ())) | set(FIX_REQUIRED.get(package, ()))
    if package == 'internal/middleware': result |= REQUIRED
    if label == 'candidate': result |= set(A23_REQUIRED.get(package, ()))
    return result

CANARIES = ['synthetic-client-secret', 'synthetic-access-token', 'synthetic-refresh-token', 'synthetic-password', 'synthetic-secret', 'synthetic-access', 'synthetic-refresh', 'registered-secret', 'other-secret', 'tenant-a-token', 'tenant-b-token', 'other-principal-token', 'synthetic-revoke-a', 'synthetic-revoke-b', 'synthetic-revoke-other', 'a23-isolated-password', 'a23-isolated-signing-key-for-tests-only', 'synthetic-foreign-only']

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

def management_subcases(record, log):
    events = []
    for line in log.splitlines():
        try: events.append(json.loads(line))
        except ValueError: pass
    counts = {}
    for group in ['viewer-', 'machine-is-not-manager-']:
        prefix = 'TestA23MCPEndpointManagementHTTPPermissions/' + group
        names = {a: {e.get('Test') for e in events if e.get('Action') == a and e.get('Test', '').startswith(prefix)} for a in ['run', 'pass']}
        counts[group] = {'run': len(names['run']), 'pass': len(names['pass'])}
        if len(names['run']) != 4 or names['run'] != names['pass']:
            record['gatePass'] = False
            record['status'] = 'BLOCKED'
    record['managementSubcaseCoverage'] = counts
    return record


def execute(root, command, log):
    with log.open('wb') as output:
        result = subprocess.run(command, cwd=root, stdout=output, stderr=subprocess.STDOUT)
    return result.returncode

def identity(root, expected_sha, expected_tree, expected_patch=PATCH_SHA256):
    root = Path(root)
    require(len(expected_sha) == 40 and len(expected_tree) == 40, 'Full SHA/tree required')
    require(git(root, 'rev-parse', 'HEAD').decode().strip() == expected_sha, 'Candidate SHA mismatch')
    require(git(root, 'rev-parse', 'HEAD^').decode().strip() == PARENT, 'Candidate parent mismatch')
    require(git(root, 'rev-parse', 'HEAD^{tree}').decode().strip() == expected_tree, 'Candidate tree mismatch')
    require(not git(root, 'status', '--porcelain', '--untracked-files=all'), 'Candidate checkout dirty')
    paths = set(git(root, 'diff', '--name-only', PARENT, 'HEAD').decode().splitlines())
    require(paths == set(BLOBS) | CI_FILES, 'Candidate whitelist mismatch')
    for name, blob in BLOBS.items():
        require(git(root, 'rev-parse', 'HEAD:' + name).decode().strip() == blob, 'Source blob mismatch: ' + name)
    patch = git(root, 'diff', '--binary', '--abbrev=9', '--no-ext-diff', '--no-textconv', PARENT, 'HEAD', '--', *sorted(BLOBS))
    require(sha(patch) == expected_patch, 'Source patch mismatch')
    require(not git(root, 'diff', '--name-only', PARENT, 'HEAD', '--', 'go.mod', 'go.sum', 'migrations', 'frontend/package-lock.json'), 'Dependency or migration drift')
    git(root, 'diff', '--check', PARENT, 'HEAD')
    return {'identity': 'PASS', 'candidate': expected_sha, 'parent': PARENT, 'tree': expected_tree, 'patchSHA256': sha(patch), 'files': len(paths), 'testsExecuted': False}


def identity_cli():
    import argparse
    parser = argparse.ArgumentParser(description='Real Git identity only; never a Linux test PASS')
    parser.add_argument('--identity-only', action='store_true', required=True)
    parser.add_argument('--root', required=True)
    parser.add_argument('--expected-sha', required=True)
    parser.add_argument('--expected-tree', required=True)
    args = parser.parse_args()
    try:
        print(json.dumps(identity(args.root, args.expected_sha, args.expected_tree), indent=2))
        return 0
    except Exception as error:
        print(json.dumps({'identity': 'BLOCKED', 'error': str(error), 'testsExecuted': False}))
        return 1


def main():
    workspace = Path(os.environ['GITHUB_WORKSPACE'])
    evidence = workspace / 'evidence'
    evidence.mkdir(exist_ok=True)
    result = {'purpose': 'G/H accepted baseline plus frozen A2.3 regression; not production/PG acceptance', 'gate': 'BLOCKED', 'results': {}}
    try:
        require(platform.system() == 'Linux' and platform.machine() == 'x86_64', 'Runner architecture mismatch')
        require(os.environ.get('CGO_ENABLED') == '1' and os.environ.get('GOTOOLCHAIN') == 'local', 'CGO/toolchain mismatch')
        for key in ['DATABASE_URL', 'DB_DSN', 'POSTGRES_DSN', 'SYSTEM_AES_KEY', 'JWT_SECRET']:
            require(not os.environ.get(key), 'Unexpected credential environment: ' + key)
        version = subprocess.check_output(['go', 'version']).decode()
        require(version.strip() == 'go version go1.26.7 linux/amd64', 'Go version mismatch')
        save(evidence / 'environment.json', {'go': version,
             'goEnv': subprocess.check_output(['go', 'env', 'GOOS', 'GOARCH', 'CGO_ENABLED', 'CC', 'GOTOOLCHAIN', 'GOMODCACHE']).decode(),
             'gcc': subprocess.check_output(['gcc', '--version']).decode()})
        candidate, baseline = workspace / 'candidate', workspace / 'baseline'
        candidate_sha = os.environ['GITHUB_SHA']
        # The Actions checkout ref is the trusted run SHA; resolve its committed tree.
        candidate_tree = git(candidate, 'rev-parse', candidate_sha + '^{tree}').decode().strip()
        checked = identity(candidate, candidate_sha, candidate_tree)
        require(git(baseline, 'rev-parse', 'HEAD').decode().strip() == BASELINE, 'Baseline mismatch')
        result.update(candidate=candidate_sha, baseline=BASELINE, tree=checked['tree'], patchSHA256=checked['patchSHA256'])
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
                required = requirements(label, package)
                record = parse(log, compile_code, test_code, required)
                if label == 'candidate' and package == 'internal/mcpserver':
                    record = management_subcases(record, log)
                result['results'][label][package] = record
                save(folder / 'result.json', record)
                save(evidence / 'comparison.json', result)
            for package in RACE:
                folder = evidence / label / ('race_' + package.replace('/', '_'))
                folder.mkdir()
                code = execute(root, ['go', 'test', '-mod=readonly', '-p', '2', '-count=1', '-race', '-json', './' + package], folder / 'test.jsonl')
                required = requirements(label, package)
                record = parse((folder / 'test.jsonl').read_text(errors='replace'), None, code, required)
                # Race go test compiles and tests in one invocation; no standalone race compile claim.
                record['gatePass'] = code == 0 and record['run'] > 0 and not record['fail'] and not record['requiredMissing'] and any(
                    json.loads(line).get('Action') == 'pass' and 'Test' not in json.loads(line)
                    for line in (folder / 'test.jsonl').read_text().splitlines() if line.startswith('{'))
                record['status'] = 'PASS' if record['gatePass'] else 'BLOCKED'
                if label == 'candidate' and package == 'internal/mcpserver':
                    record = management_subcases(record, (folder / 'test.jsonl').read_text(errors='replace'))
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
                pattern = r'(?<![A-Za-z0-9_])mcp_[A-Za-z0-9_-]{43}(?![A-Za-z0-9_-])' + '|' + '|'.join(re.escape(value) for value in CANARIES)
                if re.search(pattern, content):
                    leaks.append(str(log.relative_to(evidence)))
                    # Preserve the leakage finding, but never publish a usable synthetic bearer.
                    log.write_text(re.sub(pattern, '[REDACTED_ENDPOINT_BEARER]', content))
        result['possibleCredentialLeakFiles'] = leaks
        ok = not leaks and all(r['gatePass'] for values in result['results'].values() for r in values.values())
        result['gate'] = 'KNOWHUB25_GH_A23_LINUX_MODULES_PASS' if ok else 'BLOCKED'
        result['remediationGate'] = 'PRODUCTION_REVIEW_PENDING'
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
                pattern = r'(?<![A-Za-z0-9_])mcp_[A-Za-z0-9_-]{43}(?![A-Za-z0-9_-])' + '|' + '|'.join(re.escape(value) for value in CANARIES)
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
    sys.exit(identity_cli() if '--identity-only' in sys.argv[1:] else main())
