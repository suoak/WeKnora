package service

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/infrastructure/docparser/anydoc"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestPersonalWorkbookUnusedVBADeclarationPreservesCells(t *testing.T) {
	data := standaloneTestWorkbook(t, func(parts map[string][]byte) {
		appendStandaloneTestXML(parts, "[Content_Types].xml", "</Types>",
			`<Default Extension="bin" ContentType="application/vnd.ms-office.vbaProject"/>`)
	})
	parsed, err := parseExactStandaloneWorkbook(context.Background(), "export.xlsx", data)
	require.NoError(t, err)
	var text strings.Builder
	for _, section := range parsed.Sections {
		text.WriteString(section.markdown)
	}
	require.Contains(t, text.String(), "first sheet")
	require.Contains(t, text.String(), "hidden value")
	parts, err := exactWorkbookPartsForFormat(data, true)
	require.NoError(t, err)
	parts["xl/vbaproject.bin"] = []byte("not accepted")
	require.Error(t, standaloneContentTypes(parts), "a real binary part is never a declaration-only exception")
}

func TestPersonalWorkbookLiteralHyperlinks(t *testing.T) {
	for _, target := range []string{
		"https://example.org/path", "mailto:team@example.org", "ssh://git@example.org/repo",
	} {
		t.Run(target, func(t *testing.T) {
			require.True(t, exactWorkbookLiteralHyperlink(target, true))
		})
	}
	for _, target := range []string{
		"file:///etc/passwd", "javascript:alert(1)", "data:text/plain,test",
		"ssh://user:password@example.org/repo", "https://user@example.org", "mailto:", "https://example.org/\n",
	} {
		t.Run(target, func(t *testing.T) {
			require.False(t, exactWorkbookLiteralHyperlink(target, true))
		})
	}
	require.False(t, exactWorkbookLiteralHyperlink("mailto:team@example.org", false))
	require.False(t, exactWorkbookLiteralHyperlink("ssh://example.org", false))
}

func TestPersonalWorkbookLongSheetNameKeepsSourceIdentity(t *testing.T) {
	longName := strings.Repeat("long-sheet-", 4)
	data := standaloneTestWorkbook(t, func(parts map[string][]byte) {
		parts["xl/workbook.xml"] = []byte(strings.ReplaceAll(
			strings.ReplaceAll(string(parts["xl/workbook.xml"]), `name="Sheet1"`, `name="`+longName+`"`),
			`name="Empty"`, `name="__FEISHU_READ_0"`))
	})
	parsed, err := parseExactStandaloneWorkbook(context.Background(), "export.xlsx", data)
	require.NoError(t, err)
	var text strings.Builder
	for _, section := range parsed.Sections {
		text.WriteString(section.markdown)
	}
	require.Contains(t, text.String(), "### Worksheet: "+longName)
	require.Contains(t, text.String(), "name="+longName)
	require.Contains(t, text.String(), "first sheet")
	require.Contains(t, text.String(), "hidden value")
	require.Contains(t, text.String(), "### Worksheet: __FEISHU_READ_0")
	require.NotContains(t, text.String(), "### Worksheet: __feishu_read_0_0")
}

func TestPersonalWorkbookKeepsCoveredMergedValuesAndWideColumns(t *testing.T) {
	data := standaloneTestWorkbook(t, func(parts map[string][]byte) {
		parts["xl/worksheets/sheet1.xml"] = []byte(`<worksheet xmlns="` + standaloneMainNS + `"><sheetData>
<row r="1"><c r="A1"><v>1</v></c><c r="B1"><v>2</v></c><c r="BW1"><v>3</v></c></row>
</sheetData><mergeCells count="1"><mergeCell ref="A1:B1"/></mergeCells></worksheet>`)
	})
	parsed, err := parseExactStandaloneWorkbook(context.Background(), "export.xlsx", data)
	require.NoError(t, err)
	var text strings.Builder
	for _, section := range parsed.Sections {
		if section.sheet == "Sheet1" {
			text.WriteString(section.markdown)
		}
	}
	require.Contains(t, text.String(), "| 1 | 1 | 2 |")
	require.Contains(t, text.String(), " BW |")
	require.Contains(t, text.String(), " 3 |")
	require.Contains(t, text.String(), "Merged cells: A1:B1")
	require.Contains(t, text.String(), "covered merged cell B1")
}

func TestPersonalWorkbookDetachedCommentsRemainExplicitlyUnassigned(t *testing.T) {
	person := "11111111-1111-4111-8111-111111111111"
	parts := map[string][]byte{
		"xl/persons/person.xml": []byte(`<personList xmlns="` + standaloneThreadNS +
			`"><person id="` + person + `" displayName="Author"/></personList>`),
		"xl/threadedcomments/thread.xml": []byte(`<ThreadedComments xmlns="` + standaloneThreadNS +
			`"><threadedComment id="22222222-2222-4222-8222-222222222222" personId="` + person +
			`" ref="A1"><text>Detached source annotation</text></threadedComment></ThreadedComments>`),
	}
	notes, err := standaloneThreadFacts(parts, []standaloneWorkbookSheet{{Part: "xl/worksheets/sheet1.xml"}})
	require.NoError(t, err)
	require.Contains(t, notes["xl/worksheets/sheet1.xml"],
		"Unassigned workbook threaded comment (xl/threadedcomments/thread.xml)")
	require.Contains(t, notes["xl/worksheets/sheet1.xml"], "Detached source annotation")
	parts["xl/threadedcomments/thread.xml"] = []byte(strings.ReplaceAll(
		string(parts["xl/threadedcomments/thread.xml"]), person, "33333333-3333-4333-8333-333333333333"))
	_, err = standaloneThreadFacts(parts, []standaloneWorkbookSheet{{Part: "xl/worksheets/sheet1.xml"}})
	require.Error(t, err, "detached comments still require a validated author")
}

func TestPersonalWorkbookChartFactsPreserveTitleAndCachedValues(t *testing.T) {
	chart := `<c:chartSpace xmlns:c="` + standaloneChartNS + `" xmlns:a="` + exactDrawingMainNamespace +
		`"><c:chart><c:title><c:tx><c:rich><a:bodyPr/><a:p><a:r><a:t>Source title</a:t></a:r></a:p>` +
		`</c:rich></c:tx></c:title><c:plotArea><c:pieChart><c:ser><c:idx val="0"/><c:val><c:numRef>` +
		`<c:f>Sheet1!$A$1</c:f><c:numCache><c:formatCode>General</c:formatCode><c:ptCount val="1"/>` +
		`<c:pt idx="0"><c:v>42</c:v></c:pt></c:numCache></c:numRef></c:val></c:ser>` +
		`</c:pieChart></c:plotArea></c:chart></c:chartSpace>`
	facts, err := standaloneChartFacts([]byte(chart))
	require.NoError(t, err)
	require.Contains(t, facts, "Source title")
	require.Contains(t, facts, "42")
	require.Contains(t, facts, "Sheet1!$A$1")
	_, err = standaloneChartFacts([]byte(strings.Replace(chart, "Sheet1!$A$1", "[external.xlsx]Sheet1!A1", 1)))
	require.Error(t, err, "external formula dependencies must not be silently admitted")
	_, err = standaloneChartFacts([]byte(strings.Replace(chart, "<c:chart>", "<c:chart><c:externalData/>", 1)))
	require.Error(t, err)
}

func TestPersonalWorkbookLongRowRetainsEveryCellValue(t *testing.T) {
	value := strings.Repeat("甲&amp;<br>", types.DefaultParserSemanticChunkMaxChars/4)
	heading := "\n\n## Workbook: export.xlsx\n\n### Worksheet: Sheet1\n\n"
	rows := "| Row | A | B |\n| --- | --- | --- |\n| 1 | " + value + " | tail |\n"
	sections, err := splitExactWorkbookRows(heading+rows, heading)
	require.NoError(t, err)
	require.Greater(t, len(sections), 1)
	var reconstructed strings.Builder
	for _, section := range sections {
		require.LessOrEqual(t, utf8.RuneCountInString(section), types.DefaultParserSemanticChunkMaxChars)
		for _, line := range strings.Split(section, "\n") {
			const label = "Cell A1 source value continuation: "
			if strings.HasPrefix(line, label) {
				fragment := strings.TrimPrefix(line, label)
				require.False(t, strings.HasSuffix(fragment, "&am"))
				require.False(t, strings.HasSuffix(fragment, "<br"))
				reconstructed.WriteString(fragment)
			}
		}
	}
	require.Equal(t, value, reconstructed.String())
	require.Contains(t, strings.Join(sections, ""), "Cell B1 source value continuation: tail")
}

func TestPersonalWorkbookReservesSemanticRoomForImageHandles(t *testing.T) {
	heading := "\n## Workbook: images.xlsx\n### Worksheet: Sheet1\n"
	grid := "| Row | A |\n| --- | --- |\n| 1 | value |\n"
	for i := 0; i < 62; i++ {
		grid += "\n![Worksheet image](" + anydoc.ImageDir + "image.png)\n"
	}
	sections, err := splitExactWorkbookRows(heading+grid, heading)
	require.NoError(t, err)
	require.Greater(t, len(sections), 1)
	for _, section := range sections {
		require.LessOrEqual(t, exactWorkbookSemanticCost(section), types.DefaultParserSemanticChunkMaxChars)
		resolved := strings.ReplaceAll(section, anydoc.ImageDir+"image.png", strings.Repeat("x", 400))
		require.LessOrEqual(t, utf8.RuneCountInString(resolved), types.DefaultParserSemanticChunkMaxChars)
	}
	require.Equal(t, 62, strings.Count(strings.Join(sections, ""), "![Worksheet image]"))
}
