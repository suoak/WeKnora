import unittest

import pandas as pd

from docreader.parser.spreadsheet_schema import detect_sheet_schema


class SpreadsheetSchemaTest(unittest.TestCase):
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
