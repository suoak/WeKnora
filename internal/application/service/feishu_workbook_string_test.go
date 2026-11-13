package service

import (
	"archive/zip"
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"
)

func TestEmbeddedWorkbookDirectStringCells(t *testing.T) {
	file := excelize.NewFile()
	t.Cleanup(func() { require.NoError(t, file.Close()) })
	require.NoError(t, file.SetCellDefault("Sheet1", "A1", "complete direct string"))
	raw, err := file.WriteToBuffer()
	require.NoError(t, err)
	reader, err := zip.NewReader(bytes.NewReader(raw.Bytes()), int64(raw.Len()))
	require.NoError(t, err)
	var direct bytes.Buffer
	writer := zip.NewWriter(&direct)
	for _, entry := range reader.File {
		stream, err := entry.Open()
		require.NoError(t, err)
		body, err := io.ReadAll(stream)
		require.NoError(t, err)
		require.NoError(t, stream.Close())
		if entry.Name == "xl/worksheets/sheet1.xml" {
			body = bytes.ReplaceAll(body, []byte(`t="inlineStr"`), []byte(`t="str"`))
			body = bytes.ReplaceAll(body, []byte("<is><t>"), []byte("<v>"))
			body = bytes.ReplaceAll(body, []byte("</t></is>"), []byte("</v>"))
		}
		part, err := writer.Create(entry.Name)
		require.NoError(t, err)
		_, err = part.Write(body)
		require.NoError(t, err)
	}
	require.NoError(t, writer.Close())
	parsed, err := excelize.OpenReader(bytes.NewReader(direct.Bytes()))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, parsed.Close()) })
	parts, err := exactWorkbookParts(direct.Bytes())
	require.NoError(t, err)
	body := parts["xl/worksheets/sheet1.xml"]
	require.Contains(t, string(body), `t="str"`)
	grid, rows, cols, cells, err := exactWorkbookCellGrid(parsed, "Sheet1", body)
	require.NoError(t, err)
	require.Contains(t, grid, "complete direct string")
	require.Equal(t, []int{1, 1, 1}, []int{rows, cols, cells})
	unknown := bytes.ReplaceAll(body, []byte(`t="str"`), []byte(`t="unknown"`))
	_, _, _, _, err = exactWorkbookCellGrid(file, "Sheet1", unknown)
	require.Error(t, err)
}
