package service

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/Tencent/WeKnora/internal/infrastructure/docparser/anydoc"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"
)

func TestExactWorkbookCellGridPreservesCoordinatesAndRows(t *testing.T) {
	file := excelize.NewFile()
	defer func() { require.NoError(t, file.Close()) }()
	for cell, value := range map[string]string{
		"A1": "Name", "C1": "Value", "A2": "first", "C2": "1|2\n3", "A4": "last",
	} {
		require.NoError(t, file.SetCellStr("Sheet1", cell, value))
	}
	buffer, err := file.WriteToBuffer()
	require.NoError(t, err)
	parts, err := exactWorkbookParts(buffer.Bytes())
	require.NoError(t, err)
	grid, rows, columns, cells, err := exactWorkbookCellGrid(file, "Sheet1", parts["xl/worksheets/sheet1.xml"])
	require.NoError(t, err)
	require.Equal(t, 4, rows)
	require.Equal(t, 3, columns)
	require.Equal(t, 5, cells)
	require.Contains(t, grid, "| Row | A | B | C |")
	require.Contains(t, grid, "| 1 | Name |  | Value |")
	require.Contains(t, grid, "| 2 | first |  | 1&#124;2<br>3 |")
	require.Contains(t, grid, "| 3 |  |  |  |")
	require.Contains(t, grid, "| 4 | last |  |  |")
}

func TestExactWorkbookRejectsUnprovedDependencies(t *testing.T) {
	file := excelize.NewFile()
	defer func() { require.NoError(t, file.Close()) }()
	require.NoError(t, file.SetCellFormula("Sheet1", "A1", "1+1"))
	buffer, err := file.WriteToBuffer()
	require.NoError(t, err)
	_, err = exactWorkbookParts(buffer.Bytes())
	require.ErrorIs(t, err, errExactWorkbookUnsupported)

	for _, name := range []string{"../outside", "xl/vbaProject.bin", "xl/externalLinks/externalLink1.xml"} {
		var raw bytes.Buffer
		z := zip.NewWriter(&raw)
		entry, createErr := z.Create(name)
		require.NoError(t, createErr)
		_, createErr = entry.Write([]byte("not permitted"))
		require.NoError(t, createErr)
		require.NoError(t, z.Close())
		_, err = exactWorkbookParts(raw.Bytes())
		require.ErrorIs(t, err, errExactWorkbookUnsupported)
	}
}

// Runs only against the six already-copied isolated fixtures. No credentials,
// provider calls, production writes or document contents enter the output.
func TestExactEmbeddedOriginalSixCompleteCoverage(t *testing.T) {
	root := os.Getenv("EXACT_EMBEDDED_FIXTURE_DIR")
	if root == "" || !anydoc.Available() {
		t.Skip("requires isolated original fixtures and the linked native parser")
	}
	t.Setenv("FEISHU_PERSONAL_EXACT_EMBEDDED_XLSX", "1")
	files, err := filepath.Glob(filepath.Join(root, "*.docx"))
	require.NoError(t, err)
	require.Len(t, files, 6)
	for _, name := range files {
		t.Run(filepath.Base(name), func(t *testing.T) {
			data, err := os.ReadFile(name)
			require.NoError(t, err)
			require.Error(t, checkExactDOCXDependencies(data), "the default guard must remain closed")
			workbooks, err := prepareExactEmbeddedWorkbooks(data)
			require.NoError(t, err)
			require.Len(t, workbooks, 1)
			workbook := workbooks[0]
			require.Len(t, workbook.Sections, 1)
			require.Len(t, workbook.Assets, 1, "worksheet background image must not disappear")
			section := workbook.Sections[0]
			require.Equal(t, 5, section.columns)
			require.GreaterOrEqual(t, section.rows, 2)
			require.Equal(t, section.rows*section.columns, section.cells)
			require.NotEmpty(t, workbook.SHA256)
			require.NotEmpty(t, workbook.NativeMarkdownSHA256)

			// Independently enumerate stored shared-string cell coordinates.
			// Rebuild each row from actual values and require it in the
			// authoritative grid, including duplicate and empty cells.
			parent, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
			require.NoError(t, err)
			var child []byte
			for _, entry := range parent.File {
				if entry.Name == "word/embeddings/Microsoft_Excel_Worksheet1.xlsx" {
					reader, openErr := entry.Open()
					require.NoError(t, openErr)
					child, err = io.ReadAll(reader)
					require.NoError(t, reader.Close())
					require.NoError(t, err)
				}
			}
			require.NotEmpty(t, child)
			parts, err := exactWorkbookParts(child)
			require.NoError(t, err)
			book, err := excelize.OpenReader(bytes.NewReader(child))
			require.NoError(t, err)
			grid, rows, columns, cells, err := exactWorkbookCellGrid(
				book, book.GetSheetList()[0], parts["xl/worksheets/sheet2.xml"])
			require.NoError(t, err)
			require.NoError(t, book.Close())
			require.Contains(t, section.markdown, grid)
			require.Equal(t, section.rows, rows)
			require.Equal(t, section.columns, columns)
			require.Equal(t, section.cells, cells)
			t.Logf("complete parent-bound workbook: rows=%d columns=%d cells=%d images=%d",
				rows, columns, cells, len(workbook.Assets))
		})
	}
}
