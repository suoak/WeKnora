"""Real OLE directory fixtures: protected OOXML must never enter conversion."""

import struct
import unittest
from unittest.mock import patch

from docreader.parser.excel_convert import detect_excel_format
from docreader.parser.excel_parser import _open_excel_file
from docreader.parser.markitdown_parser import StdMarkitdownParser


def ole_container(stream_name: str, nested: bool = False) -> bytes:
    """A valid CFB v3 directory with one regular 4096-byte stream."""
    free, end, fat = 0xFFFFFFFF, 0xFFFFFFFE, 0xFFFFFFFD
    header = bytearray(512)
    header[:8] = bytes.fromhex("d0cf11e0a1b11ae1")
    struct.pack_into("<HHHHH", header, 24, 0x3E, 3, 0xFFFE, 9, 6)
    struct.pack_into("<IIIIIIIII", header, 40, 0, 1, 1, 0, 4096, end, 0, end, 0)
    struct.pack_into("<109I", header, 76, 0, *([free] * 108))
    allocation = [fat, end, *range(3, 10), end]
    if nested:
        allocation += [*range(11, 18), end]
    allocation += [free] * (128 - len(allocation))

    def entry(name: str, kind: int, child: int, first: int, size: int, left: int = free) -> bytes:
        data = bytearray(128)
        encoded = (name + "\0").encode("utf-16le")
        data[: len(encoded)] = encoded
        struct.pack_into("<HBBIII", data, 64, len(encoded), kind, 1, left, free, child)
        struct.pack_into("<IQ", data, 116, first, size)
        return bytes(data)

    directory = entry("Root Entry", 5, 1, end, 0)
    directory += entry(stream_name, 2, free, 2, 4096, 2 if nested else free)
    if nested:
        directory += entry("ObjectPool", 1, 3, end, 0)
        directory += entry("EncryptedPackage", 2, free, 10, 4096)
    else:
        directory += bytes(256)
    return bytes(header) + struct.pack("<128I", *allocation) + directory + bytes(8192 if nested else 4096)


class ProtectedExcelTest(unittest.TestCase):
    def test_encrypted_ooxml_is_not_legacy_xls(self):
        with self.assertRaisesRegex(ValueError, "文件已加密或受保护"):
            detect_excel_format(ole_container("EncryptedPackage"))

    def test_protected_file_never_reaches_reader_or_converter(self):
        with patch("docreader.parser.excel_parser.pd.ExcelFile") as reader, patch(
            "docreader.parser.excel_parser.convert_excel_to_xlsx_bytes"
        ) as converter:
            for extension in ("xlsx", "xls"):
                with self.subTest(extension=extension), self.assertRaisesRegex(
                    ValueError, "获准导出的未加密版本"
                ):
                    _open_excel_file(ole_container("EncryptedPackage"), extension)
            reader.assert_not_called()
            converter.assert_not_called()

    def test_ordinary_legacy_workbook_stays_supported(self):
        for stream in ("Workbook", "Book"):
            with self.subTest(stream=stream):
                self.assertEqual(detect_excel_format(ole_container(stream)), "xls")

    def test_embedded_protected_object_is_not_whole_workbook_encryption(self):
        self.assertEqual(detect_excel_format(ole_container("Workbook", nested=True)), "xls")

    def test_markitdown_cannot_bypass_protected_container(self):
        with patch("docreader.parser.markitdown_parser.MarkItDown") as converter:
            for extension in ("xlsx", "xls", "xlsm"):
                with self.subTest(extension=extension), self.assertRaisesRegex(
                    ValueError, "文件已加密或受保护"
                ):
                    StdMarkitdownParser(file_type=extension).parse_into_text(
                        ole_container("EncryptedPackage")
                    )
            converter.return_value.convert_stream.assert_not_called()

    def test_word_container_is_not_an_excel_workbook(self):
        with self.assertRaisesRegex(ValueError, "不是可识别的 Excel"):
            detect_excel_format(ole_container("WordDocument"))


if __name__ == "__main__":
    unittest.main()
