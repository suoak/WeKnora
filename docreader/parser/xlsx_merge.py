"""Fill merged cell values before pandas reads an XLSX workbook."""

from __future__ import annotations

import logging
import zipfile
from io import BytesIO

logger = logging.getLogger(__name__)


def install_readonly_merge_fill(content: bytes, excel_file) -> bool:
    """Fill merge values before pandas type inference, using its read-only reader.

    Only the sheet currently being parsed is materialized. Pivot caches, charts
    and unrelated cell graphs are never loaded into a mutable workbook.
    Unknown reader layouts retain the existing package-rewrite path.
    """
    from openpyxl.utils.cell import range_boundaries
    from inspect import signature
    from weakref import ref

    reader = getattr(excel_file, "_reader", None)
    original = getattr(reader, "get_sheet_data", None)
    workbook = getattr(excel_file, "book", None)
    if not callable(original) or not getattr(workbook, "read_only", False):
        return False
    if list(signature(original).parameters) != ["sheet", "file_rows_needed"]:
        return False
    implementation = getattr(original, "__func__", None)
    if not callable(implementation):
        return False
    reader_ref = ref(reader)
    ranges = {}
    with zipfile.ZipFile(BytesIO(content)) as archive:
        for sheet in workbook.worksheets:
            path = getattr(sheet, "_worksheet_path", None)
            if not isinstance(path, str) or path not in archive.namelist():
                return False
            with archive.open(path) as source:
                bounds = [range_boundaries(value) for value in _read_merge_ranges(source)]
            # Overlapping merges have order-dependent/invalid legacy behavior;
            # do not invent a different interpretation in the optimized path.
            active = []
            for cell_range in sorted(bounds, key=lambda b: b[1]):
                left, top, right, bottom = cell_range
                if None in cell_range:
                    return False
                active = [b for b in active if b[3] >= top]
                if any(left <= b[2] and b[0] <= right for b in active):
                    return False
                active.append(cell_range)
            ranges[sheet.title] = bounds

    def get_sheet_data(sheet, file_rows_needed=None):
        owner = reader_ref()
        if owner is None:
            raise RuntimeError("Excel reader is no longer available")
        rows = implementation(owner, sheet, file_rows_needed)
        sheet_ranges = ranges.get(sheet.title, ())
        if not sheet_ranges:
            return rows
        for left, top, right, bottom in sheet_ranges:
            value = ""
            if top <= len(rows) and left <= len(rows[top - 1]):
                value = rows[top - 1][left - 1]
            row_end = min(bottom, file_rows_needed) if file_rows_needed else bottom
            if value != "":
                while len(rows) < row_end:
                    rows.append([])
            for index in range(top - 1, min(row_end, len(rows))):
                row = rows[index]
                end = right if value != "" else min(right, len(row))
                if len(row) < end:
                    row.extend([""] * (end - len(row)))
                row[left - 1:end] = [value] * max(0, end - left + 1)
        # Match openpyxl/pandas' trimming and rectangular padding after merge
        # expansion, including blank masters and ranges past stored dimensions.
        for row in rows:
            while row and row[-1] == "":
                row.pop()
        while rows and not rows[-1]:
            rows.pop()
        width = max((len(row) for row in rows), default=0)
        for row in rows:
            row.extend([""] * (width - len(row)))
        return rows

    reader.get_sheet_data = get_sheet_data
    return True


def _read_merge_ranges(source):
    """Validate complete XML without constructing or retaining cell elements."""
    from xml.parsers.expat import ParserCreate

    target = _MergeRanges()
    parser = ParserCreate(namespace_separator="}")

    def start(tag, attributes):
        target.start("{" + tag if "}" in tag else tag, attributes)

    def forbidden(*args):
        raise ValueError("XML entities and external references are forbidden")

    parser.StartElementHandler = start
    parser.EndElementHandler = target.end
    parser.EntityDeclHandler = forbidden
    parser.ExternalEntityRefHandler = forbidden
    # Parameter entities are never fetched; declarations are rejected above.
    from xml.parsers.expat import XML_PARAM_ENTITY_PARSING_NEVER
    parser.SetParamEntityParsing(XML_PARAM_ENTITY_PARSING_NEVER)
    while block := source.read(64 << 10):
        parser.Parse(block, False)
    parser.Parse(b"", True)
    return target.refs


class _MergeRanges:
    def __init__(self):
        self.refs = []
        self.depth = 0
        self.worksheet = False
        self.in_merges = False

    def start(self, tag, attributes):
        self.depth += 1
        ns = "{http://schemas.openxmlformats.org/spreadsheetml/2006/main}"
        if self.depth == 1:
            self.worksheet = tag == ns + "worksheet"
        elif self.depth == 2:
            self.in_merges = self.worksheet and tag == ns + "mergeCells"
        elif self.depth == 3 and self.in_merges and tag == ns + "mergeCell":
            self.refs.append(attributes["ref"])

    def end(self, tag):
        if self.depth == 2:
            self.in_merges = False
        self.depth -= 1

    def data(self, text):
        pass

    def close(self):
        pass


class _MergeProbe:
    """SAX target: scan complete worksheet XML without retaining cell trees."""

    found = False

    def start(self, tag, attributes):
        if tag.rsplit("}", 1)[-1] in {"mergeCell", "mergeCells"}:
            self.found = True

    def end(self, tag):
        pass

    def data(self, text):
        pass

    def close(self):
        pass


def _has_merged_cells(content: bytes) -> bool:
    from defusedxml.ElementTree import XMLParser
    from openpyxl import load_workbook

    # Read-only loading still validates packaging, shared strings and styles.
    # In particular, preserve the caller's malformed-style repair path.
    workbook = load_workbook(BytesIO(content), read_only=True, data_only=True)
    try:
        with zipfile.ZipFile(BytesIO(content)) as archive:
            for sheet in workbook.worksheets:
                # Use the path openpyxl resolved from workbook relationships;
                # worksheet parts need not use conventional sheetN.xml names.
                path = getattr(sheet, "_worksheet_path", None)
                if not isinstance(path, str) or path not in archive.namelist():
                    return True  # Unknown reader layout retains the full path.
                target = _MergeProbe()
                parser = XMLParser(target=target)
                with archive.open(path) as source:
                    while block := source.read(64 << 10):
                        parser.feed(block)
                        if target.found:
                            return True
                    parser.close()  # Also reject malformed XML late in a sheet.
        return False
    finally:
        workbook.close()


def fill_merged_cells_xlsx(content: bytes) -> bytes:
    """Unmerge ranges and copy the master cell value into every covered cell.

    openpyxl only stores values on the top-left cell of a merge; pandas then
    sees NaN in the rest. Filling makes row-wise RAG chunks retain context.
    """
    if not zipfile.is_zipfile(BytesIO(content)):
        return content
    if not _has_merged_cells(content):
        return content

    from openpyxl import load_workbook

    wb = load_workbook(BytesIO(content), data_only=True)
    changed = False
    for ws in wb.worksheets:
        if not ws.merged_cells.ranges:
            continue
        for merge_range in list(ws.merged_cells.ranges):
            master_value = ws.cell(merge_range.min_row, merge_range.min_col).value
            ws.unmerge_cells(str(merge_range))
            for row in range(merge_range.min_row, merge_range.max_row + 1):
                for col in range(merge_range.min_col, merge_range.max_col + 1):
                    ws.cell(row, col).value = master_value
            changed = True

    if not changed:
        return content

    # Charts are not part of the cell text extracted by DocReader. Remove
    # dedicated chart sheets through the public workbook API, then clear
    # embedded worksheet charts (openpyxl has no public removal API for them)
    # before saving this temporary workbook.
    active_sheet = wb.active
    for chart_sheet in list(wb.chartsheets):
        wb.remove(chart_sheet)
    if active_sheet in wb.worksheets:
        wb.active = active_sheet
    elif wb.worksheets:
        wb.active = wb.worksheets[0]
    for ws in wb.worksheets:
        ws._charts.clear()

    out = BytesIO()
    wb.save(out)
    logger.info("Filled merged cells in XLSX before parse")
    return out.getvalue()
