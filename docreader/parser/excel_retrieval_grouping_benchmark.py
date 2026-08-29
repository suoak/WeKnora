"""Read-only grouping and embedding benchmark for Excel retrieval text.

This module is deliberately detached from ingestion. It reads parser dataframes,
generates in-memory representations, and optionally calls an embedding endpoint.
It never writes WeKnora chunks, embeddings, retrieval stores, or databases.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import math
import os
import time
from dataclasses import asdict, dataclass
from pathlib import Path
from typing import Any, Iterable, Iterator, Sequence

import numpy as np
import pandas as pd
import requests

from docreader.parser.excel_parser import (
    ExcelParser,
    _is_image_function,
    _open_excel_file,
    _read_sheet_dataframe,
)
from docreader.parser.excel_wide_table_dry_run import (
    XLSX_RETRIEVAL_MODE_AUTO,
    XLSX_RETRIEVAL_MODES,
    SheetWideTableDryRun,
    WorkbookWideTableDryRun,
    analyze_excel_wide_table,
)

SUPPORTED_GROUP_SIZES = (1, 4, 8)


@dataclass(frozen=True)
class RetrievalRepresentation:
    representation_id: str
    sheet: str
    excel_row: int
    group_size: int
    group_index: int
    payload_column_indices: list[int]
    payload_headers: list[str]
    content: str


@dataclass(frozen=True)
class SheetGroupingCount:
    sheet: str
    canonical_rows: int | None
    payload_columns: int
    retrieval_representation_count: int
    fallback_reason: str


@dataclass(frozen=True)
class GroupingCountSummary:
    payload_group_size: int
    sheets: list[SheetGroupingCount]
    final_canonical_chunk_count: int | None
    retrieval_count: int
    total_index_units: int | None
    retrieval_to_canonical_ratio: float | None
    total_index_expansion_ratio: float | None


@dataclass(frozen=True)
class GroupingDryRunResult:
    file_name: str
    parser_semantic_chunk_count: int
    final_canonical_chunk_count: int | None
    groupings: list[GroupingCountSummary]
    previews: dict[str, RetrievalRepresentation | None]

    def to_dict(self) -> dict[str, Any]:
        return asdict(self)


@dataclass(frozen=True)
class CorpusEntry:
    entry_id: str
    sheet: str
    excel_row: int
    content: str


@dataclass(frozen=True)
class TargetSpec:
    label: str
    required_substrings: tuple[str, ...]


@dataclass(frozen=True)
class QuerySpec:
    query_id: str
    query: str
    target_sheet: str
    targets: tuple[TargetSpec, ...]


R13_QUERY_SPECS = (
    QuerySpec(
        query_id="Q1",
        query="RG-NBR-N7204-E 最大并发连接数（IPv4+IPv6）是多少？",
        target_sheet="容量指标",
        targets=(TargetSpec("50W", (
            "最大并发连接数（IPv4+IPv6）", "RG-NBR-N7204-E: 50W")),),
    ),
    QuerySpec(
        query_id="Q2",
        query="RG-NBR-N7204-E 状态防火墙64字节性能是多少？",
        target_sheet="性能指标",
        targets=(TargetSpec("190M", (
            "状态防火墙64字节性能", "RG-NBR-N7204-E: 190M")),),
    ),
    QuerySpec(
        query_id="Q3",
        query="RG-NBR-N7204-E CPU 是什么？",
        target_sheet="硬件指标",
        targets=(
            TargetSpec("MARVELL 88F3720", ("二级规格: CPU", "RG-NBR-N7204-E: MARVELL 88F3720")),
            TargetSpec("1GHz", ("二级规格: 频点", "RG-NBR-N7204-E: 1GHz")),
            TargetSpec("2 cores", ("二级规格: 核数", "RG-NBR-N7204-E: 2核")),
        ),
    ),
)


def _valid_cell(value: Any) -> bool:
    return pd.notna(value) and not _is_image_function(value)


def _valid_rows(df: pd.DataFrame) -> pd.DataFrame:
    mask = df.apply(lambda row: any(_valid_cell(value) for value in row.tolist()), axis=1)
    return df.loc[mask].copy()


def _load_sheet_frames(
    content: bytes, parser: ExcelParser
) -> dict[str, pd.DataFrame]:
    excel_file = _open_excel_file(content, file_type=parser.file_type)
    frames: dict[str, pd.DataFrame] = {}
    for sheet_name in excel_file.sheet_names:
        use_header = parser.xlsx_first_row_as_header or parser.xlsx_chunking_mode == "row-aware"
        df, _, _ = _read_sheet_dataframe(
            excel_file, sheet_name, xlsx_first_row_as_header=use_header
        )
        df.dropna(how="all", inplace=True)
        frames[str(sheet_name)] = _valid_rows(df)
    return frames


def _accepted_sheet_map(
    detector_result: WorkbookWideTableDryRun,
) -> dict[str, SheetWideTableDryRun]:
    return {
        sheet.sheet: sheet
        for sheet in detector_result.sheets
        if not sheet.fallback_reason
        and sheet.payload_start is not None
        and sheet.payload_end is not None
    }


def _row_payload_cells(
    row: pd.Series, start: int, end: int
) -> list[tuple[int, str, str]]:
    return [
        (index, str(row.index[index]), str(row.iloc[index]))
        for index in range(start, end)
        if _valid_cell(row.iloc[index])
    ]


def _batches(values: Sequence[Any], size: int) -> Iterator[Sequence[Any]]:
    for offset in range(0, len(values), size):
        yield values[offset: offset + size]


def _render_representation(
    *,
    file_name: str,
    sheet_name: str,
    excel_row: int,
    context: Sequence[tuple[str, str]],
    payload: Sequence[tuple[int, str, str]],
) -> str:
    lines = [f"文档: {file_name}", f"Sheet: {sheet_name}", f"Excel Row: {excel_row}"]
    lines.extend(f"{header}: {value}" for header, value in context)
    lines.extend(f"{header}: {value}" for _, header, value in payload)
    return "\n".join(lines)


def iter_retrieval_representations(
    *,
    content: bytes,
    parser: ExcelParser,
    detector_result: WorkbookWideTableDryRun,
    payload_group_size: int,
    sheet_filter: set[str] | None = None,
    frames: dict[str, pd.DataFrame] | None = None,
) -> Iterator[RetrievalRepresentation]:
    """Yield grouped texts without crossing a parser row or sheet."""
    if payload_group_size not in SUPPORTED_GROUP_SIZES:
        raise ValueError(f"payload_group_size must be one of {SUPPORTED_GROUP_SIZES}")
    if frames is None:
        frames = _load_sheet_frames(content, parser)
    accepted = _accepted_sheet_map(detector_result)
    workbook_digest = hashlib.sha256(content).hexdigest()
    for sheet_name, sheet in accepted.items():
        if sheet_filter is not None and sheet_name not in sheet_filter:
            continue
        frame = frames[sheet_name]
        start, end = sheet.payload_start, sheet.payload_end
        assert start is not None and end is not None
        for row_index, row in frame.iterrows():
            context = [
                (str(row.index[index]), str(row.iloc[index]))
                for index in range(start)
                if _valid_cell(row.iloc[index])
            ]
            payload_cells = _row_payload_cells(row, start, end)
            excel_row = int(row_index) + 1
            for group_index, group in enumerate(_batches(payload_cells, payload_group_size)):
                group_list = list(group)
                identity = json.dumps(
                    [workbook_digest, sheet_name, excel_row, payload_group_size,
                     [cell[0] for cell in group_list]],
                    ensure_ascii=False,
                    separators=(",", ":"),
                )
                yield RetrievalRepresentation(
                    representation_id=hashlib.sha256(identity.encode("utf-8")).hexdigest(),
                    sheet=sheet_name,
                    excel_row=excel_row,
                    group_size=payload_group_size,
                    group_index=group_index,
                    payload_column_indices=[cell[0] for cell in group_list],
                    payload_headers=[cell[1] for cell in group_list],
                    content=_render_representation(
                        file_name=parser.file_name,
                        sheet_name=sheet_name,
                        excel_row=excel_row,
                        context=context,
                        payload=group_list,
                    ),
                )


def _count_grouping(
    *,
    detector_result: WorkbookWideTableDryRun,
    frames: dict[str, pd.DataFrame],
    group_size: int,
    final_canonical_chunk_count: int | None,
) -> GroupingCountSummary:
    sheets: list[SheetGroupingCount] = []
    for sheet in detector_result.sheets:
        count = 0
        if not sheet.fallback_reason and sheet.payload_start is not None and sheet.payload_end is not None:
            frame = frames[sheet.sheet]
            for _, row in frame.iterrows():
                nonempty = len(_row_payload_cells(row, sheet.payload_start, sheet.payload_end))
                count += math.ceil(nonempty / group_size)
        sheets.append(SheetGroupingCount(
            sheet=sheet.sheet,
            canonical_rows=sheet.canonical_rows,
            payload_columns=len(sheet.payload_columns),
            retrieval_representation_count=count,
            fallback_reason=sheet.fallback_reason,
        ))
    retrieval_count = sum(sheet.retrieval_representation_count for sheet in sheets)
    total = (final_canonical_chunk_count + retrieval_count
             if final_canonical_chunk_count is not None else None)
    ratio = (retrieval_count / final_canonical_chunk_count
             if final_canonical_chunk_count else None)
    expansion = total / final_canonical_chunk_count if final_canonical_chunk_count else None
    return GroupingCountSummary(
        payload_group_size=group_size,
        sheets=sheets,
        final_canonical_chunk_count=final_canonical_chunk_count,
        retrieval_count=retrieval_count,
        total_index_units=total,
        retrieval_to_canonical_ratio=round(ratio, 6) if ratio is not None else None,
        total_index_expansion_ratio=round(expansion, 6) if expansion is not None else None,
    )


def run_grouping_dry_run(
    content: bytes,
    *,
    parser: ExcelParser,
    group_sizes: Sequence[int] = SUPPORTED_GROUP_SIZES,
    retrieval_mode: str = XLSX_RETRIEVAL_MODE_AUTO,
    context_column_count: int = 0,
    final_canonical_chunk_count: int | None = None,
    preview_sheet: str | None = None,
    preview_excel_row: int | None = None,
    preview_required_substrings: Sequence[str] = (),
) -> GroupingDryRunResult:
    for group_size in group_sizes:
        if group_size not in SUPPORTED_GROUP_SIZES:
            raise ValueError(f"payload_group_size must be one of {SUPPORTED_GROUP_SIZES}")
    detector = analyze_excel_wide_table(
        content,
        parser=parser,
        retrieval_mode=retrieval_mode,
        context_column_count=context_column_count,
        payload_group_size=1,
        final_canonical_chunk_count=final_canonical_chunk_count,
    )
    frames = _load_sheet_frames(content, parser)
    groupings = [
        _count_grouping(
            detector_result=detector,
            frames=frames,
            group_size=group_size,
            final_canonical_chunk_count=final_canonical_chunk_count,
        )
        for group_size in group_sizes
    ]
    previews: dict[str, RetrievalRepresentation | None] = {}
    if preview_sheet is not None and preview_excel_row is not None:
        for group_size in group_sizes:
            found = None
            for representation in iter_retrieval_representations(
                content=content,
                parser=parser,
                detector_result=detector,
                payload_group_size=group_size,
                sheet_filter={preview_sheet},
                frames=frames,
            ):
                if representation.excel_row != preview_excel_row:
                    continue
                if all(value in representation.content for value in preview_required_substrings):
                    found = representation
                    break
            previews[str(group_size)] = found
    return GroupingDryRunResult(
        file_name=parser.file_name,
        parser_semantic_chunk_count=detector.parser_semantic_chunk_count,
        final_canonical_chunk_count=final_canonical_chunk_count,
        groupings=groupings,
        previews=previews,
    )


class JsonlEmbeddingCache:
    """Append-only vector cache that never stores credentials or plaintext."""

    def __init__(self, path: Path):
        self.path = path
        self.vectors: dict[str, list[float]] = {}
        if path.exists():
            with path.open("r", encoding="utf-8") as handle:
                for line in handle:
                    if line.strip():
                        record = json.loads(line)
                        key = record.get("cache_key", record.get("key"))
                        if key:
                            self.vectors[str(key)] = record["vector"]

    def get(self, key: str) -> list[float] | None:
        return self.vectors.get(key)

    def put_many(
        self,
        records: Sequence[tuple[str, str, str, list[float]]],
    ) -> None:
        self.path.parent.mkdir(parents=True, exist_ok=True)
        with self.path.open("a", encoding="utf-8") as handle:
            for key, model_id, text_hash, vector in records:
                if key in self.vectors:
                    continue
                self.vectors[key] = vector
                handle.write(json.dumps({
                    "cache_key": key,
                    "model_id": model_id,
                    "text_hash": text_hash,
                    "dimension": len(vector),
                    "vector": vector,
                }, separators=(",", ":")) + "\n")


@dataclass(frozen=True)
class EmbeddingCallStats:
    requests: int
    cache_hits: int
    elapsed_ms: int


class OpenAICompatibleEmbedder:
    def __init__(self, *, base_url: str, api_key: str, model: str,
                 cache: JsonlEmbeddingCache, batch_size: int = 10):
        self.base_url = base_url.rstrip("/")
        self.api_key = api_key
        self.model = model
        self.cache = cache
        self.batch_size = batch_size

    def _key(self, text: str) -> str:
        material = f"{self.base_url}\0{self.model}\0{text}".encode("utf-8")
        return hashlib.sha256(material).hexdigest()

    def embed_with_stats(self, texts: Sequence[str]) -> tuple[np.ndarray, EmbeddingCallStats]:
        started = time.perf_counter()
        keys = [self._key(text) for text in texts]
        cache_hits = sum(self.cache.get(key) is not None for key in keys)
        missing: list[tuple[str, str]] = [
            (key, text) for key, text in zip(keys, texts) if self.cache.get(key) is None
        ]
        request_count = 0
        for batch in _batches(missing, self.batch_size):
            batch_list = list(batch)
            response = None
            for attempt in range(3):
                request_count += 1
                response = requests.post(
                    f"{self.base_url}/embeddings",
                    headers={"Authorization": f"Bearer {self.api_key}",
                             "Content-Type": "application/json"},
                    json={"model": self.model,
                          "input": [text for _, text in batch_list],
                          "encoding_format": "float"},
                    timeout=90,
                )
                if response.ok:
                    break
                if response.status_code not in {429, 500, 502, 503, 504} or attempt == 2:
                    response.raise_for_status()
                time.sleep(2 ** attempt)
            assert response is not None
            data = sorted(response.json()["data"], key=lambda item: item["index"])
            if len(data) != len(batch_list):
                raise RuntimeError("embedding endpoint returned an unexpected vector count")
            self.cache.put_many([
                (key, self.model, hashlib.sha256(text.encode("utf-8")).hexdigest(),
                 [float(value) for value in item["embedding"]])
                for (key, text), item in zip(batch_list, data)
            ])
        vectors = np.asarray([self.cache.get(key) for key in keys], dtype=np.float32)
        return vectors, EmbeddingCallStats(
            requests=request_count,
            cache_hits=cache_hits,
            elapsed_ms=round((time.perf_counter() - started) * 1000),
        )

    def embed(self, texts: Sequence[str]) -> np.ndarray:
        return self.embed_with_stats(texts)[0]


class WeKnoraEmbeddingBackend:
    """Client for the server-side saved-model batch debug adapter."""

    def __init__(self, *, base_url: str, model_id: str, api_key: str,
                 cache: JsonlEmbeddingCache, batch_size: int = 32):
        if not 1 <= batch_size <= 32:
            raise ValueError("batch_size must be between 1 and 32")
        normalized = base_url.strip().rstrip("/")
        self.base_url = normalized[:-7] if normalized.endswith("/api/v1") else normalized
        self.model_id = model_id.strip()
        self.api_key = api_key
        self.cache = cache
        self.batch_size = batch_size

    def _key(self, text: str) -> str:
        material = (
            f"weknora\0{self.base_url}\0{self.model_id}\0{text}"
        ).encode("utf-8")
        return hashlib.sha256(material).hexdigest()

    def _request_batches(
        self, missing: Sequence[tuple[str, str]]
    ) -> Iterator[list[tuple[str, str]]]:
        """Bound requests by both the API item limit and its 64 KiB body limit."""
        batch: list[tuple[str, str]] = []
        for item in missing:
            candidate = [*batch, item]
            body_size = len(json.dumps(
                {"texts": [text for _, text in candidate]},
                ensure_ascii=False,
                separators=(",", ":"),
            ).encode("utf-8"))
            if body_size > 64 * 1024:
                if not batch:
                    raise ValueError("one embedding text exceeds the 64 KiB request limit")
                yield batch
                batch = [item]
                single_size = len(json.dumps(
                    {"texts": [item[1]]}, ensure_ascii=False, separators=(",", ":")
                ).encode("utf-8"))
                if single_size > 64 * 1024:
                    raise ValueError("one embedding text exceeds the 64 KiB request limit")
            else:
                batch = candidate
            if len(batch) == self.batch_size:
                yield batch
                batch = []
        if batch:
            yield batch

    def embed_with_stats(self, texts: Sequence[str]) -> tuple[np.ndarray, EmbeddingCallStats]:
        started = time.perf_counter()
        keys = [self._key(text) for text in texts]
        cache_hits = sum(self.cache.get(key) is not None for key in keys)
        missing = [
            (key, text) for key, text in zip(keys, texts)
            if self.cache.get(key) is None
        ]
        request_count = 0
        for batch_list in self._request_batches(missing):
            response = None
            for attempt in range(3):
                request_count += 1
                try:
                    response = requests.post(
                        f"{self.base_url}/api/v1/models/{self.model_id}/debug/embeddings",
                        headers={
                            "X-API-Key": self.api_key,
                            "Content-Type": "application/json",
                        },
                        json={"texts": [text for _, text in batch_list]},
                        timeout=120,
                    )
                except requests.RequestException:
                    if attempt == 2:
                        raise
                    time.sleep(2 ** attempt)
                    continue
                if response.ok:
                    break
                if response.status_code not in {429, 500, 502, 503, 504} or attempt == 2:
                    response.raise_for_status()
                time.sleep(2 ** attempt)
            if response is None or not response.ok:
                raise RuntimeError("WeKnora embedding request did not complete")
            envelope = response.json()
            data = envelope.get("data", {})
            vectors = data.get("vectors", [])
            if data.get("model_id") != self.model_id:
                raise RuntimeError("WeKnora returned an unexpected model_id")
            if data.get("count") != len(batch_list) or len(vectors) != len(batch_list):
                raise RuntimeError("WeKnora returned an unexpected vector count")
            dimensions = {len(vector) for vector in vectors}
            if dimensions != {data.get("dimension")} or 0 in dimensions:
                raise RuntimeError("WeKnora returned inconsistent vector dimensions")
            self.cache.put_many([
                (key, self.model_id, hashlib.sha256(text.encode("utf-8")).hexdigest(),
                 [float(value) for value in vector])
                for (key, text), vector in zip(batch_list, vectors)
            ])
        result = np.asarray([self.cache.get(key) for key in keys], dtype=np.float32)
        return result, EmbeddingCallStats(
            requests=request_count,
            cache_hits=cache_hits,
            elapsed_ms=round((time.perf_counter() - started) * 1000),
        )

    def embed(self, texts: Sequence[str]) -> np.ndarray:
        return self.embed_with_stats(texts)[0]


def _canonical_corpus(content: bytes, parser: ExcelParser) -> list[CorpusEntry]:
    result: list[CorpusEntry] = []
    for chunk in parser.parse_into_text(content).chunks:
        result.append(CorpusEntry(
            entry_id=f"canonical:{chunk.seq}",
            sheet=str(chunk.metadata.get("parser.sheet", "")),
            excel_row=int(chunk.metadata.get("parser.row", 0) or 0),
            content=chunk.content.strip(),
        ))
    return result


def _representation_corpus(representations: Iterable[RetrievalRepresentation]) -> list[CorpusEntry]:
    return [CorpusEntry(rep.representation_id, rep.sheet, rep.excel_row, rep.content)
            for rep in representations]


def _cosine_scores(matrix: np.ndarray, query: np.ndarray) -> np.ndarray:
    matrix_norm = np.linalg.norm(matrix, axis=1)
    query_norm = float(np.linalg.norm(query))
    denominator = matrix_norm * query_norm
    return np.divide(matrix @ query, denominator, out=np.zeros_like(matrix_norm), where=denominator != 0)


def _preview(text: str, limit: int = 500) -> str:
    compact = "\n".join(line.rstrip() for line in text.strip().splitlines())
    return compact if len(compact) <= limit else compact[:limit] + "…"


def _distribution_stats(values: Sequence[int]) -> dict[str, int]:
    materialized = np.asarray(values, dtype=np.int64)
    if not len(materialized):
        return {"min": 0, "p50": 0, "p90": 0, "p95": 0, "max": 0}
    return {
        "min": int(materialized.min()),
        "p50": int(np.percentile(materialized, 50, method="nearest")),
        "p90": int(np.percentile(materialized, 90, method="nearest")),
        "p95": int(np.percentile(materialized, 95, method="nearest")),
        "max": int(materialized.max()),
    }


def _representation_length_stats(corpus: Sequence[CorpusEntry]) -> dict[str, Any]:
    lengths = [len(entry.content) for entry in corpus]
    return {
        "chars": {
            **_distribution_stats(lengths),
            "over_511": sum(length > 511 for length in lengths),
            "over_1024": sum(length > 1024 for length in lengths),
            "over_2048": sum(length > 2048 for length in lengths),
        },
        "tokens": None,
        "tokens_unavailable_reason": (
            "the reusable token counters are Go-only and are approximations for chat/chunking; "
            "the Python benchmark does not substitute a different tokenizer"
        ),
    }


def _rank_query(
    *, corpus: Sequence[CorpusEntry], query: QuerySpec,
    corpus_vectors: np.ndarray, query_vector: np.ndarray,
) -> dict[str, Any]:
    scores = _cosine_scores(corpus_vectors, query_vector)
    order = np.argsort(-scores)
    ranks = {int(corpus_index): rank for rank, corpus_index in enumerate(order, start=1)}
    target_results: dict[str, Any] = {}
    target_indices: list[int] = []
    for target in query.targets:
        matches = [
            index for index, entry in enumerate(corpus)
            if entry.sheet == query.target_sheet
            and all(value in entry.content for value in target.required_substrings)
        ]
        target_indices.extend(matches)
        if not matches:
            target_results[target.label] = {"rank": None, "cosine_score": None}
            continue
        best_index = min(matches, key=lambda index: ranks[index])
        target_results[target.label] = {
            "rank": ranks[best_index],
            "cosine_score": round(float(scores[best_index]), 6),
        }
    best_target = min(target_indices, key=lambda index: ranks[index]) if target_indices else None
    return {
        "target_rank": ranks[best_target] if best_target is not None else None,
        "target_cosine_score": (round(float(scores[best_target]), 6)
                                if best_target is not None else None),
        "target_ranks": target_results,
        "matched_representation_preview": (_preview(corpus[best_target].content)
                                           if best_target is not None else None),
        "matched_sheet": (corpus[best_target].sheet
                          if best_target is not None else None),
        "matched_excel_row": (corpus[best_target].excel_row
                              if best_target is not None else None),
        "top10": [
            {
                "rank": rank,
                "score": round(float(scores[index]), 6),
                "id": corpus[index].entry_id,
                "sheet": corpus[index].sheet,
                "excel_row": corpus[index].excel_row,
                "preview": _preview(corpus[index].content),
            }
            for rank, index in enumerate(order[:10], start=1)
        ],
    }


def run_embedding_benchmark(
    content: bytes,
    *,
    parser: ExcelParser,
    detector_result: WorkbookWideTableDryRun,
    embedder: Any,
    group_sizes: Sequence[int] = (8,),
    sheet_filter: set[str] | None = None,
    query_ids: set[str] | None = None,
    queries: Sequence[QuerySpec] = R13_QUERY_SPECS,
) -> dict[str, Any]:
    accepted = _accepted_sheet_map(detector_result)
    frames = _load_sheet_frames(content, parser)
    selected_queries = [
        query for query in queries if query_ids is None or query.query_id in query_ids
    ]
    results: dict[str, Any] = {}
    for group_size in group_sizes:
        if group_size not in SUPPORTED_GROUP_SIZES:
            raise ValueError(f"payload_group_size must be one of {SUPPORTED_GROUP_SIZES}")
        variant = "cell-1" if group_size == 1 else f"group-{group_size}"
        corpus = _representation_corpus(iter_retrieval_representations(
            content=content,
            parser=parser,
            detector_result=detector_result,
            payload_group_size=group_size,
            sheet_filter=sheet_filter,
            frames=frames,
        ))
        corpus_vectors, corpus_stats = embedder.embed_with_stats(
            [entry.content for entry in corpus]
        )
        variant_results: dict[str, Any] = {}
        for query in selected_queries:
            if query.target_sheet not in accepted:
                fallback = next(
                    sheet.fallback_reason for sheet in detector_result.sheets
                    if sheet.sheet == query.target_sheet
                )
                variant_results[query.query_id] = {
                    "status": "unavailable/fallback",
                    "fallback_reason": fallback,
                    "corpus_size": len(corpus),
                }
                continue
            if sheet_filter is not None and query.target_sheet not in sheet_filter:
                variant_results[query.query_id] = {
                    "status": "unavailable/not_selected",
                    "corpus_size": len(corpus),
                }
                continue
            query_started = time.perf_counter()
            query_vectors, query_stats = embedder.embed_with_stats([query.query])
            ranking = _rank_query(
                corpus=corpus,
                query=query,
                corpus_vectors=corpus_vectors,
                query_vector=query_vectors[0],
            )
            ranking_elapsed = round((time.perf_counter() - query_started) * 1000)
            variant_results[query.query_id] = {
                "status": "available",
                "corpus_size": len(corpus),
                **ranking,
                "embedding_requests": {
                    "corpus_shared": corpus_stats.requests,
                    "query": query_stats.requests,
                    "total": corpus_stats.requests + query_stats.requests,
                },
                "cache_hits": {
                    "corpus_shared": corpus_stats.cache_hits,
                    "query": query_stats.cache_hits,
                    "total": corpus_stats.cache_hits + query_stats.cache_hits,
                },
                "elapsed_ms": {
                    "corpus_embedding_shared": corpus_stats.elapsed_ms,
                    "query_embedding_and_ranking": ranking_elapsed,
                    "total": corpus_stats.elapsed_ms + ranking_elapsed,
                },
            }
        # Always expose detector coverage for requested-out queries such as Q2
        # without embedding those query strings.
        for query in queries:
            if query.query_id in variant_results or query.target_sheet in accepted:
                continue
            fallback = next(
                sheet.fallback_reason for sheet in detector_result.sheets
                if sheet.sheet == query.target_sheet
            )
            variant_results[query.query_id] = {
                "status": "unavailable/fallback",
                "fallback_reason": fallback,
                "corpus_size": len(corpus),
            }
        results[variant] = {
            "corpus_size": len(corpus),
            "sheets": sorted(sheet_filter) if sheet_filter is not None else sorted(accepted),
            "representation_length_stats": _representation_length_stats(corpus),
            "queries": variant_results,
        }
    return results


def main() -> int:
    cli = argparse.ArgumentParser(description="Excel retrieval representation grouping benchmark")
    cli.add_argument("file", type=Path)
    cli.add_argument("--group-sizes", type=int, nargs="+", choices=SUPPORTED_GROUP_SIZES,
                     default=list(SUPPORTED_GROUP_SIZES))
    cli.add_argument("--retrieval-mode", choices=sorted(XLSX_RETRIEVAL_MODES), default="auto")
    cli.add_argument("--context-column-count", type=int, default=0)
    cli.add_argument("--final-canonical-chunk-count", type=int)
    cli.add_argument("--preview-sheet")
    cli.add_argument("--preview-excel-row", type=int)
    cli.add_argument("--preview-require", action="append", default=[])
    cli.add_argument("--run-embedding-benchmark", action="store_true")
    cli.add_argument("--embedding-backend", choices=["openai", "weknora"], default="openai")
    cli.add_argument("--embedding-model", default="text-embedding-v4")
    cli.add_argument("--embedding-base-url",
                     default="https://dashscope.aliyuncs.com/compatible-mode/v1")
    cli.add_argument("--embedding-api-key-env", default="DASHSCOPE_API_KEY")
    cli.add_argument("--weknora-base-url")
    cli.add_argument("--weknora-model-id")
    cli.add_argument("--weknora-api-key-env", default="WEKNORA_API_KEY")
    cli.add_argument("--embedding-cache", type=Path,
                     default=Path(".cache/excel-retrieval-embedding-cache.jsonl"))
    cli.add_argument("--batch-size", "--embedding-batch-size", type=int, default=32)
    cli.add_argument("--benchmark-group-size", type=int, choices=SUPPORTED_GROUP_SIZES,
                     default=8)
    cli.add_argument("--benchmark-sheets", nargs="+", default=["容量指标", "硬件指标"])
    cli.add_argument("--benchmark-query-ids", nargs="+", default=["Q1", "Q3"])
    args = cli.parse_args()
    content = args.file.read_bytes()
    parser = ExcelParser(
        file_name=args.file.name, file_type=args.file.suffix.lstrip("."),
        xlsx_first_row_as_header=True, xlsx_chunking_mode="auto",
        xlsx_context_column_count=(args.context_column_count or None),
    )
    grouping = run_grouping_dry_run(
        content, parser=parser, group_sizes=args.group_sizes,
        retrieval_mode=args.retrieval_mode,
        context_column_count=args.context_column_count,
        final_canonical_chunk_count=args.final_canonical_chunk_count,
        preview_sheet=args.preview_sheet,
        preview_excel_row=args.preview_excel_row,
        preview_required_substrings=args.preview_require,
    )
    output: dict[str, Any] = {"grouping_dry_run": grouping.to_dict()}
    if args.run_embedding_benchmark:
        api_key_env = (args.weknora_api_key_env
                       if args.embedding_backend == "weknora"
                       else args.embedding_api_key_env)
        api_key = os.environ.get(api_key_env, "")
        if not api_key:
            output["embedding_benchmark"] = {
                "status": "unavailable",
                "reason": f"environment variable {api_key_env} is not configured",
            }
        elif args.embedding_backend == "weknora" and (
            not args.weknora_base_url or not args.weknora_model_id
        ):
            output["embedding_benchmark"] = {
                "status": "unavailable",
                "reason": "--weknora-base-url and --weknora-model-id are required",
            }
        else:
            detector = analyze_excel_wide_table(
                content, parser=parser, retrieval_mode=args.retrieval_mode,
                context_column_count=args.context_column_count,
                final_canonical_chunk_count=args.final_canonical_chunk_count,
            )
            cache = JsonlEmbeddingCache(args.embedding_cache)
            if args.embedding_backend == "weknora":
                embedder = WeKnoraEmbeddingBackend(
                    base_url=args.weknora_base_url,
                    model_id=args.weknora_model_id,
                    api_key=api_key,
                    cache=cache,
                    batch_size=args.batch_size,
                )
                model_label = args.weknora_model_id
            else:
                embedder = OpenAICompatibleEmbedder(
                    base_url=args.embedding_base_url, api_key=api_key,
                    model=args.embedding_model, cache=cache,
                    batch_size=args.batch_size,
                )
                model_label = args.embedding_model
            output["embedding_benchmark"] = {
                "status": "completed",
                "backend": args.embedding_backend,
                "model": model_label,
                "results": run_embedding_benchmark(
                    content, parser=parser, detector_result=detector,
                    embedder=embedder,
                    group_sizes=(args.benchmark_group_size,),
                    sheet_filter=set(args.benchmark_sheets),
                    query_ids=set(args.benchmark_query_ids)),
            }
    print(json.dumps(output, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
