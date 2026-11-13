package feishudocx

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

func embeddedWorkbookParts() map[string]string {
	return map[string]string{
		"[Content_Types].xml": `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"/>`,
		"_rels/.rels":         `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"/>`,
		"xl/workbook.xml":     `<workbook xmlns="` + spreadsheetNS + `"><sheets/></workbook>`,
		"xl/worksheets/sheet1.xml": `<worksheet xmlns="` + spreadsheetNS + `"><cols>` +
			`<col min="1" max="1" width="10"/></cols>` +
			`<sheetData><row r="1"><c r="A1"><f>1+1</f><v>2</v></c></row></sheetData></worksheet>`,
		"xl/media/image.png": "workbook-image",
		"docProps/core.xml":  `<properties>unchanged source properties</properties>`,
	}
}

func docxWithEmbeddedWorkbook(t *testing.T, workbook []byte) []byte {
	t.Helper()
	parts := exampleParts("2026-11-12T09:00:00Z", "17", "decimal", "17")
	parts["word/embeddings/Microsoft_Excel_Worksheet1.xlsx"] = string(workbook)
	return archive(t, parts, false)
}

func TestEmbeddedWorkbookPackingDoesNotChangeDOCXIdentity(t *testing.T) {
	parts := embeddedWorkbookParts()
	a, b := archive(t, parts, false), archive(t, parts, true)
	if bytes.Equal(a, b) {
		t.Fatal("fixture must change timestamps, entry order and compression")
	}
	first, algorithm, err := CanonicalDOCXDigest(docxWithEmbeddedWorkbook(t, a))
	if err != nil {
		t.Fatal(err)
	}
	second, other, err := CanonicalDOCXDigest(docxWithEmbeddedWorkbook(t, b))
	if err != nil || first != second || algorithm != "feishu-docx-package-v2" || other != algorithm {
		t.Fatal("identical embedded workbook contents must have the same versioned identity", err)
	}
	for _, part := range []string{
		"xl/worksheets/sheet1.xml", "xl/media/image.png", "docProps/core.xml", "_rels/.rels",
	} {
		t.Run(part, func(t *testing.T) {
			changed := embeddedWorkbookParts()
			if strings.HasSuffix(part, ".xml") || strings.HasSuffix(part, ".rels") {
				changed[part] = strings.Replace(changed[part], "/>", ` changed="yes"/>`, 1)
				if changed[part] == parts[part] {
					changed[part] = strings.Replace(changed[part], ">", ` changed="yes">`, 1)
				}
			} else {
				changed[part] += "different"
			}
			got, _, e := CanonicalDOCXDigest(docxWithEmbeddedWorkbook(t, archive(t, changed, false)))
			if e != nil || got == first {
				t.Fatal("embedded content change must remain protected", e)
			}
		})
	}
	for _, change := range [][2]string{{`width="10"`, `width="11"`}, {"1+1", "1+2"}, {"<v>2</v>", "<v>3</v>"}} {
		changed := embeddedWorkbookParts()
		const sheet = "xl/worksheets/sheet1.xml"
		changed[sheet] = strings.ReplaceAll(changed[sheet], change[0], change[1])
		got, _, e := CanonicalDOCXDigest(docxWithEmbeddedWorkbook(t, archive(t, changed, false)))
		if e != nil || got == first {
			t.Fatal("embedded layout, formulas and cell values must remain protected", e)
		}
	}
}

func TestEmbeddedWorkbookInvalidPackagesFailClosed(t *testing.T) {
	for _, name := range []string{
		"invalid", "missing_workbook", "traversal", "case_collision", "invalid_xml", "nested_crc",
	} {
		t.Run(name, func(t *testing.T) {
			parts := embeddedWorkbookParts()
			switch name {
			case "missing_workbook":
				delete(parts, "xl/workbook.xml")
			case "traversal":
				parts["../outside"] = "invalid"
			case "case_collision":
				parts["XL/WORKBOOK.XML"] = parts["xl/workbook.xml"]
			case "invalid_xml":
				parts["xl/workbook.xml"] = "<broken>"
			}
			raw := archive(t, parts, false)
			if name == "invalid" {
				raw = []byte("not an xlsx")
			}
			if name == "nested_crc" {
				// Store entries so a byte can be changed without changing ZIP structure.
				raw = archive(t, parts, true)
				z, e := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
				if e != nil {
					t.Fatal(e)
				}
				for _, entry := range z.File {
					if entry.Name == "xl/media/image.png" {
						offset, e := entry.DataOffset()
						if e != nil {
							t.Fatal(e)
						}
						raw[offset] ^= 1
					}
				}
			}
			if _, _, e := CanonicalDOCXDigest(docxWithEmbeddedWorkbook(t, raw)); e == nil {
				t.Fatal("invalid embedded workbook accepted")
			}
		})
	}
	budget := uint64(maxExpanded - 1)
	if _, err := embeddedXLSXIdentity(archive(t, embeddedWorkbookParts(), false), &budget); err == nil {
		t.Fatal("embedded expansion must share the outer document budget")
	}
}
