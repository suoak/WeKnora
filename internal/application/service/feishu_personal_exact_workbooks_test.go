package service

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/infrastructure/docparser/anydoc"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"
)

func TestExactWorkbookMergedCellsPreserveLayout(t *testing.T) {
	file := excelize.NewFile()
	defer func() { require.NoError(t, file.Close()) }()
	require.NoError(t, file.SetCellStr("Sheet1", "A1", "merged value"))
	require.NoError(t, file.SetCellStr("Sheet1", "A2", "next row"))
	require.NoError(t, file.MergeCell("Sheet1", "A1", "C1"))
	buffer, err := file.WriteToBuffer()
	require.NoError(t, err)
	parts, err := exactWorkbookParts(buffer.Bytes())
	require.NoError(t, err)
	grid, rows, cols, _, err := exactWorkbookCellGrid(file, "Sheet1", parts["xl/worksheets/sheet1.xml"])
	require.NoError(t, err)
	require.Equal(t, 2, rows)
	require.Equal(t, 3, cols)
	require.Equal(t, 1, strings.Count(grid, "merged value"))
	require.Contains(t, grid, "| 1 | merged value |  |  |")
	require.Contains(t, grid, "Merged cells: A1:C1 (anchor A1).")
}

func TestExactWorkbookMergesRejectConflictingOrUnboundedLayouts(t *testing.T) {
	for _, input := range []string{
		`<mergeCell ref="A1:XFD1"/>`, `<mergeCell ref="A1:A1048576"/>`,
		`<mergeCell ref="B2:A1"/>`, `<mergeCell ref="A1:B1"/><mergeCell ref="B1:C1"/>`,
	} {
		body := `<worksheet><mergeCells>` + input + `</mergeCells></worksheet>`
		_, _, _, _, err := exactWorkbookMerges([]byte(body))
		require.ErrorIs(t, err, errExactWorkbookUnsupported)
	}
	for _, cell := range []string{
		`<c r="B1"><v>0</v></c>`, `<c r="B1"><is><t>hidden</t></is></c>`, `<c r="b1"><v>1</v></c>`,
	} {
		body := `<worksheet><sheetData><row>` + cell + `</row></sheetData>` +
			`<mergeCells><mergeCell ref="A1:B1"/></mergeCells></worksheet>`
		_, _, _, _, err := exactWorkbookMerges([]byte(body))
		require.ErrorIs(t, err, errExactWorkbookUnsupported)
	}
}

// Operator-only isolated fixtures. Output contains counts, never document text.
func TestExactEmbeddedStalledDOCXCompleteCoverage(t *testing.T) {
	root := os.Getenv("EXACT_STALLED_FIXTURE_DIR")
	if root == "" || !anydoc.Available() {
		t.Skip("requires the isolated stalled fixtures and native parser")
	}
	t.Setenv("FEISHU_PERSONAL_EXACT_EMBEDDED_XLSX", "1")
	for name, count := range map[string]int{
		"d97a0ca9-f7c1-4fb3-b7cf-fb30a9724a47.docx": 5,
		"231db03a-eb23-456c-989c-362eb8c1ed48.docx": 1,
	} {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(root, name))
			require.NoError(t, err)
			workbooks, err := prepareExactEmbeddedWorkbooks(data)
			require.NoError(t, err)
			require.Len(t, workbooks, count)
			parent, err := anydoc.Convert(data, anydoc.Options{Format: "docx", WithAssets: true})
			require.NoError(t, err)
			require.NoError(t, parent.AssetsError)
			require.NoError(t, replaceExactWorkbookPlaceholders(parent, workbooks))
			for _, book := range workbooks {
				require.Len(t, book.Sections, 1)
				require.Len(t, book.Assets, 1)
				require.NotEmpty(t, book.NativeMarkdownSHA256)
				require.NotEmpty(t, book.Sections[0].markdown)
			}
			t.Logf("complete parent conversion: workbooks=%d retained_parent_assets=%d", count, len(parent.Assets))
		})
	}
}

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
			parent, err := anydoc.Convert(data, anydoc.Options{Format: "docx", WithAssets: true})
			require.NoError(t, err)
			require.NoError(t, parent.AssetsError)
			for _, asset := range parent.Assets {
				t.Logf("parent image: declared_type=%s detected_type=%s bytes=%d",
					asset.MediaType, http.DetectContentType(asset.Data), len(asset.Data))
			}
			workbooks, err := prepareExactEmbeddedWorkbooks(data)
			require.NoError(t, err)
			require.Len(t, workbooks, 1)
			workbook := workbooks[0]
			require.NoError(t, replaceExactWorkbookPlaceholders(parent, workbooks))
			require.NotContains(t, parent.Markdown, "![")
			require.Empty(t, parent.Assets)
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
			archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
			require.NoError(t, err)
			var child []byte
			for _, entry := range archive.File {
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
