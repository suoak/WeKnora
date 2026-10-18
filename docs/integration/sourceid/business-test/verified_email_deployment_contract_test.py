"""Synthetic status/redirect fault injection; no real SID or Docker activity."""
import unittest
import copy
from verified_email_deployment_contract import PATHS, validate, same_container_inventory, same_resource_inventory


class DeploymentContractTest(unittest.TestCase):
    def test_inventory_set_and_app_replacement_scope(self):
        base = {'app': {'id': 'old', 'image_id': 'old', 'mounts': [], 'ports': {}},
                'pg': {'id': 'pg', 'image_id': 'pg', 'mounts': [], 'ports': {}}}
        replaced = copy.deepcopy(base)
        replaced['app'].update(id='new', image_id='new')
        self.assertTrue(same_resource_inventory(base, replaced, 'app'))
        self.assertFalse(same_resource_inventory(base, replaced))
        for mutation in ('added', 'removed', 'other_id', 'other_image', 'app_mount', 'app_port'):
            changed = copy.deepcopy(replaced)
            if mutation == 'added': changed['unexpected'] = copy.deepcopy(base['pg'])
            elif mutation == 'removed': del changed['pg']
            elif mutation == 'other_id': changed['pg']['id'] = 'changed'
            elif mutation == 'other_image': changed['pg']['image_id'] = 'changed'
            elif mutation == 'app_mount': changed['app']['mounts'].append({'Source': 'unexpected', 'Destination': '/data', 'RW': True})
            elif mutation == 'app_port': changed['app']['ports']['8080/tcp'] = [{'HostIp': '0.0.0.0', 'HostPort': '8080'}]
            self.assertFalse(same_resource_inventory(base, changed, 'app'), mutation)

    def test_frontend_and_backend_are_different(self):
        self.assertTrue(validate(80, '/login', 200))
        self.assertTrue(validate(8080, '/login', 401))
        with self.assertRaises(ValueError):
            validate(8080, '/login', 200)
        with self.assertRaises(ValueError):
            validate(80, '/login', 401)

    def test_every_endpoint_exact_status(self):
        for port in (80, 8080):
            for path in PATHS:
                expected = (200 if port == 80 else 401) if path == '/login' else (302 if path.endswith('/callback') else 200 if path in ('/health', '/api/v1/auth/oidc/config') else 401)
                location = '/#oidc_error=invalid_state' if path.endswith('/callback') else None
                with self.subTest(port=port, path=path):
                    self.assertTrue(validate(port, path, expected, location))
                    for bad in (0, 200, 302, 400, 401, 403, 404, 429, 500, 502, 503):
                        if bad != expected:
                            with self.assertRaises(ValueError):
                                validate(port, path, bad, location)

    def test_callback_never_follows_authentication_or_external_redirect(self):
        for location in (None, '/login', '/#oidc_error=other', 'https://example.invalid/#oidc_error=invalid_state', '/?code=synthetic#oidc_error=invalid_state', '/#oidc_error=invalid_state&token=synthetic'):
            with self.subTest(location=location), self.assertRaises(ValueError):
                validate(80, '/api/v1/auth/oidc/callback', 302, location)

    def test_failure_stops_iteration(self):
        visited = []
        with self.assertRaises(ValueError):
            for path, status in (('/health', 503), ('/api/v1/auth/oidc/config', 200)):
                visited.append(path)
                validate(80, path, status)
        self.assertEqual(visited, ['/health'])

    def test_unknown_contract_fails_closed(self):
        for port, path, status in ((18090, '/health', 200), (80, '/api/v1/auth/oidc/login', 200), (80, '/health', True)):
            with self.assertRaises(ValueError):
                validate(port, path, status)

    def test_inventory_order_is_not_a_resource_change(self):
        before = {'id': 'synthetic-container', 'image_id': 'synthetic-image',
                  'mounts': [{'Destination': '/a', 'Source': 'synthetic-a', 'RW': False},
                             {'Destination': '/b', 'Source': 'synthetic-b', 'RW': True}],
                  'ports': {'80/tcp': [{'HostIp': '127.0.0.1', 'HostPort': '80'},
                                      {'HostIp': '::1', 'HostPort': '80'}]}}
        after = copy.deepcopy(before)
        after['mounts'].reverse()
        after['ports']['80/tcp'].reverse()
        self.assertTrue(same_container_inventory(before, after))
        for field, value in (('id', 'different'), ('image_id', 'different')):
            changed = copy.deepcopy(after)
            changed[field] = value
            self.assertFalse(same_container_inventory(before, changed))
        for field, value in (('Source', 'different'), ('Destination', '/other'), ('RW', True)):
            changed = copy.deepcopy(before)
            changed['mounts'][0][field] = value
            self.assertFalse(same_container_inventory(before, changed))
        changed = copy.deepcopy(before)
        changed['mounts'].pop()
        self.assertFalse(same_container_inventory(before, changed))
        changed = copy.deepcopy(before)
        changed['ports']['80/tcp'][0]['HostIp'] = '0.0.0.0'
        self.assertFalse(same_container_inventory(before, changed))


if __name__ == '__main__':
    unittest.main()
