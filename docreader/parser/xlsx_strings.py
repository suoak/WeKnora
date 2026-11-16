"""Workbook-local acceleration of unformatted XLSX shared strings."""


def read_plain_string_table(source):
    """Keep openpyxl's rich-text validation, without modeling plain text runs."""
    from openpyxl.cell.text import Text
    from openpyxl.xml.constants import SHEET_MAIN_NS
    from openpyxl.xml.functions import iterparse

    string_tag = "{%s}si" % SHEET_MAIN_NS
    text_tag = "{%s}t" % SHEET_MAIN_NS
    strings = []
    for _, node in iterparse(source):
        if node.tag != string_tag:
            continue
        if not node.attrib and len(node) == 1 and node[0].tag == text_tag and not len(node[0]):
            text = node[0].text or ""
        elif not node.attrib and not len(node):
            text = ""
        else:
            # Formatted/phonetic runs and unusual layouts retain the original
            # library's validation and concatenation rules.
            text = Text.from_tree(node).content
        strings.append(text.replace("x005F_", ""))
        node.clear()
    return strings


def load_readonly_workbook(source):
    """Return a supported workbook, or None so pandas uses its usual loader."""
    import openpyxl
    import pandas as pd
    from openpyxl.reader.excel import ExcelReader
    from openpyxl.xml.constants import SHARED_STRINGS

    if (openpyxl.__version__, pd.__version__) != ("3.1.5", "2.3.3"):
        return None

    class PlainStringsReader(ExcelReader):
        def read_strings(self):
            content_type = self.package.find(SHARED_STRINGS)
            if content_type is not None:
                with self.archive.open(content_type.PartName[1:]) as strings:
                    self.shared_strings = read_plain_string_table(strings)

    reader = PlainStringsReader(source, read_only=True, data_only=True, keep_links=False)
    try:
        reader.read()
        return reader.wb
    except BaseException:
        reader.archive.close()
        raise
