#!/usr/bin/env python3
"""Offline release gate. Never opens a socket or modifies input/production.

The manifest is an operator-attested snapshot, not a topology discovery tool.
Exit 0 means its declared evidence is consistent, not permission to deploy.
"""
import argparse
import datetime as dt
import hashlib
import json
import re
import sys
import uuid


class Invalid(ValueError):
    pass


def shape(value, fields, path):
    if not isinstance(value, dict) or set(value) != set(fields):
        raise Invalid(path + ': missing or unknown fields')


def integer(value, path, minimum=0):
    if type(value) is not int or value < minimum:
        raise Invalid(path + ': invalid integer')
    return value


def text(value, path):
    if not isinstance(value, str) or not value.strip() or value == 'UNKNOWN':
        raise Invalid(path + ': unknown/empty text')
    return value


def int64(value):
    # Go ParseInt accepts signs, but neither surrounding spaces nor decimals.
    if type(value) is int:
        n = value
    elif isinstance(value, str) and re.fullmatch(r'[+-]?[0-9]+', value):
        n = int(value)
    else:
        return None
    return n if -(2 ** 63) <= n < 2 ** 63 else None


def effective_setting(row, env, default, positive):
    """Match GetInt + bootstrap-default exception + worker positive fallback."""
    value = None
    origin = 'default'
    if row is not None:
        shape(row, ['value', 'last_modified_by'], 'db_row')
        if not isinstance(row['last_modified_by'], str):
            raise Invalid('db_row.last_modified_by: invalid')
        # JSON number default is different from a JSON string of that number.
        bootstrap = (not row['last_modified_by'].strip()
                     and type(row['value']) is int and row['value'] == default)
        if not bootstrap:
            value = int64(row['value'])
            origin = 'DB'
    if value is None:
        value = int64(env)
        origin = 'ENV' if value is not None else 'default'
    if value is None or (positive and value < 1):
        value, origin = default, 'default'
    return value, origin


def unique(items, path):
    if not isinstance(items, list) or not items:
        raise Invalid(path + ': expected nonempty list')
    for item in items:
        text(item, path)
    if len(set(items)) != len(items):
        raise Invalid(path + ': duplicates')
    return set(items)


def check(m, now=None):
    failures, resolved = [], []
    shape(m, ['schema', 'observed_at', 'max_age_seconds', 'inventory_complete',
              'inventory_evidence', 'expected_sha', 'stage', 'allowed_source_ids',
              'expected_instances', 'expected_redis_endpoints', 'instances',
              'redis_endpoints', 'controls', 'canary'], 'manifest')
    if m['schema'] != 1 or type(m['schema']) is not int:
        raise Invalid('unsupported schema')
    stamp = dt.datetime.fromisoformat(text(m['observed_at'], 'observed_at').replace('Z', '+00:00'))
    if stamp.tzinfo is None:
        raise Invalid('observed_at: timezone required')
    age = ((now or dt.datetime.now(dt.timezone.utc)) - stamp).total_seconds()
    freshness = integer(m['max_age_seconds'], 'max_age_seconds', 1)
    if freshness > 300 or age < -5 or age > freshness:
        failures.append('snapshot stale/future or freshness budget > 300s')
    if m['inventory_complete'] is not True:
        failures.append('authoritative inventory incomplete')
    text(m['inventory_evidence'], 'inventory_evidence')
    if not re.fullmatch(r'[0-9a-f]{40}', text(m['expected_sha'], 'expected_sha')):
        raise Invalid('expected_sha: full commit required')
    if m['stage'] not in ['scope-none', 'single-source-ready']:
        raise Invalid('unknown stage')
    scope = m['allowed_source_ids']
    if m['stage'] == 'scope-none':
        if scope != 'none':
            failures.append('scope-none requires explicit none')
    else:
        try:
            if str(uuid.UUID(scope)) != scope:
                raise ValueError()
        except (ValueError, TypeError, AttributeError):
            failures.append('single-source requires one canonical UUID')
    instance_ids = unique(m['expected_instances'], 'expected_instances')
    redis_ids = unique(m['expected_redis_endpoints'], 'expected_redis_endpoints')
    observed, pool_ids, fingerprints, actual_endpoints = set(), set(), set(), set()
    for instance in m['instances']:
        shape(instance, ['id', 'redis_endpoint', 'sha', 'scope', 'scope_supported',
                         'scheduler', 'worker', 'maintenance_db', 'maintenance_env',
                         'model_db', 'model_env', 'observed_maintenance',
                         'control_policy_sha256', 'safety', 'evidence'], 'instance')
        iid = text(instance['id'], 'instance.id')
        if iid in observed:
            raise Invalid('duplicate instance')
        observed.add(iid)
        if instance['scope_supported'] is not True or instance['sha'] != m['expected_sha']:
            failures.append(iid + ': old/unverified worker or producer')
        if instance['scope'] != scope:
            failures.append(iid + ': scope mismatch')
        for key in ['scheduler', 'worker']:
            if type(instance[key]) is not bool:
                raise Invalid(iid + ': unknown role')
        if not instance['scheduler'] and not instance['worker']:
            failures.append(iid + ': role not identified')
        actual_endpoints.add(text(instance['redis_endpoint'], 'redis_endpoint'))
        safety = instance['safety']
        shape(safety, ['enabled', 'worker_id', 'expected_workers', 'source_sha', 'policy'], 'safety')
        if (safety['enabled'] is not True or safety['worker_id'] != iid
                or safety['source_sha'] != m['expected_sha']
                or unique(safety['expected_workers'], 'safety.expected_workers') != instance_ids):
            failures.append(iid + ': runtime safety identity/inventory mismatch')
        policy = safety['policy']
        shape(policy, ['Namespace', 'EndpointHost', 'InFlight', 'RPM', 'RPS', 'TPM',
                       'WaitBudgetDuration', 'TokenBoundVerified', 'TokenOverhead'], 'safety.policy')
        text(policy['Namespace'], 'Namespace'); text(policy['EndpointHost'], 'EndpointHost')
        limits = {'InFlight': (1, 1000), 'RPM': (1, 10000), 'RPS': (1, 10000),
                  'TPM': (1, 1000000000), 'WaitBudgetDuration': (1, 65000000000),
                  'TokenOverhead': (0, 1000000)}
        for key, (lower, upper) in limits.items():
            if integer(policy[key], key, lower) > upper:
                raise Invalid(key + ': above supported maximum')
        if policy['TokenBoundVerified'] is not True:
            failures.append(iid + ': unknown token accounting bound')
        for key in ['maintenance_env', 'model_env']:
            if instance[key] is not None and not isinstance(instance[key], str):
                raise Invalid(key + ': require captured env string or null')
        maintenance, origin = effective_setting(instance['maintenance_db'], instance['maintenance_env'], 4, True)
        model, model_origin = effective_setting(instance['model_db'], instance['model_env'], 32, False)
        observed_capacity = integer(instance['observed_maintenance'], 'observed_maintenance')
        if observed_capacity != (maintenance if instance['worker'] else 0):
            failures.append(iid + ': resolved versus running maintenance mismatch')
        if instance['worker'] and maintenance != 1:
            failures.append(iid + ': runtime controller requires maintenance=1')
        resolved.append({'instance': iid, 'maintenance': maintenance, 'origin': origin,
                         'model': model, 'model_origin': model_origin})
        policy = text(instance['control_policy_sha256'], 'control_policy_sha256')
        if not re.fullmatch(r'[0-9a-f]{64}', policy):
            raise Invalid('invalid control policy hash')
        fingerprints.add((instance['sha'], instance['scope'], policy,
                          json.dumps(safety['policy'], sort_keys=True)))
        text(instance['evidence'], 'instance.evidence')
    if observed != instance_ids or actual_endpoints != redis_ids:
        failures.append('inventory/observed instance or Redis endpoint set mismatch')
    if len(fingerprints) != 1:
        failures.append('multi-instance policy fingerprint mismatch')
    seen_redis, capacity = set(), 0
    for endpoint in m['redis_endpoints']:
        shape(endpoint, ['id', 'evidence', 'sync_paused', 'sync_active', 'live_pools'], 'redis_endpoint')
        rid = text(endpoint['id'], 'endpoint.id')
        if rid in seen_redis:
            raise Invalid('duplicate Redis endpoint')
        seen_redis.add(rid)
        text(endpoint['evidence'], 'endpoint.evidence')
        if endpoint['sync_paused'] is not True or integer(endpoint['sync_active'], 'sync_active') != 0:
            failures.append(rid + ': queue not paused/drained')
        for pool in endpoint['live_pools']:
            shape(pool, ['id', 'instance_id', 'pool', 'concurrency', 'queues'], 'pool')
            pid = text(pool['id'], 'pool.id')
            if pid in pool_ids:
                raise Invalid('duplicate pool identity')
            pool_ids.add(pid)
            iid = pool['instance_id']
            matches = [i for i in m['instances'] if i['id'] == iid]
            if len(matches) != 1 or matches[0]['redis_endpoint'] != rid or matches[0]['worker'] is not True:
                failures.append(pid + ': orphan/misrouted/undeclared worker')
            n = integer(pool['concurrency'], 'pool.concurrency', 1)
            if not isinstance(pool['queues'], dict) or not pool['queues']:
                raise Invalid('pool queues missing')
            for q, weight in pool['queues'].items():
                text(q, 'queue'); integer(weight, 'queue.weight', 1)
            if pool['pool'] == 'maintenance':
                if pool['queues'] != {'sync': 2, 'low': 1}:
                    failures.append(pid + ': sync/low queue topology drift')
                capacity += n
                if matches and n != matches[0]['observed_maintenance']:
                    failures.append(pid + ': pool/runtime capacity mismatch')
            elif 'sync' in pool['queues'] or 'low' in pool['queues']:
                failures.append(pid + ': unexpected sync/low consumer')
    if seen_redis != redis_ids:
        failures.append('missing or extra Redis endpoint observations')
    for i in m['instances']:
        count = sum(p['instance_id'] == i['id'] and p['pool'] == 'maintenance'
                    for e in m['redis_endpoints'] for p in e['live_pools'])
        if count != (1 if i['worker'] else 0):
            failures.append(i['id'] + ': missing/duplicate maintenance pool')
    shape(m['controls'], ['gateway_budget_confirmed', 'hard_egress_verified',
                          'controller_e2e_verified', 'metrics_fresh', 'watchdog_fail_closed',
                          'pause_authority_exercised_in_isolation', 'fencing_verified',
                          'backup_restore_verified', 'startup_smoke_verified', 'evidence'], 'controls')
    for key, val in m['controls'].items():
        if key == 'evidence':
            text(val, 'controls.evidence')
        elif val is not True:
            failures.append('control unverified: ' + key)
    shape(m['canary'], ['source_uuid', 'noncritical_confirmed', 'no_parent_child',
                        'version_safe', 'read_export_acl_verified', 'dns_verified', 'evidence'], 'canary')
    if m['stage'] == 'single-source-ready':
        if m['canary']['source_uuid'] != scope:
            failures.append('canary UUID mismatch')
        for key in ['noncritical_confirmed', 'no_parent_child', 'version_safe', 'read_export_acl_verified', 'dns_verified']:
            if m['canary'][key] is not True:
                failures.append('canary unverified: ' + key)
        text(m['canary']['evidence'], 'canary.evidence')
    return {'decision': 'NO-GO' if failures else 'MANIFEST-CONSISTENT',
            'failures': failures, 'settings': resolved,
            'aggregate_maintenance_slots_shared_with_low': capacity,
            'authorization_to_deploy': False,
            'note': 'Offline attestations only; no production capability is probed.'}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('manifest')
    args = parser.parse_args()
    try:
        with open(args.manifest, 'rb') as stream:
            raw = stream.read()
        def pairs(entries):
            obj = {}
            for key, value in entries:
                if key in obj:
                    raise Invalid('duplicate JSON key')
                obj[key] = value
            return obj
        result = check(json.loads(raw, object_pairs_hook=pairs))
        result['input_sha256'] = hashlib.sha256(raw).hexdigest()
    except (OSError, ValueError, TypeError, KeyError, OverflowError) as error:
        result = {'decision': 'NO-GO', 'failures': [str(error)], 'authorization_to_deploy': False}
    print(json.dumps(result, indent=2, ensure_ascii=False))
    return 0 if result['decision'] == 'MANIFEST-CONSISTENT' else 2


if __name__ == '__main__':
    sys.exit(main())
