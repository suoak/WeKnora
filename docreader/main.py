import logging
import os
import re
import sys
import traceback
import uuid
from concurrent import futures
from types import SimpleNamespace
from typing import Optional

import grpc
from grpc_health.v1 import health_pb2_grpc
from grpc_health.v1.health import HealthServicer

from docreader import config
from docreader.auth import AuthInterceptor, TLSConfigError, load_tls_credentials
from docreader.config import CONFIG
from docreader.models.document import ChunkingPolicy
from docreader.parser import Parser
from docreader.parser.registry import registry
from docreader.proto import docreader_pb2_grpc
from docreader.proto.docreader_pb2 import (
    CHUNKING_POLICY_DEFAULT,
    CHUNKING_POLICY_PRESERVE_PARSER_CHUNKS,
    ImageRef,
    ListEnginesResponse,
    ParsedTextSpan,
    ParserEngineInfo,
    ReadRequest,
    ReadResponse,
    ReadStreamMeta,
    ReadStreamResponse,
)
from docreader.proto.docreader_pb2 import (
    ParsedSegment as ProtoParsedSegment,
)
from docreader.utils.request import init_logging_request_id, request_id_context

_SURROGATE_RE = re.compile(r"[\ud800-\udfff]")


def to_valid_utf8_text(s: Optional[str]) -> str:
    if not s:
        return ""
    s = _SURROGATE_RE.sub("\ufffd", s)
    return s.encode("utf-8", errors="replace").decode("utf-8")


def _prepare_wire_chunks(result):
    """Return UTF-8-safe content and spans that address that exact content."""
    if result.chunking_policy == ChunkingPolicy.DEFAULT:
        return to_valid_utf8_text(result.content), CHUNKING_POLICY_DEFAULT, []
    if result.chunking_policy != ChunkingPolicy.PRESERVE_PARSER_CHUNKS:
        raise ValueError(f"unknown parser chunking policy: {result.chunking_policy!r}")
    if not result.chunks:
        raise ValueError("preserve_parser_chunks requires non-empty parsed chunks")

    raw_content = result.content
    cursor = 0
    wire_parts = []
    wire_spans = []
    wire_cursor = 0
    for expected_seq, chunk in enumerate(result.chunks):
        if chunk.seq != expected_seq:
            raise ValueError(
                f"parser chunk seq must be contiguous: got {chunk.seq}, "
                f"expected {expected_seq}"
            )
        if (
            chunk.start != cursor
            or chunk.end <= chunk.start
            or chunk.end > len(raw_content)
        ):
            raise ValueError(
                "parser chunks must form a contiguous, non-empty partition of content"
            )
        if raw_content[chunk.start : chunk.end] != chunk.content:
            raise ValueError("parser chunk content does not match its source span")

        clean_content = to_valid_utf8_text(chunk.content)
        if not clean_content.strip():
            raise ValueError("parser semantic chunk must not be blank")
        wire_end = wire_cursor + len(clean_content)
        wire_parts.append(clean_content)
        wire_spans.append(
            ParsedTextSpan(
                seq=expected_seq,
                start=wire_cursor,
                end=wire_end,
                metadata={
                    to_valid_utf8_text(str(k)): to_valid_utf8_text(str(v))
                    for k, v in chunk.metadata.items()
                },
            )
        )
        cursor = chunk.end
        wire_cursor = wire_end

    if cursor != len(raw_content):
        raise ValueError("parser chunks do not cover the complete content")
    return (
        "".join(wire_parts),
        CHUNKING_POLICY_PRESERVE_PARSER_CHUNKS,
        wire_spans,
    )


def _prepare_wire_segments(result):
    """Build UTF-8-safe wire content and a complete segment partition.

    Segment offsets are validated against the raw document. Preserve child
    offsets are then validated against the raw segment, before either layer is
    cleaned or normalized downstream.
    """
    if not result.segments:
        content, policy, chunks = _prepare_wire_chunks(result)
        return content, policy, chunks, []

    raw_content = result.content
    document_cursor = 0
    wire_cursor = 0
    wire_parts = []
    wire_segments = []
    for expected_seq, segment in enumerate(result.segments):
        if segment.seq != expected_seq:
            raise ValueError(
                f"parser segment seq must be contiguous: got {segment.seq}, "
                f"expected {expected_seq}"
            )
        if (
            segment.start != document_cursor
            or segment.end <= segment.start
            or segment.end > len(raw_content)
        ):
            raise ValueError(
                "parser segments must form a contiguous, non-empty partition of content"
            )

        raw_segment = raw_content[segment.start : segment.end]
        if segment.chunking_policy == ChunkingPolicy.DEFAULT:
            clean_segment = to_valid_utf8_text(raw_segment)
            child_spans = []
        elif segment.chunking_policy == ChunkingPolicy.PRESERVE_PARSER_CHUNKS:
            # Reuse the document-level validator with segment-relative chunks.
            clean_segment, _, child_spans = _prepare_wire_chunks(
                SimpleNamespace(
                    content=raw_segment,
                    chunks=segment.chunks,
                    chunking_policy=ChunkingPolicy.PRESERVE_PARSER_CHUNKS,
                )
            )
        else:
            raise ValueError(
                f"unknown parser segment chunking policy: {segment.chunking_policy!r}"
            )
        if not clean_segment.strip():
            raise ValueError("parser segment must not be blank")

        wire_end = wire_cursor + len(clean_segment)
        wire_segments.append(
            ProtoParsedSegment(
                seq=expected_seq,
                start=wire_cursor,
                end=wire_end,
                chunking_policy=(
                    CHUNKING_POLICY_PRESERVE_PARSER_CHUNKS
                    if segment.chunking_policy == ChunkingPolicy.PRESERVE_PARSER_CHUNKS
                    else CHUNKING_POLICY_DEFAULT
                ),
                parsed_chunks=child_spans,
                metadata={
                    to_valid_utf8_text(str(k)): to_valid_utf8_text(str(v))
                    for k, v in segment.metadata.items()
                },
            )
        )
        wire_parts.append(clean_segment)
        document_cursor = segment.end
        wire_cursor = wire_end

    if document_cursor != len(raw_content):
        raise ValueError("parser segments do not cover the complete content")
    return "".join(wire_parts), CHUNKING_POLICY_DEFAULT, [], wire_segments


for handler in logging.root.handlers[:]:
    logging.root.removeHandler(handler)

handler = logging.StreamHandler(sys.stdout)
logging.root.addHandler(handler)

_level_name = (os.environ.get("LOG_LEVEL") or "INFO").upper()
_level = getattr(logging, _level_name, logging.INFO)
logging.root.setLevel(_level)

logger = logging.getLogger(__name__)
logger.info("Initializing server logging, level=%s", _level_name)

init_logging_request_id()


def _resolve_images(
    images: dict, request_id: str, storage_map: dict | None = None
) -> tuple[str, list]:
    """Resolve document images into inline bytes for the Go App to persist.

    ``images`` is a dict of {relative_path: raw_data} where raw_data is
    base64-encoded string or raw bytes.

    The Go App is solely responsible for persisting images to the configured
    storage backend (local/minio/cos/tos). This function only decodes images
    and returns them as inline bytes via ImageRef.

    Returns ("", list[ImageRef]).  image_dir_path is always empty.
    """
    import base64

    if not images:
        return "", []

    mime_map = {
        ".png": "image/png",
        ".jpg": "image/jpeg",
        ".jpeg": "image/jpeg",
        ".gif": "image/gif",
        ".webp": "image/webp",
        ".bmp": "image/bmp",
    }

    refs = []
    for ref_path, b64data in images.items():
        try:
            img_bytes = base64.b64decode(b64data)
        except Exception:
            img_bytes = b64data.encode("utf-8") if isinstance(b64data, str) else b64data

        fname = os.path.basename(ref_path) or f"{uuid.uuid4().hex}.png"
        ext = os.path.splitext(fname)[1].lower()
        mime = mime_map.get(ext, "application/octet-stream")

        refs.append(
            ImageRef(
                filename=fname,
                original_ref=ref_path,
                mime_type=mime,
                image_data=img_bytes,
            )
        )

    logger.info("Resolved %d images (mode=inline)", len(refs))
    return "", refs


def _mime_for_ref(ref_path: str) -> tuple[str, str]:
    """Return (filename, mime_type) for an image reference path."""
    mime_map = {
        ".png": "image/png",
        ".jpg": "image/jpeg",
        ".jpeg": "image/jpeg",
        ".gif": "image/gif",
        ".webp": "image/webp",
        ".bmp": "image/bmp",
    }
    fname = os.path.basename(ref_path) or f"{uuid.uuid4().hex}.png"
    ext = os.path.splitext(fname)[1].lower()
    return fname, mime_map.get(ext, "application/octet-stream")


def _iter_image_refs(images: dict):
    """Yield ImageRef one at a time, freeing each source entry as we go.

    Used by the streaming RPC so we never hold every decoded image plus its
    base64 source in memory simultaneously (the inline path's peak-memory and
    message-size problem for large scanned PDFs).
    """
    import base64

    for ref_path in list(images.keys()):
        b64data = images.pop(ref_path)
        try:
            img_bytes = base64.b64decode(b64data)
        except Exception:
            img_bytes = b64data.encode("utf-8") if isinstance(b64data, str) else b64data
        del b64data
        fname, mime = _mime_for_ref(ref_path)
        yield ImageRef(
            filename=fname,
            original_ref=ref_path,
            mime_type=mime,
            image_data=img_bytes,
        )


class DocReaderServicer(docreader_pb2_grpc.DocReaderServicer):
    def __init__(self):
        super().__init__()
        self.parser = Parser()

    def _parse_request(self, request: ReadRequest):
        """Run the parser for a ReadRequest, returning (result, source_desc).

        Shared by the unary Read and streaming ReadStream RPCs.
        """
        cfg = request.config
        parser_engine = cfg.parser_engine if cfg else ""
        engine_overrides = dict(cfg.parser_engine_overrides) if cfg else {}
        if cfg and cfg.parser_semantic_chunk_max_chars:
            engine_overrides.setdefault(
                "parser_semantic_chunk_max_chars",
                str(cfg.parser_semantic_chunk_max_chars),
            )

        if request.url:
            logger.info("Read(URL): url=%s", request.url)
            result = self.parser.parse_url(
                request.url,
                request.title,
                parser_engine=parser_engine,
                engine_overrides=engine_overrides,
            )
            return result, request.url

        file_type = request.file_type or os.path.splitext(request.file_name)[1][1:]
        logger.info(
            "Read(File): file=%s, type=%s, size=%d bytes",
            request.file_name,
            file_type,
            len(request.file_content),
        )
        result = self.parser.parse_file(
            request.file_name,
            file_type,
            request.file_content,
            parser_engine=parser_engine,
            engine_overrides=engine_overrides,
        )
        return result, request.file_name

    def Read(self, request: ReadRequest, context):
        """Unified read: file mode (file_content set) or URL mode (url set)."""
        request_id = request.request_id or str(uuid.uuid4())

        with request_id_context(request_id):
            try:
                result, source_desc = self._parse_request(request)

                if not result or not result.content:
                    error_msg = f"Failed to parse: {source_desc}"
                    logger.error(error_msg)
                    return ReadResponse(error=error_msg)

                _c = to_valid_utf8_text
                wire_content, chunking_policy, parsed_chunks, parsed_segments = (
                    _prepare_wire_segments(result)
                )
                image_dir, image_refs = _resolve_images(result.images, request_id)

                response = ReadResponse(
                    markdown_content=wire_content,
                    image_refs=image_refs,
                    image_dir_path=image_dir,
                    metadata={k: _c(str(v)) for k, v in result.metadata.items()}
                    if result.metadata
                    else {},
                    chunking_policy=chunking_policy,
                    parsed_chunks=parsed_chunks,
                    parsed_segments=parsed_segments,
                )
                logger.info(
                    "Read response: content_len=%d, images=%d",
                    len(result.content),
                    len(image_refs),
                )
                return response

            except Exception as e:
                error_msg = f"Error reading document: {e}"
                logger.error(error_msg)
                logger.info("Traceback: %s", traceback.format_exc())
                return ReadResponse(error=str(e))

    def ReadStream(self, request: ReadRequest, context):
        """Streaming read: yields one meta frame, then one frame per image.

        Each frame is a small, independent gRPC message, so documents with many
        page images (large scanned PDFs) are returned without hitting the unary
        message-size cap, and neither side has to hold the whole payload at once.
        """
        request_id = request.request_id or str(uuid.uuid4())

        with request_id_context(request_id):
            _c = to_valid_utf8_text
            try:
                result, source_desc = self._parse_request(request)
            except Exception as e:
                logger.error("Error reading document: %s", e)
                logger.info("Traceback: %s", traceback.format_exc())
                yield ReadStreamResponse(meta=ReadStreamMeta(error=str(e)))
                return

            if not result or not result.content:
                error_msg = f"Failed to parse: {source_desc}"
                logger.error(error_msg)
                yield ReadStreamResponse(meta=ReadStreamMeta(error=error_msg))
                return

            images = result.images or {}
            image_count = len(images)
            try:
                wire_content, chunking_policy, parsed_chunks, parsed_segments = (
                    _prepare_wire_segments(result)
                )
            except Exception as e:
                logger.error("Invalid parser-defined chunks: %s", e)
                logger.info("Traceback: %s", traceback.format_exc())
                yield ReadStreamResponse(meta=ReadStreamMeta(error=str(e)))
                return
            yield ReadStreamResponse(
                meta=ReadStreamMeta(
                    markdown_content=wire_content,
                    image_dir_path="",
                    metadata={k: _c(str(v)) for k, v in result.metadata.items()}
                    if result.metadata
                    else {},
                    image_count=image_count,
                    chunking_policy=chunking_policy,
                    parsed_chunks=parsed_chunks,
                    parsed_segments=parsed_segments,
                )
            )

            sent = 0
            for ref in _iter_image_refs(images):
                yield ReadStreamResponse(image=ref)
                sent += 1

            logger.info(
                "ReadStream response: content_len=%d, images=%d",
                len(result.content),
                sent,
            )

    def ListEngines(self, request, context):
        overrides = dict(getattr(request, "config_overrides", None) or {})
        engines_data = registry.list_engines(overrides=overrides or None)
        engines = [
            ParserEngineInfo(
                name=e["name"],
                description=e["description"],
                file_types=e["file_types"],
                available=e.get("available", True),
                unavailable_reason=e.get("unavailable_reason", ""),
            )
            for e in engines_data
        ]
        return ListEnginesResponse(engines=engines)


def main():
    config.print_config()

    interceptors = [AuthInterceptor()]

    server = grpc.server(
        futures.ThreadPoolExecutor(max_workers=CONFIG.grpc_max_workers),
        options=[
            ("grpc.max_send_message_length", CONFIG.grpc_max_file_size_mb),
            ("grpc.max_receive_message_length", CONFIG.grpc_max_file_size_mb),
        ],
        interceptors=interceptors,
    )

    docreader_pb2_grpc.add_DocReaderServicer_to_server(DocReaderServicer(), server)

    health_servicer = HealthServicer()
    health_pb2_grpc.add_HealthServicer_to_server(health_servicer, server)

    try:
        tls_credentials = load_tls_credentials()
    except TLSConfigError as e:
        logger.error("Refusing to start: %s", e)
        sys.exit(1)

    if tls_credentials:
        server.add_secure_port(f"[::]:{CONFIG.grpc_port}", tls_credentials)
        logger.info("Server starting on port %d with TLS", CONFIG.grpc_port)
    else:
        server.add_insecure_port(f"[::]:{CONFIG.grpc_port}")
        logger.warning(
            "Server starting on port %d WITHOUT TLS (insecure mode)", CONFIG.grpc_port
        )

    server.start()

    logger.info("Server started on port %d", CONFIG.grpc_port)
    logger.info("Server is ready to accept connections")

    try:
        server.wait_for_termination()
    except KeyboardInterrupt:
        logger.info("Received termination signal, shutting down server")
        server.stop(0)


if __name__ == "__main__":
    main()
