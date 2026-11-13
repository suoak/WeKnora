"""Convert bounded Word 2003 XML to a verified DOCX for the native reader."""

import base64
import hashlib
import io
import posixpath
import shutil
import tempfile
import xml.etree.ElementTree as ET
import zipfile
from pathlib import Path

from PIL import Image

from docreader.models.document import Document
from docreader.parser.base_parser import BaseParser
from docreader.parser.doc_parser import SandboxExecutor

WORDML = "http://schemas.microsoft.com/office/word/2003/wordml"
W = "{" + WORDML + "}"
VML = "{urn:schemas-microsoft-com:vml}"
DOCX = "{http://schemas.openxmlformats.org/wordprocessingml/2006/main}"
R = "{http://schemas.openxmlformats.org/officeDocument/2006/relationships}"
A = "{http://schemas.openxmlformats.org/drawingml/2006/main}"
MAX_BYTES = 8 << 20


def parse_xml(raw: bytes):
    if not raw or len(raw) > MAX_BYTES or b"<!DOCTYPE" in raw.upper() or b"<!ENTITY" in raw.upper():
        raise ValueError("WordML XML outside supported bounds")
    root = ET.fromstring(raw)
    stack = [(root, 0)]
    count = 0
    while stack:
        node, depth = stack.pop()
        count += 1
        if depth > 96 or count > 100000:
            raise ValueError("WordML XML structure outside supported bounds")
        stack.extend((child, depth + 1) for child in node)
    return root


def source_manifest(raw: bytes):
    root = parse_xml(raw)
    if root.tag != W + "wordDocument" or len(root.findall(W + "body")) != 1:
        raise ValueError("Expected a complete Word 2003 XML document")
    body = root.find(W + "body")
    allowed = {W + name for name in (
        "body", "p", "pPr", "jc", "rPr", "rFonts", "b", "color", "sz", "sz-cs", "r", "t", "u",
        "pStyle", "listPr", "ilvl", "ilfo", "spacing", "kern", "lang", "hlink", "rStyle", "proofErr",
        "ind", "pict", "binData", "sectPr", "hdr", "ftr", "pgSz", "pgMar", "cols", "docGrid",
    )}
    allowed.update(VML + name for name in ("shape", "imagedata", "shapetype", "stroke", "formulas", "f", "path"))
    allowed.update("{http://schemas.microsoft.com/office/word/2003/auxHint}" + name for name in (
        "font", "sub-section", "t", "pBdrGroup", "borders", "bottom",
    ))
    allowed.add("{urn:schemas-microsoft-com:office:office}lock")
    if any(node.tag not in allowed for node in body.iter()):
        raise ValueError("WordML body requires unsupported coverage")
    # These features need a separate coverage proof; never convert them to a
    # text-only candidate or allow the converter to load external resources.
    forbidden = {W + n for n in (
        "object", "instrText", "fldSimple", "footnote", "endnote", "txbxContent",
        "subDoc", "altChunk", "attachedTemplate", "docOleData",
    )}
    images = {}
    links = []
    for node in root.iter():
        if node.tag in forbidden:
            raise ValueError("WordML dependency requires unsupported coverage")
        if node.tag in (W + "hdr", W + "ftr") and next(node.iter(W + "t"), None) is not None:
            raise ValueError("WordML header/footer text requires separate coverage")
        for key, value in node.attrib.items():
            if "://" in value and not (node.tag == W + "hlink" and key == W + "dest"):
                internal_image = (
                    (node.tag == VML + "imagedata" and key == "src")
                    or (node.tag == W + "binData" and key == W + "name")
                ) and value.startswith("wordml://")
                if not internal_image:
                    raise ValueError("WordML external dependency refused")
        if node.tag == W + "hlink":
            target = node.get(W + "dest")
            if not target:
                raise ValueError("WordML hyperlink target missing")
            links.append(target)
        if node.tag == W + "binData":
            name = node.get(W + "name")
            if not name or name in images or len(images) >= 32:
                raise ValueError("WordML image binding ambiguous")
            data = base64.b64decode("".join((node.text or "").split()), validate=True)
            with Image.open(io.BytesIO(data)) as image:
                if image.format not in ("PNG", "JPEG") or image.width * image.height > 16 << 20:
                    raise ValueError("WordML image outside supported bounds")
                image.verify()
            images[name] = hashlib.sha256(data).hexdigest()
    refs = [n.get("src") for n in root.iter(VML + "imagedata")]
    if len(refs) != len(images) or set(refs) != set(images):
        raise ValueError("WordML image coverage incomplete")
    for shape in root.iter(VML + "shape"):
        if len(list(shape.iter(VML + "imagedata"))) != 1:
            raise ValueError("WordML drawing requires unsupported coverage")
    events = semantic_events(body, W + "t", lambda node: images[node.get("src")]
                             if node.tag == VML + "imagedata" else None)
    return events, sorted(images.values()), sorted(links)


def semantic_events(root, text_tag, image_identity):
    events, text = [], []
    for node in root.iter():
        if node.tag == text_tag:
            text.append("".join((node.text or "").split()))
        image = image_identity(node)
        if image is not None:
            if text:
                events.append(("text", "".join(text)))
                text = []
            events.append(("image", image))
    if text:
        events.append(("text", "".join(text)))
    return [event for event in events if event[0] != "text" or event[1]]


def verify_converted(source, converted: bytes):
    events, images, links = source
    if not converted or len(converted) > MAX_BYTES:
        raise ValueError("Converted WordML package outside supported bounds")
    with zipfile.ZipFile(io.BytesIO(converted)) as package:
        members = package.infolist()
        names = [p.filename for p in members]
        if len(members) > 512 or len(names) != len(set(names)) or sum(p.file_size for p in members) > 32 << 20:
            raise ValueError("Converted WordML package ambiguous")
        if any(n.startswith(("/", "\\")) or "\\" in n or ".." in Path(n).parts for n in names):
            raise ValueError("Converted WordML package path invalid")
        body = parse_xml(package.read("word/document.xml"))
        actual_images = sorted(hashlib.sha256(package.read(n)).hexdigest()
                               for n in names if n.startswith("word/media/"))
        if actual_images != images:
            raise ValueError("Converted WordML image coverage incomplete")
        actual_links = []
        for name in names:
            if name.endswith(".rels"):
                for relationship in parse_xml(package.read(name)):
                    if relationship.get("Type", "").endswith("/hyperlink"):
                        actual_links.append(relationship.get("Target"))
                    elif relationship.get("TargetMode") == "External":
                        raise ValueError("Converted WordML external dependency refused")
        if sorted(actual_links) != links or package.testzip() is not None:
            raise ValueError("Converted WordML relationships incomplete")
        relationships = {}
        if "word/_rels/document.xml.rels" in names:
            for relationship in parse_xml(package.read("word/_rels/document.xml.rels")):
                identity = relationship.get("Id")
                if not identity or identity in relationships:
                    raise ValueError("Converted WordML relationship ambiguous")
                relationships[identity] = relationship

        def image_identity(node):
            if node.tag not in (A + "blip", VML + "imagedata"):
                return None
            identity = node.get(R + "embed") or node.get(R + "id")
            relationship = relationships.get(identity)
            if relationship is None or not relationship.get("Type", "").endswith("/image"):
                raise ValueError("Converted WordML image relationship missing")
            target = posixpath.normpath(posixpath.join("word", relationship.get("Target", "")))
            if not target.startswith("word/media/") or target not in names:
                raise ValueError("Converted WordML image target invalid")
            return hashlib.sha256(package.read(target)).hexdigest()

        # Runs may split and gain layout spaces; every non-whitespace character
        # and every image must retain its order relative to the other content.
        if semantic_events(body, DOCX + "t", image_identity) != events:
            raise ValueError("Converted WordML text/image placement coverage incomplete")


class WordMLParser(BaseParser):
    def parse_into_text(self, content: bytes) -> Document:
        manifest = source_manifest(content)
        soffice = shutil.which("soffice")
        if not soffice:
            raise ValueError("WordML converter unavailable")
        with tempfile.TemporaryDirectory(prefix="wordml-convert-") as directory:
            root = Path(directory)
            source = root / "source.xml"
            source.write_bytes(content)
            _, _, status = SandboxExecutor(default_timeout=30).execute_in_sandbox([
                soffice, "-env:UserInstallation=" + (root / "profile").as_uri(),
                "--headless", "--convert-to", "docx", "--outdir", directory, str(source),
            ])
            output = root / "source.docx"
            if status != 0 or not output.is_file() or output.stat().st_size > MAX_BYTES:
                raise ValueError("WordML conversion failed")
            converted = output.read_bytes()
        verify_converted(manifest, converted)
        return Document(content="Verified WordML conversion", metadata={
            "wordml_source_sha256": hashlib.sha256(content).hexdigest(),
            "wordml_docx_base64": base64.b64encode(converted).decode(),
        })
