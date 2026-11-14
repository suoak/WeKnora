"""Convert bounded Word 2003 XML to a verified DOCX for the native reader."""

import base64
import hashlib
import io
import posixpath
import re
import xml.etree.ElementTree as ET
import zipfile
from pathlib import Path

from PIL import Image
from docx import Document as DocxDocument
from docx.oxml import OxmlElement
from docx.opc.constants import RELATIONSHIP_TYPE

from docreader.models.document import Document
from docreader.parser.base_parser import BaseParser

WORDML = "http://schemas.microsoft.com/office/word/2003/wordml"
W = "{" + WORDML + "}"
VML = "{urn:schemas-microsoft-com:vml}"
DOCX = "{http://schemas.openxmlformats.org/wordprocessingml/2006/main}"
R = "{http://schemas.openxmlformats.org/officeDocument/2006/relationships}"
A = "{http://schemas.openxmlformats.org/drawingml/2006/main}"
HINT = "{http://schemas.microsoft.com/office/word/2003/auxHint}"
TEXT_TAGS = (W + "t", HINT + "t")
MAX_BYTES = 8 << 20


def parse_xml(raw: bytes):
    if not raw or len(raw) > MAX_BYTES:
        raise ValueError("WordML XML outside supported bounds")
    text = raw.decode("utf-8-sig")
    declared = re.search(r'<\?xml[^>]*encoding\s*=\s*[\'"]([^\'"]+)', text[:256], re.IGNORECASE)
    if "\x00" in text or "<!DOCTYPE" in text.upper() or "<!ENTITY" in text.upper() or (
        declared is not None and declared[1].lower() not in ("utf-8", "utf8")
    ):
        raise ValueError("WordML XML declaration outside supported bounds")
    root = ET.fromstring(text)
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
            separator = {
                "separator": "separator", "continuation-separator": "continuationSeparator",
            }.get(node.get(W + "type"))
            nodes = list(node.iter())
            empty_separator = (
                node.tag in (W + "footnote", W + "endnote") and separator is not None
                and [n.tag for n in nodes] == [node.tag, W + "p", W + "r", W + separator]
                and node.attrib == {W + "type": node.get(W + "type")}
                and all(not n.attrib for n in nodes[2:])
                and all(key in {"{" + WORDML + "/sp2}" + name for name in (
                    "rsidR", "rsidRDefault", "rsidP",
                )} and re.fullmatch(r"[0-9A-Fa-f]{8}", value)
                        for key, value in nodes[1].attrib.items())
                and all(not (n.text or "").strip() and not (n.tail or "").strip() for n in nodes)
            )
            if not empty_separator:
                raise ValueError("WordML dependency requires unsupported coverage")
        if node.tag in (W + "hdr", W + "ftr") and any(
            child.tag in TEXT_TAGS and (child.text or "").strip() for child in node.iter()
        ):
            raise ValueError("WordML header/footer text requires separate coverage")
        for key, value in node.attrib.items():
            if node.tag == W + "ignoreSubtree" and key == W + "val" and value == WORDML + "/sp2":
                continue  # Known schema-version metadata, not an external resource.
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
    def identity(node):
        return images[node.get("src")] if node.tag == VML + "imagedata" else None

    events = semantic_events(body, TEXT_TAGS, identity)
    paragraphs = paragraph_events(body, W + "p", TEXT_TAGS, identity)
    return events, sorted(images.values()), sorted(links), paragraphs


def semantic_events(root, text_tag, image_identity):
    events, text = [], []
    text_tags = text_tag if isinstance(text_tag, tuple) else (text_tag,)
    for node in root.iter():
        if node.tag in text_tags:
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


def paragraph_events(root, paragraph_tag, text_tag, image_identity):
    paragraphs = []
    for node in root.iter(paragraph_tag):
        events = semantic_events(node, text_tag, image_identity)
        text = "".join(value for kind, value in events if kind == "text")
        images = tuple(value for kind, value in events if kind == "image")
        if text or images:
            paragraphs.append((text, images))
    return paragraphs


def verify_converted(source, converted: bytes):
    events, images, links, paragraphs = source
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
        fields = [node.text or "" for node in body.iter(DOCX + "instrText")]
        fields.extend(node.get(DOCX + "instr", "") for node in body.iter(DOCX + "fldSimple"))
        for field in fields:
            match = re.fullmatch(
                r'\s*HYPERLINK\s+"([^"\r\n]+)"(?:\s+\\l\s+"([^"\r\n]+)")?\s*', field
            )
            if match is None or (match[2] is not None and "#" in match[1]):
                raise ValueError("Converted WordML field requires unsupported coverage")
            actual_links.append(match[1] + ("#" + match[2] if match[2] is not None else ""))
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
        actual = semantic_events(body, DOCX + "t", image_identity)
        # Floating images can move within their anchor paragraph during
        # conversion. Their paragraph, image order and all text must stay.
        original_text = "".join(value for kind, value in events if kind == "text")
        actual_text = "".join(value for kind, value in actual if kind == "text")
        if actual_text != original_text or paragraph_events(
            body, DOCX + "p", DOCX + "t", image_identity
        ) != paragraphs:
            raise ValueError("Converted WordML text/image placement coverage incomplete")


class WordMLParser(BaseParser):
    def parse_into_text(self, content: bytes) -> Document:
        manifest = source_manifest(content)
        root = parse_xml(content)
        images = {node.get(W + "name"): base64.b64decode("".join((node.text or "").split()), validate=True)
                  for node in root.iter(W + "binData")}
        document = DocxDocument()

        def append_node(node, paragraph, parent):
            if node.tag in TEXT_TAGS:
                run = paragraph.add_run(node.text or "")
                parent.append(run._r)
                return
            if node.tag == VML + "imagedata":
                run = paragraph.add_run()
                run.add_picture(io.BytesIO(images[node.get("src")]))
                parent.append(run._r)
                return
            if node.tag == W + "hlink":
                link = OxmlElement("w:hyperlink")
                identity = document.part.relate_to(node.get(W + "dest"), RELATIONSHIP_TYPE.HYPERLINK,
                                                  is_external=True)
                link.set(R + "id", identity)
                parent.append(link)
                for child in node:
                    append_node(child, paragraph, link)
                return
            if node.tag == W + "binData":
                return
            for child in node:
                append_node(child, paragraph, parent)

        for source in root.find(W + "body").iter(W + "p"):
            paragraph = document.add_paragraph()
            append_node(source, paragraph, paragraph._p)
        output = io.BytesIO()
        document.save(output)
        converted = output.getvalue()
        verify_converted(manifest, converted)
        return Document(content="Verified WordML conversion", metadata={
            "wordml_source_sha256": hashlib.sha256(content).hexdigest(),
            "wordml_docx_base64": base64.b64encode(converted).decode(),
        })
