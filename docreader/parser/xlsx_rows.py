"""Read large XLSX rows without allocating a wrapper for every scalar cell."""

from inspect import signature


def readonly_sheet_data(reader, sheet, file_rows_needed, fallback):
    """Keep pandas' scalar and sparse-row rules on the supported reader layout."""
    import numpy as np
    import openpyxl
    import pandas as pd
    from openpyxl.worksheet._read_only import ReadOnlyWorksheet
    from openpyxl.worksheet._reader import WorkSheetParser
    from pandas.io.excel._openpyxl import OpenpyxlReader

    # These are private library interfaces. Unknown readers keep the original
    # implementation; this adapter never changes a process-global class.
    if (
        (openpyxl.__version__, pd.__version__) != ("3.1.5", "2.3.3")
        or type(reader) is not OpenpyxlReader
        or type(sheet) is not ReadOnlyWorksheet
        or not reader.book.read_only
        or list(signature(WorkSheetParser).parameters) != [
            "src", "shared_strings", "data_only", "epoch", "date_formats",
            "timedelta_formats", "rich_text",
        ]
    ):
        return fallback(reader, sheet, file_rows_needed)

    sheet.reset_dimensions()

    def converted_rows():
        counter = 1
        with sheet._get_source() as source:
            parser = WorkSheetParser(
                source, sheet._shared_strings,
                data_only=sheet.parent.data_only, epoch=sheet.parent.epoch,
                date_formats=sheet.parent._date_formats,
                timedelta_formats=sheet.parent._timedelta_formats,
            )
            for index, cells in parser.parse():
                while counter < index:
                    counter += 1
                    yield []
                if counter > index:
                    continue
                counter += 1
                # Match ReadOnlyWorksheet._get_row, including its physical
                # last-column bound and last-writer behavior for duplicate cells.
                width = cells[-1]["column"] if cells else 0
                row = [""] * width
                for cell in cells:
                    column = cell["column"]
                    if not 1 <= column <= width:
                        continue
                    value = cell["value"]
                    if value is None:
                        value = ""
                    elif cell["data_type"] == "e":
                        value = np.nan
                    elif cell["data_type"] == "n":
                        integer = int(value)
                        value = integer if integer == value else float(value)
                    row[column - 1] = value
                yield row

    data = []
    last_nonempty = -1
    rows = converted_rows()
    try:
        for row in rows:
            while row and row[-1] == "":
                row.pop()
            if row:
                last_nonempty = len(data)
            data.append(row)
            if file_rows_needed is not None and len(data) >= file_rows_needed:
                break
    finally:
        # An early row limit must release the ZIP member immediately.
        rows.close()
    del data[last_nonempty + 1:]
    width = max((len(row) for row in data), default=0)
    for row in data:
        row.extend([""] * (width - len(row)))
    return data
