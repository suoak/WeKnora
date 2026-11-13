import struct
import unittest

from docreader.parser.emf_raster import validate_emf


def emf_fixture():
    header = bytearray(88)
    struct.pack_into("<IIiiii", header, 0, 1, 88, 0, 0, 80, 80)
    header[40:44] = b" EMF"
    struct.pack_into("<II", header, 48, 108, 2)
    return bytes(header) + struct.pack("<IIIII", 14, 20, 0, 0, 20)


class EMFValidationTest(unittest.TestCase):
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
