"""Read-only merge expansion must preserve the existing parser's cell values."""

import datetime
import io
import unittest
import zipfile
from unittest.mock import patch

import openpyxl
import pandas as pd

from docreader.parser.excel_parser import ExcelParser, _open_excel_file
from docreader.parser.xlsx_merge import _MergeRanges


def workbook_bytes():
    book = openpyxl.Workbook()
    sheet = book.active
    sheet.title = "Numbers"
    sheet.append(["identifier", "value", "note"])
    sheet.append(["0001", 1, "NA"])
    sheet.append([None, 2.5, "tail"])
    sheet.merge_cells("A2:A3")
    sheet.append(["0002", None, "last"])
    sheet.merge_cells("B4:B6")  # Blank master must not invent extra data rows.
    sheet["D7"] = "expanded"
    sheet.merge_cells("D7:E9")  # Fill extends past the original stored cells.
    other = book.create_sheet("Dates")
    other.append(["day", "enabled", "formula", "error"])
    other.append([datetime.datetime(2025, 1, 2), True, "=1+2", "#N/A"])
    other.append([None, False, None, "ok"])
    other.merge_cells("A2:A3")
    book.create_sheet("Empty")
    output = io.BytesIO()
    book.save(output)
    return output.getvalue()


class ReadonlyMergedFramesTests(unittest.TestCase):
    def test_fast_rows_preserve_pandas_scalar_conversion(self):
        import numpy as np
        from docreader.parser.excel_parser import _iter_frame_rows
        frames = [
            pd.DataFrame([[0.1, 0.2]], dtype=np.float32),
            pd.DataFrame({"a": [1], "b": [2.0]}),
            pd.DataFrame({"a": pd.to_datetime(["2025-01-02", None])}),
            pd.DataFrame({"a": pd.to_timedelta([1, None], unit="s")}),
            pd.DataFrame({"a": pd.to_datetime(["2025-01-02", None], utc=True)}),
            pd.DataFrame({"a": pd.array([1, pd.NA], dtype="Int64")}),
        ]
        for frame in frames:
            actual = [(index, [str(x) for x in row]) for index, row in _iter_frame_rows(frame)]
            expected = [(index, [str(x) for x in row.tolist()]) for index, row in frame.iterrows()]
            self.assertEqual(actual, expected)

    def test_integer_length_variance_matches_exact_statistics(self):
        import random
        import statistics
        from docreader.parser.excel_wide_table_dry_run import _length_variance
        rng = random.Random(17)
        groups = [[], [0], [2**54, 2**54 + 1], [1] * 100001]
        groups += [[rng.randrange(32768) for _ in range(rng.randrange(2, 100))] for _ in range(100)]
        for values in groups:
            expected = statistics.pvariance(values) if len(values) > 1 else 0.0
            self.assertEqual(_length_variance(values), expected)

    def test_merge_scan_refuses_entities_and_malformed_tail(self):
        from docreader.parser.xlsx_merge import _read_merge_ranges
        for content in (
            b'<!DOCTYPE worksheet [<!ENTITY x "secret">]><worksheet>&x;</worksheet>',
            b'<!DOCTYPE worksheet [<!ENTITY x SYSTEM "file:///unreadable">]><worksheet>&x;</worksheet>',
            b'<worksheet></worksheet><unclosed>',
        ):
            with self.subTest(content=content):
                with self.assertRaises(Exception):
                    _read_merge_ranges(io.BytesIO(content))

    def test_fill_comparison_matches_complete_rowwise_reference(self):
        import itertools
        from docreader.parser.excel_wide_table_dry_run import _FillPattern, _fill_similarity
        for size in range(6):
            patterns = list(itertools.product((False, True), repeat=size))
            for left in patterns:
                for right in patterns:
                    expected = sum(a == b for a, b in zip(left, right)) / len(left) if left else 1
                    self.assertEqual(_fill_similarity(_FillPattern(left), _FillPattern(right)), expected)
        left = (True,) * 100001
        right = (True,) * 100000 + (False,)
        self.assertEqual(_fill_similarity(_FillPattern(left), _FillPattern(right)), 100000 / 100001)
        self.assertEqual(_fill_similarity(_FillPattern(left), _FillPattern((True,))), 1 / 100001)

    def test_column_profile_serialization_keeps_boolean_sequence(self):
        import json
        from dataclasses import asdict
        from docreader.parser.excel_wide_table_dry_run import _profile_columns
        profile = _profile_columns(pd.DataFrame({"value": [1, None, 2]}))[0]
        encoded = json.loads(json.dumps(asdict(profile)))
        self.assertEqual(encoded['fill_pattern'], [True, False, True])
        self.assertNotIn('mask', encoded)

    def test_profiles_keep_numeric_boolean_and_unicode_categories_distinct(self):
        from docreader.parser.excel_wide_table_dry_run import _profile_columns, _value_shape
        values = [True, 1, 1.0, "1", "true", "x²", "a\u0085b", "a\u2003b", None]
        profile = _profile_columns(pd.DataFrame({"value": values}))[0]
        self.assertEqual(profile.numeric_ratio, 3 / 8)
        self.assertEqual(profile.boolean_enum_ratio, 1 / 4)
        self.assertEqual(profile.fill_pattern, (True,) * 8 + (False,))
        self.assertEqual(profile.value_shape_distribution, {
            "boolean": 2 / 8, "number": 3 / 8,
            "identifier": 1 / 8, "text_with_space": 2 / 8,
        })
        for char in (chr(i) for i in range(0x110000) if chr(i).isspace()):
            self.assertEqual(_value_shape("a" + char + "b"), "text_with_space")

    def test_complete_profiles_include_values_beyond_cache_capacity(self):
        from docreader.parser.excel_wide_table_dry_run import _profile_columns
        values = [f"id{i}" for i in range(9000)] + ["a b"]
        profile = _profile_columns(pd.DataFrame({"value": values}))[0]
        self.assertEqual(profile.fill_rate, 1)
        self.assertEqual(profile.distinct_ratio, 1)
        self.assertEqual(profile.value_shape_distribution["text_with_space"], 1 / 9001)

    def test_frames_match_rewritten_workbook_before_type_inference(self):
        content = workbook_bytes()
        with patch("docreader.parser.excel_parser.READONLY_MERGE_FILL_MIN_BYTES", 1 << 60):
            legacy = _open_excel_file(content, "xlsx")
        with patch("docreader.parser.excel_parser.READONLY_MERGE_FILL_MIN_BYTES", 0):
            optimized = _open_excel_file(content, "xlsx")
        try:
            self.assertEqual(legacy.sheet_names, optimized.sheet_names)
            for sheet in legacy.sheet_names:
                for header in (None, 0):
                    with self.subTest(sheet=sheet, header=header):
                        pd.testing.assert_frame_equal(
                            legacy.parse(sheet, header=header),
                            optimized.parse(sheet, header=header),
                        )
        finally:
            legacy.close()
            optimized.close()

    def test_parser_content_spans_and_metadata_match_for_each_mode(self):
        content = workbook_bytes()
        for mode in ("auto", "legacy"):
            for first_header in (True, False):
                with self.subTest(mode=mode, first_header=first_header):
                    parser = ExcelParser(
                        file_type="xlsx", xlsx_chunking_mode=mode,
                        xlsx_first_row_as_header=first_header,
                    )
                    with patch("docreader.parser.excel_parser.READONLY_MERGE_FILL_MIN_BYTES", 1 << 60):
                        expected = parser.parse_into_text(content)
                    with patch("docreader.parser.excel_parser.READONLY_MERGE_FILL_MIN_BYTES", 0):
                        actual = parser.parse_into_text(content)
                    self.assertEqual(actual.model_dump(), expected.model_dump())

    def test_optimized_reader_never_builds_mutable_workbook(self):
        original = openpyxl.load_workbook
        def checked(*args, **kwargs):
            self.assertTrue(kwargs.get("read_only"))
            return original(*args, **kwargs)
        with patch("docreader.parser.excel_parser.READONLY_MERGE_FILL_MIN_BYTES", 0), \
                patch("openpyxl.load_workbook", side_effect=checked):
            self.assertIn("expanded", ExcelParser(file_type="xlsx").parse_into_text(workbook_bytes()).content)

    def test_unknown_reader_layout_preserves_legacy_merge_fill(self):
        with patch("docreader.parser.excel_parser.READONLY_MERGE_FILL_MIN_BYTES", 0), \
                patch("docreader.parser.excel_parser.install_readonly_merge_fill", return_value=False):
            with _open_excel_file(workbook_bytes(), "xlsx") as reader:
                frame = reader.parse("Numbers", header=None)
                self.assertEqual(frame.iloc[8, 4], "expanded")

    def test_nonconventional_sheet_part_uses_relationship_path(self):
        content = workbook_bytes()
        out = io.BytesIO()
        with zipfile.ZipFile(io.BytesIO(content)) as source, zipfile.ZipFile(out, "w") as target:
            for info in source.infolist():
                data = source.read(info)
                name = info.filename
                if name == "xl/worksheets/sheet2.xml":
                    name = "xl/worksheets/custom-part.xml"
                elif name in ("xl/_rels/workbook.xml.rels", "[Content_Types].xml"):
                    data = data.replace(b"worksheets/sheet2.xml", b"worksheets/custom-part.xml")
                target.writestr(name, data)
        with patch("docreader.parser.excel_parser.READONLY_MERGE_FILL_MIN_BYTES", 0):
            with _open_excel_file(out.getvalue(), "xlsx") as reader:
                frame = reader.parse("Dates", header=None)
                self.assertEqual(frame.iloc[1, 0], frame.iloc[2, 0])

    def test_merge_scanner_ignores_foreign_or_nested_extension_elements(self):
        from defusedxml.ElementTree import XMLParser
        target = _MergeRanges()
        parser = XMLParser(target=target)
        parser.feed(b'<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:f="urn:foreign"><extLst><mergeCells><mergeCell ref="A1:B2"/></mergeCells></extLst><f:mergeCells><f:mergeCell ref="A1:C3"/></f:mergeCells><mergeCells><mergeCell ref="D1:E2"/></mergeCells></worksheet>')
        parser.close()
        self.assertEqual(target.refs, ["D1:E2"])

    def test_last_sheet_malformed_tail_is_rejected(self):
        content = workbook_bytes()
        out = io.BytesIO()
        with zipfile.ZipFile(io.BytesIO(content)) as source, zipfile.ZipFile(out, "w") as target:
            for info in source.infolist():
                data = source.read(info)
                if info.filename == "xl/worksheets/sheet3.xml":
                    data += b"<unclosed>"
                target.writestr(info, data)
        with patch("docreader.parser.excel_parser.READONLY_MERGE_FILL_MIN_BYTES", 0):
            with self.assertRaises(Exception):
                _open_excel_file(out.getvalue(), "xlsx")


if __name__ == "__main__":
    unittest.main()
