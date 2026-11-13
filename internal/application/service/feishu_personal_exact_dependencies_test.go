package service

import (
	"archive/zip"
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExactDOCXEmbeddedWorkbookCountBound(t *testing.T) {
	for _, count := range []int{5, 16, 17, 50, 64, 65} {
		var buffer bytes.Buffer
		writer := zip.NewWriter(&buffer)
		var relationships strings.Builder
		for i := 0; i < count; i++ {
			name := fmt.Sprintf("word/embeddings/workbook%d.xlsx", i)
			entry, err := writer.Create(name)
			require.NoError(t, err)
			_, err = entry.Write([]byte("bounded package bytes"))
			require.NoError(t, err)
			_, err = fmt.Fprintf(&relationships,
				`<Relationship Id="r%d" Target="embeddings/workbook%d.xlsx" Type="%s/package"/>`,
				i, i, "http://schemas.openxmlformats.org/officeDocument/2006/relationships")
			require.NoError(t, err)
		}
		entry, err := writer.Create("word/_rels/document.xml.rels")
		require.NoError(t, err)
		_, err = entry.Write([]byte("<Relationships>" + relationships.String() + "</Relationships>"))
		require.NoError(t, err)
		require.NoError(t, writer.Close())
		packages := map[string][]byte{}
		err = checkExactDOCXGraph(buffer.Bytes(), packages)
		if count <= 64 {
			require.NoError(t, err)
			require.Len(t, packages, count)
		} else {
			require.ErrorIs(t, err, errPersonalExactDocument)
		}
		require.Error(t, checkExactDOCXDependencies(buffer.Bytes()), "default path stays closed")
	}
}

func TestExactPersonalDOCXDependencies(t *testing.T) {
	for _, test := range []struct {
		name, relationship, extra string
		valid                     bool
	}{
		{"internal", `<Relationship Target="media/image1.png" Type="image"/>`, "", true},
		{
			"external image",
			`<Relationship TargetMode="External" Target="https://example.invalid/a" Type="image"/>`, "", false,
		},
		{"implicit URL", `<Relationship Target="https://example.invalid/a" Type="image"/>`, "", false},
		{
			"literal HTTPS hyperlink",
			`<Relationship Id="rIdLink" TargetMode="External" Target="https://example.invalid/a" ` +
				`Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink"/>`, "", true,
		},
		{
			"file hyperlink",
			`<Relationship TargetMode="External" Target="file:///etc/passwd" ` +
				`Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink"/>`, "", false,
		},
		{
			"credential hyperlink",
			`<Relationship TargetMode="External" Target="https://user:password@example.invalid/a" ` +
				`Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink"/>`, "", false,
		},
		{"unknown mode", `<Relationship TargetMode="external" Target="media/image1.png"/>`, "", false},
		{
			"embedded object",
			`<Relationship Target="embeddings/object.bin" Type="https://schemas.invalid/oleObject"/>`, "", false,
		},
		{"unreferenced object", "", "word/embeddings/object.bin", false},
		{"traversal", "", "../outside", false},
		{"relationship traversal", `<Relationship Target="../../../outside"/>`, "", false},
		{"encoded traversal", `<Relationship Target="%2e%2e/%2e%2e/outside"/>`, "", false},
		{"missing dependency", `<Relationship Target="media/missing.png"/>`, "", false},
		{"malformed XML", "<Relationship", "", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			var buffer bytes.Buffer
			writer := zip.NewWriter(&buffer)
			entry, err := writer.Create("word/_rels/document.xml.rels")
			require.NoError(t, err)
			_, err = entry.Write([]byte(`<Relationships>` + test.relationship + `</Relationships>`))
			require.NoError(t, err)
			_, err = writer.Create("word/media/image1.png")
			require.NoError(t, err)
			entry, err = writer.Create("word/document.xml")
			require.NoError(t, err)
			_, err = entry.Write([]byte(`<w:document ` +
				`xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" ` +
				`xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">` +
				`<w:body><w:p><w:hyperlink r:id="rIdLink"><w:r><w:t>source link</w:t></w:r>` +
				`</w:hyperlink></w:p></w:body></w:document>`))
			require.NoError(t, err)
			if test.extra != "" {
				_, err = writer.Create(test.extra)
				require.NoError(t, err)
			}
			require.NoError(t, writer.Close())
			err = checkExactDOCXDependencies(buffer.Bytes())
			if test.valid {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, errPersonalExactDocument)
			}
		})
	}
}

func TestExactPersonalDOCXDependenciesHyperlinkRoles(t *testing.T) {
	const relationship = `<Relationship Id="rIdLink" TargetMode="External" Target="https://example.invalid/a" ` +
		`Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink"/>`
	const prefix = `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" ` +
		`xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">`
	for _, test := range []struct {
		name, body, extraPart, extraBody, extraRelationship string
	}{
		{"image reference", `<w:drawing r:embed="rIdLink"/>`, "", "", ""},
		{"object reference", `<w:object r:id="rIdLink"/>`, "", "", ""},
		{"wrong namespace", `<hyperlink r:id="rIdLink"/>`, "", "", ""},
		{"wrong attribute", `<w:hyperlink r:embed="rIdLink"/>`, "", "", ""},
		{"unused relationship", `<w:p/>`, "", "", ""},
		{"duplicate ID", `<w:hyperlink r:id="rIdLink"/>`, "", "", relationship},
		{
			"another part references ID", `<w:hyperlink r:id="rIdLink"/>`,
			"word/header1.xml", prefix + `<w:hyperlink r:id="rIdLink"/></w:document>`, "",
		},
		{
			"valid link plus object", `<w:hyperlink r:id="rIdLink"/><w:object r:id="rIdLink"/>`, "", "", "",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var buffer bytes.Buffer
			writer := zip.NewWriter(&buffer)
			parts := map[string]string{
				"word/_rels/document.xml.rels": "<Relationships>" +
					relationship + test.extraRelationship + "</Relationships>",
				"word/document.xml": prefix + test.body + "</w:document>",
			}
			if test.extraPart != "" {
				parts[test.extraPart] = test.extraBody
			}
			for name, content := range parts {
				entry, err := writer.Create(name)
				require.NoError(t, err)
				_, err = entry.Write([]byte(content))
				require.NoError(t, err)
			}
			require.NoError(t, writer.Close())
			require.ErrorIs(t, checkExactDOCXDependencies(buffer.Bytes()), errPersonalExactDocument)
		})
	}
}
