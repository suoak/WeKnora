package service

import (
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/infrastructure/docparser/anydoc"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"
)

func TestExactWorkbookCommentsPreserveAuthorTextAndPlacement(t *testing.T) {
	f := excelize.NewFile()
	defer func() { require.NoError(t, f.Close()) }()
	require.NoError(t, f.SetCellStr("Sheet1", "A1", "original value"))
	require.NoError(t, f.AddComment("Sheet1", excelize.Comment{
		Cell: "A1", Author: "Original author", Text: "original note\nsecond line",
	}))
	buffer, err := f.WriteToBuffer()
	require.NoError(t, err)
	parts, err := exactWorkbookParts(buffer.Bytes())
	require.NoError(t, err)
	notes, err := exactWorkbookCommentNotes(parts)
	require.NoError(t, err)
	for _, text := range []string{"Comment cell A1:", "Original author", "original note&#10;second line"} {
		require.Contains(t, notes["xl/worksheets/sheet1.xml"], text)
	}
	if anydoc.Available() {
		book, err := parseExactEmbeddedWorkbook("word/embeddings/comments.xlsx", buffer.Bytes(), 1)
		require.NoError(t, err)
		require.Contains(t, book.Sections[0].markdown, "original value")
		require.Contains(t, book.Sections[0].markdown, notes["xl/worksheets/sheet1.xml"])
	}
	for _, change := range []string{"missing_comments", "control", "wrong_cell", "orphan", "remote_image"} {
		t.Run(change, func(t *testing.T) {
			copyParts := map[string][]byte{}
			vml, comments := "", ""
			for name, body := range parts {
				copyParts[name] = append([]byte(nil), body...)
				if strings.HasSuffix(name, ".vml") {
					vml = name
				}
				if strings.HasPrefix(name, "xl/comments") {
					comments = name
				}
			}
			require.NotEmpty(t, vml)
			require.NotEmpty(t, comments)
			switch change {
			case "missing_comments":
				delete(copyParts, comments)
			case "control":
				copyParts[vml] = []byte(strings.ReplaceAll(string(copyParts[vml]),
					`ObjectType="Note"`, `ObjectType="Button"`))
			case "wrong_cell":
				copyParts[vml] = []byte(strings.ReplaceAll(string(copyParts[vml]), ">0</x:Row>", ">1</x:Row>"))
			case "orphan":
				copyParts["xl/drawings/orphan.vml"] = copyParts[vml]
			case "remote_image":
				copyParts[vml] = []byte(strings.ReplaceAll(string(copyParts[vml]), "<v:shape ",
					`<v:shape src="https://example.invalid/never-fetch" `))
			}
			_, err := exactWorkbookCommentNotes(copyParts)
			require.Error(t, err)
		})
	}
}
