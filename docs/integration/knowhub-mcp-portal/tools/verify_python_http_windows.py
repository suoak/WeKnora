"""Run existing HTTP smoke test with only /dev/null adapted to os.devnull.

Keeps the original test and application unchanged. Synthetic local credentials.
This result must be reported separately from the unmodified suite result.
"""
import os
from pathlib import Path
import subprocess
import sys
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[4] / 'mcp-server'))
import test_mcp_transports

original_run = subprocess.run


def native_null(args, *positional, **kwargs):
    return original_run([os.devnull if x == '/dev/null' else x for x in args], *positional, **kwargs)


if __name__ == '__main__':
    with patch.object(subprocess, 'run', native_null):
        suite = unittest.defaultTestLoader.loadTestsFromTestCase(test_mcp_transports.HttpStatelessSmokeTest)
        result = unittest.TextTestRunner(verbosity=2).run(suite)
    sys.exit(0 if result.wasSuccessful() else 1)
