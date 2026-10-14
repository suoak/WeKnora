"""Filter backend log stdin into a private, bounded diagnostics JSON file.

No login, credential access, HTTP, raw log persistence, or Docker calls.
Usage: python export_token_diagnostics.py --output report.json < log-stream
"""
import argparse
import importlib.util
import json
import os
import pathlib
import re
import sys

FILTER = pathlib.Path(__file__).resolve().parents[1] / 'linux-probe' / 'probe_report_filter.py'
spec = importlib.util.spec_from_file_location('sourceid_safe_report', FILTER)
rules = importlib.util.module_from_spec(spec)
spec.loader.exec_module(rules)


def extract(line):
    if len(line) > 65536:
        return None
    # Accept direct structured events and the existing logger's plain prefix.
    # No arbitrary error text is ever copied into the exported object.
    start = line.find('{"event"')
    if start < 0:
        return None
    try:
        item = json.JSONDecoder().raw_decode(line[start:])[0]
    except (ValueError, TypeError):
        return None
    if not isinstance(item, dict) or not re.fullmatch(r'[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}', item.get('trace', '')):
        return None
    kind = item.get('event')
    if kind == 'sourceid_token_http':
        if set(item) != {'event', 'trace', 'http'} or not rules.safe({'http': item['http']}):
            return None
        if any('transport' in row and row['transport']['trace'] != item['trace'] for row in item['http']):
            return None
        return item
    if kind in {'sourceid_token_request', 'sourceid_token_send'}:
        if set(item) - {'event', 'trace', 'diagnostics', 'transport'} or not rules.transport(item.get('transport')) or item['transport']['trace'] != item['trace']:
            return None
        if kind == 'sourceid_token_request' and not rules.diagnostics(item.get('diagnostics'), ()):
            return None
        if kind == 'sourceid_token_send' and 'diagnostics' in item:
            return None
        return item
    return None


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--output', required=True, type=pathlib.Path)
    args = parser.parse_args()
    events = []
    rejected = 0
    for _ in range(10000):
        line = sys.stdin.readline(65538)
        if not line:
            break
        event = extract(line)
        if event is None:
            rejected += 1
            continue
        events.append(event)
        if len(events) == 256:
            break
    output = {'schema_version': 2, 'source': 'filtered_backend_diagnostics', 'events': events, 'ignored_or_rejected_lines': rejected, 'limit_events': 256}
    data = json.dumps(output, ensure_ascii=True, indent=2).encode('utf-8')
    if len(data) > 8 * 1024 * 1024:
        raise SystemExit('filtered report size limit exceeded')
    # Refuse to overwrite historical evidence; files are owner read/write.
    fd = os.open(str(args.output), os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, 'wb') as handle:
        handle.write(data)
    print(json.dumps({'exported_events': len(events), 'raw_logs_saved': False, 'schema_version': 2}))


if __name__ == '__main__':
    main()
