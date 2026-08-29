import io
import json
import tempfile
import unittest
from pathlib import Path
from unittest.mock import Mock, patch

import openpyxl

from docreader.parser.excel_parser import ExcelParser
from docreader.parser.excel_retrieval_grouping_benchmark import (
    JsonlEmbeddingCache,
    OpenAICompatibleEmbedder,
    WeKnoraEmbeddingBackend,
    iter_retrieval_representations,
    run_grouping_dry_run,
)
from docreader.parser.excel_wide_table_dry_run import analyze_excel_wide_table


def _workbook_bytes(rows):
    workbook = openpyxl.Workbook()
    sheet = workbook.active
    sheet.title = "Capacity"
    for row in rows:
        sheet.append(row)
    buffer = io.BytesIO()
    workbook.save(buffer)
    return buffer.getvalue()


def _rows_with_trailing_metadata():
    headers = ["id", "metric"] + [f"sku-{index}" for index in range(8)] + [
        "owner", "verified", "notes"
    ]
    rows = [headers]
    for row_index in range(6):
        payload = [f"value-{row_index}-{column}" for column in range(8)]
        if row_index < 2:
            payload[5:] = [None, None, None]
        rows.append([
            2000 + row_index,
            f"metric-{row_index}",
            *payload,
            f"owner-{row_index % 2}",
            row_index % 2 == 0,
            "management-only" if row_index == 1 else None,
        ])
    return rows


class ExcelRetrievalGroupingBenchmarkTest(unittest.TestCase):
    def setUp(self):
        self.content = _workbook_bytes(_rows_with_trailing_metadata())
        self.parser = ExcelParser(
            file_name="generic.xlsx",
            file_type="xlsx",
            xlsx_first_row_as_header=True,
            xlsx_chunking_mode="auto",
        )

    def test_grouping_count_is_per_row_not_total_divided_by_group_size(self):
        result = run_grouping_dry_run(
            self.content,
            parser=self.parser,
            group_sizes=(1, 4, 8),
            context_column_count=2,
            final_canonical_chunk_count=6,
        )

        counts = {group.payload_group_size: group for group in result.groupings}
        self.assertEqual(counts[1].retrieval_count, 42)
        # Per-row: 2*ceil(5/4) + 4*ceil(8/4) = 12; ceil(42/4) = 11.
        self.assertEqual(counts[4].retrieval_count, 12)
        self.assertEqual(counts[8].retrieval_count, 6)
        self.assertEqual(counts[4].total_index_units, 18)
        self.assertEqual(counts[4].retrieval_to_canonical_ratio, 2.0)
        self.assertEqual(counts[4].total_index_expansion_ratio, 3.0)
        sheet = counts[4].sheets[0]
        self.assertEqual(sheet.canonical_rows, 6)
        self.assertEqual(sheet.payload_columns, 8)

    def test_representation_never_includes_trailing_metadata_or_adjacent_row(self):
        detector = analyze_excel_wide_table(
            self.content,
            parser=self.parser,
            retrieval_mode="auto",
            context_column_count=2,
        )
        representations = list(iter_retrieval_representations(
            content=self.content,
            parser=self.parser,
            detector_result=detector,
            payload_group_size=4,
            sheet_filter={"Capacity"},
        ))
        first = next(rep for rep in representations if rep.excel_row == 2)

        self.assertIn("id: 2000", first.content)
        self.assertIn("metric: metric-0", first.content)
        self.assertIn("sku-0: value-0-0", first.content)
        self.assertIn("sku-3: value-0-3", first.content)
        self.assertNotIn("sku-4: value-0-4", first.content)
        self.assertNotIn("owner", first.content)
        self.assertNotIn("verified", first.content)
        self.assertNotIn("management-only", first.content)
        self.assertNotIn("metric-1", first.content)
        self.assertEqual(first.payload_column_indices, [2, 3, 4, 5])

    def test_embedding_cache_avoids_repeated_api_calls(self):
        response = Mock()
        response.ok = True
        response.json.return_value = {
            "data": [
                {"index": 0, "embedding": [1.0, 0.0]},
                {"index": 1, "embedding": [0.0, 1.0]},
            ]
        }
        with tempfile.TemporaryDirectory() as directory:
            cache_path = Path(directory) / "cache.jsonl"
            embedder = OpenAICompatibleEmbedder(
                base_url="https://embedding.invalid/v1",
                api_key="test-key",
                model="test-model",
                cache=JsonlEmbeddingCache(cache_path),
                batch_size=2,
            )
            with patch(
                "docreader.parser.excel_retrieval_grouping_benchmark.requests.post",
                return_value=response,
            ) as request:
                first = embedder.embed(["first", "second"])
                second = embedder.embed(["first", "second"])

            self.assertEqual(request.call_count, 1)
            self.assertEqual(first.tolist(), second.tolist())
            records = [json.loads(line) for line in cache_path.read_text().splitlines()]
            self.assertEqual(len(records), 2)
            self.assertEqual(
                set(records[0]),
                {"cache_key", "model_id", "text_hash", "dimension", "vector"},
            )

    def test_weknora_backend_batches_preserves_order_and_never_caches_credentials(self):
        first_response = Mock()
        first_response.ok = True
        first_response.json.return_value = {
            "success": True,
            "data": {
                "model_id": "model-1",
                "count": 2,
                "dimension": 2,
                "vectors": [[1.0, 0.0], [0.0, 1.0]],
            },
        }
        second_response = Mock()
        second_response.ok = True
        second_response.json.return_value = {
            "success": True,
            "data": {
                "model_id": "model-1",
                "count": 1,
                "dimension": 2,
                "vectors": [[0.5, 0.5]],
            },
        }
        with tempfile.TemporaryDirectory() as directory:
            cache_path = Path(directory) / "weknora-cache.jsonl"
            backend = WeKnoraEmbeddingBackend(
                base_url="http://weknora.invalid/api/v1/",
                model_id="model-1",
                api_key="must-not-be-cached",
                cache=JsonlEmbeddingCache(cache_path),
                batch_size=2,
            )
            with patch(
                "docreader.parser.excel_retrieval_grouping_benchmark.requests.post",
                side_effect=[first_response, second_response],
            ) as request:
                vectors, stats = backend.embed_with_stats(["first", "second", "third"])
                cached, cached_stats = backend.embed_with_stats(["first", "second", "third"])

            self.assertEqual(vectors.tolist(), [[1.0, 0.0], [0.0, 1.0], [0.5, 0.5]])
            self.assertEqual(vectors.tolist(), cached.tolist())
            self.assertEqual(stats.requests, 2)
            self.assertEqual(stats.cache_hits, 0)
            self.assertEqual(cached_stats.requests, 0)
            self.assertEqual(cached_stats.cache_hits, 3)
            self.assertEqual(request.call_count, 2)
            self.assertEqual(
                request.call_args_list[0].args[0],
                "http://weknora.invalid/api/v1/models/model-1/debug/embeddings",
            )
            self.assertEqual(
                request.call_args_list[0].kwargs["json"],
                {"texts": ["first", "second"]},
            )
            cache_text = cache_path.read_text(encoding="utf-8")
            self.assertNotIn("must-not-be-cached", cache_text)
            self.assertNotIn("first", cache_text)
            for line in cache_text.splitlines():
                self.assertEqual(
                    set(json.loads(line)),
                    {"cache_key", "model_id", "text_hash", "dimension", "vector"},
                )

    def test_weknora_backend_splits_batches_at_request_body_limit(self):
        response = Mock()
        response.ok = True
        response.json.side_effect = [
            {"success": True, "data": {
                "model_id": "model-1", "count": 1, "dimension": 1,
                "vectors": [[1.0]],
            }},
            {"success": True, "data": {
                "model_id": "model-1", "count": 1, "dimension": 1,
                "vectors": [[2.0]],
            }},
        ]
        texts = ["甲" * 12000, "乙" * 12000]
        with tempfile.TemporaryDirectory() as directory:
            backend = WeKnoraEmbeddingBackend(
                base_url="http://weknora.invalid",
                model_id="model-1",
                api_key="not-persisted",
                cache=JsonlEmbeddingCache(Path(directory) / "cache.jsonl"),
                batch_size=32,
            )
            with patch(
                "docreader.parser.excel_retrieval_grouping_benchmark.requests.post",
                return_value=response,
            ) as request:
                vectors, stats = backend.embed_with_stats(texts)

        self.assertEqual(vectors.tolist(), [[1.0], [2.0]])
        self.assertEqual(stats.requests, 2)
        self.assertEqual(request.call_count, 2)
        for call in request.call_args_list:
            encoded = json.dumps(
                call.kwargs["json"], ensure_ascii=False, separators=(",", ":")
            ).encode("utf-8")
            self.assertLessEqual(len(encoded), 64 * 1024)


if __name__ == "__main__":
    unittest.main()
