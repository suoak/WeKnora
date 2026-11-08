"""Bounded durable metadata delivery. Credentials are kept only in memory."""
import hashlib
import json
import logging
import os
from pathlib import Path
import threading
import time

import requests

logger = logging.getLogger(__name__)


class UsageOutbox:
    def __init__(self, url, directory=None, start=True):
        self.url = url
        self.directory = Path(directory or os.getenv("MCP_USAGE_SPOOL_DIR", "data/mcp-usage-spool"))
        self.directory.mkdir(parents=True, exist_ok=True, mode=0o700)
        # Recover admitted calls from the previous process conservatively.
        for active in self.directory.glob("*.active"):
            try:
                final = active.with_suffix(".json")
                if not final.exists():
                    item = json.loads(active.read_text(encoding="utf-8"))
                    item["payload"].update(success=False, error_code="interrupted", latency_ms=0)
                    self._write(final, item)
                active.unlink()
            except Exception:
                logger.error("MCP usage interrupted record recovery failed; file=%s", active.name)
                os.replace(active, active.with_suffix(".dead"))
        self.lock = threading.Lock()
        self.credentials = {}
        self.wake = threading.Event()
        self.stopped = threading.Event()
        if start:
            threading.Thread(target=self._run, name="mcp-usage-delivery", daemon=True).start()

    def register(self, headers):
        fingerprint = hashlib.sha256(json.dumps(headers, sort_keys=True).encode()).hexdigest()
        with self.lock:
            # Bounded live credential cache. On restart a per-user event waits
            # for that authenticated caller to return; no secret goes to disk.
            if len(self.credentials) >= 256 and fingerprint not in self.credentials:
                self.credentials.pop(next(iter(self.credentials)))
            self.credentials[fingerprint] = dict(headers)
        self.wake.set()
        return fingerprint

    @staticmethod
    def _write(path, item):
        temporary = path.with_suffix(".tmp")
        descriptor = os.open(temporary, os.O_CREAT | os.O_WRONLY | os.O_TRUNC, 0o600)
        with os.fdopen(descriptor, "w", encoding="utf-8") as stream:
            json.dump(item, stream)
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temporary, path)

    def enqueue(self, payload, headers, active=False):
        fingerprint = self.register(headers)
        with self.lock:
            name = hashlib.sha256((fingerprint + payload["event_key"]).encode()).hexdigest()
            path = self.directory / (name + (".active" if active else ".json"))
            if not path.exists() and not path.with_suffix(".active").exists() and sum(1 for _ in self.directory.iterdir()) >= 10000:
                raise RuntimeError("MCP usage outbox full; collection degraded")
            self._write(path, {
                "payload": payload, "credential_ref": fingerprint, "attempts": 0,
            })
            if not active:
                path.with_suffix(".active").unlink(missing_ok=True)
        self.wake.set()

    def drain(self):
        now = time.time()
        for path in list(self.directory.glob("*.json"))[:100]:
            try:
                item = json.loads(path.read_text(encoding="utf-8"))
                if item.get("retry_after", 0) > now:
                    continue
                with self.lock:
                    headers = self.credentials.get(item["credential_ref"])
                if headers is None:
                    # Retain metadata, rather than attributing it to a different key.
                    logger.warning("MCP usage delivery awaits authenticated key; file=%s", path.name)
                    continue
                response = requests.post(self.url, headers=headers, json=item["payload"], timeout=5)
                response.raise_for_status()
                path.unlink()
            except Exception:
                logger.error("MCP usage delivery failed; file=%s", path.name)
                if "item" not in locals():
                    os.replace(path, path.with_suffix(".dead"))
                    continue
                item["attempts"] += 1
                item["retry_after"] = now + min(120, 2 ** item["attempts"])
                self._write(path, item)
                if item["attempts"] >= 5:
                    os.replace(path, path.with_suffix(".dead"))
            finally:
                # A corrupt next file must never reuse the previous envelope.
                if "item" in locals():
                    del item

    def _run(self):
        while not self.stopped.is_set():
            self.wake.wait(5)
            self.wake.clear()
            try:
                self.drain()
            except Exception:
                logger.error("MCP usage delivery worker failed; pending metadata retained")
