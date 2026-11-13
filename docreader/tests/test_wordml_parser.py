import base64
import io
import unittest
import zipfile

from PIL import Image

from docreader.parser.wordml_parser import WORDML, source_manifest, verify_converted


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


if __name__ == "__main__":
    unittest.main()
