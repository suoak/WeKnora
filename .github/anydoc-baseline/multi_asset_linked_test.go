//go:build anydoc && cgo

package anydoc

import (
	"archive/zip"
	"bytes"
	"io"
	"strings"
	"testing"
)

// TestBaselineMultiImageAssetLinks supplements the committed corpus with an
// explicit two-image assertion for the release-baseline workflow. It runs as
// a validation overlay and is not part of the frozen application source SHA.
func TestBaselineMultiImageAssetLinks(t *testing.T) {
	one := buildDocx(t)
	r, err := zip.NewReader(bytes.NewReader(one), int64(len(one)))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	w := zip.NewWriter(&out)
	for _, file := range r.File {
		if file.Name == "word/media/image1.png" {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		text := string(body)
		switch file.Name {
		case "word/_rels/document.xml.rels":
			text = strings.Replace(text, "</Relationships>",
				`  <Relationship Id="rId11" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="media/image2.png"/>`+"\n</Relationships>", 1)
		case "word/document.xml":
			drawing := `<w:p><w:r><w:drawing><wp:inline><wp:docPr id="2" name="Chart 2" descr="Second chart"/>` +
				`<a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/picture">` +
				`<pic:pic><pic:nvPicPr><pic:cNvPr id="2" name="Chart 2"/><pic:cNvPicPr/></pic:nvPicPr>` +
				`<pic:blipFill><a:blip r:embed="rId11"/></pic:blipFill><pic:spPr/></pic:pic>` +
				`</a:graphicData></a:graphic></wp:inline></w:drawing></w:r></w:p>`
			text = strings.Replace(text, "</w:body>", drawing+"</w:body>", 1)
		}
		part, err := w.Create(file.Name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write([]byte(text)); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"word/media/image1.png", "word/media/image2.png"} {
		part, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(onePixelPNG()); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	result, err := Convert(out.Bytes(), Options{Format: "docx", WithAssets: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.AssetsError != nil {
		t.Fatal(result.AssetsError)
	}
	if len(result.Assets) != 2 {
		t.Fatalf("assets = %d, want 2", len(result.Assets))
	}
	for _, want := range []string{
		"![Shipping chart](images/image-1.png)",
		"![Second chart](images/image-2.png)",
	} {
		if !strings.Contains(result.Markdown, want) {
			t.Fatalf("missing asset link %q in:\n%s", want, result.Markdown)
		}
	}
}
