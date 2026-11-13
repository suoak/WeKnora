package repository

import (
	"archive/zip"
	"bytes"
	"io"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestFeishuLatestSnapshotXLSXDeclarationOrder(t *testing.T) {
	pack := func(reverse, changed bool) []byte {
		base := identityXLSX(t, "10")
		reader, err := zip.NewReader(bytes.NewReader(base), int64(len(base)))
		require.NoError(t, err)
		var out bytes.Buffer
		writer := zip.NewWriter(&out)
		for _, part := range reader.File {
			stream, e := part.Open()
			require.NoError(t, e)
			body, e := io.ReadAll(stream)
			require.NoError(t, e)
			require.NoError(t, stream.Close())
			if part.Name == "[Content_Types].xml" {
				a := `<Default Extension="xml" ContentType="application/xml"/>`
				b := `<Default Extension="png" ContentType="image/png"/>`
				if reverse {
					a, b = b, a
				}
				body = []byte(`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
					a + b + `</Types>`)
			}
			if changed && part.Name == "xl/worksheets/sheet1.xml" {
				body = bytes.ReplaceAll(body, []byte("<v>42</v>"), []byte("<v>43</v>"))
			}
			w, e := writer.Create(part.Name)
			require.NoError(t, e)
			_, e = w.Write(body)
			require.NoError(t, e)
		}
		require.NoError(t, writer.Close())
		return out.Bytes()
	}
	original := types.FetchedItem{
		ExternalID: "sheet", FileName: "sheet.xlsx", Title: "Sheet", UpdatedAt: time.Now().UTC(),
		Content: pack(false, false), Metadata: map[string]string{"obj_type": "sheet", "obj_token": "bound"},
	}
	fresh := original
	fresh.Content = pack(true, false)
	require.NoError(t, MatchLatestFetchedSnapshot(&original, &fresh))
	fresh.Content = pack(true, true)
	require.ErrorIs(t, MatchLatestFetchedSnapshot(&original, &fresh), ErrLegacyVersionFence)
	fresh.Content = pack(true, false)
	fresh.UpdatedAt = fresh.UpdatedAt.Add(time.Second)
	require.ErrorIs(t, MatchLatestFetchedSnapshot(&original, &fresh), ErrLegacyVersionFence)
	fresh = original
	fresh.Content = pack(true, false)
	fresh.Metadata = map[string]string{"obj_type": "sheet", "obj_token": "rebound"}
	require.ErrorIs(t, MatchLatestFetchedSnapshot(&original, &fresh), ErrLegacyVersionFence)
}
