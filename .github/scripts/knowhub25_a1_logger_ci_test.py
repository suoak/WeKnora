"""Result-parser regression tests; these are not middleware/Linux validation."""
import unittest
from knowhub25_a1_logger_ci import PACKAGE, LOGGER_TESTS, classify, comparison_gate


def executed(names, outcome="pass"):
    events = []
    for name in names:
        events += [{"Package": PACKAGE, "Test": name, "Action": "run"},
                   {"Package": PACKAGE, "Test": name, "Action": outcome}]
    return events + [{"Package": PACKAGE, "Action": "pass"}]


class ClassificationTests(unittest.TestCase):
    def test_compile_failure_is_not_a_test_failure(self):
        result = classify(1, None, [])
        self.assertEqual(result["status"], "BUILD_FAILED_TESTS_NOT_EXECUTED")
        self.assertEqual((result["pass"], result["fail"], result["skip"]), (0, 0, 0))

    def test_build_success_without_tests_cannot_pass(self):
        self.assertNotEqual(classify(0, 0, [{"Package": PACKAGE, "Action": "pass"}])["status"], "PASS")

    def test_candidate_requires_all_four_logger_regressions(self):
        self.assertEqual(classify(0, 0, executed(LOGGER_TESTS), True)["status"], "PASS")
        self.assertNotEqual(classify(0, 0, executed({"TestExisting"}), True)["status"], "PASS")

    def test_failure_or_skipped_logger_is_not_hidden(self):
        for outcome in ("fail", "skip"):
            self.assertNotEqual(classify(0, 0, executed(LOGGER_TESTS, outcome), True)["status"], "PASS")

    def test_top_level_and_subtest_counts_are_separate(self):
        result = classify(0, 0, executed({"TestParent", "TestParent/child"}))
        self.assertEqual(result["status"], "PASS")
        self.assertEqual((result["executedTopLevel"], result["executedIncludingSubtests"]), (1, 2))

    def test_all_skipped_is_not_test_pass(self):
        self.assertNotEqual(classify(0, 0, executed({"TestSkipped"}, "skip"))["status"], "PASS")

    def test_incomplete_execution_or_nonzero_exit_cannot_pass(self):
        incomplete = executed({"TestComplete"}) + [{"Package": PACKAGE, "Action": "run", "Test": "TestUnfinished"}]
        self.assertNotEqual(classify(0, 0, incomplete)["status"], "PASS")
        self.assertNotEqual(classify(0, 1, executed(LOGGER_TESTS), True)["status"], "PASS")

    def test_baseline_or_shared_cache_failure_is_not_blame_on_candidate(self):
        baseline = {"status": "PASS", "sourceUnchanged": True, "moduleListExit": 1}
        candidate = {**baseline, "moduleListExit": 0}
        self.assertEqual(comparison_gate({"baseline": baseline, "candidate": candidate}, 0),
                         (False, "BASELINE_OR_ENVIRONMENT_FAILURE"))
        self.assertEqual(comparison_gate({"baseline": candidate, "candidate": candidate}, 1),
                         (False, "BASELINE_OR_ENVIRONMENT_FAILURE"))

    def test_candidate_only_failure_blocks_and_both_valid_can_pass(self):
        baseline = {"status": "PASS", "sourceUnchanged": True, "moduleListExit": 0}
        candidate = {**baseline, "status": "TEST_OR_EXECUTION_FAILURE"}
        self.assertEqual(comparison_gate({"baseline": baseline, "candidate": candidate}, 0),
                         (False, "CANDIDATE_OR_INTEGRITY_FAILURE"))
        self.assertEqual(comparison_gate({"baseline": baseline, "candidate": baseline}, 0), (True, "BOTH_PASSED"))


if __name__ == "__main__":
    unittest.main()
