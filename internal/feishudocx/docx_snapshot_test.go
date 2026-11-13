package feishudocx

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDOCXSnapshotEmbeddedDeclarationOrder(t *testing.T) {
	a := `<Default Extension="xml" ContentType="application/xml"/>`
	b := `<Default Extension="png" ContentType="image/png"/>`
	pack := func(declarations string, cellChange bool) []byte {
		parts := embeddedWorkbookParts()
		parts["[Content_Types].xml"] = `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
			declarations + `</Types>`
		if cellChange {
			parts["xl/worksheets/sheet1.xml"] = strings.ReplaceAll(parts["xl/worksheets/sheet1.xml"], "<v>2</v>", "<v>3</v>")
		}
		return docxWithEmbeddedWorkbook(t, archive(t, parts, false))
	}
	first, second := pack(a+b, false), pack(b+a, false)
	stored, _, err := CanonicalDOCXDigest(first)
	require.NoError(t, err)
	other, _, err := CanonicalDOCXDigest(second)
	require.NoError(t, err)
	require.NotEqual(t, stored, other, "stored v2 contract remains unchanged")
	one, algorithm, err := CanonicalDOCXSnapshotDigest(first)
	require.NoError(t, err)
	two, _, err := CanonicalDOCXSnapshotDigest(second)
	require.NoError(t, err)
	require.Equal(t, "feishu-docx-snapshot-v1", algorithm)
	require.Equal(t, one, two)
	changed, _, err := CanonicalDOCXSnapshotDigest(pack(b+a, true))
	require.NoError(t, err)
	require.NotEqual(t, one, changed)
	_, _, err = CanonicalDOCXSnapshotDigest(pack(a+a, false))
	require.Error(t, err, "duplicate declarations are ambiguous")
}
