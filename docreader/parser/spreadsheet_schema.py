"""Conservative, structure-only spreadsheet schema detection.

The runtime parser uses this module to describe entity-oriented sheets without
changing row chunk boundaries.  Wide-table scoring deliberately reuses the
column profiles from the existing dry-run benchmark; business prefixes and
product names are never part of the decision.
"""

from __future__ import annotations

import statistics
from dataclasses import asdict, dataclass, field
from typing import Any

import pandas as pd


@dataclass
class SpreadsheetSchema:
    sheet_name: str
    headers: list[str]
    common_columns: list[str]
    entity_axis: str = "unknown"
    entity_headers: list[str] = field(default_factory=list)
    entity_count: int = 0
    confidence: float = 0.0
    # A compact lossless view of the detected entity table.  It is carried in
    # document metadata and materialized as one non-vector index chunk per
    # sheet, so exhaustive queries never reconstruct a collection from Top-K.
    records: list[dict[str, dict[str, str]]] = field(default_factory=list)

    def to_dict(self) -> dict[str, Any]:
        return asdict(self)


def detect_sheet_schema(df: pd.DataFrame, sheet_name: str) -> SpreadsheetSchema:
    """Return a conservative entity-axis description for *df*.

    Wide matrices are preferred because their column headers are an exact,
    naturally bounded entity collection.  A conservative row-axis fallback is
    included for ordinary record tables.  Ambiguous sheets remain ``unknown``.
    """
    headers = [str(value).strip() for value in df.columns]
    result = SpreadsheetSchema(sheet_name=str(sheet_name), headers=headers,
                               common_columns=headers.copy())
    if len(headers) < 2 or df.dropna(how="all").empty:
        return result

    interval, confidence = _detect_wide_entity_interval(df)
    if interval is not None:
        start, end = interval
        entities = _unique_nonempty(headers[start:end])
        if len(entities) >= 3:
            result.common_columns = headers[:start] + headers[end:]
            result.entity_axis = "columns"
            result.entity_headers = entities
            result.entity_count = len(entities)
            result.confidence = round(confidence, 6)
            result.records = _wide_records(df, start, end, headers)
            return result

    row_axis = _detect_row_entity_axis(df)
    if row_axis is not None:
        index, confidence = row_axis
        entities = _unique_nonempty(_clean_texts(df.iloc[:, index]))
        if len(entities) >= 3:
            result.common_columns = [h for i, h in enumerate(headers) if i != index]
            result.entity_axis = "rows"
            result.entity_headers = entities
            result.entity_count = len(entities)
            result.confidence = round(confidence, 6)
            result.records = _row_records(df, index, headers)
    return result


def _detect_wide_entity_interval(df: pd.DataFrame) -> tuple[tuple[int, int] | None, float]:
    # Lazy import avoids an import cycle: the diagnostic module imports the
    # Excel parser, while the parser calls this detector only after module init.
    from docreader.parser.excel_wide_table_dry_run import (
        XLSX_RETRIEVAL_MODE_AUTO,
        _column_similarity,
        _infer_payload_interval,
        _profile_columns,
    )

    profiles = _profile_columns(df)
    best, _, _, reason = _infer_payload_interval(profiles, XLSX_RETRIEVAL_MODE_AUTO)
    if best is not None and not reason:
        return (best.start, best.end), best.score

    # The benchmark intentionally requires at least five payload columns.  The
    # runtime contract also covers small but clear matrices (the regression
    # fixture has four), so evaluate homogeneous suffixes with the same column
    # similarity primitive and conservative thresholds.
    candidates: list[tuple[float, float, float, int]] = []
    for start in range(1, len(profiles) - 2):
        payload = profiles[start:]
        pairs = [
            _column_similarity(payload[i], payload[j])
            for i in range(len(payload))
            for j in range(i + 1, len(payload))
        ]
        if not pairs:
            continue
        homogeneity = statistics.fmean(pairs)
        boundary = 1.0 - _column_similarity(profiles[start - 1], profiles[start])
        filled = statistics.fmean(p.fill_rate for p in payload)
        score = 0.68 * homogeneity + 0.20 * min(1.0, boundary * 2.5) + 0.12 * filled
        candidates.append((score, homogeneity, boundary, start))
    if not candidates:
        return None, 0.0
    score, homogeneity, boundary, start = max(candidates)
    if homogeneity < 0.76 or boundary < 0.08 or score < 0.74:
        return None, 0.0
    return (start, len(profiles)), score


def _detect_row_entity_axis(df: pd.DataFrame) -> tuple[int, float] | None:
    from docreader.parser.excel_wide_table_dry_run import _profile_columns

    profiles = _profile_columns(df)
    row_count = len(df.dropna(how="all").index)
    if row_count < 3:
        return None
    candidates: list[tuple[float, int]] = []
    for index, profile in enumerate(profiles):
        identifier_ratio = profile.value_shape_distribution.get("identifier", 0.0)
        if profile.fill_rate < 0.85 or profile.distinct_ratio < 0.80:
            continue
        # Structure, not a header label, decides this: a densely populated,
        # mostly unique identifier column beside at least one attribute column.
        score = 0.45 * profile.fill_rate + 0.35 * profile.distinct_ratio + 0.20 * identifier_ratio
        if score >= 0.82:
            candidates.append((score, index))
    if len(candidates) != 1:
        return None
    return candidates[0][1], candidates[0][0]


def _wide_records(df: pd.DataFrame, start: int, end: int,
                  headers: list[str]) -> list[dict[str, dict[str, str]]]:
    records: list[dict[str, dict[str, str]]] = []
    for _, row in df.iterrows():
        common: dict[str, str] = {}
        entities: dict[str, str] = {}
        for index, value in enumerate(row.tolist()):
            text = _cell_text(value)
            if not text:
                continue
            if start <= index < end:
                entities[headers[index]] = text
            else:
                common[headers[index]] = text
        if common or entities:
            records.append({"common": common, "entities": entities})
    return records


def _row_records(df: pd.DataFrame, entity_index: int,
                 headers: list[str]) -> list[dict[str, dict[str, str]]]:
    records: list[dict[str, dict[str, str]]] = []
    for _, row in df.iterrows():
        entity = _cell_text(row.iloc[entity_index])
        if not entity:
            continue
        fields = {
            headers[index]: text
            for index, value in enumerate(row.tolist())
            if index != entity_index and (text := _cell_text(value))
        }
        records.append({"common": {"entity": entity}, "entities": fields})
    return records


def _clean_texts(series: pd.Series) -> list[str]:
    return [text for value in series.tolist() if (text := _cell_text(value))]


def _cell_text(value: object) -> str:
    if pd.isna(value):
        return ""
    return str(value).strip()


def _unique_nonempty(values: list[str]) -> list[str]:
    seen: set[str] = set()
    result: list[str] = []
    for value in values:
        clean = str(value).strip()
        if not clean or clean in seen or clean.startswith("__column_"):
            continue
        seen.add(clean)
        result.append(clean)
    return result
