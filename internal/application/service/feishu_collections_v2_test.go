package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
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
			http.Error(w, "wrong identity or path", http.StatusForbidden)
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
	var paths feishuCollectionEvidence
	require.NoError(t, openFeishu(pages[0].Provenance, &paths))
	require.Equal(t, []string{"first"}, paths.Links[0].Path)
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
	require.NoError(t, f.s.CollectionAction(f.ctx, "kb", row.ID, "resume"))
	require.NoError(t, f.s.ReconcileCollections(context.Background()))
	require.NoError(t, f.db.Where("id = ?", row.ID).First(&stored).Error)
	require.Equal(t, int64(2), stored.Cycle, "an explicit resume starts the next observation cycle")
	require.Equal(t, 1, stored.Pages)
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

func TestFeishuV2PostgresCollectionDeniedChildPreservesHealthySiblings(t *testing.T) {
	f := v2PostgresFixture(t)
	require.NoError(t, f.db.Exec("INSERT INTO feishu_private_kbs(kb_id,owner_id) VALUES('kb','A')").Error)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer access-A" {
			http.Error(w, "wrong identity", http.StatusForbidden)
			return
		}
		var files []map[string]any
		switch r.URL.Query().Get("folder_token") {
		case "root":
			files = []map[string]any{{"token": "denied", "type": "folder"}, {"token": "healthy", "type": "folder"}}
		case "denied":
			http.Error(w, "permission denied", http.StatusForbidden)
			return
		case "healthy":
			files = []map[string]any{
				{"token": "document", "type": "file", "url": "https://test.feishu.cn/file/document"},
			}
		default:
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).
			Encode(map[string]any{"code": 0, "data": map[string]any{"files": files, "has_more": false}})
	}))
	defer server.Close()
	f.s.newClient = func(cfg *core.Config) *core.Client { cfg.BaseURL = server.URL; return core.NewClient(cfg) }
	row, err := f.s.CreateCollection(f.ctx, "kb", FeishuResourceQuery{
		AuthMode: types.FeishuUserOAuth, Kind: "drive", Parent: "root",
	})
	require.NoError(t, err)
	for i := 0; i < 3; i++ {
		require.NoError(t, f.s.ReconcileCollections(context.Background()))
	}
	var current types.FeishuCollection
	require.NoError(t, f.db.Where("id = ?", row.ID).First(&current).Error)
	require.True(t, current.Truncated)
	require.Equal(t, "source_permission_required", current.ErrorCategory)
	var frontier core.DiscoveryFrontier
	require.NoError(t, openFeishu(current.Frontier, &frontier))
	require.Empty(t, frontier.Queue)
	require.Len(t, frontier.Deferred, 1)
	require.Equal(t, "denied", frontier.Deferred[0].Parent)
	pages, err := f.s.CollectionPages(f.ctx, "kb", row.ID, 0)
	require.NoError(t, err)
	require.Len(t, pages, 3)
	require.NotNil(t, pages[0].BatchID, "healthy sibling still gets a normal import batch")
	require.Equal(t, []string{"healthy", "document"}, pages[0].Links[0].Path)
	require.Equal(t, []string{"denied"}, pages[1].BlockedPath)
	require.Equal(t, "source_permission_required", pages[1].ErrorCategory)
	require.NoError(t, f.s.CollectionAction(f.ctx, "kb", row.ID, "discover"))
	require.NoError(t, f.s.ReconcileCollections(context.Background()))
	require.NoError(t, f.db.Where("id = ?", row.ID).First(&current).Error)
	require.Equal(t, "hold", current.State, "a partial cycle cannot be automatically restarted as complete")
}

func TestFeishuV2PostgresDiscoveryNeverReentersManagedFirstCreate(t *testing.T) {
	f := v2PostgresFixture(t)
	require.NoError(t, f.db.Exec("INSERT INTO feishu_private_kbs(kb_id,owner_id) VALUES('kb','A')").Error)
	link := "https://test.feishu.cn/file/real"
	m, err := f.s.ImportWithIdentity(f.ctx, "kb", link, types.FeishuUserOAuth)
	require.NoError(t, err)
	require.NoError(t, f.p.Process(f.ctx, f.payload(m.AttemptID)))
	// This fixture tests post-handoff admission only. Actual handoff and
	// parser/index correctness have separate native PostgreSQL tests.
	require.NoError(t, f.db.Model(&types.FeishuPersonalImport{}).Where("id = ?", m.ID).
		Update("status", "source_managed").Error)
	require.NoError(t, f.db.Model(&types.FeishuPersonalAttempt{}).Where("id = ?", m.AttemptID).
		Update("status", "obsolete").Error)
	var before int64
	require.NoError(t, f.db.Model(&types.FeishuPersonalAttempt{}).Count(&before).Error)
	for _, revision := range []int64{1, 2} {
		f.revision.Store(revision)
		existing, err := f.s.ImportWithIdentity(f.ctx, "kb", link, types.FeishuUserOAuth)
		require.NoError(t, err)
		require.Equal(t, m.ID, existing.ID)
		require.Equal(t, m.LogicalKnowledgeID, existing.KnowledgeID)
		require.Equal(t, "source_managed", existing.Status)
	}
	_, _, ref, err := f.s.ResolveCredentialReference(f.ctx, types.FeishuUserOAuth, "")
	require.NoError(t, err)
	batch, err := f.s.CreateBatch(
		f.ctx,
		"kb",
		types.FeishuUserOAuth,
		ref.ConnectionID,
		"observed-again",
		[]string{link},
	)
	require.NoError(t, err)
	require.NoError(t, f.s.ReconcileBatches(context.Background()))
	batch, err = f.s.Batch(f.ctx, batch.Batch.ID)
	require.NoError(t, err)
	require.Equal(t, "existing", batch.Items[0].Status)
	var after int64
	require.NoError(t, f.db.Model(&types.FeishuPersonalAttempt{}).Count(&after).Error)
	require.Equal(t, before, after, "discovery cannot allocate a new first-create attempt after handoff")
	// Even an old/late admission bypassing the service fast path is fenced.
	var persisted types.FeishuPersonalImport
	require.NoError(t, f.db.Where("id = ?", m.ID).First(&persisted).Error)
	require.Equal(t, "1", persisted.SourceModified, "discovery cannot acknowledge a newer indexed revision")
	_, _, attempt, err := f.p.repo.Load(f.ctx, m.AttemptID)
	require.NoError(t, err)
	_, _, err = f.p.repo.Prepare(f.ctx, &persisted, attempt)
	require.ErrorIs(t, err, repository.ErrPersonalSourceManaged)
}

func TestFeishuV2PostgresCollectionTransientRetryBudget(t *testing.T) {
	f := v2PostgresFixture(t)
	t.Setenv("FEISHU_API_RETRY_MAX", "0") // Isolate the outer durable retry budget.
	require.NoError(t, f.db.Exec("INSERT INTO feishu_private_kbs(kb_id,owner_id) VALUES('kb','A')").Error)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		http.Error(w, "synthetic unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	f.s.newClient = func(cfg *core.Config) *core.Client { cfg.BaseURL = server.URL; return core.NewClient(cfg) }
	row, err := f.s.CreateCollection(f.ctx, "kb", FeishuResourceQuery{
		AuthMode: types.FeishuUserOAuth, Kind: "drive", Parent: "root",
	})
	require.NoError(t, err)
	var original core.DiscoveryFrontier
	require.NoError(t, openFeishu(row.Frontier, &original))
	for attempt := 1; attempt <= 3; attempt++ {
		if attempt > 1 {
			// Explicit user action advances only the due time, never the cursor.
			require.NoError(t, f.s.CollectionAction(f.ctx, "kb", row.ID, "discover"))
		}
		require.NoError(t, f.s.ReconcileCollections(context.Background()))
		var current types.FeishuCollection
		require.NoError(t, f.db.Where("id = ?", row.ID).First(&current).Error)
		var frontier core.DiscoveryFrontier
		require.NoError(t, openFeishu(current.Frontier, &frontier))
		require.Equal(t, attempt, frontier.RetryCount)
		require.Equal(t, original.Queue, frontier.Queue)
		require.Zero(t, frontier.Pages)
		if attempt < 3 {
			require.Equal(t, "active", current.State)
			require.True(t, current.NextRunAt.After(time.Now().Add(50*time.Second)))
		} else {
			require.Equal(t, "hold", current.State)
		}
	}
	require.NoError(t, f.s.ReconcileCollections(context.Background()))
	require.EqualValues(t, 3, calls.Load(), "exhausted pages must not retry forever")
	require.Error(t, f.s.CollectionAction(f.ctx, "kb", row.ID, "resume"))
}
