"""Shared-string acceleration retains all text and the original rich-text rules."""

import io
import unittest
from unittest.mock import patch

from openpyxl.reader.strings import read_string_table

from docreader.parser.xlsx_strings import load_readonly_workbook, read_plain_string_table


class PlainSharedStringsTest(unittest.TestCase):
    def table(self, body):
        return ('<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">'
                + body + '</sst>').encode()

    def test_plain_rich_phonetic_empty_escaped_and_repeated_text_match_library(self):
        body = '''<si><t xml:space="preserve">  a &amp; b  </t></si>
        <si><t>_x005F_x0041_</t></si><si/><si><t/></si>
        <si><r><rPr><b/></rPr><t>rich</t></r><r><t> text</t></r></si>
        <si><t>plain</t><rPh sb="0" eb="1"><t>phonetic</t></rPh></si>
        <si><t>first</t><t>last</t></si><si><t>中文😀</t></si>'''
        raw = self.table(body)
        self.assertEqual(read_plain_string_table(io.BytesIO(raw)), read_string_table(io.BytesIO(raw)))

    def test_plain_strings_avoid_text_model_construction(self):
        raw = self.table('<si><t>last cell</t></si><si/>')
        with patch('openpyxl.cell.text.Text.from_tree', side_effect=AssertionError('model allocated')):
            self.assertEqual(read_plain_string_table(io.BytesIO(raw)), ['last cell', ''])

    def test_unusual_invalid_layout_preserves_library_refusal(self):
        raw = self.table('<si unexpected="invalid"><t>value</t></si>')
        for reader in [read_string_table, read_plain_string_table]:
            with self.subTest(reader=reader.__name__), self.assertRaises(TypeError):
                reader(io.BytesIO(raw))

    def test_truncated_xml_preserves_library_refusal(self):
        raw = self.table('<si><t>value</t></si>')[:-3]
        for reader in [read_string_table, read_plain_string_table]:
            with self.subTest(reader=reader.__name__), self.assertRaises(Exception):
                reader(io.BytesIO(raw))

    def test_unreviewed_versions_do_not_open_workbook(self):
        with patch('openpyxl.__version__', 'unreviewed'):
            self.assertIsNone(load_readonly_workbook(io.BytesIO(b'not opened')))


if __name__ == '__main__':
    unittest.main()
