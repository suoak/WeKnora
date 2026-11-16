import unittest
from unittest.mock import patch

import pandas as pd

from docreader.parser.spreadsheet_schema import detect_sheet_schema


class SpreadsheetSchemaTest(unittest.TestCase):
    def test_overlapping_suffixes_do_not_rescan_column_pairs(self):
        from docreader.parser.excel_wide_table_dry_run import (
            _column_similarity, _profile_columns,
        )
        from docreader.parser.spreadsheet_schema import _detect_wide_entity_interval

        frame = pd.DataFrame([["value"] * 12] * 100)
        profiles = _profile_columns(frame)
        with patch(
            "docreader.parser.excel_wide_table_dry_run._infer_payload_interval",
            return_value=(None, None, None, "fallback"),
        ), patch(
            "docreader.parser.excel_wide_table_dry_run._column_similarity",
            wraps=_column_similarity,
        ) as compare:
            _detect_wide_entity_interval(frame, profiles)
        self.assertLessEqual(compare.call_count, 12 * 11 // 2)

    def test_row_fallback_profiles_once_and_keeps_final_entity(self):
        from docreader.parser.excel_wide_table_dry_run import _profile_columns

        frame = pd.DataFrame(
            [[f"item-{i}", "repeat"] for i in range(1001)],
            columns=["Identifier", "Value"],
        )
        with patch(
            "docreader.parser.excel_wide_table_dry_run._profile_columns",
            wraps=_profile_columns,
        ) as profile:
            schema = detect_sheet_schema(frame, "Records")
        self.assertEqual(profile.call_count, 1)
        self.assertEqual(schema.entity_axis, "rows")
        self.assertEqual(schema.entity_count, 1001)
        self.assertEqual(schema.entity_headers[-1], "item-1000")
        self.assertEqual(schema.records[-1], {
            "common": {"entity": "item-1000"}, "entities": {"Value": "repeat"},
        })

    def test_detects_complete_wide_entity_axis(self):
        frame = pd.DataFrame(
            [
                ["Performance", "x", "100", "200", "300", "400"],
                ["WLAN", "x", "Supported", "Not supported", "Supported", "Supported"],
                ["SD-WAN", "x", "Supported", "Supported", "Not supported", "Supported"],
            ],
            columns=["Feature", "Description", "Model-A", "Model-B", "Model-C", "Model-D"],
        )
        schema = detect_sheet_schema(frame, "SPEC")
        self.assertEqual(schema.entity_axis, "columns")
        self.assertEqual(schema.common_columns, ["Feature", "Description"])
        self.assertEqual(schema.entity_headers, ["Model-A", "Model-B", "Model-C", "Model-D"])
        self.assertEqual(schema.entity_count, 4)
        self.assertGreater(schema.confidence, 0)

    def test_ambiguous_sheet_remains_unknown(self):
        frame = pd.DataFrame([["paragraph", None], [None, "note"]], columns=["A", "B"])
        schema = detect_sheet_schema(frame, "Notes")
        self.assertEqual(schema.entity_axis, "unknown")


if __name__ == "__main__":
    unittest.main()
