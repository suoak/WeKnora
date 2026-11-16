"""Direct row conversion must match the active pandas/openpyxl implementation."""

import datetime
import io
import unittest
from unittest.mock import patch

import openpyxl
import pandas as pd

from docreader.parser.xlsx_rows import readonly_sheet_data


class ReadonlyScalarRowsTest(unittest.TestCase):
    def workbook(self, epoch):
        book = openpyxl.Workbook()
        book.epoch = epoch
        sheet = book.active
        sheet.append(["ID", "number", "boolean", "date", "formula", "error"])
        sheet.append(["00001", 2.5, True, datetime.datetime(2025, 1, 2), "=1+2", "#DIV/0!"])
        sheet.append([None, -2, False, datetime.timedelta(seconds=90), None, "#N/A"])
        sheet["A7"] = "tail"
        sheet["H8"] = "last"
        sheet["A10"] = ""  # Empty trailing rows must not survive.
        stream = io.BytesIO()
        book.save(stream)
        return stream.getvalue()

    def test_values_sparse_rows_dates_errors_and_row_limits_match_pandas(self):
        from openpyxl.utils.datetime import CALENDAR_MAC_1904, CALENDAR_WINDOWS_1900

        for epoch in [CALENDAR_MAC_1904, CALENDAR_WINDOWS_1900]:
            for needed in [None, 0, 1, 2, 4, 7, 8, 20]:
                with self.subTest(epoch=epoch, needed=needed):
                    with pd.ExcelFile(io.BytesIO(self.workbook(epoch)), engine="openpyxl") as excel:
                        reader = excel._reader
                        sheet = excel.book.worksheets[0]
                        fallback = type(reader).get_sheet_data
                        expected = fallback(reader, sheet, needed)
                        actual = readonly_sheet_data(reader, sheet, needed, fallback)
                        pd.testing.assert_frame_equal(pd.DataFrame(actual), pd.DataFrame(expected))

    def test_supported_reader_does_not_allocate_readonly_cell_wrappers(self):
        with pd.ExcelFile(io.BytesIO(self.workbook(openpyxl.utils.datetime.CALENDAR_WINDOWS_1900)),
                          engine="openpyxl") as excel:
            reader = excel._reader
            with patch("openpyxl.worksheet._read_only.ReadOnlyCell",
                       side_effect=AssertionError("per-cell wrapper allocated")):
                rows = readonly_sheet_data(reader, excel.book.worksheets[0], None,
                                           type(reader).get_sheet_data)
            self.assertEqual(rows[-1][-1], "last")
            self.assertEqual(len(rows), 8)

    def test_unknown_reader_uses_original_implementation(self):
        from unittest.mock import Mock

        fallback = Mock(return_value=[["original"]])
        reader, sheet = object(), object()
        self.assertEqual(readonly_sheet_data(reader, sheet, 3, fallback), [["original"]])
        fallback.assert_called_once_with(reader, sheet, 3)

    def test_unreviewed_dependency_version_uses_original_implementation(self):
        from unittest.mock import Mock

        with pd.ExcelFile(io.BytesIO(self.workbook(openpyxl.utils.datetime.CALENDAR_WINDOWS_1900)),
                          engine="openpyxl") as excel:
            for module in [openpyxl, pd]:
                with self.subTest(module=module.__name__):
                    fallback = Mock(return_value=[["original"]])
                    with patch.object(module, "__version__", "unreviewed"):
                        result = readonly_sheet_data(excel._reader, excel.book.worksheets[0], 2, fallback)
                    self.assertEqual(result, [["original"]])
                    fallback.assert_called_once_with(excel._reader, excel.book.worksheets[0], 2)


if __name__ == "__main__":
    unittest.main()
