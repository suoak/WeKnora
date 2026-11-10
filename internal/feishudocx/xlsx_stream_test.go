package feishudocx

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
	"testing"
)

func TestXLSXStreamPreservesV1JSON(t *testing.T) {
	for _, body := range []string{
		`<empty/>`,
		`<?xml version="1.0"?><root b="2" a="&lt;&amp;&#34;"> mixed <x/><x>汉字</x><!--note--><![CDATA[abc]]></root>`,
		`<worksheet xmlns="` + spreadsheetNS + `"><cols><col min="1" max="3" width="12" hidden="1"/></cols>` +
			`<sheetData><row><c><f>A1+B1</f><v>9</v></c></row></sheetData></worksheet>`,
		`<worksheet xmlns="` + spreadsheetNS + `"><cols> </cols><sheetData/></worksheet>`,
		`<worksheet xmlns="` + spreadsheetNS + `"><cols><col min="1" max="3" style="1"/>` +
			`<col min="2" max="4" style="2"/></cols><sheetData/></worksheet>`,
		`<metadata><worksheet xmlns="` + spreadsheetNS + `"><cols>` +
			`<col min="1" max="3" width="12"/></cols></worksheet></metadata>`,
	} {
		root, err := parseXML([]byte(body))
		if err != nil {
			t.Fatal(err)
		}
		if root.Name == (xml.Name{Space: spreadsheetNS, Local: "worksheet"}) {
			if err := normalizeExportColumnWidths(root); err != nil {
				t.Fatal(err)
			}
		}
		want, _ := json.Marshal(root)
		var got bytes.Buffer
		if err := streamXLSXXML(strings.NewReader(body), &got, xml.Name{}); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(want, got.Bytes()) {
			t.Fatalf("v1 canonical JSON changed: want %s got %s", want, got.Bytes())
		}
	}
}

func TestXLSXStreamRejectsUnsafeXML(t *testing.T) {
	for _, body := range []string{
		`<!DOCTYPE root><root/>`, `<a/><b/>`, `<a x="1" x="2"/>`, `<a><?hidden x?></a>`,
		`<!--outside--><a/>`, `<a>broken`, strings.Repeat(`<a>`, 129) + strings.Repeat(`</a>`, 129),
		`<worksheet xmlns="` + spreadsheetNS + `"><cols/><cols/></worksheet>`,
		`<worksheet xmlns="` + spreadsheetNS + `"><cols><col min="1" max="16384"/>` +
			`<col min="1" max="16384"/><col min="1" max="16384"/></cols></worksheet>`,
	} {
		if err := streamXLSXXML(strings.NewReader(body), io.Discard, xml.Name{}); err == nil {
			t.Fatal("accepted unsafe XML")
		}
	}
	if err := streamXLSXXML(strings.NewReader(`<wrong/>`), io.Discard, xml.Name{Local: "workbook"}); err == nil {
		t.Fatal("accepted wrong root")
	}
}

// This synthetic workbook exceeds both old production limits (32 MiB per part,
// 128 MiB expanded). No business data is used.
func TestXLSXLargeTableStreaming(t *testing.T) {
	if testing.Short() {
		t.Skip("large bounded workbook")
	}
	var out bytes.Buffer
	z := zip.NewWriter(&out)
	add := func(name, body string) {
		t.Helper()
		w, err := z.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = io.WriteString(w, body); err != nil {
			t.Fatal(err)
		}
	}
	add("[Content_Types].xml", `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"/>`)
	add("_rels/.rels", `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"/>`)
	add("xl/workbook.xml", `<workbook xmlns="`+spreadsheetNS+`"/>`)
	// Padding is varied pseudorandom text so the anti-zip-bomb ratio stays valid.
	var cell strings.Builder
	for i := 0; i < 1024; i++ {
		h := sha256.Sum256([]byte(fmt.Sprint(i)))
		for _, c := range hex.EncodeToString(h[:]) {
			cell.WriteByte(byte('a' + c%3))
		}
	}
	for sheet := 0; sheet < 3; sheet++ {
		w, err := z.Create(fmt.Sprintf("xl/worksheets/sheet%d.xml", sheet))
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.WriteString(w, `<worksheet xmlns="`+spreadsheetNS+`"><sheetData>`)
		for row := 0; row < 720; row++ {
			_, _ = io.WriteString(w, `<row><c><v>`+cell.String()+`</v></c></row>`)
		}
		_, _ = io.WriteString(w, `</sheetData></worksheet>`)
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := CanonicalXLSXDigest(out.Bytes()); err != nil {
		t.Fatalf("large legitimate workbook rejected: %v", err)
	}
}

func TestXLSXStreamTokenBudget(t *testing.T) {
	body := `<sheetData>` + strings.Repeat(`<c><v>1</v></c>`, 210000) + `</sheetData>`
	if err := streamXLSXXML(strings.NewReader(body), io.Discard, xml.Name{}); err != nil {
		t.Fatal("million-cell-token table rejected", err)
	}
	s := &xlsxXMLStream{d: xml.NewDecoder(strings.NewReader(`<a/>`)), w: io.Discard, tokens: 16000000}
	if _, err := s.next(); err == nil {
		t.Fatal("token budget not enforced")
	}
}
