"""Fail-closed report validation before an isolated runtime persists output.

This module never reads credentials, bodies or cookies and has no executable
login entrypoint. Custom public codes need explicit operator approval.
"""
import datetime
import re

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
            if not isinstance(row, dict) or set(row) - {'stage', 'status', 'duration_ms', 'failure', 'oauth_error', 'client_auth', 'authorization_header_present', 'callback_to_token_dispatch_ms', 'callback_to_token_sent_ms', 'diagnostics'}:
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
        return all(schema(x[k]) for k in ['claims', 'userinfo_schema'] if k in x)
    except (TypeError, ValueError, KeyError, AttributeError):
        return False
