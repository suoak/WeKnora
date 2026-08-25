import io
import os
import shutil
import subprocess
import tempfile
import unittest
import zipfile
from unittest.mock import patch

import openpyxl
import pandas as pd
from openpyxl.chart import BarChart, Reference

from docreader.models.document import ChunkingPolicy
from docreader.parser.excel_convert import detect_excel_format, engine_for_format
from docreader.parser.excel_parser import ExcelParser
from docreader.parser.xlsx_merge import fill_merged_cells_xlsx
from docreader.parser.xlsx_repair import repair_xlsx_bytes


def _xlsx_with_phantom_shared_strings() -> bytes:
    """Workbook with inline strings but a dangling sharedStrings manifest entry."""
    wb = openpyxl.Workbook()
    ws = wb.active
    ws["A1"] = "hello"
    ws["B1"] = 42
    bio = io.BytesIO()
    wb.save(bio)

    with tempfile.TemporaryDirectory() as tmpdir:
        with zipfile.ZipFile(io.BytesIO(bio.getvalue()), "r") as zin:
            zin.extractall(tmpdir)

        ct_path = f"{tmpdir}/[Content_Types].xml"
        with open(ct_path, encoding="utf-8") as f:
            ct = f.read()
        override = (
            '<Override PartName="/xl/sharedStrings.xml" '
            'ContentType="application/vnd.openxmlformats-officedocument.'
            'spreadsheetml.sharedStrings+xml"/>'
        )
        with open(ct_path, "w", encoding="utf-8") as f:
            f.write(ct.replace("</Types>", override + "</Types>"))

        out = io.BytesIO()
        with zipfile.ZipFile(out, "w", zipfile.ZIP_DEFLATED) as zout:
            for root, _, files in os.walk(tmpdir):
                for name in files:
                    path = os.path.join(root, name)
                    arc = os.path.relpath(path, tmpdir)
                    zout.write(path, arc)
        return out.getvalue()


class ExcelFormatDetectionTest(unittest.TestCase):
    def test_detect_xlsx_and_engine(self):
        wb = openpyxl.Workbook()
        bio = io.BytesIO()
        wb.save(bio)
        content = bio.getvalue()
        self.assertEqual(detect_excel_format(content), "xlsx")
        self.assertEqual(engine_for_format("xlsx"), "openpyxl")

    def test_detect_xls_magic(self):
        content = b"\xd0\xcf\x11\xe0\xa1\xb1\x1a\xe1" + b"\x00" * 512
        self.assertEqual(detect_excel_format(content), "xls")
        self.assertEqual(engine_for_format("xls"), "xlrd")

    def test_open_legacy_xls_bytes_with_xlsx_extension(self):
        if not shutil.which("soffice"):
            self.skipTest("LibreOffice not available")
        wb = openpyxl.Workbook()
        ws = wb.active
        ws["A1"] = "legacy"
        xlsx_bio = io.BytesIO()
        wb.save(xlsx_bio)
        with tempfile.TemporaryDirectory() as tmpdir:
            src = os.path.join(tmpdir, "sheet.xlsx")
            with open(src, "wb") as handle:
                handle.write(xlsx_bio.getvalue())
            subprocess.run(
                [
                    "soffice",
                    "--headless",
                    "--convert-to",
                    "xls",
                    "--outdir",
                    tmpdir,
                    src,
                ],
                check=True,
                capture_output=True,
            )
            xls_path = os.path.join(tmpdir, "sheet.xls")
            with open(xls_path, "rb") as handle:
                xls_bytes = handle.read()

        document = ExcelParser(file_name="fake.xlsx", file_type="xlsx").parse_into_text(
            xls_bytes
        )
        self.assertIn("legacy", document.content)


class XlsxRepairTest(unittest.TestCase):
    def test_repair_removes_phantom_shared_strings_reference(self):
        broken = _xlsx_with_phantom_shared_strings()
        with self.assertRaises(KeyError):
            pd.read_excel(io.BytesIO(broken))

        repaired = repair_xlsx_bytes(broken)
        self.assertIsNotNone(repaired)
        df = pd.read_excel(io.BytesIO(repaired), header=None)
        self.assertEqual(df.values.tolist(), [["hello", 42]])

    def test_repair_skips_when_shared_string_cells_need_table(self):
        import xlsxwriter

        bio = io.BytesIO()
        wb = xlsxwriter.Workbook(bio, {"in_memory": True})
        ws = wb.add_worksheet()
        ws.write(0, 0, "hello")
        wb.close()

        with tempfile.TemporaryDirectory() as tmpdir:
            with zipfile.ZipFile(io.BytesIO(bio.getvalue()), "r") as zin:
                zin.extractall(tmpdir)
            os.remove(f"{tmpdir}/xl/sharedStrings.xml")

            out = io.BytesIO()
            with zipfile.ZipFile(out, "w", zipfile.ZIP_DEFLATED) as zout:
                for root, _, files in os.walk(tmpdir):
                    for name in files:
                        path = os.path.join(root, name)
                        arc = os.path.relpath(path, tmpdir)
                        zout.write(path, arc)
            broken = out.getvalue()

        self.assertIsNone(repair_xlsx_bytes(broken))


class XlsxMergeFillTest(unittest.TestCase):
    @staticmethod
    def _workbook_with_merged_cells_and_chart() -> bytes:
        wb = openpyxl.Workbook()
        ws = wb.active
        ws["A1"] = "title"
        ws.merge_cells("A1:B1")
        ws.append(["category", "value"])
        ws.append(["one", 1])
        ws.append(["two", 2])

        chart = BarChart()
        chart.add_data(
            Reference(ws, min_col=2, min_row=2, max_row=4),
            titles_from_data=True,
        )
        chart.set_categories(Reference(ws, min_col=1, min_row=3, max_row=4))
        ws.add_chart(chart, "D2")

        bio = io.BytesIO()
        wb.save(bio)
        return bio.getvalue()

    def test_fill_merged_cells_propagates_master_value(self):
        wb = openpyxl.Workbook()
        ws = wb.active
        ws["A1"] = "title"
        ws.merge_cells("A1:B1")
        ws["A2"] = "left"
        ws["B2"] = "right"
        ws.merge_cells("A2:A3")
        ws["B3"] = "only-b"
        bio = io.BytesIO()
        wb.save(bio)

        filled = fill_merged_cells_xlsx(bio.getvalue())
        out_wb = openpyxl.load_workbook(io.BytesIO(filled), data_only=True)
        out_ws = out_wb.active
        self.assertEqual(out_ws["B1"].value, "title")
        self.assertEqual(out_ws["A3"].value, "left")
        self.assertEqual(out_ws["B3"].value, "only-b")

    def test_fill_merged_cells_does_not_serialize_charts(self):
        content = self._workbook_with_merged_cells_and_chart()

        with patch(
            "openpyxl.chart._chart.ChartBase._write",
            side_effect=AttributeError("'Typed' object has no attribute 'to_tree'"),
        ):
            filled = fill_merged_cells_xlsx(content)

        out_wb = openpyxl.load_workbook(io.BytesIO(filled), data_only=True)
        out_ws = out_wb.active
        self.assertEqual(out_ws["B1"].value, "title")
        self.assertEqual(out_ws["A3"].value, "one")
        self.assertEqual(out_ws["B4"].value, 2)
        self.assertEqual(out_ws._charts, [])

    def test_excel_parser_reads_merged_workbook_with_chart(self):
        document = ExcelParser().parse_into_text(
            self._workbook_with_merged_cells_and_chart()
        )

        self.assertIn("A: title,B: title", document.content)
        self.assertIn("A: one,B: 1", document.content)
        self.assertIn("A: two,B: 2", document.content)

    def test_chart_workbook_without_merges_passes_through_unchanged(self):
        content = self._workbook_with_merged_cells_and_chart()
        wb = openpyxl.load_workbook(io.BytesIO(content))
        wb.active.unmerge_cells("A1:B1")
        bio = io.BytesIO()
        wb.save(bio)
        content = bio.getvalue()

        self.assertEqual(fill_merged_cells_xlsx(content), content)

    def test_fill_does_not_serialize_dedicated_chart_sheets(self):
        content = self._workbook_with_merged_cells_and_chart()
        wb = openpyxl.load_workbook(io.BytesIO(content))
        ws = wb.active
        chart = BarChart()
        chart.add_data(
            Reference(ws, min_col=2, min_row=2, max_row=4),
            titles_from_data=True,
        )
        chart.set_categories(Reference(ws, min_col=1, min_row=3, max_row=4))
        chart_sheet = wb.create_chartsheet("Summary")
        chart_sheet.add_chart(chart)
        wb.active = chart_sheet
        bio = io.BytesIO()
        wb.save(bio)

        with patch(
            "openpyxl.chart._chart.ChartBase._write",
            side_effect=AttributeError("'Typed' object has no attribute 'to_tree'"),
        ):
            filled = fill_merged_cells_xlsx(bio.getvalue())

        out_wb = openpyxl.load_workbook(io.BytesIO(filled), data_only=True)
        self.assertEqual(out_wb.active._charts, [])
        self.assertEqual(out_wb.active.title, "Sheet")
        self.assertEqual(out_wb.chartsheets, [])

    def test_fill_keeps_active_worksheet_after_removing_earlier_chart_sheet(self):
        content = self._workbook_with_merged_cells_and_chart()
        wb = openpyxl.load_workbook(io.BytesIO(content))
        data_sheet = wb.active
        chart = BarChart()
        chart.add_data(
            Reference(data_sheet, min_col=2, min_row=2, max_row=4),
            titles_from_data=True,
        )
        chart_sheet = wb.create_chartsheet("Summary", 0)
        chart_sheet.add_chart(chart)
        wb.active = data_sheet
        bio = io.BytesIO()
        wb.save(bio)

        filled = fill_merged_cells_xlsx(bio.getvalue())

        out_wb = openpyxl.load_workbook(io.BytesIO(filled), data_only=True)
        self.assertEqual(out_wb.active.title, "Sheet")
        self.assertEqual(out_wb.chartsheets, [])

    def test_parse_en_mergecell_workbook(self):
        path = os.path.join(
            os.path.dirname(__file__),
            "..",
            "..",
            "testdata",
            "rag_test",
            "xlsx",
            "en_mergecell.xlsx",
        )
        if not os.path.isfile(path):
            self.skipTest("en_mergecell.xlsx fixture not available")
        with open(path, "rb") as handle:
            document = ExcelParser().parse_into_text(handle.read())

        chunks = [chunk.content.strip() for chunk in document.chunks]
        self.assertEqual(len(chunks), 12)
        self.assertIn("A: A1", chunks[0])
        self.assertIn("A: A2", chunks[1])
        self.assertIn("B: B3", chunks[2])
        self.assertNotIn("Unnamed:", document.content)
        self.assertIn("A: A7", chunks[6])
        self.assertIn("A: A7", chunks[7])
        self.assertIn("D: D10", chunks[9])


class ExcelImageFilterTest(unittest.TestCase):
    """Tests for filtering embedded image function strings (#1779)."""

    def _xlsx_with_image_functions(self) -> bytes:
        """Create an XLSX where image functions are stored as text values.

        WPS embeds images using =DISPIMG("ID",1) which may appear as plain
        text (not a formula) in some export scenarios.
        """
        wb = openpyxl.Workbook()
        ws = wb.active
        ws["A1"] = "Name"
        ws["B1"] = "Photo"
        ws["A2"] = "Alice"
        ws["B2"] = '_xlfn.DISPIMG("ID_ABCDEF123",1)'
        ws["A3"] = "Bob"
        ws["B3"] = '_xlfn.DISPIMG("ID_GHIJKL456",1)'
        ws["A4"] = "Charlie"
        ws["B4"] = "real data"
        bio = io.BytesIO()
        wb.save(bio)
        return bio.getvalue()

    def test_dispimg_text_values_are_excluded(self):
        """Image function strings stored as text must not appear in output."""
        document = ExcelParser().parse_into_text(self._xlsx_with_image_functions())
        self.assertNotIn("DISPIMG", document.content)
        self.assertNotIn("_xlfn", document.content)
        # Real data must still be present
        self.assertIn("Alice", document.content)
        self.assertIn("Bob", document.content)
        self.assertIn("Charlie", document.content)
        self.assertIn("real data", document.content)

    def test_dispimg_with_equals_prefix(self):
        """=_xlfn.DISPIMG(...) as text (not formula) should also be filtered."""
        wb = openpyxl.Workbook()
        ws = wb.active
        ws["A1"] = "Name"
        ws["B1"] = "Photo"
        ws["A2"] = "Alice"
        # Stored as text with = prefix (some exporters do this)
        ws["B2"] = '=_xlfn.DISPIMG("ID_123",1)'
        bio = io.BytesIO()
        wb.save(bio)
        # Note: openpyxl treats strings starting with = as formulas,
        # so data_only=True in fill_merged_cells_xlsx will turn them to None.
        # This test verifies the formula path also works correctly.
        document = ExcelParser().parse_into_text(bio.getvalue())
        self.assertNotIn("DISPIMG", document.content)
        self.assertIn("Alice", document.content)

    def test_image_function_variations(self):
        """Various image function patterns should all be filtered."""
        wb = openpyxl.Workbook()
        ws = wb.active
        ws["A1"] = '_xlfn.DISPIMG("ID_001",1)'
        ws["A2"] = 'DISPIMG("ID_002",1)'  # no prefix at all
        ws["A3"] = '_xlfn.IMAGE("https://example.com/img.png")'
        ws["A4"] = 'IMAGE("https://example.com/img.png",1)'
        ws["A5"] = "Normal text"
        bio = io.BytesIO()
        wb.save(bio)
        document = ExcelParser().parse_into_text(bio.getvalue())
        self.assertNotIn("DISPIMG", document.content)
        self.assertNotIn("IMAGE(", document.content)
        self.assertIn("Normal text", document.content)

    def test_real_world_wps_dispimg(self):
        """Exact pattern from issue #1779 screenshot: =DISPIMG("ID_...",1)."""
        wb = openpyxl.Workbook()
        ws = wb.active
        ws["A1"] = "Product"
        ws["B1"] = "Image"
        ws["C1"] = "Price"
        ws["A2"] = "Basic"
        # This is the exact format shown in the issue screenshot
        ws["B2"] = 'DISPIMG("ID_5A60F9ED501E48A38EBEE5D326E18235",1)'
        ws["C2"] = "39.9"
        ws["A3"] = "Pro"
        ws["B3"] = 'DISPIMG("ID_AABBCCDD",1)'
        ws["C3"] = "99.9"
        bio = io.BytesIO()
        wb.save(bio)
        document = ExcelParser().parse_into_text(bio.getvalue())
        self.assertNotIn("DISPIMG", document.content)
        self.assertNotIn("ID_5A60F9ED", document.content)
        self.assertIn("Basic", document.content)
        self.assertIn("39.9", document.content)
        self.assertIn("Pro", document.content)
        self.assertIn("99.9", document.content)


class ExcelParserTest(unittest.TestCase):
    @staticmethod
    def _workbook_bytes(rows):
        wb = openpyxl.Workbook()
        ws = wb.active
        for row in rows:
            ws.append(row)
        bio = io.BytesIO()
        wb.save(bio)
        return bio.getvalue()

    def test_xlsx_first_row_header_mode_repeats_column_context(self):
        content = self._workbook_bytes(
            [
                ["Name", "Age", "City"],
                ["Alice", 30, "Shenzhen"],
                ["Bob", 28, "Shanghai"],
            ]
        )

        document = ExcelParser(
            file_name="people.xlsx",
            file_type="xlsx",
            xlsx_first_row_as_header="true",
        ).parse_into_text(content)

        chunks = [chunk.content.strip() for chunk in document.chunks]
        self.assertEqual(
            chunks,
            [
                "Name: Alice,Age: 30,City: Shenzhen",
                "Name: Bob,Age: 28,City: Shanghai",
            ],
        )
        self.assertNotIn("A: Name", document.content)
        self.assertEqual(
            document.chunking_policy, ChunkingPolicy.PRESERVE_PARSER_CHUNKS
        )
        self.assertEqual(document.chunks[0].metadata["parser.sheet"], "Sheet")
        self.assertEqual(document.chunks[0].metadata["parser.row"], "2")

    def test_xlsx_keeps_first_row_as_data_by_default(self):
        content = self._workbook_bytes(
            [
                ["Name", "City"],
                ["Alice", "Shenzhen"],
            ]
        )

        document = ExcelParser(
            file_name="people.xlsx", file_type="xlsx"
        ).parse_into_text(content)

        chunks = [chunk.content.strip() for chunk in document.chunks]
        self.assertEqual(len(chunks), 2)
        self.assertEqual(chunks[0], "A: Name,B: City")
        self.assertEqual(chunks[1], "A: Alice,B: Shenzhen")
        self.assertEqual(document.chunking_policy, ChunkingPolicy.DEFAULT)

    def test_single_row_xlsx_is_not_consumed_in_header_mode(self):
        content = self._workbook_bytes([["Name", "Age", "City"]])

        document = ExcelParser(
            file_name="single.xlsx",
            file_type="xlsx",
            xlsx_first_row_as_header=True,
        ).parse_into_text(content)

        self.assertEqual(
            [chunk.content.strip() for chunk in document.chunks],
            ["A: Name,B: Age,C: City"],
        )

    def test_header_mode_generates_stable_labels_for_duplicate_and_empty_cells(self):
        content = self._workbook_bytes(
            [
                ["Name", "Name", None],
                ["Alice", "Alias", "Shenzhen"],
            ]
        )

        document = ExcelParser(
            file_name="people.xlsx",
            file_type="xlsx",
            xlsx_first_row_as_header="yes",
        ).parse_into_text(content)

        self.assertEqual(
            [chunk.content.strip() for chunk in document.chunks],
            ["Name: Alice,Name__2: Alias,__column_C: Shenzhen"],
        )

    def test_empty_header_fallback_avoids_real_header_collision(self):
        content = self._workbook_bytes(
            [
                [None, "__column_A", "Name", "City", "IP"],
                ["fallback-value", "real-value", "Alice", "SZ", "10.0.0.1"],
            ]
        )
        document = ExcelParser(
            xlsx_first_row_as_header=True,
            xlsx_chunking_mode="row-aware",
        ).parse_into_text(content)
        self.assertIn("__column_A__2: fallback-value", document.chunks[0].content)
        self.assertIn("__column_A: real-value", document.chunks[0].content)

    def test_xlsx_explicit_false_override_keeps_first_row_as_data(self):
        content = self._workbook_bytes(
            [
                ["Name", "Age"],
                ["Alice", 30],
            ]
        )

        document = ExcelParser(
            file_name="people.xlsx",
            file_type="xlsx",
            xlsx_first_row_as_header="false",
        ).parse_into_text(content)

        chunks = [chunk.content.strip() for chunk in document.chunks]
        self.assertEqual(chunks[0], "A: Name,B: Age")
        self.assertEqual(chunks[1], "A: Alice,B: 30")

    def test_legacy_mode_never_declares_parser_chunks(self):
        content = self._workbook_bytes([["Name", "Age"], ["Alice", 30]])
        document = ExcelParser(
            xlsx_first_row_as_header=True, xlsx_chunking_mode="legacy"
        ).parse_into_text(content)
        self.assertEqual(document.chunking_policy, ChunkingPolicy.DEFAULT)
        self.assertEqual(document.segments, [])

    def test_mixed_workbook_produces_one_policy_segment_per_nonempty_sheet(self):
        wb = openpyxl.Workbook()
        wb.remove(wb.active)

        assets = wb.create_sheet("Assets")
        assets.append(["Asset ID", "Device", "IP"])
        assets.append(["A-1", "server-1", "10.0.0.1"])
        assets.append(["A-2", "server-2", "10.0.0.2"])

        notes = wb.create_sheet("Notes")
        notes.append(["Instructions"])
        notes.append(["Read this page first"])

        products = wb.create_sheet("Products")
        products.append(["Metric ID", "Metric", None, "MODEL-B", "MODEL-C"])
        products.append(["2024", "Connections", "50W", "50W", "100W"])
        products.append(["793", "Static ARP", "1024", "1024", "2048"])

        matrix = wb.create_sheet("Matrix")
        matrix.append(["Identity", "Network", None])
        matrix.merge_cells("B1:C1")
        matrix.append(["Asset ID", "IPv4", "IPv6"])
        matrix.append(["A-1", "10.0.0.1", "::1"])

        bio = io.BytesIO()
        wb.save(bio)
        document = ExcelParser(
            xlsx_first_row_as_header=True,
            xlsx_chunking_mode="auto",
        ).parse_into_text(bio.getvalue())

        self.assertEqual(len(document.segments), 4)
        self.assertEqual(
            [segment.metadata["parser.sheet"] for segment in document.segments],
            ["Assets", "Notes", "Products", "Matrix"],
        )
        self.assertEqual(
            [segment.chunking_policy for segment in document.segments],
            [
                ChunkingPolicy.PRESERVE_PARSER_CHUNKS,
                ChunkingPolicy.DEFAULT,
                ChunkingPolicy.PRESERVE_PARSER_CHUNKS,
                ChunkingPolicy.DEFAULT,
            ],
        )
        cursor = 0
        for segment in document.segments:
            self.assertEqual(segment.start, cursor)
            self.assertGreater(segment.end, segment.start)
            cursor = segment.end
        self.assertEqual(cursor, len(document.content))
        self.assertIn("__column_C: 50W", document.segments[2].chunks[0].content)
        self.assertEqual(len(document.segments[0].chunks), 2)
        self.assertEqual(len(document.segments[2].chunks), 2)
        self.assertEqual(document.segments[1].chunks, [])
        self.assertEqual(document.segments[3].chunks, [])

    def test_row_aware_mode_implies_first_row_header(self):
        content = self._workbook_bytes([["Name", "Age"], ["Alice", 30]])
        document = ExcelParser(xlsx_chunking_mode="row-aware").parse_into_text(content)
        self.assertEqual(
            document.chunking_policy, ChunkingPolicy.PRESERVE_PARSER_CHUNKS
        )
        self.assertEqual(document.chunks[0].content.strip(), "Name: Alice,Age: 30")

    def test_auto_rejects_single_column_text_sheet(self):
        content = self._workbook_bytes([["Notes"], ["first paragraph"], ["second"]])
        document = ExcelParser(
            xlsx_first_row_as_header=True, xlsx_chunking_mode="auto"
        ).parse_into_text(content)
        self.assertEqual(document.chunking_policy, ChunkingPolicy.DEFAULT)

    def test_auto_rejects_repeated_header_inside_data(self):
        content = self._workbook_bytes(
            [["ID", "Name"], ["1", "alpha"], ["ID", "Name"], ["2", "beta"]]
        )
        document = ExcelParser(xlsx_first_row_as_header=True).parse_into_text(content)
        self.assertEqual(document.chunking_policy, ChunkingPolicy.DEFAULT)

    def test_auto_allows_small_number_of_missing_active_headers(self):
        content = self._workbook_bytes(
            [
                [
                    "ID",
                    "Metric",
                    None,
                    "MODEL-B",
                    "MODEL-C",
                    "MODEL-D",
                    None,
                    "MODEL-F",
                    "MODEL-G",
                    "MODEL-H",
                ],
                [
                    "2024",
                    "Connections",
                    "50W",
                    "50W",
                    "100W",
                    "100W",
                    "200W",
                    "200W",
                    "300W",
                    "300W",
                ],
            ]
        )
        document = ExcelParser(xlsx_first_row_as_header=True).parse_into_text(content)
        self.assertEqual(
            document.segments[0].chunking_policy,
            ChunkingPolicy.PRESERVE_PARSER_CHUNKS,
        )
        self.assertIn("__column_C: 50W", document.chunks[0].content)
        self.assertIn("__column_G: 200W", document.chunks[0].content)

    def test_auto_rejects_missing_header_coverage_below_eighty_percent(self):
        content = self._workbook_bytes(
            [
                ["ID", None, None, "MODEL-C", "MODEL-D"],
                ["2024", "Connections", "50W", "50W", "100W"],
            ]
        )
        document = ExcelParser(xlsx_first_row_as_header=True).parse_into_text(content)
        self.assertEqual(
            document.segments[0].chunking_policy,
            ChunkingPolicy.DEFAULT,
        )

    def test_auto_rejects_merged_multi_row_header(self):
        wb = openpyxl.Workbook()
        ws = wb.active
        ws.append(["Identity", "Network", None])
        ws.merge_cells("B1:C1")
        ws.append(["Asset ID", "IPv4", "IPv6"])
        ws.append(["A-1", "10.0.0.1", "::1"])
        bio = io.BytesIO()
        wb.save(bio)

        document = ExcelParser(xlsx_first_row_as_header=True).parse_into_text(
            bio.getvalue()
        )
        self.assertEqual(document.chunking_policy, ChunkingPolicy.DEFAULT)

    def test_semantic_row_larger_than_normal_chunk_size_is_preserved(self):
        # The parser semantic limit is independent of the KB's ordinary 4000
        # character chunk target. This record is intentionally above 4000.
        headers = ["指标ID", "分类", "指标名称"] + [
            f"MODEL-{i:03d}" for i in range(220)
        ]
        values = ["2024", "系统性能容量", "最大并发连接数（IPv4+IPv6）"] + [
            "50W-value-padding" for _ in range(220)
        ]
        content = self._workbook_bytes([headers, values])
        document = ExcelParser(
            xlsx_first_row_as_header=True,
            xlsx_chunking_mode="auto",
            parser_semantic_chunk_max_chars=7500,
        ).parse_into_text(content)
        self.assertEqual(
            document.chunking_policy, ChunkingPolicy.PRESERVE_PARSER_CHUNKS
        )
        self.assertEqual(len(document.chunks), 1)
        self.assertGreater(len(document.chunks[0].content), 4000)
        self.assertIn("最大并发连接数（IPv4+IPv6）", document.chunks[0].content)
        self.assertIn("MODEL-219: 50W-value-padding", document.chunks[0].content)

    def test_oversized_semantic_row_requires_explicit_context_columns(self):
        content = self._workbook_bytes(
            [["ID", "Name", "A", "B"], ["1", "metric", "x" * 40, "y" * 40]]
        )
        with self.assertRaisesRegex(
            ValueError,
            r"parser semantic chunk too large.*sheet='Sheet'.*row=2.*size=",
        ):
            ExcelParser(
                xlsx_first_row_as_header=True,
                parser_semantic_chunk_max_chars=50,
            ).parse_into_text(content)

    def test_oversized_row_splits_at_cells_and_repeats_explicit_context(self):
        content = self._workbook_bytes(
            [
                ["ID", "Metric", "MODEL-A", "MODEL-B", "MODEL-C"],
                ["2024", "连接数: IPv4,IPv6\n总计", "A,1", "B:2", "C\n3"],
            ]
        )
        document = ExcelParser(
            xlsx_first_row_as_header=True,
            parser_semantic_chunk_max_chars=65,
            xlsx_context_column_count=2,
        ).parse_into_text(content)
        self.assertGreater(len(document.chunks), 1)
        for chunk in document.chunks:
            self.assertIn("ID: 2024", chunk.content)
            self.assertIn("Metric: 连接数: IPv4,IPv6\n总计", chunk.content)
            self.assertLessEqual(len(chunk.content), 65)
        joined = "".join(chunk.content for chunk in document.chunks)
        self.assertIn("MODEL-A: A,1", joined)
        self.assertIn("MODEL-B: B:2", joined)
        self.assertIn("MODEL-C: C\n3", joined)

    def test_single_cell_over_hard_limit_fails_closed(self):
        content = self._workbook_bytes([["ID", "Payload"], ["1", "x" * 100]])
        with self.assertRaisesRegex(
            ValueError, r"single Excel cell exceeds.*column='Payload'"
        ):
            ExcelParser(
                xlsx_first_row_as_header=True,
                parser_semantic_chunk_max_chars=40,
                xlsx_context_column_count=1,
            ).parse_into_text(content)

    def test_asset_rows_are_independent_semantic_chunks(self):
        content = self._workbook_bytes(
            [
                ["资产编号", "设备名称", "IP", "负责人", "位置", "状态"],
                ["A-1", "server-1", "10.0.0.1", "Alice", "SZ", "online"],
                ["A-2", "server-2", "10.0.0.2", "Bob", "SH", "offline"],
            ]
        )
        document = ExcelParser(xlsx_first_row_as_header=True).parse_into_text(content)
        self.assertEqual(len(document.chunks), 2)
        self.assertIn("资产编号: A-1", document.chunks[0].content)
        self.assertNotIn("A-2", document.chunks[0].content)
        self.assertIn("资产编号: A-2", document.chunks[1].content)

    def test_blank_rows_are_skipped_without_merging_neighbor_records(self):
        content = self._workbook_bytes(
            [["ID", "Name"], ["1", "alpha"], [None, None], ["2", "beta"]]
        )
        document = ExcelParser(xlsx_first_row_as_header=True).parse_into_text(content)
        self.assertEqual(len(document.chunks), 2)
        self.assertIn("ID: 1", document.chunks[0].content)
        self.assertNotIn("ID: 2", document.chunks[0].content)
        self.assertEqual(document.chunks[1].metadata["parser.row"], "4")

    def test_formula_cells_follow_existing_data_only_behavior(self):
        content = self._workbook_bytes(
            [["ID", "Name", "Calculated"], ["1", "alpha", "=1+1"]]
        )
        document = ExcelParser(xlsx_first_row_as_header=True).parse_into_text(content)
        self.assertEqual(
            document.chunking_policy, ChunkingPolicy.PRESERVE_PARSER_CHUNKS
        )
        self.assertEqual(document.chunks[0].content.strip(), "ID: 1,Name: alpha")
        self.assertNotIn("=1+1", document.content)

    def test_multi_sheet_rows_keep_provenance_and_never_mix(self):
        wb = openpyxl.Workbook()
        wb.remove(wb.active)
        for sheet_name, device in [
            ("服务器", "server-1"),
            ("交换机", "switch-1"),
            ("防火墙", "firewall-1"),
        ]:
            ws = wb.create_sheet(sheet_name)
            ws.append(["资产编号", "设备名称"])
            ws.append([f"{sheet_name}-001", device])
        bio = io.BytesIO()
        wb.save(bio)

        document = ExcelParser(xlsx_first_row_as_header=True).parse_into_text(
            bio.getvalue()
        )
        self.assertEqual(len(document.chunks), 3)
        self.assertEqual(
            [chunk.metadata["parser.sheet"] for chunk in document.chunks],
            ["服务器", "交换机", "防火墙"],
        )
        for chunk in document.chunks:
            self.assertEqual(chunk.content.count("资产编号:"), 1)

    def test_parse_phantom_shared_strings_workbook(self):
        document = ExcelParser().parse_into_text(_xlsx_with_phantom_shared_strings())
        self.assertIn("hello", document.content)
        self.assertIn("42", document.content)
        self.assertGreater(len(document.chunks), 0)

    def test_parse_en_calcchain_shared_strings_case(self):
        path = os.path.join(
            os.path.dirname(__file__),
            "..",
            "..",
            "testdata",
            "rag_test",
            "xlsx",
            "en_calcchain.xlsx",
        )
        if not os.path.isfile(path):
            self.skipTest("en_calcchain.xlsx fixture not available")
        with open(path, "rb") as f:
            document = ExcelParser().parse_into_text(f.read())
        self.assertGreater(len(document.content), 0)
        self.assertGreater(len(document.chunks), 0)


if __name__ == "__main__":
    unittest.main()
