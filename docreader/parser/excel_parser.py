"""
Excel Parser Module

This module provides functionality to parse Excel files (.xlsx, .xls) into
structured Document objects with text content and chunks. It supports multiple
sheets and handles various Excel formats using pandas.
"""

import json
import logging
import re
from io import BytesIO
from typing import Any, List, Sequence, Tuple

import pandas as pd

from docreader.models.document import Chunk, ChunkingPolicy, Document, ParsedSegment
from docreader.parser.base_parser import BaseParser
from docreader.parser.excel_convert import (
    convert_excel_to_xlsx_bytes,
    detect_excel_format,
    engine_for_format,
    normalize_excel_bytes,
)
from docreader.parser.xlsx_merge import fill_merged_cells_xlsx
from docreader.parser.xlsx_repair import repair_xlsx_bytes

logger = logging.getLogger(__name__)

XLSX_CHUNKING_MODE_AUTO = "auto"
XLSX_CHUNKING_MODE_ROW_AWARE = "row-aware"
XLSX_CHUNKING_MODE_LEGACY = "legacy"
_XLSX_CHUNKING_MODES = {
    XLSX_CHUNKING_MODE_AUTO,
    XLSX_CHUNKING_MODE_ROW_AWARE,
    XLSX_CHUNKING_MODE_LEGACY,
}
DEFAULT_PARSER_SEMANTIC_CHUNK_MAX_CHARS = 7500

# Pattern to detect Excel image function strings that should be excluded from
# parsed text content.  WPS uses =DISPIMG("ID",mode) to embed images in cells;
# when opened by other tools the formula may appear as plain text prefixed with
# "_xlfn." or "=".  Office 365 uses =_xlfn.IMAGE(url, ...) similarly.
# The _xlfn. prefix is optional — WPS may omit it (e.g. =DISPIMG("ID",1)).
_IMAGE_FUNC_RE = re.compile(r"^=?(_xlfn\.)?(DISPIMG|IMAGE)\(", re.IGNORECASE)


def _is_image_function(value: object) -> bool:
    """Return True if *value* looks like an embedded-image function string."""
    if not isinstance(value, str):
        return False
    return _IMAGE_FUNC_RE.match(value) is not None


class ExcelParser(BaseParser):
    """Parser for Excel files (.xlsx, .xls).

    This parser extracts text content from Excel files by processing all sheets
    and converting each row into a structured text format. Each row becomes a
    separate chunk with key-value pairs.

    Features:
        - Supports multiple sheets in a single Excel file
        - Automatically removes completely empty rows
        - Converts each row to "column: value" format
        - Creates individual chunks for each row for better granularity

    Example:
        >>> parser = ExcelParser()
        >>> with open("data.xlsx", "rb") as f:
        ...     content = f.read()
        ...     document = parser.parse_into_text(content)
        >>> print(document.content)
        Name: John,Age: 30,City: NYC
        Name: Jane,Age: 25,City: LA
    """

    def __init__(
        self,
        file_name: str = "",
        file_type: str | None = None,
        xlsx_first_row_as_header: Any = True,
        xlsx_chunking_mode: str = XLSX_CHUNKING_MODE_AUTO,
        xlsx_context_column_count: Any = None,
        parser_semantic_chunk_max_chars: Any = DEFAULT_PARSER_SEMANTIC_CHUNK_MAX_CHARS,
        **kwargs: Any,
    ):
        super().__init__(file_name=file_name, file_type=file_type, **kwargs)
        self.xlsx_first_row_as_header = _parse_bool(xlsx_first_row_as_header)
        self.xlsx_chunking_mode = str(xlsx_chunking_mode or "auto").strip().lower()
        if self.xlsx_chunking_mode not in _XLSX_CHUNKING_MODES:
            raise ValueError(
                "invalid xlsx_chunking_mode %r (expected auto, row-aware, or legacy)"
                % xlsx_chunking_mode
            )
        self.xlsx_context_column_count = _parse_optional_positive_int(
            xlsx_context_column_count, "xlsx_context_column_count"
        )
        self.parser_semantic_chunk_max_chars = _parse_positive_int(
            parser_semantic_chunk_max_chars,
            "parser_semantic_chunk_max_chars",
            DEFAULT_PARSER_SEMANTIC_CHUNK_MAX_CHARS,
        )

    def parse_into_text(self, content: bytes) -> Document:
        """Parse Excel file bytes into a Document object.

        Args:
            content: Raw bytes of the Excel file

        Returns:
            Document: Parsed document containing:
                - content: Full text with all rows from all sheets
                - chunks: List of Chunk objects, one per row

        Note:
            - Empty rows (all NaN values) are automatically skipped
            - Each row is formatted as: "col1: val1,col2: val2,..."
            - Chunks maintain sequential ordering across all sheets
        """
        chunks: List[Chunk] = []
        text: List[str] = []
        start, end = 0, 0
        saw_nonempty_sheet = False

        excel_file = _open_excel_file(content, file_type=self.file_type)
        sheets: List[Tuple[str, pd.DataFrame, ChunkingPolicy]] = []
        spreadsheet_schemas: List[dict[str, Any]] = []

        # Inspect every sheet before rendering. In non-legacy modes each
        # non-empty sheet becomes an independent parser-defined segment.
        for excel_sheet_name in excel_file.sheet_names:
            use_header = self.xlsx_first_row_as_header or (
                self.xlsx_chunking_mode == XLSX_CHUNKING_MODE_ROW_AWARE
            )
            df, header_applied, row_table_safe = _read_sheet_dataframe(
                excel_file,
                excel_sheet_name,
                xlsx_first_row_as_header=use_header,
            )
            # Remove rows where all values are NaN (completely empty rows)
            df.dropna(how="all", inplace=True)
            if df.empty:
                continue
            saw_nonempty_sheet = True

            # Schema detection is independent from row chunking.  When row 1 is
            # intentionally retained as data for legacy chunk output, inspect a
            # second dataframe with row 1 interpreted as headers; this preserves
            # existing chunks while allowing a wide SPEC matrix to expose its
            # complete entity axis.
            schema_df = df
            if not header_applied:
                schema_df, _, _ = _read_sheet_dataframe(
                    excel_file, excel_sheet_name, xlsx_first_row_as_header=True
                )
                schema_df.dropna(how="all", inplace=True)
            if not schema_df.empty:
                from docreader.parser.spreadsheet_schema import detect_sheet_schema

                spreadsheet_schemas.append(
                    detect_sheet_schema(schema_df, excel_sheet_name).to_dict()
                )

            sheet_policy = ChunkingPolicy.DEFAULT
            if self.xlsx_chunking_mode == XLSX_CHUNKING_MODE_ROW_AWARE:
                if not header_applied or not row_table_safe:
                    raise ValueError(
                        "xlsx row-aware mode requires a valid first-row header and "
                        f"record-shaped rows (sheet={excel_sheet_name!r})"
                    )
                sheet_policy = ChunkingPolicy.PRESERVE_PARSER_CHUNKS
            elif self.xlsx_chunking_mode == XLSX_CHUNKING_MODE_AUTO:
                if self.xlsx_first_row_as_header and header_applied and row_table_safe:
                    sheet_policy = ChunkingPolicy.PRESERVE_PARSER_CHUNKS

            sheets.append((excel_sheet_name, df, sheet_policy))

        # Process each sheet in workbook order.
        segments: List[ParsedSegment] = []
        all_sheets_preserved = saw_nonempty_sheet
        for excel_sheet_name, df, sheet_policy in sheets:
            segment_start = end
            segment_chunks: List[Chunk] = []
            segment_cursor = 0
            preserve_sheet = sheet_policy == ChunkingPolicy.PRESERVE_PARSER_CHUNKS
            all_sheets_preserved = all_sheets_preserved and preserve_sheet
            # Process each row in the DataFrame
            for row_index, row in df.iterrows():
                page_fields: List[Tuple[str, str]] = []
                # Build key-value pairs for non-null values
                for k, v in row.items():
                    if pd.notna(v) and not _is_image_function(v):
                        page_fields.append((str(k), str(v)))

                # Skip rows with no valid content
                if not page_fields:
                    continue

                excel_row_number = int(row_index) + 1
                if preserve_sheet:
                    row_chunks = _split_semantic_excel_row(
                        page_fields,
                        max_chars=self.parser_semantic_chunk_max_chars,
                        context_column_count=self.xlsx_context_column_count,
                        sheet_name=excel_sheet_name,
                        row_number=excel_row_number,
                    )
                else:
                    row_chunks = [_render_fields(page_fields)]
                for content_row in row_chunks:
                    end += len(content_row)
                    text.append(content_row)
                    row_metadata = {
                        "parser.source_kind": "excel_row",
                        "parser.sheet": str(excel_sheet_name),
                        "parser.row": str(excel_row_number),
                    }
                    document_chunk = Chunk(
                        content=content_row,
                        seq=len(chunks),
                        start=start,
                        end=end,
                        metadata=row_metadata,
                    )
                    chunks.append(document_chunk)
                    if preserve_sheet:
                        segment_end = segment_cursor + len(content_row)
                        segment_chunks.append(
                            Chunk(
                                content=content_row,
                                seq=len(segment_chunks),
                                start=segment_cursor,
                                end=segment_end,
                                metadata=row_metadata,
                            )
                        )
                        segment_cursor = segment_end
                    start = end

            if (
                self.xlsx_chunking_mode != XLSX_CHUNKING_MODE_LEGACY
                and end > segment_start
            ):
                segments.append(
                    ParsedSegment(
                        seq=len(segments),
                        start=segment_start,
                        end=end,
                        chunking_policy=sheet_policy,
                        chunks=segment_chunks,
                        metadata={
                            "parser.segment_kind": "excel_sheet",
                            "parser.sheet": str(excel_sheet_name),
                        },
                    )
                )

        # Combine all text and return as Document
        policy = ChunkingPolicy.DEFAULT
        if (
            self.xlsx_chunking_mode != XLSX_CHUNKING_MODE_LEGACY
            and all_sheets_preserved
        ):
            policy = ChunkingPolicy.PRESERVE_PARSER_CHUNKS
        document_metadata = {
            "parser.type": "xlsx",
            "spreadsheet.schemas": json.dumps(
                spreadsheet_schemas, ensure_ascii=False, separators=(",", ":")
            ),
        }
        return Document(
            content="".join(text),
            chunks=chunks,
            segments=segments,
            chunking_policy=policy,
            metadata=document_metadata,
        )


def _read_sheet_dataframe(
    excel_file: pd.ExcelFile,
    sheet_name: str,
    xlsx_first_row_as_header: bool = False,
) -> tuple[pd.DataFrame, bool, bool]:
    """Read a worksheet into a DataFrame with stable column labels."""
    from openpyxl.utils import get_column_letter

    # Use row 1 as semantic column context by default for both XLSX and legacy
    # XLS. Callers can explicitly disable the mode when row 1 is data.
    df = excel_file.parse(sheet_name=sheet_name, header=None)
    if xlsx_first_row_as_header and len(df.index) >= 2:
        header_values = df.iloc[0].tolist()
        row_table_safe = _is_conservative_row_table(header_values, df.iloc[1:])
        df.columns = _stable_header_labels(header_values)
        return df.iloc[1:].copy(), True, row_table_safe

    df.columns = [get_column_letter(idx + 1) for idx in range(len(df.columns))]
    return df, False, False


def _is_conservative_row_table(
    header_values: Sequence[object], data_rows: pd.DataFrame
) -> bool:
    """Return whether a sheet is conservatively record-shaped.

    The existing first-row-as-header option is the primary structural signal.
    These checks deliberately reject single-column prose, empty headers, and
    sparse/free-form layouts; false negatives retain the legacy Go chunker.
    """
    nonempty_data = data_rows.dropna(how="all")
    if nonempty_data.empty:
        return False

    active_columns = [
        idx
        for idx in range(len(header_values))
        if nonempty_data.iloc[:, idx].notna().any()
    ]
    if len(active_columns) < 2:
        return False

    missing_headers = []
    for idx in active_columns:
        value = header_values[idx]
        if pd.isna(value) or _is_image_function(value) or not str(value).strip():
            missing_headers.append(idx)

    header_coverage = (len(active_columns) - len(missing_headers)) / len(active_columns)
    missing_ratio = len(missing_headers) / len(active_columns)
    if header_coverage < 0.80 or not (
        len(missing_headers) <= 2 or missing_ratio <= 0.10
    ):
        return False

    normalized_header = [
        "" if idx in missing_headers else str(header_values[idx]).strip()
        for idx in active_columns
    ]
    nonempty_header = [value for value in normalized_header if value]
    if len(set(nonempty_header)) != len(nonempty_header):
        # Repeated top-level labels are common after merged cells in a
        # multi-row header. Auto mode cannot safely decide which header level
        # describes records; row-aware remains available as an explicit opt-in.
        return False
    for _, row in nonempty_data.iterrows():
        normalized_row = [
            "" if pd.isna(row.iloc[idx]) else str(row.iloc[idx]).strip()
            for idx in active_columns
        ]
        if normalized_row == normalized_header:
            # A repeated header inside the data region usually marks multiple
            # table blocks rather than one homogeneous record stream.
            return False

    # A record-shaped row must carry at least two fields. Requiring this for
    # every non-empty row rejects note/title lines embedded in a data region.
    return all(
        sum(pd.notna(row.iloc[idx]) for idx in active_columns) >= 2
        for _, row in nonempty_data.iterrows()
    )


def _render_fields(fields: Sequence[Tuple[str, str]]) -> str:
    return ",".join(f"{key}: {value}" for key, value in fields) + "\n"


def _split_semantic_excel_row(
    fields: Sequence[Tuple[str, str]],
    *,
    max_chars: int,
    context_column_count: int | None,
    sheet_name: str,
    row_number: int,
) -> List[str]:
    """Render one record, splitting only at complete cell boundaries."""
    rendered = _render_fields(fields)
    if len(rendered) <= max_chars:
        return [rendered]

    diagnostic = (
        f"sheet={sheet_name!r}, row={row_number}, size={len(rendered)}, "
        f"hard_max_chars={max_chars}"
    )
    if context_column_count is None:
        raise ValueError(
            "parser semantic chunk too large; set "
            f"xlsx_context_column_count to enable cell-level splitting ({diagnostic})"
        )
    if context_column_count >= len(fields):
        raise ValueError(
            "parser semantic chunk too large; xlsx_context_column_count must leave "
            f"at least one payload column ({diagnostic})"
        )

    context = list(fields[:context_column_count])
    payload = list(fields[context_column_count:])
    if len(_render_fields(context)) >= max_chars:
        raise ValueError(
            f"parser semantic chunk context exceeds hard limit ({diagnostic})"
        )

    result: List[str] = []
    batch: List[Tuple[str, str]] = []
    for field in payload:
        candidate = _render_fields([*context, *batch, field])
        if len(candidate) <= max_chars:
            batch.append(field)
            continue
        if not batch:
            raise ValueError(
                "single Excel cell exceeds parser semantic chunk hard limit "
                f"(column={field[0]!r}, {diagnostic})"
            )
        result.append(_render_fields([*context, *batch]))
        batch = [field]
        if len(_render_fields([*context, *batch])) > max_chars:
            raise ValueError(
                "single Excel cell exceeds parser semantic chunk hard limit "
                f"(column={field[0]!r}, {diagnostic})"
            )
    if batch:
        result.append(_render_fields([*context, *batch]))
    return result


def _stable_header_labels(values: List[object]) -> List[str]:
    """Build non-empty, unique labels from an explicitly selected header row."""
    from openpyxl.utils import get_column_letter

    labels: List[str] = []
    used: set[str] = set()
    reserved_real_labels = {
        str(value).strip()
        for value in values
        if pd.notna(value) and not _is_image_function(value) and str(value).strip()
    }
    for index, value in enumerate(values, start=1):
        label = ""
        if pd.notna(value) and not _is_image_function(value):
            label = str(value).strip()
        if not label:
            base = f"__column_{get_column_letter(index)}"
            label = base
            suffix = 2
            while label in used or label in reserved_real_labels:
                label = f"{base}__{suffix}"
                suffix += 1
        elif label in used:
            base = label
            suffix = 2
            label = f"{base}__{suffix}"
            while label in used or label in reserved_real_labels:
                suffix += 1
                label = f"{base}__{suffix}"
        labels.append(label)
        used.add(label)
    return labels


def _parse_bool(value: Any) -> bool:
    if isinstance(value, bool):
        return value
    return str(value).strip().lower() in {"1", "true", "yes", "on"}


def _parse_optional_positive_int(value: Any, name: str) -> int | None:
    if value is None or str(value).strip() == "":
        return None
    return _parse_positive_int(value, name, 0)


def _parse_positive_int(value: Any, name: str, default: int) -> int:
    if value is None or str(value).strip() == "":
        return default
    try:
        parsed = int(value)
    except (TypeError, ValueError) as exc:
        raise ValueError(f"{name} must be a positive integer") from exc
    if parsed <= 0:
        raise ValueError(f"{name} must be a positive integer")
    return parsed


def _prepare_xlsx_bytes(data: bytes) -> bytes:
    repaired = repair_xlsx_bytes(data)
    if repaired is not None:
        data = repaired
    return fill_merged_cells_xlsx(data)


def _open_excel_file(content: bytes, file_type: str | None = None) -> pd.ExcelFile:
    """Open an Excel workbook with explicit engine selection and fallbacks."""
    data = content
    converted_via_soffice = False

    while True:
        ext = detect_excel_format(data)
        if ext is None:
            if converted_via_soffice:
                raise ValueError(
                    "Excel file format cannot be determined, you must specify an "
                    "engine manually."
                )
            try:
                data = normalize_excel_bytes(data, file_type=file_type)
            except ValueError as exc:
                raise ValueError(
                    "Excel file format cannot be determined, you must specify an "
                    "engine manually."
                ) from exc
            converted_via_soffice = True
            continue

        if ext == "ods":
            converted = convert_excel_to_xlsx_bytes(data, suffix=".ods")
            if converted:
                data = converted
                continue

        engine = engine_for_format(ext)
        if ext == "xlsx":
            data = _prepare_xlsx_bytes(data)
            engine = "openpyxl"
        try:
            return pd.ExcelFile(BytesIO(data), engine=engine)
        except ImportError as exc:
            raise ValueError(
                f"Excel engine {engine!r} is not available for .{ext} files"
            ) from exc
        except KeyError as exc:
            if "sharedStrings.xml" not in str(exc) or engine != "openpyxl":
                raise
            repaired = repair_xlsx_bytes(data)
            if repaired is None:
                raise
            logger.info("Repaired XLSX sharedStrings packaging before parse")
            data = _prepare_xlsx_bytes(repaired)
            continue
        except ValueError as exc:
            if converted_via_soffice or "cannot be determined" not in str(exc):
                raise
            try:
                data = normalize_excel_bytes(content, file_type=file_type)
            except ValueError:
                raise
            converted_via_soffice = True
            continue


if __name__ == "__main__":
    # Example usage: Parse an Excel file and display results
    logging.basicConfig(level=logging.DEBUG)

    # Specify the path to your Excel file
    your_file = "/path/to/your/file.xlsx"
    parser = ExcelParser()

    # Read and parse the Excel file
    with open(your_file, "rb") as f:
        content = f.read()
        document = parser.parse_into_text(content)

        # Display the full document content
        logger.error(document.content)

        # Display the first chunk as an example
        for chunk in document.chunks:
            logger.error(chunk.content)
            break  # Only show the first chunk
