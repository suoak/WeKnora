package feishudocx

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func revisionBoundWorkbookParts(description, uid string) map[string]string {
	parts := embeddedWorkbookParts()
	parts["xl/drawings/drawing1.xml"] = `<xdr:wsDr xmlns:xdr="` + sheetDrawingNS + `">` +
		`<xdr:twoCellAnchor><xdr:pic><xdr:nvPicPr><xdr:cNvPr id="1" name="picture" descr="` +
		description + `"/></xdr:nvPicPr></xdr:pic></xdr:twoCellAnchor></xdr:wsDr>`
	parts["xl/comments1.xml"] = `<comments xmlns="` + spreadsheetNS + `" xmlns:xr="` + sheetRevisionNS +
		`"><authors><author>Author</author></authors><commentList><comment ref="A1" authorId="0" xr:uid="` +
		uid + `"><text><t>annotation</t></text></comment></commentList></comments>`
	return parts
}

func TestRevisionBoundXLSXOnlyCanonicalizesObservedExporterFields(t *testing.T) {
	firstUID := "{11111111-1111-1111-1111-111111111111}"
	secondUID := "{22222222-2222-2222-2222-222222222222}"
	parts := revisionBoundWorkbookParts("aBcDef", firstUID)
	first := archive(t, parts, false)
	second := archive(t, revisionBoundWorkbookParts("xYzAbC", secondUID), true)
	baseline, algorithm, err := CanonicalRevisionBoundXLSXSnapshotDigest(first)
	require.NoError(t, err)
	actual, _, err := CanonicalRevisionBoundXLSXSnapshotDigest(second)
	require.NoError(t, err)
	require.Equal(t, "feishu-xlsx-revision-bound-snapshot-v3", algorithm)
	require.Equal(t, baseline, actual)
	for _, digest := range []func([]byte) (string, string, error){CanonicalXLSXDigest, CanonicalXLSXSnapshotDigest} {
		left, _, err := digest(first)
		require.NoError(t, err)
		right, _, err := digest(second)
		require.NoError(t, err)
		require.NotEqual(t, left, right, "ordinary and stored contracts must remain strict")
	}
	for _, mutation := range []struct{ part, before, after string }{
		{"xl/media/image.png", "workbook-image", "changed image"},
		{"xl/worksheets/sheet1.xml", "1+1", "1+2"},
		{"xl/worksheets/sheet1.xml", "<v>2</v>", "<v>3</v>"},
		{"xl/drawings/drawing1.xml", `name="picture"`, `name="different"`},
		{"xl/drawings/drawing1.xml", `id="1"`, `id="2"`},
		{"xl/drawings/drawing1.xml", ` descr="aBcDef"`, ""},
		{"xl/drawings/drawing1.xml", `descr="aBcDef"`, `descr="author supplied alternative text"`},
		{"xl/comments1.xml", "annotation", "changed comment"},
		{"xl/comments1.xml", "Author", "Another author"},
		{"xl/comments1.xml", `ref="A1"`, `ref="B1"`},
		{"xl/comments1.xml", ` xr:uid="` + firstUID + `"`, ""},
	} {
		t.Run(mutation.part+mutation.after, func(t *testing.T) {
			changed := revisionBoundWorkbookParts("aBcDef", firstUID)
			changed[mutation.part] = strings.Replace(changed[mutation.part], mutation.before, mutation.after, 1)
			require.NotEqual(t, parts[mutation.part], changed[mutation.part])
			actual, _, err := CanonicalRevisionBoundXLSXSnapshotDigest(archive(t, changed, false))
			require.NoError(t, err)
			require.NotEqual(t, baseline, actual)
		})
	}
	_, _, err = CanonicalRevisionBoundXLSXSnapshotDigest(archive(t, revisionBoundWorkbookParts("aBcDef", "bad"), false))
	require.Error(t, err)
}
