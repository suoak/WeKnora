package feishudocx

import (
	"archive/zip"
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func xlsxIdentityFixture(t *testing.T, columns, cell, asset, relationship string, reverse bool) []byte {
	t.Helper()
	parts := [][2]string{
		{"[Content_Types].xml", `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"/>`},
		{"_rels/.rels", `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"/>`},
		{"xl/workbook.xml", `<workbook xmlns="` + spreadsheetNS + `"><sheets/></workbook>`},
		{"xl/worksheets/sheet1.xml", `<worksheet xmlns="` + spreadsheetNS + `">` +
			`<cols>` + columns + `</cols><sheetData><row r="1"><c r="A1">` + cell +
			`</c></row></sheetData></worksheet>`},
		{"xl/media/image1.png", asset},
		{"xl/worksheets/_rels/sheet1.xml.rels", `<Relationships ` +
			`xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			relationship + `</Relationships>`},
	}
	var out bytes.Buffer
	z := zip.NewWriter(&out)
	for i := range parts {
		index := i
		if reverse {
			index = len(parts) - i - 1
		}
		w, err := z.Create(parts[index][0])
		require.NoError(t, err)
		_, err = w.Write([]byte(parts[index][1]))
		require.NoError(t, err)
	}
	require.NoError(t, z.Close())
	return out.Bytes()
}

func TestFeishuXLSXIdentityProtectsContents(t *testing.T) {
	base := `<col min="1" max="2" width="10" customWidth="1"/>`
	split := `<col min="1" max="1" width="11.2" customWidth="1"/>` +
		`<col min="2" max="2" width="9.7" customWidth="1"/>`
	digest, algorithm, err := CanonicalXLSXDigest(xlsxIdentityFixture(t, base, `<v>1</v>`, "asset", "", false))
	require.NoError(t, err)
	require.Equal(t, "feishu-xlsx-package-v1", algorithm)
	equal, _, err := CanonicalXLSXDigest(xlsxIdentityFixture(t, split, `<v>1</v>`, "asset", "", true))
	require.NoError(t, err)
	require.Equal(t, digest, equal, "only ZIP packing and exporter column widths may differ")
	for _, change := range []struct{ name, cols, cell, asset, rel string }{
		{"cell", base, `<v>2</v>`, "asset", ""},
		{"formula", base, `<f>SUM(B1:B2)</f><v>1</v>`, "asset", ""},
		{"asset", base, `<v>1</v>`, "changed image", ""},
		{"relationship", base, `<v>1</v>`, "asset", `<Relationship Id="r1" Target="new.png"/>`},
		{"hidden", `<col min="1" max="2" width="10" customWidth="1" hidden="1"/>`, `<v>1</v>`, "asset", ""},
		{"style", `<col min="1" max="2" width="10" customWidth="1" style="2"/>`, `<v>1</v>`, "asset", ""},
		{"coverage", `<col min="1" max="3" width="10" customWidth="1"/>`, `<v>1</v>`, "asset", ""},
	} {
		t.Run(change.name, func(t *testing.T) {
			next, _, e := CanonicalXLSXDigest(xlsxIdentityFixture(t,
				change.cols, change.cell, change.asset, change.rel, false))
			require.NoError(t, e)
			require.NotEqual(t, digest, next)
		})
	}
}

func TestFeishuXLSXIdentityRejectsAmbiguousColumns(t *testing.T) {
	for _, columns := range []string{
		`<col min="1" max="2" width="NaN"/>`,
		`<col min="1" max="16385" width="1"/>`,
		`<col min="2" max="1" width="1"/>`,
		`<col min="1" max="2"/><col min="2" max="3"/>`,
		`<col min="1" max="2"><v>unsupported</v></col>`,
		`<unknown/>`,
	} {
		_, _, err := CanonicalXLSXDigest(xlsxIdentityFixture(t, columns, `<v>1</v>`, "asset", "", false))
		require.ErrorIs(t, err, ErrInvalidXLSXPackage)
	}
}
