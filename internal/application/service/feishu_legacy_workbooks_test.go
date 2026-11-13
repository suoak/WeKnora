package service

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/infrastructure/docparser/anydoc"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"
)

func TestLegacyWorkbooksPreserveImagesAndRejectUnknown(t *testing.T) {
	image := types.ImageRef{OriginalRef: "images/a.png", ImageData: []byte("unchanged image")}
	original := types.ReadResult{MarkdownContent: "![a](images/a.png)", ImageRefs: []types.ImageRef{image}}
	require.NoError(t, expandLegacyWorkbooks(&original, "docx"))
	require.Equal(t, []types.ImageRef{image}, original.ImageRefs)
	broken := types.ReadResult{MarkdownContent: "before ![book](images/object.bin) after", ImageRefs: []types.ImageRef{{
		OriginalRef: "images/object.bin", Filename: "object.bin", MimeType: "application/vnd.ms-ole-object",
		ImageData: []byte("unknown OLE bytes"),
	}}}
	before := broken.MarkdownContent
	require.Error(t, expandLegacyWorkbooks(&broken, "docx"))
	require.Equal(t, before, broken.MarkdownContent)
	require.Len(t, broken.ImageRefs, 1, "a refusal must not silently drop a package")
}

func TestLegacyWorkbooksCompleteCellCoverage(t *testing.T) {
	if !anydoc.Available() {
		t.Skip("requires native XLSX parser")
	}
	workbook := excelize.NewFile()
	defer func() { require.NoError(t, workbook.Close()) }()
	require.NoError(t, workbook.SetCellStr("Sheet1", "A1", "first exact cell"))
	require.NoError(t, workbook.SetCellStr("Sheet1", "B2", "last exact cell"))
	raw, err := workbook.WriteToBuffer()
	require.NoError(t, err)
	makeResult := func(markdown string) *types.ReadResult {
		return &types.ReadResult{MarkdownContent: markdown, ImageRefs: []types.ImageRef{{
			OriginalRef: "images/object.bin", Filename: "object.bin", MimeType: "application/vnd.ms-ole-object",
			ImageData: raw.Bytes(),
		}}}
	}
	parsed := makeResult("before ![book](images/object.bin) after")
	require.NoError(t, expandLegacyWorkbooks(parsed, "docx"))
	require.Contains(t, parsed.MarkdownContent, "before")
	require.Contains(t, parsed.MarkdownContent, "after")
	require.Contains(t, parsed.MarkdownContent, "first exact cell")
	require.Contains(t, parsed.MarkdownContent, "last exact cell")
	require.Empty(t, parsed.ImageRefs)
	for _, markdown := range []string{"missing package placement", "![a](images/object.bin) ![b](images/object.bin)"} {
		bad := makeResult(markdown)
		require.Error(t, expandLegacyWorkbooks(bad, "docx"))
		require.Equal(t, markdown, bad.MarkdownContent)
	}
	segmented := makeResult("![a](images/object.bin)")
	segmented.ParsedChunks = []types.ParserChunkSpan{{Start: 0, End: 1}}
	require.Error(t, expandLegacyWorkbooks(segmented, "docx"), "unmapped semantic offsets remain refused")
}
