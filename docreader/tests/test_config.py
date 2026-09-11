import os
import unittest
from unittest.mock import patch

from docreader import config


class DocReaderConfigTest(unittest.TestCase):
    def test_grpc_headroom_formula(self):
        cases = ((100, 132), (200, 250), (20, 52))
        for upload_mb, expected_mb in cases:
            with self.subTest(upload_mb=upload_mb):
                self.assertEqual(
                    config.default_grpc_message_size_mb(upload_mb), expected_mb
                )

    def test_grpc_limit_adds_headroom_to_upload_limit(self):
        with patch.dict(os.environ, {"MAX_FILE_SIZE_MB": "100"}, clear=True):
            cfg = config.load_config()

        self.assertEqual(cfg.grpc_max_file_size_mb, 132 * 1024 * 1024)

    def test_explicit_grpc_limit_overrides_derived_headroom(self):
        env = {
            "MAX_FILE_SIZE_MB": "100",
            "DOCREADER_GRPC_MAX_FILE_SIZE_MB": "180",
        }
        with patch.dict(os.environ, env, clear=True):
            cfg = config.load_config()

        self.assertEqual(cfg.grpc_max_file_size_mb, 180 * 1024 * 1024)

    def test_parser_concurrency_defaults_are_conservative(self):
        with patch.dict(os.environ, {}, clear=True):
            cfg = config.load_config()

        self.assertEqual(cfg.markitdown_max_workers, 1)
        self.assertEqual(cfg.pdf_render_max_workers, 1)
        self.assertEqual(cfg.pdf_render_dpi, 200)
        self.assertEqual(cfg.pdf_jpeg_quality, 85)

    def test_loads_parser_concurrency_env(self):
        env = {
            "DOCREADER_MARKITDOWN_MAX_WORKERS": "3",
            "DOCREADER_PDF_RENDER_MAX_WORKERS": "2",
            "DOCREADER_PDF_RENDER_DPI": "180",
            "DOCREADER_PDF_JPEG_QUALITY": "85",
        }
        with patch.dict(os.environ, env):
            cfg = config.load_config()

        self.assertEqual(cfg.markitdown_max_workers, 3)
        self.assertEqual(cfg.pdf_render_max_workers, 2)
        self.assertEqual(cfg.pdf_render_dpi, 180)
        self.assertEqual(cfg.pdf_jpeg_quality, 85)

    def test_dump_config_includes_parser_limits(self):
        dumped = config.dump_config()

        self.assertIn("DOCREADER_MARKITDOWN_MAX_WORKERS", dumped)
        self.assertIn("DOCREADER_PDF_RENDER_MAX_WORKERS", dumped)
        self.assertIn("DOCREADER_PDF_RENDER_DPI", dumped)
        self.assertIn("DOCREADER_PDF_JPEG_QUALITY", dumped)


if __name__ == "__main__":
    unittest.main()
