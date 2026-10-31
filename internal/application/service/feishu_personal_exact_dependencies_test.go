package service

import (
	"archive/zip"
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

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
