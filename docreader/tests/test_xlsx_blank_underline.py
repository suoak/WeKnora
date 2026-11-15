import io
import unittest
import zipfile
from xml.etree import ElementTree as ET

import openpyxl
from defusedxml.common import DefusedXmlException

from docreader.parser.excel_parser import ExcelParser
from docreader.parser.markitdown_parser import StdMarkitdownParser
from docreader.parser.xlsx_repair import normalize_blank_underline_xlsx


NS = "{http://schemas.openxmlformats.org/spreadsheetml/2006/main}"


def workbook(underline=" "):
    wb = openpyxl.Workbook()
    ws = wb.active
    ws.append(["name", "count"])
    ws.append(["retained & <text>", 42])
    out = io.BytesIO()
    wb.save(out)
    with zipfile.ZipFile(io.BytesIO(out.getvalue())) as z:
        parts = {entry.filename: z.read(entry) for entry in z.infolist()}
    parts["[Content_Types].xml"] = parts["[Content_Types].xml"].replace(
        b"</Types>", b'<Override PartName="/xl/sharedStrings.xml" '
        b'ContentType="application/vnd.openxmlformats-officedocument.'
        b'spreadsheetml.sharedStrings+xml"/></Types>'
    )
    root = ET.fromstring(parts["xl/worksheets/sheet1.xml"])
    for c in root.iter(NS + "c"):
        if c.attrib["r"] == "A2":
            c.attrib["t"] = "s"
            c.clear()
            c.attrib.update(r="A2", t="s")
            ET.SubElement(c, NS + "v").text = "0"
    parts["xl/worksheets/sheet1.xml"] = ET.tostring(root)
    parts["xl/sharedStrings.xml"] = (
        '<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" '
        'count="1" uniqueCount="1"><si><r><rPr><b/><u val="' + underline
        + '"/></rPr><t xml:space="preserve">retained &amp; &lt;text&gt;</t></r>'
        '<r><rPr><u val="single"/></rPr><t> suffix</t></r></si></sst>'
    ).encode()
    return package(parts), parts


def package(parts):
    out = io.BytesIO()
    with zipfile.ZipFile(out, "w", zipfile.ZIP_DEFLATED) as z:
        for name, data in parts.items():
            z.writestr(name, data)
    return out.getvalue()


class BlankUnderlineTest(unittest.TestCase):
    def test_recovers_real_failure_without_losing_cells_or_parts(self):
        data, original = workbook()
        with self.assertRaises(ValueError):
            openpyxl.load_workbook(io.BytesIO(data), read_only=True)
        patched = normalize_blank_underline_xlsx(data)
        self.assertIsNotNone(patched)
        with zipfile.ZipFile(io.BytesIO(patched)) as z:
            self.assertEqual(set(z.namelist()), set(original))
            for name in original:
                if name != "xl/sharedStrings.xml":
                    self.assertEqual(z.read(name), original[name])
            strings = ET.fromstring(z.read("xl/sharedStrings.xml"))
            self.assertEqual([u.attrib["val"] for u in strings.iter(NS + "u")], ["single"])
            self.assertEqual("".join(t.text for t in strings.iter(NS + "t")),
                             "retained & <text> suffix")
        wb = openpyxl.load_workbook(io.BytesIO(patched), read_only=True)
        self.assertEqual(list(wb.active.values), [("name", "count"), ("retained & <text> suffix", 42)])
        wb.close()
        document = ExcelParser(file_name="test.xlsx", file_type="xlsx").parse_into_text(data)
        self.assertIn("retained & <text> suffix", document.content)
        self.assertIn("42", document.content)
        markdown = StdMarkitdownParser(file_name="test.xlsx", file_type="xlsx").parse_into_text(data)
        self.assertIn("retained & <text> suffix", markdown.content)
        self.assertIn("42", markdown.content)
        self.assertIsNone(normalize_blank_underline_xlsx(patched))

    def test_valid_and_unknown_values_are_not_silently_rewritten(self):
        for value in ["single", "double", "doubleAccounting", "unknown"]:
            data, _ = workbook(value)
            self.assertIsNone(normalize_blank_underline_xlsx(data))
        data, _ = workbook("unknown")
        with self.assertRaises(ValueError):
            ExcelParser(file_name="test.xlsx", file_type="xlsx").parse_into_text(data)

    def test_preserves_character_reference_line_breaks(self):
        _, parts = workbook()
        parts["xl/sharedStrings.xml"] = parts["xl/sharedStrings.xml"].replace(
            b"retained &amp; &lt;text&gt;", b"before&#13;after&#10;line"
        )
        patched = normalize_blank_underline_xlsx(package(parts))
        with zipfile.ZipFile(io.BytesIO(patched)) as z:
            root = ET.fromstring(z.read("xl/sharedStrings.xml"))
        self.assertEqual(next(root.iter(NS + "t")).text, "before\rafter\nline")

    def test_does_not_interpret_text_as_style_or_expand_entities(self):
        _, parts = workbook("single")
        parts["xl/sharedStrings.xml"] = parts["xl/sharedStrings.xml"].replace(
            b"retained &amp; &lt;text&gt;", b'&lt;u val=" " /&gt;'
        )
        self.assertIsNone(normalize_blank_underline_xlsx(package(parts)))
        parts["xl/sharedStrings.xml"] = (
            b'<!DOCTYPE sst [<!ENTITY injected "secret">]>' + parts["xl/sharedStrings.xml"]
        )
        with self.assertRaises(DefusedXmlException):
            normalize_blank_underline_xlsx(package(parts))


if __name__ == "__main__":
    unittest.main()
