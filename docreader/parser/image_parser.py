import base64
import logging
import os

from docreader.models.document import Document
from docreader.parser.base_parser import BaseParser

logger = logging.getLogger(__name__)


class ImageParser(BaseParser):
    """Parser for standalone image files.

    Returns the image as a markdown reference with the raw image data
    in Document.images so that the Go-side ImageResolver (or main.py's
    _resolve_images) can handle storage upload.
    """

    def parse_into_text(self, content: bytes) -> Document:
        logger.info("Parsing image file=%s, size=%d bytes", self.file_name, len(content))

        if self.file_type.lower().lstrip(".") == "emf":
            from docreader.parser.emf_raster import rasterize_emf

            rendered = rasterize_emf(content)
            ref = "images/rendered.png"
            return Document(
                content=f"![Rendered embedded image]({ref})",
                images={ref: base64.b64encode(rendered).decode()},
            )

        ext = os.path.splitext(self.file_name)[1].lower() or ".png"
        ref_path = f"images/{self.file_name}"

        text = f"![{self.file_name}]({ref_path})"
        images = {ref_path: base64.b64encode(content).decode()}

        return Document(content=text, images=images)
