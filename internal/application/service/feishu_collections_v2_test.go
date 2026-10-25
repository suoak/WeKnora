package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestFeishuV2PostgresCollectionPageAdmissionAndRestart(t *testing.T) {
	f := v2PostgresFixture(t)
	require.NoError(t, f.db.Exec("INSERT INTO feishu_private_kbs(kb_id,owner_id) VALUES('kb','A')").Error)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer access-A" || r.URL.Path != "/open-apis/drive/v1/files" {
			http.Error(w, "wrong identity or path", 403)
			return
		}
		more := r.URL.Query().Get("page_token") == ""
		token := "first"
		if !more {
			token = "second"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{
			"files": []map[string]any{{
				"token": token, "name": token, "type": "file",
				"url": "https://test.feishu.cn/file/" + token,
			}}, "has_more": more, "next_page_token": "second-page",
		}})
	}))
	defer server.Close()
	f.s.newClient = func(cfg *core.Config) *core.Client { cfg.BaseURL = server.URL; return core.NewClient(cfg) }
	q := FeishuResourceQuery{AuthMode: types.FeishuUserOAuth, Kind: "drive", Parent: "root"}
	row, err := f.s.CreateCollection(f.ctx, "kb", q)
	require.NoError(t, err)
	duplicate, err := f.s.CreateCollection(f.ctx, "kb", q)
	require.NoError(t, err)
	require.Equal(t, row.ID, duplicate.ID)
	require.NoError(t, f.s.ReconcileCollections(context.Background()))
	var stored types.FeishuCollection
	require.NoError(t, f.db.Where("id = ?", row.ID).First(&stored).Error)
	require.Equal(t, 1, stored.Pages)
	var pages []types.FeishuCollectionPage
	require.NoError(t, f.db.Where("collection_id = ?", row.ID).Find(&pages).Error)
	require.Len(t, pages, 1)
	require.NotNil(t, pages[0].BatchID)
	visible, err := f.s.CollectionPages(f.ctx, "kb", row.ID, 0)
	require.NoError(t, err)
	require.Equal(t, []string{"first"}, visible[0].Links[0].Path)
	_, err = f.s.CollectionPages(f.userContext("B"), "kb", row.ID, 0)
	require.Error(t, err)
	var paths []core.DiscoveredLink
	require.NoError(t, openFeishu(pages[0].Provenance, &paths))
	require.Equal(t, []string{"first"}, paths[0].Path)
	// A new service instance continues the durable second provider page.
	restarted := *f.s
	require.NoError(t, restarted.ReconcileCollections(context.Background()))
	require.NoError(t, f.db.Where("id = ?", row.ID).First(&stored).Error)
	require.Equal(t, 2, stored.Pages)
	require.Equal(t, int64(1), stored.Cycle)
	require.True(t, stored.NextRunAt.After(time.Now().Add(14*time.Minute)))
	require.NoError(t, f.db.Where("collection_id = ?", row.ID).Find(&pages).Error)
	require.Len(t, pages, 2)
	var batches int64
	require.NoError(t, f.db.Model(&types.FeishuBatch{}).Count(&batches).Error)
	require.EqualValues(t, 2, batches)
	require.NoError(t, restarted.ReconcileCollections(context.Background()))
	require.EqualValues(t, 2, calls.Load(), "completed discovery waits before starting a fresh observation cycle")
	other, err := f.s.ListCollections(f.userContext("B"), "kb", 0)
	require.NoError(t, err)
	require.Empty(t, other)
	require.Error(t, f.s.CollectionAction(f.userContext("B"), "kb", row.ID, "pause"))
	require.NoError(t, f.s.CollectionAction(f.ctx, "kb", row.ID, "pause"))
	duplicate, err = f.s.CreateCollection(f.ctx, "kb", q)
	require.NoError(t, err)
	require.Equal(t, "paused", duplicate.State)
	require.NoError(t, f.s.ReconcileCollections(context.Background()))
	require.EqualValues(t, 2, calls.Load())
}

func TestFeishuV2PostgresCollectionRollbackAndPauseFence(t *testing.T) {
	for _, fault := range []string{"page_insert", "paused_during_read"} {
		t.Run(fault, func(t *testing.T) {
			f := v2PostgresFixture(t)
			require.NoError(t, f.db.Exec("INSERT INTO feishu_private_kbs(kb_id,owner_id) VALUES('kb','A')").Error)
			started, release := make(chan struct{}), make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				close(started)
				<-release
				_, _ = w.Write([]byte(`{"code":0,"data":{"files":[{"token":"real","type":"file",` +
					`"url":"https://test.feishu.cn/file/real"}],"has_more":false}}`))
			}))
			defer server.Close()
			f.s.newClient = func(cfg *core.Config) *core.Client { cfg.BaseURL = server.URL; return core.NewClient(cfg) }
			row, err := f.s.CreateCollection(f.ctx, "kb", FeishuResourceQuery{
				AuthMode: types.FeishuUserOAuth, Kind: "drive", Parent: "root",
			})
			require.NoError(t, err)
			if fault == "page_insert" {
				require.NoError(t, f.db.Exec(`CREATE FUNCTION reject_collection_page() RETURNS trigger AS $$
				BEGIN RAISE EXCEPTION 'injected page commit failure'; END; $$ LANGUAGE plpgsql;
				CREATE TRIGGER reject_page BEFORE INSERT ON feishu_collection_pages
				FOR EACH ROW EXECUTE FUNCTION reject_collection_page();`).Error)
			}
			done := make(chan error, 1)
			go func() { done <- f.s.ReconcileCollections(context.Background()) }()
			<-started
			// A second worker cannot acquire the page while its lease is live.
			require.NoError(t, f.s.ReconcileCollections(context.Background()))
			if fault == "paused_during_read" {
				require.NoError(t, f.s.CollectionAction(f.ctx, "kb", row.ID, "pause"))
			}
			close(release)
			require.NoError(t, <-done)
			var current types.FeishuCollection
			require.NoError(t, f.db.Where("id = ?", row.ID).First(&current).Error)
			require.Equal(t, row.Frontier, current.Frontier, "never acknowledge an uncommitted page")
			var count int64
			require.NoError(t, f.db.Model(&types.FeishuBatch{}).Count(&count).Error)
			require.Zero(t, count, "batch and frontier must roll back together")
			if fault == "page_insert" {
				require.Equal(t, "hold", current.State)
				require.Error(t, f.s.CollectionAction(f.ctx, "kb", row.ID, "resume"))
			} else {
				require.Equal(t, "paused", current.State)
			}
		})
	}
}
