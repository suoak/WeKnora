import unittest

from docreader.main import _prepare_wire_chunks, _prepare_wire_segments
from docreader.models.document import (
    Chunk,
    ChunkingPolicy,
    Document,
    ParsedSegment,
)
from docreader.proto.docreader_pb2 import (
    CHUNKING_POLICY_DEFAULT,
    CHUNKING_POLICY_PRESERVE_PARSER_CHUNKS,
)


class ParserDefinedWireChunksTest(unittest.TestCase):
    def test_default_policy_does_not_transport_parser_chunks(self):
        document = Document(
            content="first\nsecond\n",
            chunks=[Chunk(content="first\n", seq=0, start=0, end=6)],
        )
        content, policy, spans = _prepare_wire_chunks(document)
        self.assertEqual(content, "first\nsecond\n")
        self.assertEqual(policy, CHUNKING_POLICY_DEFAULT)
        self.assertEqual(spans, [])

    def test_preserve_rebuilds_utf8_safe_content_and_offsets(self):
        first = "中文😀\r\n"
        second = "bad\ud800value\r"
        document = Document(
            content=first + second,
            chunks=[
                Chunk(
                    content=first,
                    seq=0,
                    start=0,
                    end=len(first),
                    metadata={"parser.source_kind": "test"},
                ),
                Chunk(
                    content=second,
                    seq=1,
                    start=len(first),
                    end=len(first) + len(second),
                ),
            ],
            chunking_policy=ChunkingPolicy.PRESERVE_PARSER_CHUNKS,
        )
        content, policy, spans = _prepare_wire_chunks(document)
        self.assertEqual(policy, CHUNKING_POLICY_PRESERVE_PARSER_CHUNKS)
        self.assertEqual(content, "中文😀\r\nbad�value\r")
        self.assertEqual((spans[0].start, spans[0].end), (0, len(first)))
        self.assertEqual(spans[1].start, spans[0].end)
        self.assertEqual(spans[1].end, len(content))

    def test_preserve_rejects_gap(self):
        document = Document(
            content="one\ntwo\n",
            chunks=[
                Chunk(content="one\n", seq=0, start=0, end=4),
                Chunk(content="two\n", seq=1, start=5, end=9),
            ],
            chunking_policy=ChunkingPolicy.PRESERVE_PARSER_CHUNKS,
        )
        with self.assertRaisesRegex(ValueError, "contiguous"):
            _prepare_wire_chunks(document)

    def test_segments_use_document_offsets_and_segment_relative_child_offsets(self):
        first = "default\r\n"
        second_a = "中文😀\r"
        second_b = "tail\r\n"
        document = Document(
            content=first + second_a + second_b,
            segments=[
                ParsedSegment(
                    seq=0,
                    start=0,
                    end=len(first),
                    metadata={"parser.sheet": "Notes"},
                ),
                ParsedSegment(
                    seq=1,
                    start=len(first),
                    end=len(first + second_a + second_b),
                    chunking_policy=ChunkingPolicy.PRESERVE_PARSER_CHUNKS,
                    metadata={"parser.sheet": "Data"},
                    chunks=[
                        Chunk(
                            content=second_a,
                            seq=0,
                            start=0,
                            end=len(second_a),
                            metadata={"parser.sheet": "Data", "parser.row": "2"},
                        ),
                        Chunk(
                            content=second_b,
                            seq=1,
                            start=len(second_a),
                            end=len(second_a + second_b),
                            metadata={"parser.sheet": "Data", "parser.row": "3"},
                        ),
                    ],
                ),
            ],
        )

        content, policy, chunks, segments = _prepare_wire_segments(document)
        self.assertEqual(policy, CHUNKING_POLICY_DEFAULT)
        self.assertEqual(chunks, [])
        self.assertEqual(content, first + second_a + second_b)
        self.assertEqual((segments[0].start, segments[0].end), (0, len(first)))
        self.assertEqual(segments[1].start, segments[0].end)
        self.assertEqual(segments[1].end, len(content))
        self.assertEqual(
            (segments[1].parsed_chunks[0].start, segments[1].parsed_chunks[0].end),
            (0, len(second_a)),
        )
        self.assertEqual(segments[1].parsed_chunks[1].start, len(second_a))


if __name__ == "__main__":
    unittest.main()
