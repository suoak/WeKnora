package feishudocx

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEmbeddedColumnPermutationPreservesCompleteDefinitions(t *testing.T) {
	first := `<col min="1" max="2" width="10" hidden="1"/>`
	second := `<col min="4" max="4" width="22" customWidth="1"/>`
	for _, styled := range []bool{false, true} {
		name := "without_styles"
		if styled {
			name = "with_styles"
		}
		t.Run(name, func(t *testing.T) {
			pack := func(columns string, changeCell bool) []byte {
				parts := embeddedWorkbookParts()
				parts["xl/worksheets/sheet1.xml"] = `<worksheet xmlns="` + spreadsheetNS + `"><cols>` +
					columns + `</cols><sheetData><row r="1"><c r="A1"><f>1+1</f><v>2</v></c>` +
					`</row></sheetData></worksheet>`
				if styled {
					parts["xl/styles.xml"] = `<styleSheet xmlns="` + spreadsheetNS + `">` +
						`<borders count="1"><border/></borders><cellXfs count="1">` +
						`<xf borderId="0"/></cellXfs></styleSheet>`
				}
				if changeCell {
					parts["xl/worksheets/sheet1.xml"] = strings.ReplaceAll(
						parts["xl/worksheets/sheet1.xml"], "<v>2</v>", "<v>3</v>",
					)
				}
				return docxWithEmbeddedWorkbook(t, archive(t, parts, false))
			}
			a, b := pack(first+second, false), pack(second+first, false)
			stored, storedAlgorithm, err := CanonicalDOCXDigest(a)
			require.NoError(t, err)
			otherStored, otherAlgorithm, err := CanonicalDOCXDigest(b)
			require.NoError(t, err)
			require.Equal(t, "feishu-docx-package-v2", storedAlgorithm)
			require.Equal(t, storedAlgorithm, otherAlgorithm)
			require.NotEqual(t, stored, otherStored, "stored source identity remains unchanged")
			baseline, _, err := CanonicalDOCXSnapshotDigest(a)
			require.NoError(t, err)
			current, _, err := CanonicalDOCXSnapshotDigest(b)
			require.NoError(t, err)
			require.Equal(t, baseline, current, "disjoint definitions commute without dropping any attributes")
			for _, change := range [][2]string{
				{`width="10"`, `width="11"`},
				{`hidden="1"`, `hidden="0"`},
				{`customWidth="1"`, `customWidth="0"`},
				{`max="2"`, `max="3"`},
			} {
				current, _, err := CanonicalDOCXSnapshotDigest(pack(
					strings.ReplaceAll(second+first, change[0], change[1]), false,
				))
				require.NoError(t, err)
				require.NotEqual(t, baseline, current, "every complete definition remains bound")
			}
			current, _, err = CanonicalDOCXSnapshotDigest(pack(second+first, true))
			require.NoError(t, err)
			require.NotEqual(t, baseline, current, "actual cell changes remain bound")
		})
	}
}

func TestEmbeddedColumnOverlapAndInvalidDefinitions(t *testing.T) {
	digest := func(columns string) (string, error) {
		parts := embeddedWorkbookParts()
		parts["xl/worksheets/sheet1.xml"] = `<worksheet xmlns="` + spreadsheetNS + `"><cols>` +
			columns + `</cols><sheetData/></worksheet>`
		digest, _, err := CanonicalDOCXSnapshotDigest(docxWithEmbeddedWorkbook(t, archive(t, parts, false)))
		return digest, err
	}
	first := `<col min="1" max="3" width="10"/>`
	second := `<col min="2" max="4" width="20"/>`
	a, err := digest(first + second)
	require.NoError(t, err)
	b, err := digest(second + first)
	require.NoError(t, err)
	require.NotEqual(t, a, b, "overlapping range precedence must never be reordered")
	for _, columns := range []string{
		`<col min="0" max="1"/>`, `<col min="2" max="1"/>`, `<col min="1" max="16385"/>`,
		`<col min="1"/>`, `<col min="not-a-column" max="2"/>`, `<col min="1" max="1"><v/></col>`,
		`<unexpected min="1" max="1"/>`,
	} {
		_, err := digest(columns)
		require.ErrorIs(t, err, ErrInvalidPackage)
	}
	parts := embeddedWorkbookParts()
	parts["xl/worksheets/sheet1.xml"] = `<worksheet xmlns="` + spreadsheetNS + `"><cols>` +
		first + `</cols><cols>` + second + `</cols></worksheet>`
	_, _, err = CanonicalDOCXSnapshotDigest(docxWithEmbeddedWorkbook(t, archive(t, parts, false)))
	require.ErrorIs(t, err, ErrInvalidPackage)
}
