"""No-credential route contract; shared by calibration and APP-only retry."""
import urllib.parse
import json

PROTECTED = ('/api/v1/auth/me', '/api/v1/knowledge-bases',
             '/api/v1/system/admin/feishu', '/api/v1/me/feishu',
             '/mcp/nonexistent-deployment-probe')
PATHS = ('/login', '/health', '/api/v1/auth/oidc/config',
         '/api/v1/auth/oidc/callback') + PROTECTED


def same_container_inventory(before, after):
    """Docker does not guarantee mount/port-binding array ordering."""
    def canonical(value):
        result = dict(value)
        result['mounts'] = sorted(result['mounts'], key=lambda x: json.dumps(x, sort_keys=True))
        result['ports'] = {k: sorted(v, key=lambda x: json.dumps(x, sort_keys=True)) if v else v
                           for k, v in (result.get('ports') or {}).items()}
        return result
    return canonical(before) == canonical(after)


def validate(port, path, status, location=None):
    if port not in (80, 8080) or path not in PATHS:
        raise ValueError('unsupported_probe_contract')
    expected = (200 if port == 80 else 401) if path == '/login' else (
        302 if path.endswith('/callback') else
        200 if path in ('/health', '/api/v1/auth/oidc/config') else 401)
    if type(status) is not int or status != expected:
        raise ValueError('unexpected_probe_status')
    if path.endswith('/callback'):
        target = urllib.parse.urlsplit(location or '')
        query = urllib.parse.parse_qs(target.query)
        fragment = urllib.parse.parse_qs(target.fragment)
        if target.hostname not in (None, 'knowhub.ruijie.com.cn') or fragment.get('oidc_error') != ['invalid_state']:
            raise ValueError('unsafe_callback_destination')
        if any(k in query or k in fragment for k in ('code', 'token', 'access_token', 'id_token')):
            raise ValueError('callback_probe_credential_unexpected')
    return True
