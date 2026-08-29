import io
import unittest

import openpyxl

from docreader.parser.excel_parser import ExcelParser
from docreader.parser.excel_wide_table_dry_run import analyze_excel_wide_table


def _workbook_bytes(sheets):
    workbook = openpyxl.Workbook()
    workbook.remove(workbook.active)
    for sheet_name, rows in sheets:
        sheet = workbook.create_sheet(sheet_name)
        for row in rows:
            sheet.append(row)
    buffer = io.BytesIO()
    workbook.save(buffer)
    return buffer.getvalue()


class ExcelWideTableDryRunTest(unittest.TestCase):
    @staticmethod
    def _regular_wide_rows():
        headers = ["record id", "metric description"] + [
            f"product-{index}" for index in range(1, 9)
        ] + ["owner", "review state", "notes"]
        rows = [headers]
        for row_index in range(1, 7):
            rows.append(
                [
                    2000 + row_index,
                    f"Long metric description number {row_index}",
                    *[f"{row_index * product_index} units" for product_index in range(1, 9)],
                    f"person {row_index % 2}",
                    row_index % 2 == 0,
                    None if row_index % 3 else "exception note",
                ]
            )
        return rows

    def test_auto_infers_leading_context_and_counts_payload_cells(self):
        content = _workbook_bytes([("Capacity", self._regular_wide_rows())])
        parser = ExcelParser(
            file_name="generic.xlsx",
            file_type="xlsx",
            xlsx_first_row_as_header=True,
            xlsx_chunking_mode="auto",
        )

        result = analyze_excel_wide_table(
            content, parser=parser, retrieval_mode="auto", context_column_count=0
        )

        self.assertEqual(result.parser_semantic_chunk_count, 6)
        self.assertIsNone(result.final_canonical_chunk_count)
        self.assertEqual(result.estimated_retrieval_count, 48)
        self.assertEqual(result.estimated_embedding_count, 48)
        self.assertIsNone(result.retrieval_to_canonical_ratio)
        self.assertIsNone(result.total_index_units)
        self.assertIsNone(result.total_index_expansion_ratio)
        sheet = result.sheets[0]
        self.assertEqual(sheet.policy, "preserve_parser_chunks")
        self.assertEqual(
            sheet.leading_context_columns, ["record id", "metric description"]
        )
        self.assertEqual(len(sheet.payload_columns), 8)
        self.assertEqual(
            sheet.trailing_metadata_columns, ["owner", "review state", "notes"]
        )
        self.assertEqual((sheet.payload_start, sheet.payload_end), (2, 10))
        self.assertEqual(sheet.column_count, 13)
        self.assertEqual(sheet.non_empty_payload_cells, 48)
        self.assertEqual(sheet.fallback_reason, "")
        self.assertGreaterEqual(sheet.confidence, 0.80)

    def test_off_is_a_noop_and_does_not_change_parser_output(self):
        content = _workbook_bytes([("Capacity", self._regular_wide_rows())])
        parser = ExcelParser(
            file_name="generic.xlsx",
            file_type="xlsx",
            xlsx_first_row_as_header=True,
        )
        before = parser.parse_into_text(content).model_dump()

        result = analyze_excel_wide_table(
            content, parser=parser, retrieval_mode="off", context_column_count=0
        )
        after = parser.parse_into_text(content).model_dump()

        self.assertEqual(before, after)
        self.assertEqual(result.estimated_retrieval_count, 0)
        self.assertEqual(result.sheets[0].fallback_reason, "retrieval_mode_off")

    def test_auto_falls_back_when_no_structural_boundary_exists(self):
        headers = [f"field-{index}" for index in range(10)]
        rows = [headers] + [
            [f"value-{row}-{column}" for column in range(10)] for row in range(6)
        ]
        content = _workbook_bytes([("Uniform", rows)])
        parser = ExcelParser(
            file_name="uniform.xlsx",
            file_type="xlsx",
            xlsx_first_row_as_header=True,
        )

        result = analyze_excel_wide_table(
            content, parser=parser, retrieval_mode="auto", context_column_count=0
        )

        sheet = result.sheets[0]
        self.assertEqual(sheet.leading_context_columns, [])
        self.assertEqual(sheet.expected_retrieval_representation_count, 0)
        self.assertIn(
            sheet.fallback_reason,
            {
                "payload_boundary_has_no_structural_contrast",
                "ambiguous_payload_interval_margin:0.000",
            },
        )

    def test_explicit_context_override_uses_structure_without_name_rules(self):
        rows = self._regular_wide_rows()
        # Make every header and value use the same broad shape. The explicit
        # boundary must still be honored without recognizing business names.
        rows[0] = [f"column-{index}" for index in range(13)]
        content = _workbook_bytes([("Data", rows)])
        parser = ExcelParser(
            file_name="arbitrary.xlsx",
            file_type="xlsx",
            xlsx_first_row_as_header=True,
        )

        result = analyze_excel_wide_table(
            content,
            parser=parser,
            retrieval_mode="wide-cell",
            context_column_count=2,
        )

        sheet = result.sheets[0]
        self.assertEqual(sheet.leading_context_columns, ["column-0", "column-1"])
        self.assertEqual(sheet.expected_retrieval_representation_count, 48)
        self.assertEqual((sheet.payload_start, sheet.payload_end), (2, 10))
        self.assertEqual(sheet.fallback_reason, "")

    def test_explicit_final_canonical_baseline_is_used_only_for_ratios(self):
        content = _workbook_bytes([("Capacity", self._regular_wide_rows())])
        parser = ExcelParser(
            file_name="generic.xlsx", file_type="xlsx", xlsx_first_row_as_header=True
        )

        result = analyze_excel_wide_table(
            content,
            parser=parser,
            retrieval_mode="auto",
            final_canonical_chunk_count=6,
        )

        self.assertEqual(result.parser_semantic_chunk_count, 6)
        self.assertEqual(result.final_canonical_chunk_count, 6)
        self.assertEqual(result.retrieval_to_canonical_ratio, 8.0)
        self.assertEqual(result.total_index_units, 54)
        self.assertEqual(result.total_index_expansion_ratio, 9.0)

    def test_non_regular_sheet_reports_policy_fallback(self):
        content = _workbook_bytes(
            [("Notes", [["Notes"], ["first paragraph"], ["second paragraph"]])]
        )
        parser = ExcelParser(
            file_name="notes.xlsx",
            file_type="xlsx",
            xlsx_first_row_as_header=True,
        )

        result = analyze_excel_wide_table(
            content, parser=parser, retrieval_mode="auto", context_column_count=0
        )

        sheet = result.sheets[0]
        self.assertEqual(sheet.policy, "default")
        self.assertEqual(sheet.expected_retrieval_representation_count, 0)
        self.assertEqual(
            sheet.fallback_reason, "sheet_policy_is_not_preserve_parser_chunks"
        )


if __name__ == "__main__":
    unittest.main()
