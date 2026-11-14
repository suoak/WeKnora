import struct
import unittest
import io

from PIL import Image
from docreader.parser.emf_raster import crop_rendered_emf, validate_emf


def emf_fixture():
    header = bytearray(88)
    struct.pack_into("<IIiiii", header, 0, 1, 88, 0, 0, 80, 80)
    header[40:44] = b" EMF"
    struct.pack_into("<II", header, 48, 108, 2)
    return bytes(header) + struct.pack("<IIIII", 14, 20, 0, 0, 20)


class EMFValidationTest(unittest.TestCase):
    def test_remove_only_surrounding_white_canvas(self):
        image = Image.new("RGB", (100, 100), "white")
        image.paste("black", (30, 40, 60, 70))
        raw = io.BytesIO()
        image.save(raw, format="PNG")
        cropped = Image.open(io.BytesIO(crop_rendered_emf(raw.getvalue())))
        self.assertEqual(cropped.size, (46, 46))
        self.assertEqual(cropped.getpixel((8, 8)), (0, 0, 0))

    def test_blank_or_page_edge_content_is_refused(self):
        for box in [None, (0, 0, 20, 20)]:
            image = Image.new("RGB", (100, 100), "white")
            if box:
                image.paste("black", box)
            raw = io.BytesIO()
            image.save(raw, format="PNG")
            with self.assertRaises(ValueError):
                crop_rendered_emf(raw.getvalue())

    def test_complete_bounded_records(self):
        validate_emf(emf_fixture())

    def test_invalid_headers_records_and_limits(self):
        base = emf_fixture()
        variants = [b"not an image", base[:-1], base + b"extra"]
        for offset, value in [(4, 80), (48, 0), (52, 100001), (92, 4), (88, 1)]:
            changed = bytearray(base)
            struct.pack_into("<I", changed, offset, value)
            variants.append(bytes(changed))
        changed = bytearray(base)
        struct.pack_into("<ii", changed, 16, 100000, 100000)
        variants.append(bytes(changed))
        for data in variants:
            with self.subTest(bytes=len(data)):
                with self.assertRaises(ValueError):
                    validate_emf(data)
