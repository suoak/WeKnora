import unittest
from unittest.mock import Mock

from docreader.main import DocReaderServicer
from docreader.models.document import Document
from docreader.proto.docreader_pb2 import ReadRequest


class EmptyParserResultTest(unittest.TestCase):
    def _servicer(self, result=None, error=None):
        service = object.__new__(DocReaderServicer)
        service._parse_request = Mock(return_value=(result, "private/source.xlsx"))
        if error is not None:
            service._parse_request.side_effect = error
        return service

    def test_unary_and_stream_report_actual_empty_document(self):
        for streaming in [False, True]:
            with self.subTest(streaming=streaming):
                service = self._servicer(Document(content=""))
                request = ReadRequest(file_name="source.xlsx")
                if streaming:
                    frames = list(service.ReadStream(request, None))
                    self.assertEqual(len(frames), 1)
                    result = frames[0].meta
                else:
                    result = service.Read(request, None)
                self.assertTrue(result.error)
                self.assertFalse(result.markdown_content)
                self.assertEqual(result.metadata["parser_failure_protocol"], "v1")
                self.assertEqual(
                    result.metadata["parser_failure_code"], "empty_parsed_content"
                )
                self.assertNotIn("private/", str(dict(result.metadata)))

    def test_missing_result_and_backend_exception_remain_unknown(self):
        for streaming in [False, True]:
            for error in [None, RuntimeError("backend unavailable")]:
                with self.subTest(streaming=streaming, error=error):
                    service = self._servicer(error=error)
                    request = ReadRequest(file_name="source.xlsx")
                    if streaming:
                        result = list(service.ReadStream(request, None))[0].meta
                    else:
                        result = service.Read(request, None)
                    self.assertTrue(result.error)
                    self.assertFalse(result.metadata)


if __name__ == "__main__":
    unittest.main()
