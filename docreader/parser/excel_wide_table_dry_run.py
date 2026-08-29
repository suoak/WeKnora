"""Side-effect-free analysis of regular wide-table Excel retrieval expansion.

The detector works on the dataframe produced by the existing Excel parser and
uses physical column positions. It never creates chunks, embeddings, or state.
"""

from __future__ import annotations

import argparse
import difflib
import json
import math
import re
import statistics
import unicodedata
from collections import Counter
from dataclasses import asdict, dataclass
from pathlib import Path
from typing import Any, Sequence

import pandas as pd

from docreader.models.document import ChunkingPolicy
from docreader.parser.excel_parser import (
    XLSX_CHUNKING_MODE_AUTO, XLSX_CHUNKING_MODE_LEGACY,
    XLSX_CHUNKING_MODE_ROW_AWARE, ExcelParser, _is_image_function,
    _open_excel_file, _read_sheet_dataframe,
)

XLSX_RETRIEVAL_MODE_OFF = "off"
XLSX_RETRIEVAL_MODE_AUTO = "auto"
XLSX_RETRIEVAL_MODE_WIDE_CELL = "wide-cell"
XLSX_RETRIEVAL_MODES = {XLSX_RETRIEVAL_MODE_OFF, XLSX_RETRIEVAL_MODE_AUTO,
                        XLSX_RETRIEVAL_MODE_WIDE_CELL}
MIN_PAYLOAD_WIDTH = 5
MIN_PAYLOAD_FRACTION = 0.15
_NUMBER_RE = re.compile(r"^[+-]?(?:\d+(?:\.\d+)?|\.\d+)$")
_NUMBER_UNIT_RE = re.compile(r"^[+-]?(?:\d+(?:\.\d+)?|\.\d+)\s*[^\d\s].*$")


@dataclass(frozen=True)
class ColumnProfile:
    name: str
    fill_rate: float
    distinct_ratio: float
    numeric_ratio: float
    numeric_with_unit_ratio: float
    boolean_enum_ratio: float
    median_value_length: float
    value_length_variance: float
    dominant_value_shape: str
    dominant_shape_ratio: float
    header_unicode_shape: str
    value_shape_distribution: dict[str, float]
    fill_pattern: tuple[bool, ...]


@dataclass(frozen=True)
class IntervalCandidate:
    start: int
    end: int
    score: float
    internal_homogeneity: float
    adjacent_similarity: float
    fill_pattern_similarity: float
    value_shape_similarity: float
    left_contrast: float
    right_contrast: float
    edge_cohesion: float
    sparse_penalty: float
    heterogeneity_penalty: float


@dataclass(frozen=True)
class SheetWideTableDryRun:
    sheet: str
    policy: str
    row_count: int
    canonical_rows: int | None
    column_count: int
    leading_context_columns: list[str]
    payload_columns: list[str]
    trailing_metadata_columns: list[str]
    payload_start: int | None
    payload_end: int | None
    non_empty_payload_cells: int
    expected_retrieval_representation_count: int
    confidence: float
    second_best_score: float | None
    score_margin: float | None
    fallback_reason: str


@dataclass(frozen=True)
class WorkbookWideTableDryRun:
    file_name: str
    retrieval_mode: str
    context_column_count: int
    payload_group_size: int
    sheets: list[SheetWideTableDryRun]
    parser_semantic_chunk_count: int
    final_canonical_chunk_count: int | None
    estimated_retrieval_count: int
    retrieval_to_canonical_ratio: float | None
    total_index_units: int | None
    total_index_expansion_ratio: float | None
    estimated_embedding_count: int

    def to_dict(self) -> dict[str, Any]:
        return asdict(self)


def _clean_values(series: pd.Series) -> list[Any]:
    return [v for v in series.tolist() if pd.notna(v) and not _is_image_function(v)]


def _value_shape(value: Any) -> str:
    if isinstance(value, bool):
        return "boolean"
    if isinstance(value, (int, float)) and not isinstance(value, bool):
        return "number"
    text = str(value).strip()
    if text.casefold() in {"true", "false", "yes", "no", "y", "n"}:
        return "boolean"
    if _NUMBER_RE.fullmatch(text):
        return "number"
    if _NUMBER_UNIT_RE.fullmatch(text):
        return "number_unit"
    if any(char.isspace() for char in text):
        return "text_with_space"
    if len(text) <= 32 and any(char.isdigit() for char in text):
        return "identifier"
    return "text"


def _unicode_shape(value: str) -> str:
    result: list[str] = []
    for char in str(value).strip():
        category = unicodedata.category(char)
        marker = ("L" if category.startswith("L") else "N" if category.startswith("N")
                  else "S" if category.startswith("Z") else "P")
        if not result or result[-1] != marker:
            result.append(marker)
    return "".join(result) or "EMPTY"


def _profile_columns(df: pd.DataFrame) -> list[ColumnProfile]:
    row_count = max(len(df.index), 1)
    profiles: list[ColumnProfile] = []
    for index in range(len(df.columns)):
        series = df.iloc[:, index]
        values = _clean_values(series)
        texts = [str(v).strip() for v in values]
        shapes = Counter(_value_shape(v) for v in values)
        dominant, dominant_count = shapes.most_common(1)[0] if shapes else ("empty", 0)
        distribution = ({shape: count / len(values) for shape, count in shapes.items()}
                        if values else {"empty": 1.0})
        lengths = [len(v) for v in texts]
        repeated = sum(count for count in Counter(texts).values() if count >= 2)
        profiles.append(ColumnProfile(
            name=str(df.columns[index]), fill_rate=len(values) / row_count,
            distinct_ratio=len(set(texts)) / len(texts) if texts else 0.0,
            numeric_ratio=shapes.get("number", 0) / len(values) if values else 0.0,
            numeric_with_unit_ratio=shapes.get("number_unit", 0) / len(values) if values else 0.0,
            boolean_enum_ratio=(shapes.get("boolean", 0) + repeated) / (2 * len(values)) if values else 0.0,
            median_value_length=statistics.median(lengths) if lengths else 0.0,
            value_length_variance=statistics.pvariance(lengths) if len(lengths) > 1 else 0.0,
            dominant_value_shape=dominant,
            dominant_shape_ratio=dominant_count / len(values) if values else 0.0,
            header_unicode_shape=_unicode_shape(str(df.columns[index])),
            value_shape_distribution=distribution,
            fill_pattern=tuple(pd.notna(v) and not _is_image_function(v) for v in series.tolist()),
        ))
    return profiles


def _ratio_similarity(left: float, right: float, scale: float = 1.0) -> float:
    return max(0.0, 1.0 - min(1.0, abs(left - right) / max(scale, 1e-9)))


def _distribution_similarity(left: dict[str, float], right: dict[str, float]) -> float:
    keys = set(left) | set(right)
    return max(0.0, 1.0 - 0.5 * sum(abs(left.get(k, 0) - right.get(k, 0)) for k in keys))


def _fill_similarity(left: tuple[bool, ...], right: tuple[bool, ...]) -> float:
    return sum(a == b for a, b in zip(left, right)) / len(left) if left else 1.0


def _length_similarity(left: ColumnProfile, right: ColumnProfile) -> float:
    median = _ratio_similarity(left.median_value_length, right.median_value_length,
                               max(4.0, left.median_value_length, right.median_value_length))
    left_sd, right_sd = math.sqrt(left.value_length_variance), math.sqrt(right.value_length_variance)
    variance = _ratio_similarity(left_sd, right_sd, max(4.0, left_sd, right_sd))
    return statistics.fmean((median, variance))


def _column_similarity(left: ColumnProfile, right: ColumnProfile) -> float:
    header = difflib.SequenceMatcher(None, left.header_unicode_shape,
                                    right.header_unicode_shape).ratio()
    # Header *shape*, not header text, is intentionally material: repeated
    # payload columns commonly share an identifier-like Unicode skeleton while
    # context/management columns do not.
    return (
        0.11 * _fill_similarity(left.fill_pattern, right.fill_pattern)
        + 0.07 * _ratio_similarity(left.fill_rate, right.fill_rate, 0.35)
        + 0.09 * _ratio_similarity(left.distinct_ratio, right.distinct_ratio, 0.60)
        + 0.07 * _ratio_similarity(left.numeric_ratio, right.numeric_ratio, 0.70)
        + 0.07 * _ratio_similarity(left.numeric_with_unit_ratio, right.numeric_with_unit_ratio, 0.70)
        + 0.05 * _ratio_similarity(left.boolean_enum_ratio, right.boolean_enum_ratio, 0.70)
        + 0.11 * _length_similarity(left, right)
        + 0.14 * _distribution_similarity(left.value_shape_distribution, right.value_shape_distribution)
        + 0.29 * header
    )


def _matrix_prefix(matrix: list[list[float]]) -> list[list[float]]:
    size = len(matrix)
    prefix = [[0.0] * (size + 1) for _ in range(size + 1)]
    for row in range(size):
        running = 0.0
        for column in range(size):
            running += matrix[row][column]
            prefix[row + 1][column + 1] = prefix[row][column + 1] + running
    return prefix


def _interval_pair_mean(prefix: list[list[float]], start: int, end: int) -> float:
    width = end - start
    if width <= 1:
        return 1.0
    square_sum = (prefix[end][end] - prefix[start][end]
                  - prefix[end][start] + prefix[start][start])
    # Symmetric matrix: remove the unit diagonal and divide by both triangles.
    return (square_sum - width) / (width * (width - 1))


def _boundary_contrast(matrix: list[list[float]], start: int, end: int,
                       column_count: int) -> tuple[float, float]:
    # A boundary is a local change point. Looking several columns ahead can
    # skip the first metadata column and incorrectly snap to a later, stronger
    # change (for example, a blank or boolean management column).
    left = 1.0 - matrix[start - 1][start] if start > 0 else 0.0
    right = 1.0 - matrix[end - 1][end] if end < column_count else 0.0
    return left, right


def _score_interval(profiles: Sequence[ColumnProfile], matrix: list[list[float]],
                    pair_prefix: list[list[float]], fill_prefix: list[list[float]],
                    shape_prefix: list[list[float]], adjacent_prefix: list[float],
                    start: int, end: int) -> IntervalCandidate:
    indices = range(start, end)
    pair = _interval_pair_mean(pair_prefix, start, end)
    adjacent = (adjacent_prefix[end - 1] - adjacent_prefix[start]) / (end - start - 1)
    fill = _interval_pair_mean(fill_prefix, start, end)
    shape = _interval_pair_mean(shape_prefix, start, end)
    internal = statistics.fmean((pair, adjacent, fill, shape))
    left, right = _boundary_contrast(matrix, start, end, len(profiles))
    edge_window = min(5, end - start - 1)
    left_edge = statistics.fmean(matrix[start][i] for i in range(start + 1, start + 1 + edge_window))
    right_edge = statistics.fmean(matrix[end - 1][i] for i in range(end - 1 - edge_window, end - 1))
    edge_cohesion = statistics.fmean((left_edge, right_edge))
    sparse = statistics.fmean(max(0.0, 0.65 - profiles[i].fill_rate) / 0.65 for i in indices)
    heterogeneity = 1.0 - pair
    width_reward = min(1.0, ((end - start) / len(profiles)) / 0.65)
    contrasts = [v for v, exists in ((left, start > 0), (right, end < len(profiles))) if exists]
    boundary = statistics.fmean(contrasts) if contrasts else 0.0
    score = (0.32 * internal + 0.10 * adjacent + 0.08 * fill + 0.08 * shape
             + 0.18 * min(1.0, boundary * 2.5) + 0.12 * width_reward
             + 0.12 * edge_cohesion - 0.13 * heterogeneity - 0.16 * sparse)
    return IntervalCandidate(start, end, max(0.0, min(1.0, score)), internal,
                             adjacent, fill, shape, left, right, edge_cohesion,
                             sparse, heterogeneity)


def _infer_payload_interval(profiles: Sequence[ColumnProfile], mode: str,
                            explicit_start: int = 0) -> tuple[IntervalCandidate | None, float | None, float | None, str]:
    count = len(profiles)
    active_count = sum(p.fill_rate > 0 for p in profiles)
    if active_count < 8:
        return None, None, None, "not_wide_enough: requires at least 8 non-empty columns"
    matrix = [[0.0] * count for _ in range(count)]
    fill_matrix = [[0.0] * count for _ in range(count)]
    shape_matrix = [[0.0] * count for _ in range(count)]
    for a in range(count):
        matrix[a][a] = 1.0
        fill_matrix[a][a] = 1.0
        shape_matrix[a][a] = 1.0
        for b in range(a + 1, count):
            matrix[a][b] = matrix[b][a] = _column_similarity(profiles[a], profiles[b])
            fill_matrix[a][b] = fill_matrix[b][a] = _fill_similarity(
                profiles[a].fill_pattern, profiles[b].fill_pattern)
            shape_matrix[a][b] = shape_matrix[b][a] = _distribution_similarity(
                profiles[a].value_shape_distribution, profiles[b].value_shape_distribution)
    pair_prefix = _matrix_prefix(matrix)
    fill_prefix = _matrix_prefix(fill_matrix)
    shape_prefix = _matrix_prefix(shape_matrix)
    adjacent_prefix = [0.0]
    for index in range(count - 1):
        adjacent_prefix.append(adjacent_prefix[-1] + matrix[index][index + 1])
    starts = [explicit_start] if explicit_start > 0 else range(1, count - MIN_PAYLOAD_WIDTH + 1)
    candidates: list[IntervalCandidate] = []
    for start in starts:
        if start < 1 or start >= count:
            continue
        for end in range(start + MIN_PAYLOAD_WIDTH, count + 1):
            width = end - start
            if sum(profiles[i].fill_rate > 0 for i in range(start, end)) < MIN_PAYLOAD_WIDTH:
                continue
            if width / max(active_count, 1) < MIN_PAYLOAD_FRACTION:
                continue
            candidates.append(_score_interval(
                profiles, matrix, pair_prefix, fill_prefix, shape_prefix,
                adjacent_prefix, start, end))
    if not candidates:
        return None, None, None, "no_valid_contiguous_payload_interval"
    candidates.sort(key=lambda c: (-c.score, c.start, -c.end))
    best, second = candidates[0], candidates[1] if len(candidates) > 1 else None
    second_score = second.score if second else None
    margin = best.score - second.score if second else best.score
    min_score = 0.79 if mode == XLSX_RETRIEVAL_MODE_AUTO else 0.74
    min_internal = 0.78 if mode == XLSX_RETRIEVAL_MODE_AUTO else 0.72
    contrasts = [v for v, exists in ((best.left_contrast, best.start > 0),
                 (best.right_contrast, best.end < count)) if exists]
    if best.internal_homogeneity < min_internal:
        reason = "payload_columns_are_not_homogeneous"
    elif best.adjacent_similarity < min_internal:
        reason = "payload_adjacent_columns_are_not_similar"
    elif best.sparse_penalty > 0.20:
        reason = "payload_block_is_too_sparse"
    elif not contrasts or max(contrasts) < 0.10:
        reason = "payload_boundary_has_no_structural_contrast"
    elif best.score < min_score:
        reason = f"confidence_below_threshold:{min_score:.3f}"
    elif margin < 0.002 and max(contrasts) < 0.18:
        reason = f"ambiguous_payload_interval_margin:{margin:.3f}"
    else:
        reason = ""
    # Return the evaluated winner even on fallback so diagnostics retain its
    # confidence and ambiguity. The caller only materializes boundaries when
    # ``reason`` is empty.
    return best, second_score, margin, reason


def _sheet_policy(parser: ExcelParser, header_applied: bool,
                  row_table_safe: bool) -> ChunkingPolicy:
    if parser.xlsx_chunking_mode == XLSX_CHUNKING_MODE_ROW_AWARE:
        return ChunkingPolicy.PRESERVE_PARSER_CHUNKS if header_applied and row_table_safe else ChunkingPolicy.DEFAULT
    if parser.xlsx_chunking_mode == XLSX_CHUNKING_MODE_AUTO and parser.xlsx_first_row_as_header and header_applied and row_table_safe:
        return ChunkingPolicy.PRESERVE_PARSER_CHUNKS
    return ChunkingPolicy.DEFAULT


def analyze_excel_wide_table(content: bytes, *, parser: ExcelParser,
                             retrieval_mode: str = XLSX_RETRIEVAL_MODE_OFF,
                             context_column_count: int = 0,
                             payload_group_size: int = 1,
                             final_canonical_chunk_count: int | None = None) -> WorkbookWideTableDryRun:
    """Estimate expansion using parser dataframes without changing parser output."""
    mode = str(retrieval_mode or XLSX_RETRIEVAL_MODE_OFF).strip().lower()
    if mode not in XLSX_RETRIEVAL_MODES:
        raise ValueError(f"invalid xlsx_retrieval_mode: {retrieval_mode!r}")
    if context_column_count < 0:
        raise ValueError("xlsx_context_column_count must be zero or positive")
    if payload_group_size not in {1, 4, 8}:
        raise ValueError("payload_group_size must be one of: 1, 4, 8")
    if final_canonical_chunk_count is not None and final_canonical_chunk_count < 0:
        raise ValueError("final_canonical_chunk_count must be zero or positive")
    parser_semantic_count = len(parser.parse_into_text(content).chunks)
    excel_file = _open_excel_file(content, file_type=parser.file_type)
    sheet_results: list[SheetWideTableDryRun] = []
    for sheet_name in excel_file.sheet_names:
        use_header = parser.xlsx_first_row_as_header or parser.xlsx_chunking_mode == XLSX_CHUNKING_MODE_ROW_AWARE
        df, header_applied, row_table_safe = _read_sheet_dataframe(
            excel_file, sheet_name, xlsx_first_row_as_header=use_header)
        df.dropna(how="all", inplace=True)
        valid_rows = df.apply(lambda row: any(pd.notna(v) and not _is_image_function(v)
                                              for v in row.tolist()), axis=1)
        analysis_df = df.loc[valid_rows].copy()
        policy = _sheet_policy(parser, header_applied, row_table_safe)
        profiles = _profile_columns(analysis_df)
        best = None
        second_score = margin = None
        if mode == XLSX_RETRIEVAL_MODE_OFF:
            reason = "retrieval_mode_off"
        elif policy != ChunkingPolicy.PRESERVE_PARSER_CHUNKS:
            reason = "sheet_policy_is_not_preserve_parser_chunks"
        elif not header_applied or not row_table_safe:
            reason = "sheet_is_not_a_conservative_regular_table"
        else:
            best, second_score, margin, reason = _infer_payload_interval(
                profiles, mode, context_column_count)
        accepted = best if best is not None and not reason else None
        start, end = (accepted.start, accepted.end) if accepted else (None, None)
        leading = [p.name for p in profiles[:start]] if start is not None else []
        payload = [p.name for p in profiles[start:end]] if start is not None else []
        trailing = [p.name for p in profiles[end:]] if end is not None else []
        nonempty = (sum(len(_clean_values(analysis_df.iloc[:, i]))
                        for i in range(start, end)) if start is not None else 0)
        representation_count = 0
        if start is not None and end is not None:
            for _, row in analysis_df.iterrows():
                row_nonempty = sum(
                    pd.notna(row.iloc[index])
                    and not _is_image_function(row.iloc[index])
                    for index in range(start, end)
                )
                representation_count += math.ceil(row_nonempty / payload_group_size)
        sheet_results.append(SheetWideTableDryRun(
            sheet=str(sheet_name), policy=str(policy.value),
            row_count=len(analysis_df.index),
            canonical_rows=(len(analysis_df.index)
                            if policy == ChunkingPolicy.PRESERVE_PARSER_CHUNKS
                            else None),
            column_count=len(analysis_df.columns), leading_context_columns=leading,
            payload_columns=payload, trailing_metadata_columns=trailing,
            payload_start=start, payload_end=end,
            non_empty_payload_cells=nonempty,
            expected_retrieval_representation_count=representation_count,
            confidence=round(best.score if best else 0.0, 6),
            second_best_score=(round(second_score, 6)
                               if second_score is not None else None),
            score_margin=round(margin, 6) if margin is not None else None,
            fallback_reason=reason))
    estimated = sum(s.expected_retrieval_representation_count for s in sheet_results)
    ratio = estimated / final_canonical_chunk_count if final_canonical_chunk_count else None
    total = final_canonical_chunk_count + estimated if final_canonical_chunk_count is not None else None
    total_ratio = total / final_canonical_chunk_count if final_canonical_chunk_count else None
    return WorkbookWideTableDryRun(
        file_name=parser.file_name, retrieval_mode=mode,
        context_column_count=context_column_count,
        payload_group_size=payload_group_size, sheets=sheet_results,
        parser_semantic_chunk_count=parser_semantic_count,
        final_canonical_chunk_count=final_canonical_chunk_count,
        estimated_retrieval_count=estimated,
        retrieval_to_canonical_ratio=(round(ratio, 6) if ratio is not None else None),
        total_index_units=total,
        total_index_expansion_ratio=(round(total_ratio, 6)
                                     if total_ratio is not None else None),
        estimated_embedding_count=estimated)


def _parse_bool_arg(value: str) -> bool:
    normalized = value.strip().lower()
    if normalized in {"1", "true", "yes", "on"}:
        return True
    if normalized in {"0", "false", "no", "off"}:
        return False
    raise argparse.ArgumentTypeError("expected true or false")


def main() -> int:
    cli = argparse.ArgumentParser(description="Dry-run contiguous wide-table payload detection")
    cli.add_argument("file", type=Path)
    cli.add_argument("--retrieval-mode", choices=sorted(XLSX_RETRIEVAL_MODES), default="auto")
    cli.add_argument("--context-column-count", type=int, default=0)
    cli.add_argument("--payload-group-size", type=int, choices=[1, 4, 8], default=1)
    cli.add_argument("--final-canonical-chunk-count", type=int)
    cli.add_argument("--first-row-as-header", type=_parse_bool_arg, default=True)
    cli.add_argument("--chunking-mode", choices=[XLSX_CHUNKING_MODE_AUTO,
                     XLSX_CHUNKING_MODE_ROW_AWARE, XLSX_CHUNKING_MODE_LEGACY], default=XLSX_CHUNKING_MODE_AUTO)
    args = cli.parse_args()
    raw = args.file.read_bytes()
    parser = ExcelParser(file_name=args.file.name, file_type=args.file.suffix.lstrip("."),
                         xlsx_first_row_as_header=args.first_row_as_header,
                         xlsx_chunking_mode=args.chunking_mode,
                         xlsx_context_column_count=(args.context_column_count or None))
    result = analyze_excel_wide_table(
        raw, parser=parser, retrieval_mode=args.retrieval_mode,
        context_column_count=args.context_column_count,
        payload_group_size=args.payload_group_size,
        final_canonical_chunk_count=args.final_canonical_chunk_count)
    print(json.dumps(result.to_dict(), ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
