package repository

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/utils"
	"github.com/stretchr/testify/require"
)

func revisionSnapshotXLSX(t *testing.T, descr, uid, image string) []byte {
	t.Helper()
	var result bytes.Buffer
	w := zip.NewWriter(&result)
	r, err := zip.NewReader(bytes.NewReader(identityXLSX(t, "10")), int64(len(identityXLSX(t, "10"))))
	require.NoError(t, err)
	parts := map[string][]byte{}
	for _, p := range r.File {
		reader, e := p.Open()
		require.NoError(t, e)
		parts[p.Name], e = io.ReadAll(reader)
		require.NoError(t, e)
		require.NoError(t, reader.Close())
	}
	const drawingNS = "http://schemas.openxmlformats.org/drawingml/2006/spreadsheetDrawing"
	const sheetNS = "http://schemas.openxmlformats.org/spreadsheetml/2006/main"
	const revisionNS = "http://schemas.microsoft.com/office/spreadsheetml/2014/revision"
	parts["xl/drawings/drawing1.xml"] = []byte(`<wsDr xmlns="` + drawingNS + `"><twoCellAnchor><pic>` +
		`<nvPicPr><cNvPr id="1" name="picture" descr="` + descr + `"/></nvPicPr></pic></twoCellAnchor></wsDr>`)
	parts["xl/comments1.xml"] = []byte(`<comments xmlns="` + sheetNS + `" xmlns:xr="` + revisionNS +
		`"><authors><author>Author</author></authors><commentList><comment ref="A1" authorId="0" xr:uid="` +
		uid + `"><text><t>comment</t></text></comment></commentList></comments>`)
	parts["xl/media/image.png"] = []byte(image)
	for name, content := range parts {
		p, e := w.Create(name)
		require.NoError(t, e)
		_, e = p.Write(content)
		require.NoError(t, e)
	}
	require.NoError(t, w.Close())
	return result.Bytes()
}

func TestFeishuSheetNonceEquivalenceRequiresLiveImmutableRevisionProof(t *testing.T) {
	t.Setenv("KNOWHUB_FEISHU_LATEST_CONTENT", "true")
	t.Setenv("SSRF_WHITELIST", "127.0.0.1")
	utils.ResetSSRFWhitelistForTest()
	t.Cleanup(utils.ResetSSRFWhitelistForTest)
	var revision atomic.Int64
	revision.Store(3030)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, `{"code":0,"data":{"properties":{"revision":%d}}}`, revision.Load())
	}))
	defer server.Close()
	client := core.NewClient(&core.Config{AppID: "revision-proof", BaseURL: server.URL})
	ctx := core.WithApplicationIdentity(context.Background(), func(context.Context) (string, error) {
		return "fixture-application", nil
	})
	fetch := func(description, uid, image string) *types.FetchedItem {
		item, err := client.WithSheetSnapshotProof(ctx, "sheet", "sheet", func() (*types.FetchedItem, error) {
			return &types.FetchedItem{
				ExternalID: "node", SourceResourceID: "root", FileName: "sheet.xlsx", Title: "sheet",
				URL: "https://example.feishu.cn/wiki/node", UpdatedAt: time.Unix(100, 0),
				Content:  revisionSnapshotXLSX(t, description, uid, image),
				Metadata: map[string]string{"obj_type": "sheet", "obj_token": "sheet"},
			}, nil
		})
		require.NoError(t, err)
		return item
	}
	firstUID := "{11111111-1111-1111-1111-111111111111}"
	secondUID := "{22222222-2222-2222-2222-222222222222}"
	original := fetch("aBcDef", firstUID, "same image")
	current := fetch("xYzAbC", secondUID, "same image")
	require.NoError(t, MatchLatestFetchedSnapshot(original, current))
	left, right := *original, *current
	left.ExportSnapshotProof, right.ExportSnapshotProof = nil, nil
	require.ErrorIs(t, MatchLatestFetchedSnapshot(&left, &right), ErrLegacyVersionFence)
	left.ExportSnapshotProof = map[string]any{"revision": 3030}
	right.ExportSnapshotProof = map[string]any{"revision": 3030}
	require.ErrorIs(t, MatchLatestFetchedSnapshot(&left, &right), ErrLegacyVersionFence)
	raw, err := json.Marshal(current)
	require.NoError(t, err)
	var decoded types.FetchedItem
	require.NoError(t, json.Unmarshal(raw, &decoded))
	require.ErrorIs(t, MatchLatestFetchedSnapshot(original, &decoded), ErrLegacyVersionFence)
	changedImage := fetch("xYzAbC", secondUID, "changed image")
	require.ErrorIs(t, MatchLatestFetchedSnapshot(original, changedImage), ErrLegacyVersionFence)
	authored := fetch("author supplied description", firstUID, "same image")
	changedDescription := fetch("changed authored description", secondUID, "same image")
	require.ErrorIs(t, MatchLatestFetchedSnapshot(authored, changedDescription), ErrLegacyVersionFence)
	// Even an authored six-letter alt text change is protected by the actual
	// document revision, including when obj_edit_time has only second precision.
	authoredSixLetters := fetch("router", firstUID, "same image")
	revision.Add(1)
	changedSixLetters := fetch("switch", secondUID, "same image")
	require.True(t, authoredSixLetters.UpdatedAt.Equal(changedSixLetters.UpdatedAt))
	require.ErrorIs(t, MatchLatestFetchedSnapshot(authoredSixLetters, changedSixLetters), ErrLegacyVersionFence)
	// A proof cannot be transplanted onto new bytes or another source binding.
	mutated := *current
	mutated.Content = original.Content
	require.ErrorIs(t, MatchLatestFetchedSnapshot(original, &mutated), ErrLegacyVersionFence)
	mutated = *current
	mutated.Metadata = maps.Clone(current.Metadata)
	mutated.Metadata["obj_token"] = "another-sheet"
	require.ErrorIs(t, MatchLatestFetchedSnapshot(original, &mutated), ErrLegacyVersionFence)
}
