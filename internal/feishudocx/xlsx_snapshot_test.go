package feishudocx

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestXLSXSnapshotContentTypeOrder(t *testing.T) {
	const ns = "http://schemas.openxmlformats.org/package/2006/content-types"
	first := `<Default Extension="png" ContentType="image/png"/>`
	second := `<Override PartName="/xl/workbook.xml" ContentType="application/xml"/>`
	parts := embeddedWorkbookParts()
	pack := func(declarations string, mutation bool) []byte {
		copyParts := map[string]string{}
		for k, v := range parts {
			copyParts[k] = v
		}
		copyParts["[Content_Types].xml"] = `<Types xmlns="` + ns + `">` + declarations + `</Types>`
		if mutation {
			copyParts["xl/worksheets/sheet1.xml"] = strings.ReplaceAll(
				copyParts["xl/worksheets/sheet1.xml"], "<v>2</v>", "<v>3</v>",
			)
		}
		return archive(t, copyParts, false)
	}
	a, b := pack(first+second, false), pack(second+first, false)
	oldA, _, err := CanonicalXLSXDigest(a)
	require.NoError(t, err)
	oldB, _, err := CanonicalXLSXDigest(b)
	require.NoError(t, err)
	require.NotEqual(t, oldA, oldB, "stored v1 identity remains unchanged")
	one, algorithm, err := CanonicalXLSXSnapshotDigest(a)
	require.NoError(t, err)
	two, _, err := CanonicalXLSXSnapshotDigest(b)
	require.NoError(t, err)
	require.Equal(t, "feishu-xlsx-snapshot-v2", algorithm)
	require.Equal(t, one, two)
	changed, _, err := CanonicalXLSXSnapshotDigest(pack(second+first, true))
	require.NoError(t, err)
	require.NotEqual(t, one, changed)
	changed, _, err = CanonicalXLSXSnapshotDigest(
		pack(strings.ReplaceAll(first, "image/png", "image/jpeg")+second, false),
	)
	require.NoError(t, err)
	require.NotEqual(t, one, changed, "content type mapping still binds its value")
	for _, declarations := range []string{
		first + first, first + `<Default Extension="PNG" ContentType="image/png"/>`,
		`<Unexpected/>`, `<Default Extension="png"/>`,
		`<Default Extension="png" ContentType="image/png"><Unexpected/></Default>`,
	} {
		_, _, err := CanonicalXLSXSnapshotDigest(pack(declarations, false))
		require.Error(t, err)
	}
}
