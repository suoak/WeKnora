import logging
import os
from pathlib import Path
import signal
import subprocess
import sys
import tempfile
import time
import unittest
from unittest.mock import Mock

from docreader.shutdown import wait_for_termination


class ShutdownTest(unittest.TestCase):
    def test_regular_server_exit_keeps_signal_handler_and_does_not_stop_twice(self):
        server = Mock()
        previous = signal.getsignal(signal.SIGTERM)
        wait_for_termination(server, logging.getLogger(__name__))
        self.assertEqual(signal.getsignal(signal.SIGTERM), previous)
        server.stop.assert_not_called()

    def test_server_error_remains_visible_and_restores_signal_handler(self):
        server = Mock()
        server.wait_for_termination.side_effect = RuntimeError("server failed")
        previous = signal.getsignal(signal.SIGTERM)
        with self.assertRaisesRegex(RuntimeError, "server failed"):
            wait_for_termination(server, logging.getLogger(__name__))
        self.assertEqual(signal.getsignal(signal.SIGTERM), previous)
        server.stop.assert_not_called()

    @unittest.skipUnless(os.name == "posix", "requires catchable POSIX process signals")
    def test_real_termination_signals_stop_server_and_exit_cleanly(self):
        for sig in (signal.SIGTERM, signal.SIGINT):
            with self.subTest(signal=sig), tempfile.TemporaryDirectory() as folder:
                marker = Path(folder) / "ready"
                script = """
import logging, sys, threading
from pathlib import Path
from docreader.shutdown import wait_for_termination
class Server:
    def wait_for_termination(self):
        Path(sys.argv[1]).write_text('ready')
        threading.Event().wait(30)
    def stop(self, grace):
        assert grace == 0
        print('grpc-stopped', flush=True)
        event = threading.Event()
        event.set()
        return event
wait_for_termination(Server(), logging.getLogger('shutdown-test'))
print('clean-exit', flush=True)
"""
                process = subprocess.Popen(
                    [sys.executable, "-c", script, str(marker)],
                    stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True,
                )
                try:
                    deadline = time.monotonic() + 5
                    while not marker.exists() and process.poll() is None and time.monotonic() < deadline:
                        time.sleep(0.01)
                    self.assertTrue(marker.exists(), "child failed to install its shutdown handler")
                    process.send_signal(sig)
                    stdout, stderr = process.communicate(timeout=5)
                    self.assertEqual(process.returncode, 0, stderr)
                    self.assertEqual(stdout.splitlines(), ["grpc-stopped", "clean-exit"])
                finally:
                    if process.poll() is None:
                        process.kill()  # Only this test's isolated child, never an application container.
                        process.communicate(timeout=5)


if __name__ == "__main__":
    unittest.main()
