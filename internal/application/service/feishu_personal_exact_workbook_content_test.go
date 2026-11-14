package service

import (
	"archive/zip"
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/infrastructure/docparser/anydoc"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"
)

func TestExactWorkbookLargeWorksheetSplitsOnlyBetweenCompleteRows(t *testing.T) {
	heading := "\n## Embedded workbook: test\n### Worksheet: Sheet1\n"
	grid := "| Row | A | B |\n| --- | --- | --- |\n"
	rows := []string{}
	for i := 1; i <= 100; i++ {
		row := fmt.Sprintf("| %d | row-%d | %s |\n", i, i, strings.Repeat("值", 120))
		rows = append(rows, row)
		grid += row
	}
	grid += "\nHyperlink cell A1: url=https://example.invalid/;\n"
	sections, err := splitExactWorkbookRows(heading+grid, heading)
	require.NoError(t, err)
	require.Greater(t, len(sections), 1)
	for _, section := range sections {
		require.LessOrEqual(t, utf8.RuneCountInString(section), types.DefaultParserSemanticChunkMaxChars)
		require.True(t, strings.HasPrefix(section, heading+"| Row | A | B |\n| --- | --- | --- |\n"))
	}
	joined := strings.Join(sections, "")
	for _, row := range rows {
		require.Equal(t, 1, strings.Count(joined, row))
	}
	require.Equal(t, 1, strings.Count(joined, "Hyperlink cell A1:"))
	_, err = splitExactWorkbookRows(heading+"| Row | A |\n| --- | --- |\n| 1 | "+strings.Repeat("值", types.DefaultParserSemanticChunkMaxChars)+" |\n", heading)
	require.Error(t, err, "a single oversized row must never be truncated")
}

func exactContentWorkbook(t *testing.T, cells, extra string, additional map[string][]byte) []byte {
	t.Helper()
	f := excelize.NewFile()
	defer f.Close()
	b, err := f.WriteToBuffer()
	require.NoError(t, err)
	z, err := zip.NewReader(bytes.NewReader(b.Bytes()), int64(b.Len()))
	require.NoError(t, err)
	parts := map[string][]byte{}
	for _, entry := range z.File {
		r, err := entry.Open()
		require.NoError(t, err)
		parts[entry.Name], err = io.ReadAll(r)
		require.NoError(t, err)
		r.Close()
	}
	parts["xl/worksheets/sheet1.xml"] = []byte(`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="` + exactWorkbookRelNamespace + `"><sheetData><row r="1">` + cells + `</row></sheetData>` + extra + `</worksheet>`)
	for name, body := range additional {
		parts[name] = body
	}
	var out bytes.Buffer
	w := zip.NewWriter(&out)
	for name, body := range parts {
		e, err := w.Create(name)
		require.NoError(t, err)
		_, err = e.Write(body)
		require.NoError(t, err)
	}
	require.NoError(t, w.Close())
	return out.Bytes()
}

func TestExactWorkbookFormulasPreserveSavedValuesAndSharedExpressions(t *testing.T) {
	cells := `<c r="A1"><f>1+1</f><v>2</v></c><c r="B1"><f t="shared" si="0" ref="B1:C` +
		`1">A1+1</f><v>3</v></c><c r="C1"><f t="shared" si="0"/><v>4</v></c>`
	data := exactContentWorkbook(t, cells, "", nil)
	parts, err := exactWorkbookParts(data)
	require.NoError(t, err)
	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()
	grid, _, _, _, err := exactWorkbookCellGrid(f, "Sheet1", parts["xl/worksheets/sheet1.xml"])
	require.NoError(t, err)
	require.Contains(t, grid, "| 1 | 2 | 3 | 4 |")
	notes, err := exactWorkbookFormulaNotes(f, "Sheet1", parts["xl/worksheets/sheet1.xml"])
	require.NoError(t, err)
	for _, expression := range []string{"1+1", "A1+1", "B1+1"} {
		require.Contains(t, notes, expression)
	}
	if anydoc.Available() {
		book, err := parseExactEmbeddedWorkbook("word/embeddings/a.xlsx", data, 1)
		require.NoError(t, err)
		require.Contains(t, book.Sections[0].markdown, notes)
	}
	for _, bad := range []string{
		`<c r="A1"><f t="shared" si="0"/></c>`,
		`<c r="A1"><f t="shared" si="0" ref="A1:B1">1+1</f></c>`,
		`<c r="A1"><f t="array" ref="A1:B1">1+1</f></c>`,
		`<c r="A1"><f>[1]Sheet1!A1</f></c>`,
	} {
		_, err := exactWorkbookFormulaNotes(f, "Sheet1", []byte(`<worksheet><sheetData><row>`+bad+`</row></sheetData></worksheet>`))
		require.Error(t, err)
	}
}

func exactDrawingFixture() map[string][]byte {
	return map[string][]byte{
		"xl/worksheets/sheet1.xml":            []byte(`<worksheet xmlns:r="` + exactWorkbookRelNamespace + `"><drawing r:id="drawing"/></worksheet>`),
		"xl/worksheets/_rels/sheet1.xml.rels": []byte(`<Relationships><Relationship Id="drawing" Type="` + exactWorkbookRelNamespace + `/drawing" Target="../drawings/drawing1.xml"/></Relationships>`),
		"xl/drawings/drawing1.xml":            []byte(`<xdr:wsDr xmlns:xdr="` + exactDrawingNamespace + `" xmlns:a="` + exactDrawingMainNamespace + `" xmlns:r="` + exactWorkbookRelNamespace + `"><xdr:twoCellAnchor><xdr:from><xdr:col>0</xdr:col><xdr:colOff>0</xdr:colOff><xdr:row>0</xdr:row><xdr:rowOff>0</xdr:rowOff></xdr:from><xdr:to><xdr:col>2</xdr:col><xdr:colOff>0</xdr:colOff><xdr:row>2</xdr:row><xdr:rowOff>0</xdr:rowOff></xdr:to><xdr:pic><xdr:nvPicPr><xdr:cNvPr id="1" name="Original picture" descr="Source diagram"/><xdr:cNvPicPr/></xdr:nvPicPr><xdr:blipFill><a:blip r:embed="image"/><a:stretch><a:fillRect/></a:stretch></xdr:blipFill><xdr:spPr><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></xdr:spPr></xdr:pic><xdr:clientData/></xdr:twoCellAnchor></xdr:wsDr>`),
		"xl/drawings/_rels/drawing1.xml.rels": []byte(`<Relationships><Relationship Id="image" Type="` + exactWorkbookRelNamespace + `/image" Target="../media/image1.jpeg"/></Relationships>`),
		"xl/media/image1.jpeg":                []byte("image"),
	}
}

func TestExactWorkbookDrawingPreservesPlacementAndRejectsUnknownInputs(t *testing.T) {
	for _, change := range []string{"valid", "external", "orphan", "chart", "out_of_bounds", "missing_image", "wrong_role"} {
		t.Run(change, func(t *testing.T) {
			parts := exactDrawingFixture()
			key := "xl/drawings/drawing1.xml"
			switch change {
			case "external":
				parts["xl/drawings/_rels/drawing1.xml.rels"] = []byte(strings.Replace(string(parts["xl/drawings/_rels/drawing1.xml.rels"]), `Target="../media/image1.jpeg"`, `Target="https://example.invalid/image" TargetMode="External"`, 1))
			case "orphan":
				parts["xl/drawings/drawing2.xml"] = parts[key]
			case "chart":
				parts[key] = []byte(strings.Replace(string(parts[key]), "xdr:pic>", "xdr:graphicFrame>", -1))
			case "out_of_bounds":
				parts[key] = []byte(strings.Replace(string(parts[key]), "<xdr:col>2</xdr:col>", "<xdr:col>64</xdr:col>", 1))
			case "missing_image":
				delete(parts, "xl/media/image1.jpeg")
			case "wrong_role":
				parts["xl/drawings/_rels/drawing1.xml.rels"] = []byte(strings.Replace(string(parts["xl/drawings/_rels/drawing1.xml.rels"]), "/image", "/hyperlink", 1))
			}
			notes, err := exactWorkbookDrawingNotes(parts)
			if change != "valid" {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			for _, want := range []string{"Original picture", "Source diagram", "from A1", "to C3"} {
				require.Contains(t, notes["xl/worksheets/sheet1.xml"], want)
			}
		})
	}
}

func TestExactWorkbookDrawingNativeJPEGCompleteCoverage(t *testing.T) {
	if !anydoc.Available() {
		t.Skip("requires linked native parser")
	}
	parts := exactDrawingFixture()
	delete(parts, "xl/worksheets/sheet1.xml")
	// Feishu exports use signed end offsets for an inset picture border.
	drawing := "xl/drawings/drawing1.xml"
	parts[drawing] = []byte(strings.Replace(string(parts[drawing]), "<xdr:colOff>0</xdr:colOff>",
		"<xdr:colOff>-38100</xdr:colOff>", -1))
	var jpegBytes bytes.Buffer
	require.NoError(t, jpeg.Encode(&jpegBytes, image.NewRGBA(image.Rect(0, 0, 2, 2)), nil))
	parts["xl/media/image1.jpeg"] = jpegBytes.Bytes()
	var background bytes.Buffer
	require.NoError(t, png.Encode(&background, image.NewRGBA(image.Rect(0, 0, 2, 2))))
	parts["xl/media/background.png"] = background.Bytes()
	rels := "xl/worksheets/_rels/sheet1.xml.rels"
	parts[rels] = []byte(strings.Replace(string(parts[rels]), "</Relationships>", `<Relationship Id="background" Type="`+exactWorkbookRelNamespace+`/image" Target="../media/background.png"/></Relationships>`, 1))
	data := exactContentWorkbook(t, `<c r="A1" t="inlineStr"><is><t>Retained worksheet label</t></is></c>`, `<drawing r:id="drawing"/><picture r:id="background"/>`, parts)
	book, err := parseExactEmbeddedWorkbook("word/embeddings/a.xlsx", data, 1)
	require.NoError(t, err)
	require.Len(t, book.Assets, 2)
	for _, asset := range book.Assets {
		if asset.MediaType == "image/jpeg" {
			require.Equal(t, jpegBytes.Bytes(), asset.Data)
		} else {
			require.Equal(t, "image/png", asset.MediaType)
			require.Equal(t, background.Bytes(), asset.Data)
		}
	}
	require.Contains(t, book.Sections[0].markdown, "Retained worksheet label")
	require.Contains(t, book.Sections[0].markdown, "Source diagram")
	require.Contains(t, book.Sections[0].markdown, "-38100")
}
