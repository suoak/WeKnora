"""Bounded conversion of complete embedded EMF images to PNG."""

import io
import shutil
import struct
import tempfile
from pathlib import Path

from PIL import Image


def validate_emf(data: bytes) -> None:
    if len(data) < 88 or len(data) > 8 << 20:
        raise ValueError("EMF size outside supported bounds")
    kind, header_size = struct.unpack_from("<II", data)
    left, top, right, bottom = struct.unpack_from("<iiii", data, 8)
    size, records = struct.unpack_from("<II", data, 48)
    width, height = right - left, bottom - top
    if (
        kind != 1 or header_size < 88 or data[40:44] != b" EMF"
        or size != len(data) or not 2 <= records <= 100000
        or width <= 0 or height <= 0 or width * height > 16 << 20
    ):
        raise ValueError("EMF header outside supported bounds")
    offset, count, last = 0, 0, 0
    while offset < len(data):
        if offset + 8 > len(data):
            raise ValueError("Truncated EMF record")
        last, length = struct.unpack_from("<II", data, offset)
        if not 1 <= last <= 122 or length < 8 or length % 4 or offset + length > len(data):
            raise ValueError("Invalid EMF record")
        if count and last == 1 or last == 14 and offset + length != len(data):
            raise ValueError("Ambiguous EMF record sequence")
        count += 1
        if count > records:
            raise ValueError("EMF record count exceeded")
        offset += length
    if count != records or last != 14:
        raise ValueError("Incomplete EMF record sequence")


def rasterize_emf(data: bytes) -> bytes:
    validate_emf(data)
    soffice = shutil.which("soffice")
    if not soffice:
        raise ValueError("EMF renderer unavailable")
    # Reuse the existing process-group timeout and blocked-proxy executor.
    from docreader.parser.doc_parser import SandboxExecutor

    with tempfile.TemporaryDirectory(prefix="emf-render-") as directory:
        root = Path(directory)
        source = root / "source.emf"
        output = root / "source.png"
        source.write_bytes(data)
        _, _, status = SandboxExecutor(default_timeout=30).execute_in_sandbox([
            soffice, "-env:UserInstallation=" + (root / "profile").as_uri(),
            "--headless", "--convert-to", "png", "--outdir", directory, str(source),
        ])
        if status != 0 or not output.is_file() or not 0 < output.stat().st_size <= 32 << 20:
            raise ValueError("EMF rasterization failed")
        png = output.read_bytes()
        with Image.open(io.BytesIO(png)) as image:
            width, height = image.size
            if image.format != "PNG" or width <= 0 or height <= 0 or width * height > 16 << 20:
                raise ValueError("Rendered EMF image outside supported bounds")
            image.verify()
        return png
