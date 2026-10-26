"""Check the actual remote deployment's bounded environment rewrite offline."""
import ast
import importlib.util
from pathlib import Path
import unittest


class UpgradeEnvironmentTests(unittest.TestCase):
    def test_diagnostics_cannot_enable_migration_or_extend_authorization(self):
        root = Path(__file__).resolve().parents[4]
        spec = importlib.util.spec_from_file_location(
            "upgrade", root / "scripts/deploy/feishu_v2_upgrade_site.py")
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        tree = ast.parse(module.REMOTE)
        function = next(n for n in tree.body if isinstance(n, ast.FunctionDef)
                        and n.name == "upgraded_environment")
        namespace = {}
        exec(compile(ast.Module(body=[function], type_ignores=[]), "remote", "exec"), namespace)
        before = "\n".join([
            "FEISHU_SYNC_SOURCE_SHA=old", "FEISHU_SYNC_CONTROL_DIAGNOSTICS=0",
            "FEISHU_TEST_HTTP_CALLBACK_EXPIRES_AT=unchanged-deadline",
            "FEISHU_LEGACY_SYNC_ALLOWED_SOURCE_IDS=original-pair",
            "FEISHU_SYNC_SAFETY_POLICY_JSON=original-budget",
            "FEISHU_SOURCE_REBIND_OPERATIONS_ENABLED=0",
        ]) + "\n"
        after = namespace["upgraded_environment"](before, "candidate", True).splitlines()
        self.assertEqual(set(after) - set(before.splitlines()), {
            "FEISHU_SYNC_SOURCE_SHA=candidate", "FEISHU_SYNC_CONTROL_DIAGNOSTICS=1"})
        self.assertEqual(set(before.splitlines()) - set(after), {
            "FEISHU_SYNC_SOURCE_SHA=old", "FEISHU_SYNC_CONTROL_DIAGNOSTICS=0"})
        self.assertEqual(after.count("FEISHU_SYNC_CONTROL_DIAGNOSTICS=1"), 1)


if __name__ == "__main__":
    unittest.main()
