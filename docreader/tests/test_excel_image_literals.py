import unittest
from io import BytesIO

from openpyxl import Workbook

from docreader.parser.excel_parser import ExcelParser, _literal_cell_image_markup


class ExcelImageLiteralsTest(unittest.TestCase):
    def test_plain_cells_keep_exact_text(self):
        for value in ["plain text", "a < b", "![unfinished", "path\\file", "[ordinary](link)"]:
            expected = "!\\[unfinished" if value == "![unfinished" else value
            self.assertEqual(_literal_cell_image_markup(value), expected)

    def test_image_syntax_is_literal_in_headers_and_values(self):
        workbook = Workbook()
        sheet = workbook.active
        sheet.append(["![header](header.png)", "Details", "HTML"])
        sheet.append(["row-1", "![quoted](http://169.254.169.254/image.png)", '<IMG src="missing.png">'])
        sheet.append(["row-2", r"\![already escaped](missing.png)", "ordinary value"])
        data = BytesIO()
        workbook.save(data)
        workbook.close()

        raw = data.getvalue()
        result = ExcelParser(file_name="source.xlsx", xlsx_chunking_mode="row-aware").parse_into_text(raw)
        self.assertIn("!\\[header](header.png)", result.content)
        self.assertIn("!\\[quoted](http://169.254.169.254/image.png)", result.content)
        self.assertIn('&lt;IMG src="missing.png">', result.content)
        self.assertNotIn("![", result.content)
        self.assertNotIn("<IMG", result.content)
        self.assertIn("ordinary value", result.content)
        self.assertEqual(raw, data.getvalue())
        for chunk in result.chunks:
            self.assertEqual(result.content[chunk.start:chunk.end], chunk.content)
        for segment in result.segments:
            for chunk in segment.chunks:
                start, end = segment.start + chunk.start, segment.start + chunk.end
                self.assertEqual(result.content[start:end], chunk.content)
        for block in result.source_blocks:
            self.assertGreater(block["end"], block["start"])
            self.assertLessEqual(block["end"], len(result.content))


if __name__ == "__main__":
    unittest.main()
