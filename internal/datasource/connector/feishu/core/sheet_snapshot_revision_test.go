package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/datasource"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestSheetSnapshotProofBindsLiveRevisionBytesAndSource(t *testing.T) {
	t.Setenv("KNOWHUB_FEISHU_LATEST_CONTENT", "true")
	var revision atomic.Int64
	revision.Store(3030)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer fixture-application", r.Header.Get("Authorization"))
		_, _ = fmt.Fprintf(w, `{"code":0,"data":{"properties":{"revision":%d}}}`, revision.Load())
	}))
	defer srv.Close()
	client := NewClient(&Config{AppID: "sheet-proof", BaseURL: srv.URL})
	ctx := WithApplicationIdentity(context.Background(), func(context.Context) (string, error) {
		return "fixture-application", nil
	})
	fetch := func() (*types.FetchedItem, error) {
		return &types.FetchedItem{
			ExternalID: "node", SourceResourceID: "root", Title: "sheet", FileName: "sheet.xlsx",
			URL: "https://example.feishu.cn/wiki/node", UpdatedAt: time.Unix(100, 0), Content: []byte("export"),
			Metadata: map[string]string{"obj_type": "sheet", "obj_token": "sheet", "node_token": "node"},
		}, nil
	}
	first, err := client.WithSheetSnapshotProof(ctx, "sheet", "sheet", fetch)
	require.NoError(t, err)
	second, err := client.WithSheetSnapshotProof(ctx, "sheet", "sheet", fetch)
	require.NoError(t, err)
	require.True(t, SameImmutableSheetRevision(first, second))
	for _, mutate := range []func(*types.FetchedItem){
		func(x *types.FetchedItem) { x.Content = []byte("changed") },
		func(x *types.FetchedItem) { x.SourceResourceID = "other-root" },
		func(x *types.FetchedItem) { x.Metadata["obj_token"] = "other-sheet" },
		func(x *types.FetchedItem) { x.Metadata["feishu_sheet_revision"] = "999" },
		func(x *types.FetchedItem) { x.Title = "other title" },
		func(x *types.FetchedItem) { x.FileName = "other.xlsx" },
		func(x *types.FetchedItem) { x.ExportSnapshotProof = map[string]any{"revision": 3030} },
	} {
		candidate := *second
		candidate.Metadata = maps.Clone(second.Metadata)
		mutate(&candidate)
		require.False(t, SameImmutableSheetRevision(first, &candidate))
	}
	raw, err := json.Marshal(second)
	require.NoError(t, err)
	var decoded types.FetchedItem
	require.NoError(t, json.Unmarshal(raw, &decoded))
	require.Nil(t, decoded.ExportSnapshotProof)
	require.False(t, SameImmutableSheetRevision(first, &decoded))
	revision.Store(3031)
	third, err := client.WithSheetSnapshotProof(ctx, "sheet", "sheet", fetch)
	require.NoError(t, err)
	require.True(t, first.UpdatedAt.Equal(third.UpdatedAt))
	require.False(t, SameImmutableSheetRevision(first, third), "same-second edits must remain fenced")
	_, err = client.WithSheetSnapshotProof(ctx, "sheet", "sheet", func() (*types.FetchedItem, error) {
		revision.Add(1)
		return fetch()
	})
	var failure *datasource.DocumentFailureError
	require.True(t, errors.As(err, &failure))
	require.Equal(t, "source_snapshot_changed", failure.Code)
}

func TestSheetSnapshotMissingMetadataDoesNotIssueAuthority(t *testing.T) {
	t.Setenv("KNOWHUB_FEISHU_LATEST_CONTENT", "true")
	for _, body := range []string{
		`{"code":0,"data":{"properties":{}}}`,
		`{"code":99991672,"msg":"missing optional sheet scope"}`,
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = fmt.Fprint(w, body)
		}))
		client := NewClient(&Config{BaseURL: srv.URL})
		ctx := WithApplicationIdentity(context.Background(), func(context.Context) (string, error) {
			return "fixture-application", nil
		})
		item, err := client.WithSheetSnapshotProof(ctx, "sheet", "sheet", func() (*types.FetchedItem, error) {
			return &types.FetchedItem{Content: []byte("existing export")}, nil
		})
		srv.Close()
		require.NoError(t, err)
		require.Nil(t, item.ExportSnapshotProof)
	}
}
