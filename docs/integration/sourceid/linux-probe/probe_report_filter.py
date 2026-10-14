"""Fail-closed report validation before an isolated runtime persists output.

This module never reads credentials, bodies or cookies and has no executable
login entrypoint. Custom public codes need explicit operator approval.
"""
import datetime
import re
import ipaddress

ENDPOINTS = {
    'issuer': 'https://sid.ruijie.com.cn',
    'authorization': 'https://sid.ruijie.com.cn/oidc/authorize',
    'token': 'https://sid.ruijie.com.cn/oidc/accessToken',
    'jwks': 'https://sid.ruijie.com.cn/oidc/jwks',
    'userinfo': 'https://sid.ruijie.com.cn/oidc/profile',
    'callback': 'http://knowhub.ruijie.com.cn/api/v1/auth/oidc/callback',
    'probe_start': 'http://127.0.0.1:18183/probe/start',
}
OAUTH = {'invalid_request', 'invalid_client', 'invalid_grant', 'unauthorized_client', 'unsupported_grant_type', 'invalid_scope', 'invalid_token', 'access_denied', 'server_error', 'temporarily_unavailable'}
MEDIA = {'absent', 'other', 'application/json', 'application/problem+json', 'text/html', 'text/plain', 'application/x-www-form-urlencoded'}
FORM = {'grant_type', 'code', 'redirect_uri', 'client_id', 'client_secret', 'code_verifier'}
ERROR_FIELDS = {'error', 'error_description', 'errorCode', 'error_code', 'code', 'message', 'access_token', 'id_token', 'refresh_token', 'token_type'}
TYPES = {'string', 'number', 'boolean', 'null', 'array', 'object', 'invalid', 'unknown'}

def integer(x, lo=0, hi=86400000):
    return type(x) is int and lo <= x <= hi

def booleans(x, keys):
    return isinstance(x, dict) and not set(x) - keys and all(type(v) is bool for v in x.values())

def timestamp(x):
    if not isinstance(x, str) or not re.fullmatch(r'\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?Z', x):
        return False
    try:
        normalized = re.sub(r'(\.\d{6})\d{1,3}(?=Z)', r'\1', x)
        datetime.datetime.fromisoformat(normalized.replace('Z', '+00:00'))
        return True
    except ValueError:
        return False

def schema(x, depth=0):
    if depth > 8:
        return False
    if isinstance(x, str):
        return x in TYPES - {'array', 'object', 'invalid'}
    if type(x) is bool:
        return True
    if not isinstance(x, dict):
        return False
    return all(isinstance(k, str) and re.fullmatch(r'[A-Za-z_][A-Za-z0-9_-]{0,63}', k) and (v in {'object', 'array'} if k == 'type' and isinstance(v, str) else schema(v, depth+1)) for k, v in x.items())

def diagnostics(x, public_codes):
    if not isinstance(x, dict) or set(x) - {'request', 'response'}:
        return False
    q = x.get('request')
    required = {'method', 'host', 'path', 'host_matches_endpoint', 'content_type', 'authorization_type', 'authorization_present', 'basic_format_valid', 'configured_client_id', 'configured_redirect_uri', 'form_fields', 'field_presence', 'field_checks', 'unknown_field_count', 'form_inspected', 'query_present', 'dispatch_utc'}
    if not isinstance(q, dict) or not required <= set(q) or set(q) - required - {'sent_utc'}:
        return False
    if q['method'] not in {'POST', 'other'} or q['host'] != 'sid.ruijie.com.cn' or q['path'] != '/oidc/accessToken' or q['content_type'] not in MEDIA or q['authorization_type'] not in {'absent', 'other', 'Basic', 'Bearer'} or q['configured_client_id'] != 'knowhub' or q['configured_redirect_uri'] != ENDPOINTS['callback']:
        return False
    if any(type(q[k]) is not bool for k in ['host_matches_endpoint', 'authorization_present', 'basic_format_valid', 'form_inspected', 'query_present']):
        return False
    if not isinstance(q['form_fields'], list) or len(q['form_fields']) > 6 or any(v not in FORM for v in q['form_fields']) or not booleans(q['field_presence'], FORM) or not booleans(q['field_checks'], {'grant_type_authorization_code', 'client_id_matches_config', 'redirect_uri_matches_config', 'code_single_nonempty'}) or not integer(q['unknown_field_count'], hi=65536) or not timestamp(q['dispatch_utc']) or ('sent_utc' in q and not timestamp(q['sent_utc'])):
        return False
    if 'response' not in x:
        return True
    p = x['response']
    required = {'content_type', 'declared_length', 'bytes_read', 'body_eof', 'prefix_truncated', 'read_error', 'body_elapsed_ms', 'body_kind', 'unknown_field_count', 'error_description_present', 'vendor_code_suppressed', 'www_authenticate_present', 'www_realm_present', 'www_description_present', 'correlation', 'gateway_suspected'}
    optional = {'error_fields', 'error_description_summary', 'oauth_error', 'vendor_error_code', 'www_schemes', 'www_oauth_error'}
    if not isinstance(p, dict) or not required <= set(p) or set(p) - required - optional:
        return False
    if p['content_type'] not in MEDIA or not integer(p['declared_length'], -1, 2**50) or not integer(p['bytes_read'], hi=2**21) or not integer(p['body_elapsed_ms']) or not integer(p['unknown_field_count'], hi=65536) or p['body_kind'] not in {'not_observed', 'empty', 'json_object', 'html', 'non_json_or_incomplete'}:
        return False
    if any(type(p[k]) is not bool for k in ['body_eof', 'prefix_truncated', 'read_error', 'error_description_present', 'vendor_code_suppressed', 'www_authenticate_present', 'www_realm_present', 'www_description_present', 'gateway_suspected']):
        return False
    if 'error_description_summary' in p and p['error_description_summary'] != 'withheld_untrusted_description':
        return False
    if any(k in p and p[k] not in OAUTH for k in ['oauth_error', 'www_oauth_error']):
        return False
    if 'vendor_error_code' in p and (p['vendor_error_code'] not in public_codes or not re.fullmatch(r'(?:[A-Z][A-Z0-9_]{2,47}|[1-9][0-9]{2,7})', p['vendor_error_code'])):
        return False
    if 'www_schemes' in p and (not isinstance(p['www_schemes'], list) or len(p['www_schemes']) > 2 or any(v not in {'basic', 'bearer', 'other'} for v in p['www_schemes'])):
        return False
    if 'error_fields' in p and (not isinstance(p['error_fields'], dict) or set(p['error_fields']) - ERROR_FIELDS or any(v not in TYPES for v in p['error_fields'].values())):
        return False
    if not isinstance(p['correlation'], dict) or set(p['correlation']) - {'X-Request-ID', 'X-Correlation-ID', 'Traceparent'}:
        return False
    for value in p['correlation'].values():
        if not isinstance(value, dict) or set(value) - {'present', 'format_valid', 'sha256'} or type(value.get('present')) is not bool or type(value.get('format_valid')) is not bool or ('sha256' in value and not re.fullmatch('[a-f0-9]{64}', value['sha256'])):
            return False
    return True

def safe(x, public_codes=()):
    try:
        if not isinstance(x, dict) or set(x) - {'endpoint', 'alg', 'claims', 'userinfo_schema', 'validation', 'http'}:
            return False
        if any(k not in ENDPOINTS or v != ENDPOINTS[k] for k, v in x.get('endpoint', {}).items()) or ('alg' in x and x['alg'] != 'RS256'):
            return False
        if any(not re.fullmatch('[a-z_]{1,64}', k) or type(v) is not bool for k, v in x.get('validation', {}).items()):
            return False
        rows = x.get('http', [])
        if not isinstance(rows, list) or len(rows) > 8:
            return False
        for row in rows:
            if not isinstance(row, dict) or set(row) - {'stage', 'status', 'duration_ms', 'failure', 'oauth_error', 'client_auth', 'authorization_header_present', 'callback_to_token_dispatch_ms', 'callback_to_token_sent_ms', 'diagnostics', 'transport'}:
                return False
            if row.get('stage') not in {'token', 'jwks', 'userinfo', 'unknown'} or not integer(row.get('status'), 0, 599) or (row['status'] != 0 and row['status'] < 100) or not integer(row.get('duration_ms')):
                return False
            if 'failure' in row and row['failure'] not in {'transport_error', 'timeout', 'tls_error', 'http_status', 'invalid_response_json', 'missing_tokens'}:
                return False
            if 'client_auth' in row and row['client_auth'] not in {'client_secret_basic', 'client_secret_post'}:
                return False
            if 'authorization_header_present' in row and type(row['authorization_header_present']) is not bool:
                return False
            if any(k in row and not integer(row[k]) for k in ['callback_to_token_dispatch_ms', 'callback_to_token_sent_ms']):
                return False
            if 'oauth_error' in row and row['oauth_error'] not in OAUTH:
                return False
            if 'diagnostics' in row and (row['stage'] != 'token' or not diagnostics(row['diagnostics'], public_codes)):
                return False
            if 'transport' in row and (row['stage'] != 'token' or not transport(row['transport'])):
                return False
        return all(schema(x[k]) for k in ['claims', 'userinfo_schema'] if k in x)
    except (TypeError, ValueError, KeyError, AttributeError):
        return False


def transport(x):
    """Closed schema for business httptrace telemetry; no raw header text."""
    enums = {
        'scheme': {'http', 'https', 'other'},
        'accept_class': {'application/json', 'other'},
        'user_agent_class': {'transport_default_not_observed', 'Go-http-client/1.1', 'other'},
        'basic_strategy': {'not_basic', 'oauth_form_escape_then_set_basic_auth_base64_once'},
        'state_validation': {'PASS', 'NOT_CAPTURED'},
        'nonce_validation_at_token_dispatch': {'NOT_YET_EXECUTED'},
        'proxy_policy': {'no_proxy_in_configured_transport'},
        'tls_version': {'NOT_CAPTURED', 'TLS1.2', 'TLS1.3', 'other'},
        'negotiated_protocol': {'NOT_CAPTURED', 'h2', 'http/1.1', 'absent', 'other'},
        'response_http_version': {'NOT_CAPTURED', 'HTTP/1.1', 'HTTP/2.0', 'other'},
        'server_class': {'absent', 'nginx', 'openresty', 'envoy', 'apache', 'microsoft-iis', 'other'},
        'via_class': {'absent', 'present_value_withheld'},
        'www_realm_class': {'absent', 'not_detected', 'present_value_withheld'},
        'network_failure_class': {'none', 'tls_error', 'timeout', 'transport_error'},
        'rejecting_layer': {'UNKNOWN'},
        'outcome': {'', 'UNKNOWN', 'network_or_tls_error', 'empty_401_basic_challenge', 'standard_oauth_error', 'html_response_origin_unknown', 'invalid_token_response_structure', 'missing_token_fields', 'token_fields_present_verification_pending', 'http_error_origin_unknown'},
    }
    bools = {'user_agent_header_present', 'client_secret_loaded', 'oauth_form_encoding_executed', 'set_basic_auth_used', 'duplicate_authorization', 'duplicate_body_fields', 'pkce_disabled', 'connection_reused', 'retry_observed', 'redirect_response', 'proxy_authenticate_present'}
    ints = {'authorization_header_value_count', 'body_value_count', 'events_dropped', 'observed_write_attempts', 'observed_completed_writes'}
    other = {'trace', 'utc', 'beijing', 'remote_address', 'response_date_utc', 'known_body_field_counts', 'network_events'}
    try:
        if not isinstance(x, dict) or set(x) != set(enums) | bools | ints | other:
            return False
        if not re.fullmatch('[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}', x['trace']):
            return False
        if any(x[k] not in v for k, v in enums.items()) or not booleans({k: x[k] for k in bools}, bools) or any(not integer(x[k], hi=2**31-1) for k in ints):
            return False
        def beijing(v):
            return isinstance(v, str) and bool(re.fullmatch(r'\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?\+08:00', v)) and timestamp(v[:-6]+'Z')
        def address(v):
            if v == 'NOT_CAPTURED':
                return True
            host, port = v.rsplit(':', 1)
            ipaddress.ip_address(host.strip('[]'))
            return port.isdigit() and 1 <= int(port) <= 65535
        if not timestamp(x['utc']) or not beijing(x['beijing']) or not address(x['remote_address']) or (x['response_date_utc'] != 'NOT_CAPTURED' and not timestamp(x['response_date_utc'])):
            return False
        if not isinstance(x['known_body_field_counts'], dict) or set(x['known_body_field_counts']) - FORM or any(not integer(n, hi=65536) for n in x['known_body_field_counts'].values()):
            return False
        if not isinstance(x['network_events'], list) or len(x['network_events']) > 64:
            return False
        for e in x['network_events']:
            if not isinstance(e, dict) or set(e) - {'kind', 'utc', 'beijing', 'result', 'addresses', 'duration_ms'} or e.get('kind') not in {'dns_start', 'dns_done', 'tcp_start', 'tcp_done', 'tls_start', 'tls_done', 'connection_acquired', 'headers_written', 'request_write_completed', 'first_response_byte', 'exchange_complete'} or not timestamp(e.get('utc')) or not beijing(e.get('beijing')) or e.get('result') not in {'ok', 'error', 'started', 'finished'}:
                return False
            if 'duration_ms' in e and not integer(e['duration_ms']):
                return False
            values = e.get('addresses', [])
            if not isinstance(values, list) or len(values) > 8:
                return False
            for v in values:
                if e['kind'] == 'dns_done':
                    ipaddress.ip_address(v)
                elif not address(v):
                    return False
        return True
    except (TypeError, ValueError, KeyError, AttributeError):
        return False
