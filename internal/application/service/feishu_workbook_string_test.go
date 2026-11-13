package service

import (
	"bytes"
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
	parts, err := exactWorkbookParts(raw.Bytes())
	require.NoError(t, err)
	body := parts["xl/worksheets/sheet1.xml"]
	require.Contains(t, string(body), `t="str"`)
	grid, rows, cols, cells, err := exactWorkbookCellGrid(file, "Sheet1", body)
	require.NoError(t, err)
	require.Contains(t, grid, "complete direct string")
	require.Equal(t, []int{1, 1, 1}, []int{rows, cols, cells})
	unknown := bytes.ReplaceAll(body, []byte(`t="str"`), []byte(`t="unknown"`))
	_, _, _, _, err = exactWorkbookCellGrid(file, "Sheet1", unknown)
	require.Error(t, err)
}
