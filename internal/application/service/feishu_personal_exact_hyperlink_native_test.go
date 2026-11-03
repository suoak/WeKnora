//go:build anydoc

package service

import (
	"archive/zip"
	"bytes"
	"html"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/Tencent/WeKnora/internal/infrastructure/docparser/anydoc"
	"github.com/stretchr/testify/require"
)

func TestExactPersonalDOCXHyperlinkNativeNeverFetches(t *testing.T) {
	require.True(t, anydoc.Available(), "this gate requires the pinned native parser")
	for _, secure := range []bool{false, true} {
		t.Run(map[bool]string{false: "http", true: "https"}[secure], func(t *testing.T) {
			var connections atomic.Int32
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				t.Error("a literal hyperlink must never be fetched")
			}))
			server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
				if state == http.StateNew {
					connections.Add(1)
				}
			}
			if secure {
				server.StartTLS()
			} else {
				server.Start()
			}
			defer server.Close()
			target := server.URL + "/must-not-be-fetched"
			parts := map[string]string{
				"[Content_Types].xml": `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
					`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
					`<Override PartName="/word/document.xml" ` +
					`ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`,
				"_rels/.rels": `<Relationships ` +
					`xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
					`<Relationship Id="rId1" ` +
					`Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" ` +
					`Target="word/document.xml"/></Relationships>`,
				"word/document.xml": `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" ` +
					`xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><w:body><w:p>` +
					`<w:r><w:t>Verified local document text. </w:t></w:r><w:hyperlink r:id="rIdLink">` +
					`<w:r><w:t>Distinctive source link label</w:t></w:r></w:hyperlink></w:p></w:body></w:document>`,
				"word/_rels/document.xml.rels": `<Relationships ` +
					`xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
					`<Relationship Id="rIdLink" ` +
					`Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink" ` +
					`TargetMode="External" Target="` + html.EscapeString(target) + `"/></Relationships>`,
			}
			var buffer bytes.Buffer
			writer := zip.NewWriter(&buffer)
			for name, body := range parts {
				entry, err := writer.Create(name)
				require.NoError(t, err)
				_, err = entry.Write([]byte(body))
				require.NoError(t, err)
			}
			require.NoError(t, writer.Close())
			require.NoError(t, checkExactDOCXDependencies(buffer.Bytes()))
			parsed, err := anydoc.Convert(buffer.Bytes(), anydoc.Options{Format: "docx", WithAssets: true})
			require.NoError(t, err)
			require.NotNil(t, parsed)
			require.NoError(t, parsed.AssetsError)
			require.Contains(t, parsed.Markdown, "Distinctive source link label")
			require.Contains(t, parsed.Markdown, target, "the source reference must not be silently discarded")
			require.Empty(t, parsed.Assets)
			require.Zero(t, connections.Load(), "the native parser must not dial a hyperlink target")
		})
	}
}
