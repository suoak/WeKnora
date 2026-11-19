package service

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"
)

func TestStandaloneWorkbookPreservesConditionalFormatting(t *testing.T) {
	data := standaloneTestWorkbook(t, func(parts map[string][]byte) {
		appendStandaloneTestXML(parts, "xl/worksheets/sheet1.xml", "</worksheet>",
			`<conditionalFormatting sqref="A1:A8"><cfRule type="expression" priority="1"><formula>A1&gt;3</formula></cfRule></conditionalFormatting>`)
	})
	parsed, err := parseExactStandaloneWorkbook(context.Background(), "rules.xlsx", data)
	require.NoError(t, err)
	var text strings.Builder
	for _, section := range parsed.Sections {
		text.WriteString(section.markdown)
	}
	require.Contains(t, text.String(), "conditionalFormatting/cfRule/formula text: A1&gt;3")
	require.Contains(t, text.String(), "sqref=A1:A8")
	_, err = exactWorkbookPartsForFormat(data, false)
	require.Error(t, err, "embedded workbook policy remains bounded separately")
}

func TestStandaloneWorkbookRefusesUnboundOrExternalConditionalFormula(t *testing.T) {
	for _, extra := range []string{
		`<formula>A1</formula>`,
		`<conditionalFormatting sqref="A1"><cfRule type="expression"><formula>[other.xlsx]Sheet1!A1</formula></cfRule></conditionalFormatting>`,
	} {
		data := standaloneTestWorkbook(t, func(parts map[string][]byte) {
			appendStandaloneTestXML(parts, "xl/worksheets/sheet1.xml", "</worksheet>", extra)
		})
		_, err := parseExactStandaloneWorkbook(context.Background(), "rules.xlsx", data)
		require.Error(t, err)
	}
}

func TestStandaloneFormulaStringLiterals(t *testing.T) {
	for _, formula := range []string{
		`HYPERLINK("https://example.invalid/doc", "[issue] A | B")`,
		`HYPERLINK("https://example.invalid/doc", "a ""quoted"" [label]")`,
	} {
		require.False(t, standaloneFormulaDependency(formula))
	}
	for _, formula := range []string{
		`'[other.xlsx]Sheet1'!A1`, `app|topic!A1`, `"text"&[other.xlsx]Sheet1!A1`, `"unclosed`,
	} {
		require.True(t, standaloneFormulaDependency(formula))
	}
	data := standaloneTestWorkbook(t, func(parts map[string][]byte) {
		parts["xl/worksheets/sheet1.xml"] = []byte(`<worksheet xmlns="` + standaloneMainNS + `"><sheetData><row r="1"><c r="A1" t="str"><f>HYPERLINK("https://example.invalid/doc", "[label] A | B")</f><v>[label] A | B</v></c></row></sheetData></worksheet>`)
	})
	parsed, err := parseExactStandaloneWorkbook(context.Background(), "literal.xlsx", data)
	require.NoError(t, err)
	var text strings.Builder
	for _, section := range parsed.Sections {
		text.WriteString(section.markdown)
	}
	require.Contains(t, text.String(), "[label] A &#124; B")
}

func TestStandaloneWorkbookPreservesMoreThanTenThousandCells(t *testing.T) {
	file := excelize.NewFile()
	defer file.Close()
	for row := 1; row <= 101; row++ {
		values := make([]interface{}, 100)
		for i := range values {
			values[i] = "saved"
		}
		ref, err := excelize.CoordinatesToCellName(1, row)
		require.NoError(t, err)
		require.NoError(t, file.SetSheetRow("Sheet1", ref, &values))
	}
	require.NoError(t, file.SetCellStr("Sheet1", "CV101", "last cell preserved"))
	buf, err := file.WriteToBuffer()
	require.NoError(t, err)
	parsed, err := parseExactStandaloneWorkbook(context.Background(), "large.xlsx", buf.Bytes())
	require.NoError(t, err)
	var text strings.Builder
	for _, section := range parsed.Sections {
		require.Equal(t, 10100, section.cells)
		text.WriteString(section.markdown)
	}
	require.Contains(t, text.String(), "last cell preserved")
}

func TestStandaloneWorkbookPreservesCellXMLSpace(t *testing.T) {
	for _, mode := range []string{"preserve", "default", "unknown"} {
		data := standaloneTestWorkbook(t, func(parts map[string][]byte) {
			parts["xl/worksheets/sheet1.xml"] = []byte(`<worksheet xmlns="` + standaloneMainNS + `"><sheetData><row r="1"><c r="A1" t="inlineStr" xml:space="` + mode + `"><is><t xml:space="preserve">  text  </t></is></c></row></sheetData></worksheet>`)
		})
		parsed, err := parseExactStandaloneWorkbook(context.Background(), "spaces.xlsx", data)
		if mode == "unknown" {
			require.Error(t, err)
			continue
		}
		require.NoError(t, err)
		require.Contains(t, parsed.Sections[0].markdown, "   text   |")
	}
}

func TestStandaloneWorkbookIncidentReplay(t *testing.T) {
	filename := os.Getenv("KNOWHUB_XLSX_INCIDENT_FIXTURE")
	if filename == "" {
		t.Skip("private incident fixture not configured")
	}
	data, err := os.ReadFile(filename)
	require.NoError(t, err)
	parsed, err := parseExactStandaloneWorkbook(context.Background(), "incident.xlsx", data)
	require.NoError(t, err)
	sheets := map[string]bool{}
	bytes := 0
	for _, section := range parsed.Sections {
		sheets[section.name] = true
		bytes += len(section.markdown)
	}
	require.Len(t, sheets, 21)
	t.Logf("sheets=%d sections=%d markdown_bytes=%d", len(sheets), len(parsed.Sections), bytes)
}
