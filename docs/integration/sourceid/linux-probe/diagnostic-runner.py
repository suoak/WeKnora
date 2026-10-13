"""Single isolated Probe runtime; only validated sanitized JSON may be saved."""
import json
import os
from pathlib import Path
import signal
import subprocess
import sys

from probe_report_filter import safe

base = Path(__file__).resolve().parent
process = None

def stop(*_):
    if process is not None and process.poll() is None:
        os.killpg(process.pid, signal.SIGTERM)

signal.signal(signal.SIGTERM, stop)
signal.signal(signal.SIGINT, stop)
try:
    if (base / 'first-real-result.json').exists():
        raise RuntimeError('already_consumed')
    public = json.loads((base / 'first-real-metadata.json').read_text())
    approved_codes = public.get('diagnostic_public_error_codes', [])
    process = subprocess.Popen([str(base / 'run-probe.sh'), str(base / 'first-real-metadata.json')], stdin=sys.stdin.buffer, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, start_new_session=True)
    (base / 'first-real-probe.pid').write_text(str(process.pid))
    os.chmod(base / 'first-real-probe.pid', 0o600)
    for line in process.stdout:
        try:
            report = json.loads(line)
        except (ValueError, UnicodeError):
            continue
        if not safe(report, approved_codes):
            continue
        print(json.dumps(report), flush=True)
        if 'validation' in report:
            target = base / 'first-real-result.json'
            target.write_text(json.dumps(report, indent=2) + '\n')
            os.chmod(target, 0o600)
    process.wait()
except Exception:
    print(json.dumps({'validation': {'runtime': False, 'completed': False}}), flush=True)
finally:
    stop()
    if process is not None:
        process.wait(timeout=10)
    (base / 'first-real-probe.pid').unlink(missing_ok=True)
sys.exit(0 if process is not None and process.returncode == 0 else 1)
