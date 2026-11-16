package service

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func standaloneExportMetadataFixture(parts map[string][]byte) {
	standaloneTestDynamicMetadata(parts)
	parts["xl/_rels/workbook.xml.rels"] = bytes.ReplaceAll(
		parts["xl/_rels/workbook.xml.rels"],
		[]byte(`<Relationship Id="dynamic" Type="`+exactWorkbookRelNamespace+`/sheetMetadata" Target="metadata.xml"/>`),
		nil,
	)
	appendStandaloneTestXML(parts, "[Content_Types].xml", "</Types>",
		`<Default Extension="bmp" ContentType="image/bmp"/>`)
}

func TestExactStandaloneExportPreservesUnusedDynamicMetadata(t *testing.T) {
	data := standaloneTestWorkbook(t, standaloneExportMetadataFixture)
	original := bytes.Clone(data)
	parsed, err := parseExactStandaloneWorkbook(t.Context(), "source.xlsx", data)
	require.NoError(t, err)
	require.Equal(t, original, data)
	var text string
	for _, section := range parsed.Sections {
		text += section.markdown
	}
	require.Contains(t, text, "first sheet")
	require.Contains(t, text, "hidden value")
	require.Contains(t, text, "XLDAPR")
	require.Contains(t, text, "fDynamic")
}

func TestExactStandaloneExportRejectsUnprovedMetadataAndBitmapPayloads(t *testing.T) {
	for _, scenario := range []string{"cell metadata", "value metadata", "unknown metadata", "bitmap payload"} {
		t.Run(scenario, func(t *testing.T) {
			data := standaloneTestWorkbook(t, func(parts map[string][]byte) {
				standaloneExportMetadataFixture(parts)
				switch scenario {
				case "cell metadata", "value metadata":
					attr := "cm"
					if scenario == "value metadata" {
						attr = "vm"
					}
					parts["xl/worksheets/sheet1.xml"] = bytes.Replace(
						parts["xl/worksheets/sheet1.xml"], []byte(`<c `), []byte(`<c `+attr+`="1" `), 1,
					)
				case "unknown metadata":
					parts["xl/metadata.xml"] = bytes.ReplaceAll(
						parts["xl/metadata.xml"], []byte("XLDAPR"), []byte("UNKNOWN"),
					)
				case "bitmap payload":
					parts["xl/media/image.bmp"] = []byte("BM")
				}
			})
			_, err := parseExactStandaloneWorkbook(t.Context(), "source.xlsx", data)
			require.Error(t, err)
		})
	}
}
