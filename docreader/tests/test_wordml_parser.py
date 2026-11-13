import base64
import io
import unittest
import zipfile

from PIL import Image

from docreader.parser.wordml_parser import WORDML, WordMLParser, source_manifest, verify_converted


class WordMLCoverageTests(unittest.TestCase):
    def setUp(self):
        image = io.BytesIO()
        Image.new("RGB", (2, 2), "red").save(image, format="PNG")
        self.png = image.getvalue()
        self.source = (
            f'<w:wordDocument xmlns:w="{WORDML}" xmlns:v="urn:schemas-microsoft-com:vml">'
            '<w:body><w:p><w:r><w:t>complete text</w:t><w:pict>'
            '<w:binData w:name="wordml://image.png">'
            + base64.b64encode(self.png).decode()
            + '</w:binData><v:shape><v:imagedata src="wordml://image.png"/></v:shape>'
            '</w:pict></w:r></w:p></w:body></w:wordDocument>'
        ).encode()

    def package(self, text="complete text", image=True):
        output = io.BytesIO()
        with zipfile.ZipFile(output, "w") as package:
            package.writestr("word/document.xml", (
                '<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" '
                'xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" '
                'xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main">'
                '<w:body><w:p><w:r><w:t>' + text + '</w:t><w:drawing><a:blip r:embed="image1"/>'
                '</w:drawing></w:r></w:p></w:body></w:document>'
            ))
            package.writestr("word/_rels/document.xml.rels", (
                '<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">'
                '<Relationship Id="image1" Type="http://schemas.openxmlformats.org/officeDocument/'
                '2006/relationships/image" Target="media/image.png"/></Relationships>'
            ))
            if image:
                package.writestr("word/media/image.png", self.png)
        return output.getvalue()

    def test_complete_text_and_images_required(self):
        manifest = source_manifest(self.source)
        verify_converted(manifest, self.package())
        for package in (self.package(text="partial"), self.package(image=False)):
            with self.assertRaises(ValueError):
                verify_converted(manifest, package)

    def test_closed_builder_retains_source_binding_and_coverage(self):
        result = WordMLParser(file_name="source.xml", file_type="wordml").parse_into_text(self.source)
        self.assertTrue(result.metadata["wordml_source_sha256"])
        converted = base64.b64decode(result.metadata["wordml_docx_base64"], validate=True)
        verify_converted(source_manifest(self.source), converted)

    def test_source_dependencies_and_ambiguity_refused(self):
        for source in (
            self.source.replace(b"wordml://image.png\"/>", b"https://example.com/image.png\"/>"),
            self.source.replace(b"</w:body>", b"<w:object/></w:body>"),
            self.source.replace(b"</w:body>", b"<w:hdr><w:t>header</w:t></w:hdr></w:body>"),
            b'<!DOCTYPE a [<!ENTITY b "x">]>' + self.source,
        ):
            with self.assertRaises(ValueError):
                source_manifest(source)

    def test_hyperlinks_remain_bound(self):
        source = self.source.replace(b"</w:body>", b'<w:hlink w:dest="https://example.com/"/></w:body>')
        with self.assertRaises(ValueError):
            verify_converted(source_manifest(source), self.package())

    def test_image_placement_and_unknown_body_remain_closed(self):
        package = self.package()
        output = io.BytesIO()
        with zipfile.ZipFile(io.BytesIO(package)) as original, zipfile.ZipFile(output, "w") as changed:
            for name in original.namelist():
                raw = original.read(name)
                if name == "word/document.xml":
                    raw = raw.replace(b'<w:drawing>', b'</w:r></w:p><w:p><w:r><w:drawing>')
                changed.writestr(name, raw)
        with self.assertRaises(ValueError):
            verify_converted(source_manifest(self.source), output.getvalue())
        with self.assertRaises(ValueError):
            source_manifest(self.source.replace(b'</w:body>', b'<w:unknown/></w:body>'))

    def test_empty_note_separators_do_not_admit_note_content(self):
        separator = (
            '<w:footnote w:type="separator"><w:p xmlns:wx="' + WORDML + '/sp2" '
            'wx:rsidR="005D34DE"><w:r><w:separator/></w:r></w:p></w:footnote>'
        ).encode()
        source = self.source.replace(b'</w:wordDocument>', separator + b'</w:wordDocument>')
        source_manifest(source)
        with self.assertRaises(ValueError):
            source_manifest(source.replace(b'<w:separator/>', b'<w:t>note content</w:t>'))

    def test_field_hyperlink_keeps_fragment(self):
        source = self.source.replace(b'</w:body>', b'<w:hlink w:dest="https://example.com/#a"/></w:body>')
        output = io.BytesIO()
        with zipfile.ZipFile(io.BytesIO(self.package())) as original, zipfile.ZipFile(output, 'w') as changed:
            for name in original.namelist():
                raw = original.read(name)
                if name == 'word/document.xml':
                    raw = raw.replace(b'</w:r>', (
                        b'<w:instrText> HYPERLINK "https://example.com/" \\l "a" </w:instrText></w:r>'
                    ))
                changed.writestr(name, raw)
        verify_converted(source_manifest(source), output.getvalue())
        with self.assertRaises(ValueError):
            verify_converted(source_manifest(source.replace(b'#a', b'#b')), output.getvalue())


if __name__ == "__main__":
    unittest.main()
