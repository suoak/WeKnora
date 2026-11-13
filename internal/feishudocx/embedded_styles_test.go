package feishudocx

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEmbeddedStyleIndexAllocationPreservesResolvedCells(t *testing.T) {
	pack := func(reverse, changed, invalid bool) []byte {
		parts := embeddedWorkbookParts()
		borders := `<border/><border><left style="thin"/></border><border><top style="double"/></border>`
		styles := `<xf borderId="0"/><xf borderId="1" fillId="1"/><xf borderId="2" fillId="2"/>`
		index := "1"
		if reverse {
			borders = `<border/><border><top style="double"/></border><border><left style="thin"/></border>`
			styles = `<xf borderId="0"/><xf borderId="1" fillId="2"/><xf borderId="2" fillId="1"/>`
			index = "2"
		}
		if invalid {
			index = "99"
		}
		parts["xl/styles.xml"] = `<styleSheet xmlns="` + spreadsheetNS + `"><borders count="3">` +
			borders + `</borders><cellXfs count="3">` + styles + `</cellXfs></styleSheet>`
		parts["xl/worksheets/sheet1.xml"] = `<worksheet xmlns="` + spreadsheetNS + `"><sheetData>` +
			`<row r="1"><c r="A1" s="` + index + `"><v>2</v></c></row></sheetData></worksheet>`
		if changed {
			parts["xl/styles.xml"] = strings.ReplaceAll(parts["xl/styles.xml"], `style="thin"`, `style="dashed"`)
		}
		return docxWithEmbeddedWorkbook(t, archive(t, parts, false))
	}
	a, _, err := CanonicalDOCXSnapshotDigest(pack(false, false, false))
	require.NoError(t, err)
	b, _, err := CanonicalDOCXSnapshotDigest(pack(true, false, false))
	require.NoError(t, err)
	require.Equal(t, a, b, "table reallocation must preserve complete resolved style definitions")
	b, _, err = CanonicalDOCXSnapshotDigest(pack(true, true, false))
	require.NoError(t, err)
	require.NotEqual(t, a, b, "actual border changes remain bound")
	_, _, err = CanonicalDOCXSnapshotDigest(pack(true, false, true))
	require.Error(t, err, "dangling indexes cannot prove a snapshot")
}

func TestEmbeddedDuplicateFillsKeepCompleteDefinitions(t *testing.T) {
	style := `<styleSheet xmlns="` + spreadsheetNS + `"><borders count="1"><border/></borders>` +
		`<fills count="3"><fill/><fill><patternFill patternType="solid"/></fill>` +
		`<fill><patternFill patternType="solid"/></fill></fills>` +
		`<cellXfs count="2"><xf borderId="0" fillId="0"/><xf borderId="0" fillId="FILL"/></cellXfs></styleSheet>`
	pack := func(fill string) map[string][]byte {
		return map[string][]byte{
			"xl/styles.xml": []byte(strings.ReplaceAll(style, "FILL", fill)),
			"xl/worksheets/sheet1.xml": []byte(`<worksheet xmlns="` + spreadsheetNS +
				`"><sheetData><row r="1"><c r="A1" s="1"><v>2</v></c></row></sheetData></worksheet>`),
		}
	}
	a, err := normalizeEmbeddedStyleIndexes(pack("1"))
	require.NoError(t, err)
	b, err := normalizeEmbeddedStyleIndexes(pack("2"))
	require.NoError(t, err)
	require.Equal(t, a, b, "equivalent duplicated definitions resolve to the same style")
	changed := pack("2")
	changed["xl/styles.xml"] = []byte(strings.Replace(string(changed["xl/styles.xml"]), "solid", "gray125", 1))
	b, err = normalizeEmbeddedStyleIndexes(changed)
	require.NoError(t, err)
	require.NotEqual(t, a, b, "the complete table remains bound, including unused definitions")
	_, err = normalizeEmbeddedStyleIndexes(pack("99"))
	require.Error(t, err)
}
