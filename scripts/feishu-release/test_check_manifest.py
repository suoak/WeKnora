import copy
import datetime as dt
import unittest

from check_manifest import Invalid, check, effective_setting


NOW = dt.datetime(2026, 10, 23, 12, tzinfo=dt.timezone.utc)
SOURCE = '5c7bc873-0ba3-43c7-90a0-32c23e6bd529'


def fixture():
    # Synthetic isolated evidence; this is not a production release approval.
    return {
        'schema': 1, 'observed_at': NOW.isoformat(), 'max_age_seconds': 120,
        'inventory_complete': True, 'inventory_evidence': 'fixture/cmdb',
        'expected_sha': '7' * 40, 'stage': 'scope-none', 'allowed_source_ids': 'none',
        'expected_instances': ['app-a'], 'expected_redis_endpoints': ['isolated/redis-db0'],
        'instances': [{
            'id': 'app-a', 'redis_endpoint': 'isolated/redis-db0', 'sha': '7' * 40,
            'scope': 'none', 'scope_supported': True, 'scheduler': True, 'worker': True,
            'maintenance_db': None, 'maintenance_env': '1', 'model_db': None,
            'model_env': '8', 'observed_maintenance': 1,
            'control_policy_sha256': 'a' * 64, 'evidence': 'fixture/runtime-app-a',
            'safety': {'enabled': True, 'worker_id': 'app-a', 'expected_workers': ['app-a'],
                       'source_sha': '7' * 40, 'alert_url': 'https://alerts.example.test/feishu', 'policy': {
                           'Namespace': 'isolated-fixture', 'EndpointHost': 'fake-gateway.test',
                           'InFlight': 1, 'RPM': 2, 'RPS': 1, 'TPM': 10,
                           'WaitBudgetDuration': 65000000000,
                           'TokenBoundVerified': True, 'TokenOverhead': 0}},
        }],
        'redis_endpoints': [{
            'id': 'isolated/redis-db0', 'evidence': 'fixture/asynq', 'sync_paused': True,
            'sync_active': 0, 'live_pools': [{
                'id': 'asynq-a', 'instance_id': 'app-a', 'pool': 'maintenance',
                'concurrency': 1, 'queues': {'sync': 2, 'low': 1},
            }],
        }],
        'controls': {
            'gateway_budget_confirmed': True, 'hard_egress_verified': True,
            'controller_e2e_verified': True, 'metrics_fresh': True,
            'watchdog_fail_closed': True, 'pause_authority_exercised_in_isolation': True,
            'fencing_verified': True, 'backup_restore_verified': True,
            'startup_smoke_verified': True, 'evidence': 'fixture/receipts',
        },
        'canary': {
            'source_uuid': None, 'noncritical_confirmed': False, 'no_parent_child': False,
            'version_safe': False, 'read_export_acl_verified': False,
            'dns_verified': False, 'evidence': 'not-selected',
        },
    }


class GateTests(unittest.TestCase):
    def test_scope_none(self):
        result = check(fixture(), NOW)
        self.assertEqual(result['decision'], 'MANIFEST-CONSISTENT')
        self.assertFalse(result['authorization_to_deploy'])
        self.assertEqual(result['aggregate_maintenance_slots_shared_with_low'], 1)

    def test_single_source(self):
        m = fixture()
        m['stage'], m['allowed_source_ids'] = 'single-source-ready', SOURCE
        m['instances'][0]['scope'] = SOURCE
        m['canary'] = {k: True for k in m['canary']}
        m['canary'].update(source_uuid=SOURCE, evidence='fixture/acl-dns-owner')
        self.assertEqual(check(m, NOW)['decision'], 'MANIFEST-CONSISTENT')
        m['canary']['version_safe'] = 'UNKNOWN'
        self.assertEqual(check(m, NOW)['decision'], 'NO-GO')

    def test_settings_precedence(self):
        cases = [
            (None, '2', 4, True, (2, 'ENV')),
            (None, None, 4, True, (4, 'default')),
            ({'value': 3, 'last_modified_by': 'admin'}, '2', 4, True, (3, 'DB')),
            ({'value': 4, 'last_modified_by': ''}, '2', 4, True, (2, 'ENV')),
            ({'value': '4', 'last_modified_by': ''}, '2', 4, True, (4, 'DB')),
            ({'value': 4, 'last_modified_by': 'admin'}, '2', 4, True, (4, 'DB')),
            ({'value': 'broken', 'last_modified_by': 'admin'}, '2', 4, True, (2, 'ENV')),
            (None, '0', 4, True, (4, 'default')),
            (None, '-1', 32, False, (-1, 'ENV')),
            (None, ' 2', 4, True, (4, 'default')),
            ({'value': True, 'last_modified_by': ''}, '2', 4, True, (2, 'ENV')),
        ]
        for row, env, default, positive, expected in cases:
            with self.subTest(row=row, env=env):
                self.assertEqual(effective_setting(row, env, default, positive), expected)

    def test_mismatches_fail_closed(self):
        mutations = [
            lambda m: m.update(inventory_complete=False),
            lambda m: m.update(expected_instances=['app-a', 'unseen-app']),
            lambda m: m.update(expected_redis_endpoints=['other/redis']),
            lambda m: m.update(observed_at='2026-10-22T12:00:00+00:00'),
            lambda m: m['instances'][0].update(scope=''),
            lambda m: m['instances'][0].update(scope_supported=False),
            lambda m: m['instances'][0].update(sha='8' * 40),
            lambda m: m['instances'][0].update(observed_maintenance=4),
            lambda m: m['instances'][0]['safety'].update(enabled=False),
            lambda m: m['instances'][0]['safety']['policy'].update(TokenBoundVerified=False),
            lambda m: m['redis_endpoints'][0].update(sync_paused=False),
            lambda m: m['redis_endpoints'][0].update(sync_active=1),
            lambda m: m['redis_endpoints'][0].update(live_pools=[]),
            lambda m: m['redis_endpoints'][0]['live_pools'][0].update(queues={'sync': 1}),
            lambda m: m['redis_endpoints'][0]['live_pools'][0].update(instance_id='old-worker'),
            lambda m: m['controls'].update(watchdog_fail_closed='UNKNOWN'),
            lambda m: m['controls'].update(controller_e2e_verified=False),
            lambda m: m['controls'].update(pause_authority_exercised_in_isolation='ACL_DRYRUN_ONLY'),
        ]
        for mutate in mutations:
            m = fixture(); mutate(m)
            with self.subTest(mutate=mutate):
                self.assertEqual(check(m, NOW)['decision'], 'NO-GO')

    def test_multi_replica_policy_drift(self):
        m = fixture()
        other = copy.deepcopy(m['instances'][0])
        other['id'], other['control_policy_sha256'] = 'app-b', 'b' * 64
        m['expected_instances'].append('app-b'); m['instances'].append(other)
        for instance in m['instances']:
            instance['safety']['worker_id'] = instance['id']
            instance['safety']['expected_workers'] = ['app-a', 'app-b']
        pool = copy.deepcopy(m['redis_endpoints'][0]['live_pools'][0])
        pool.update(id='asynq-b', instance_id='app-b')
        m['redis_endpoints'][0]['live_pools'].append(pool)
        self.assertIn('multi-instance policy fingerprint mismatch', check(m, NOW)['failures'])
        other['control_policy_sha256'] = 'a' * 64
        self.assertEqual(check(m, NOW)['decision'], 'MANIFEST-CONSISTENT')
        other['safety']['policy']['RPM'] = 3
        self.assertIn('multi-instance policy fingerprint mismatch', check(m, NOW)['failures'])
        other['safety']['policy']['RPM'] = 2
        other['safety']['alert_url'] = 'https://other-alert.example.test/feishu'
        self.assertIn('multi-instance policy fingerprint mismatch', check(m, NOW)['failures'])

    def test_alert_url_required_and_safe(self):
        m = fixture(); del m['instances'][0]['safety']['alert_url']
        with self.assertRaises(Invalid):
            check(m, NOW)
        for url in ['', 'UNKNOWN', 'http://alerts.test/', 'https:///missing-host',
                    'https://user:secret@alerts.test/', 'https://user@alerts.test/',
                    'https://alerts.test/#fragment', 'https://alerts.test/#',
                    'https://alerts.test:bad/', 'https://alerts.test/\n']:
            m = fixture(); m['instances'][0]['safety']['alert_url'] = url
            with self.subTest(url=url), self.assertRaises(ValueError):
                check(m, NOW)

    def test_unknown_or_missing_fields(self):
        for path in [None, 'controls', 'canary']:
            m = fixture()
            target = m if path is None else m[path]
            target['unrecognized'] = True
            with self.assertRaises(Invalid):
                check(m, NOW)
        m = fixture(); del m['inventory_evidence']
        with self.assertRaises(Invalid):
            check(m, NOW)


if __name__ == '__main__':
    unittest.main()
