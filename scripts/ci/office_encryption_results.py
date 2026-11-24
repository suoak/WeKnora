"""Require actual passed tests; a skipped PostgreSQL test is not acceptance."""
import json
import sys

events = [json.loads(line) for line in open(sys.argv[1], encoding='utf-8') if line.startswith('{')]
failures = [event for event in events if event.get('Action') == 'fail']
required = {
    'TestOfficeCreateRejectsBeforeStorageDatabaseAndQueue',
    'TestOfficeReplaceRejectsBeforeDequeueOrOldVersionMutation',
    'TestOfficeMCPErrorUsesControlledCodeAndMessage',
    'TestOfficeSyncPostgresRetainsOldVersionAndContinuesBatch',
    'TestOfficePersonalPostgresFirstImportRejectsBeforeManifestOrKnowledge',
    'TestOfficePersonalPostgresEncryptedUpdatePreservesExistingPolicyAndVersion',
    'TestOfficeManifestRejectsEncryptedAttachmentsBeforeAnyWrite',
}
passed = {event.get('Test') for event in events if event.get('Action') == 'pass'}
if failures or required - passed:
    print(json.dumps({'failed': [{'package': e.get('Package'), 'test': e.get('Test')} for e in failures],
                      'missing_required_pass': sorted(required - passed)}, indent=2))
    sys.exit(1)
print(json.dumps({'required_tests_passed': sorted(required), 'skips': [
    {'package': e.get('Package'), 'test': e.get('Test')} for e in events if e.get('Action') == 'skip'
]}, indent=2))
