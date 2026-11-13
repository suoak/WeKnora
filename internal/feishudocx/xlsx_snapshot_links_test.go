package feishudocx

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestXLSXSnapshotHyperlinkBindings(t *testing.T) {
	rel := func(id, target string) string {
		return `<Relationship Id="` + id + `" Type="` + hyperlinkRelNS +
			`/hyperlink" Target="` + target + `" TargetMode="External"/>`
	}
	link := func(id, ref string) string {
		return `<hyperlink ref="` + ref + `" r:id="` + id + `" display="label" tooltip="tip"/>`
	}
	pack := func(rels, links, extra string) []byte {
		parts := embeddedWorkbookParts()
		const relationshipsNS = "http://schemas.openxmlformats.org/package/2006/relationships"
		parts["xl/worksheets/_rels/sheet1.xml.rels"] = `<Relationships xmlns="` + relationshipsNS + `">` + rels + `</Relationships>`
		parts["xl/worksheets/sheet1.xml"] = `<worksheet xmlns="` + spreadsheetNS + `" xmlns:r="` +
			hyperlinkRelNS + `"><sheetData/><hyperlinks>` + links + `</hyperlinks>` + extra + `</worksheet>`
		return archive(t, parts, false)
	}
	a := pack(rel("rId1", "https://example.com/a")+rel("rId2", "https://example.com/b"),
		link("rId1", "A1")+link("rId2", "B2"), "")
	b := pack(rel("rId7", "https://example.com/b")+rel("rId9", "https://example.com/a"),
		link("rId7", "B2")+link("rId9", "A1"), "")
	one, _, err := CanonicalXLSXSnapshotDigest(a)
	require.NoError(t, err)
	two, _, err := CanonicalXLSXSnapshotDigest(b)
	require.NoError(t, err)
	require.Equal(t, one, two)
	oldA, algorithm, err := CanonicalXLSXDigest(a)
	require.NoError(t, err)
	oldB, _, err := CanonicalXLSXDigest(b)
	require.NoError(t, err)
	require.Equal(t, "feishu-xlsx-package-v1", algorithm)
	require.NotEqual(t, oldA, oldB)
	for _, mutation := range []struct{ rels, links string }{
		{rel("rId1", "https://example.com/changed"), link("rId1", "A1")},
		{rel("rId1", "https://example.com/a"), link("rId1", "C3")},
		{rel("rId1", "https://example.com/a"), strings.ReplaceAll(link("rId1", "A1"), "label", "changed")},
		{rel("rId1", "https://example.com/a"), strings.ReplaceAll(link("rId1", "A1"), "tip", "changed")},
	} {
		baseline, _, err := CanonicalXLSXSnapshotDigest(pack(
			rel("rId1", "https://example.com/a"), link("rId1", "A1"), ""))
		require.NoError(t, err)
		changed, _, err := CanonicalXLSXSnapshotDigest(pack(mutation.rels, mutation.links, ""))
		require.NoError(t, err)
		require.NotEqual(t, baseline, changed)
	}
	for _, malformed := range []struct{ rels, links, extra string }{
		{rel("rId1", "https://example.com/a") + rel("rId1", "https://example.com/b"), link("rId1", "A1"), ""},
		{rel("rId1", "https://example.com/a"), link("rId1", "A1") + link("rId1", "a1"), ""},
		{rel("rId1", "https://example.com/a"), link("missing", "A1"), ""},
		{rel("rId1", "https://example.com/a"), link("rId1", "A1"), `<drawing r:id="rId1"/>`},
	} {
		_, _, err := CanonicalXLSXSnapshotDigest(pack(malformed.rels, malformed.links, malformed.extra))
		require.Error(t, err)
	}
}
