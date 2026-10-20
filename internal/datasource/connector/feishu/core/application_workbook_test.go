package core

import (
	"archive/zip"
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestP4WorkbookBackgroundAndHyperlinkKeepSheetIdentity(t *testing.T) {
	for _, mode := range []string{"success", "missing_processor", "orphan_image", "drawing", "unknown_sheet"} {
		t.Run(mode, func(t *testing.T) {
			files := map[string]string{
				"xl/workbook.xml": "<workbook xmlns:r=\"http://schemas.openxmlformats.org/officeDocument/2" +
					"006/relationships\"><sheets><sheet name=\"Sales\" " +
					"r:id=\"rId1\"/></sheets></workbook>",
				"xl/_rels/workbook.xml.rels": "<Relationships><Relationship Id=\"rId1\" " +
					"Target=\"worksheets/sheet1.xml\" Type=\"http://schemas.openxmlformats." +
					"org/officeDocument/2006/relationships/worksheet\"/></Relationships>",
				"xl/worksheets/sheet1.xml": "<worksheet xmlns:r=\"http://schemas.openxmlformats.org/officeDocument/" +
					"2006/relationships\"><hyperlinks><hyperlink ref=\"D2\" " +
					"r:id=\"rId2\"/></hyperlinks><picture r:id=\"rId1\"/></worksheet>",
				"xl/worksheets/_rels/sheet1.xml.rels": "<Relationships><Relationship Id=\"rId1\" " +
					"Target=\"../media/picture.png\" Type=\"http://schemas.openxmlformats.o" +
					"rg/officeDocument/2006/relationships/image\"/><Relationship " +
					"Id=\"rId2\" Target=\"https://example.com/business\" " +
					"TargetMode=\"External\" Type=\"http://schemas.openxmlformats.org/offic" +
					"eDocument/2006/relationships/hyperlink\"/></Relationships>",
				"xl/media/picture.png": "fixture image bytes",
			}
			if mode == "orphan_image" {
				files["xl/media/orphan.png"] = "unreferenced"
			}
			if mode == "drawing" {
				files["xl/drawings/drawing1.xml"] = "<drawing/>"
			}
			var b bytes.Buffer
			z := zip.NewWriter(&b)
			for name, text := range files {
				w, err := z.Create(name)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = w.Write([]byte(text)); err != nil {
					t.Fatal(err)
				}
			}
			if err := z.Close(); err != nil {
				t.Fatal(err)
			}
			workbook, err := openApplicationWorkbook(b.Bytes())
			if mode == "drawing" {
				if err == nil {
					t.Fatal("unparsed drawing accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			if mode != "missing_processor" {
				ctx = WithApplicationAttachmentProcessor(
					ctx,
					func(_ context.Context, a ApplicationAttachment) (ApplicationAttachment, error) {
						a.Text = "background description"
						a.Parser = "fixture"
						return a, nil
					},
				)
			}
			s := &ApplicationTableSnapshot{
				Resource: PersonalResource{Type: "sheet", Title: "Book"},
				Tables:   []ApplicationTable{{ID: "sheetStable", Title: "Sales"}},
			}
			if mode == "unknown_sheet" {
				s.Tables[0].Title = "Different"
			}
			err = workbook.enrich(ctx, s)
			if mode != "success" {
				if err == nil {
					t.Fatal("incomplete workbook accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(s.Attachments) != 1 || s.Attachments[0].TableID != "sheetStable" ||
				s.Attachments[0].RecordID != "sheet_background" {
				t.Fatal("background identity lost")
			}
			text, err := s.IndexText()
			if err != nil {
				t.Fatal(err)
			}
			for _, v := range []string{"D2", "https://example.com/business", "background description", "sheetStable"} {
				if !strings.Contains(text, v) {
					t.Fatalf("missing %s", v)
				}
			}
		})
	}
}
