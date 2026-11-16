"""Large-sheet scanning must retain conservative admission at the final row."""

import unittest

import numpy as np
import pandas as pd

from docreader.parser.excel_parser import _is_conservative_row_table


class ExcelRowScanTest(unittest.TestCase):
    def test_admission_checks_all_row_shapes_against_complete_reference(self):
        import itertools
        headers = ["Key", "Value", "Tail"]
        for row in itertools.product((None, "Key", "Value", "Tail", "field"), repeat=3):
            frame = pd.DataFrame([["first", "record", "data"], row])
            full_row = ["" if pd.isna(v) else str(v).strip() for v in row]
            expected = sum(pd.notna(v) for v in row) >= 2 and full_row != headers
            self.assertEqual(_is_conservative_row_table(headers, frame), expected, row)

    def test_final_repeated_header_and_sparse_note_are_rejected(self):
        rows = [[f"key-{i}", "value"] for i in range(4096)]
        self.assertTrue(_is_conservative_row_table(["Key", "Value"], pd.DataFrame(rows)))
        for final_row in [["Key", "Value"], ["note", None]]:
            with self.subTest(final_row=final_row):
                self.assertFalse(_is_conservative_row_table(
                    ["Key", "Value"], pd.DataFrame([*rows, final_row]),
                ))

    def test_numeric_row_coercion_still_recognizes_repeated_header(self):
        frame = pd.DataFrame({"A": [1], "B": [2.0]}, index=[99])
        self.assertFalse(_is_conservative_row_table(["1.0", "2.0"], frame))

    def test_float32_scalar_spelling_is_preserved(self):
        frame = pd.DataFrame([[0.1, 0.2]], dtype=np.float32)
        self.assertFalse(_is_conservative_row_table(["0.1", "0.2"], frame))

    def test_nullable_rows_and_nonpositional_index(self):
        frame = pd.DataFrame({
            "A": pd.array([1, 2, pd.NA], dtype="Int64"),
            "B": pd.array(["one", "two", pd.NA], dtype="string"),
        }, index=[11, 22, 33])
        self.assertTrue(_is_conservative_row_table(["Key", "Value"], frame))


if __name__ == "__main__":
    unittest.main()
