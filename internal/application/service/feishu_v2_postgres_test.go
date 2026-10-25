package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func v2PostgresFixture(t *testing.T) *personalRecoveryFixture {
	t.Helper()
	t.Setenv("FEISHU_ACCESS_V2_ENABLED", "1")
	f := newMVPFixture(t)
	// This admission fixture has no parser/index worker. Supply the deployed
	// vector column type for read-only progress counts; native tests exercise
	// the complete embedding schema, BM25 and publication receipts.
	require.NoError(t, f.db.Exec("CREATE EXTENSION IF NOT EXISTS vector WITH SCHEMA public").Error)
	require.NoError(t, f.db.Exec("CREATE TABLE embeddings(knowledge_id varchar(64),embedding public.halfvec)").Error)
	ddl, err := os.ReadFile("../../../migrations/versioned/003039_feishu_access_v2.up.sql")
	require.NoError(t, err)
	require.NoError(t, f.db.Exec(string(ddl)).Error)
	ddl, err = os.ReadFile("../../../migrations/versioned/003040_feishu_permission_collaboration.up.sql")
	require.NoError(t, err)
	require.NoError(t, f.db.Exec(string(ddl)).Error)
	ddl, err = os.ReadFile("../../../migrations/versioned/003041_feishu_collections.up.sql")
	require.NoError(t, err)
	require.NoError(t, f.db.Exec(string(ddl)).Error)
	f.db = types.EnablePrivateKBGuards(f.db)
	f.s.db = f.db
	cfg, err := f.s.app(f.ctx)
	require.NoError(t, err)
	cfg.Capabilities = &types.FeishuAccessCapabilities{PersonalEnabled: true, ApplicationEnabled: true}
	admin := context.WithValue(f.ctx, types.SystemAdminContextKey, true)
	require.NoError(t, f.s.SaveApp(admin, cfg))
	return f
}

type batchProgressInspector struct {
	interfaces.TaskInspector
	ids map[string]struct{}
	err error
}

func (p *batchProgressInspector) QueuedKnowledgeIDs(context.Context) (map[string]struct{}, error) {
	return p.ids, p.err
}

func TestFeishuV2PostgresStallRequiresAbsentLeaseHeartbeatAndQueue(t *testing.T) {
	f := v2PostgresFixture(t)
	_, _, ref, err := f.s.ResolveCredentialReference(f.ctx, types.FeishuUserOAuth, "")
	require.NoError(t, err)
	link := "https://test.feishu.cn/file/real"
	_, err = f.s.ApproveSharing(f.ctx, "kb", ref.ConnectionID, link, "controlled test", true)
	require.NoError(t, err)
	view, err := f.s.QuickImport(f.ctx, "kb", "stall-check", []string{link})
	require.NoError(t, err)
	require.NoError(t, f.s.ReconcileBatches(context.Background()))
	view, err = f.s.Batch(f.ctx, view.Batch.ID)
	require.NoError(t, err)
	importID := view.Items[0].ImportID
	var attempt types.FeishuPersonalAttempt
	require.NoError(t, f.db.Where("import_id = ?", importID).First(&attempt).Error)
	old := time.Now().Add(-20 * time.Minute)
	probe := &batchProgressInspector{ids: map[string]struct{}{}}
	f.ks.taskInspector = probe
	for _, tc := range []struct {
		name      string
		lease     bool
		heartbeat bool
		queued    bool
		unknown   bool
		stalled   bool
	}{
		{"abandoned", false, false, false, false, true},
		{"long document lease", true, false, false, false, false},
		{"worker heartbeat", false, true, false, false, false},
		{"queued or active", false, false, true, false, false},
		{"queue unavailable", false, false, false, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var lease *time.Time
			if tc.lease {
				until := time.Now().Add(time.Minute)
				lease = &until
			}
			activity := old
			if tc.heartbeat {
				activity = time.Now()
			}
			probe.ids = map[string]struct{}{}
			if tc.queued {
				probe.ids[attempt.CandidateVersionID] = struct{}{}
			}
			probe.err = nil
			if tc.unknown {
				probe.err = errors.New("queue unavailable")
			}
			require.NoError(t, f.db.Model(&types.FeishuPersonalImport{}).Where("id = ?", importID).
				UpdateColumn("updated_at", old).Error)
			require.NoError(t, f.db.Model(&types.FeishuPersonalAttempt{}).Where("id = ?", attempt.ID).
				UpdateColumns(map[string]interface{}{"updated_at": activity, "lease_until": lease}).Error)
			// Reconciler claims are not downstream worker activity.
			until := time.Now().Add(time.Minute)
			require.NoError(t, f.db.Model(&types.FeishuBatchItem{}).Where("id = ?", view.Items[0].ID).
				UpdateColumns(map[string]interface{}{"updated_at": time.Now(), "lease_until": &until}).Error)
			current, e := f.s.Batch(f.ctx, view.Batch.ID)
			require.NoError(t, e)
			require.Equal(t, tc.stalled, current.Items[0].Stalled)
			require.Equal(t, "dispatched", current.Items[0].Status, "observing a stall never cancels the job")
		})
	}
}

func TestFeishuV2PostgresSharedAdmissionRequiresRevisionApproval(t *testing.T) {
	f := v2PostgresFixture(t)
	_, _, ref, err := f.s.ResolveCredentialReference(f.ctx, types.FeishuUserOAuth, "")
	require.NoError(t, err)
	links := []string{"https://test.feishu.cn/file/real"}
	preview, err := f.s.Preview(f.ctx, "kb", types.FeishuUserOAuth, ref.ConnectionID, links)
	require.NoError(t, err)
	require.Equal(t, "sharing_approval_required", preview[0].ErrorCategory)
	_, err = f.s.ImportWithIdentity(f.ctx, "kb", links[0], types.FeishuUserOAuth)
	require.ErrorIs(t, err, ErrFeishuSharingApproval)
	_, err = f.s.ApproveSharing(
		f.ctx,
		"kb",
		ref.ConnectionID,
		links[0],
		"controlled shared publication",
		false,
	)
	require.Error(t, err)
	approval, err := f.s.ApproveSharing(
		f.ctx,
		"kb",
		ref.ConnectionID,
		links[0],
		"controlled shared publication",
		true,
	)
	require.NoError(t, err)
	m, err := f.s.ImportWithIdentity(f.ctx, "kb", links[0], types.FeishuUserOAuth)
	require.NoError(t, err)
	require.NotEmpty(t, m.AttemptID)
	history, err := f.s.ListSharingApprovals(f.ctx, "kb", 0)
	require.NoError(t, err)
	require.Len(t, history, 1)
	require.Equal(t, approval.ID, history[0].ID)
	other, err := f.s.ListSharingApprovals(f.userContext("B"), "kb", 0)
	require.NoError(t, err)
	require.Empty(t, other)
	require.Error(t, f.s.RevokeSharingApproval(f.userContext("B"), "kb", approval.ID))
	// Revocation closes the existing attempt's next authorization, preserving
	// the original records instead of resetting any lease or cursor.
	require.NoError(t, f.s.RevokeSharingApproval(f.ctx, "kb", approval.ID))
	require.NoError(t, f.s.RevokeSharingApproval(f.ctx, "kb", approval.ID), "revocation is idempotent")
	_, _, attempt, err := f.p.repo.Load(f.ctx, m.AttemptID)
	require.NoError(t, err)
	_, err = f.p.authorize(f.ctx, m, attempt)
	require.ErrorIs(t, err, ErrFeishuSharingApproval)
	var count int64
	require.NoError(t, f.db.Model(&types.Knowledge{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestFeishuV2PostgresCollaborationNeverGrantsOrRetries(t *testing.T) {
	f := v2PostgresFixture(t)
	_, _, ref, err := f.s.ResolveCredentialReference(f.ctx, types.FeishuUserOAuth, "")
	require.NoError(t, err)
	view, err := f.s.CreateBatch(
		f.ctx,
		"kb",
		types.FeishuUserOAuth,
		ref.ConnectionID,
		"collaboration",
		[]string{"https://test.feishu.cn/file/real"},
	)
	require.NoError(t, err)
	require.NoError(t, f.s.ReconcileBatches(context.Background()))
	view, err = f.s.Batch(f.ctx, view.Batch.ID)
	require.NoError(t, err)
	require.Equal(t, "blocked", view.Items[0].Status)
	item := view.Items[0]
	row, err := f.s.Collaborate(
		f.ctx,
		view.Batch.ID,
		item.ID,
		"reported",
		"Document owner",
		"Owner reports access updated",
	)
	require.NoError(t, err)
	require.Equal(t, "reported_unverified", row.Result)
	row, err = f.s.Collaborate(f.ctx, view.Batch.ID, item.ID, "recheck", "Document owner", "")
	require.NoError(t, err)
	require.Equal(t, "sharing_approval_required", row.Result)
	_, err = f.s.Collaboration(f.userContext("B"), view.Batch.ID)
	require.Error(t, err)
	_, err = f.s.Collaborate(f.userContext("B"), view.Batch.ID, item.ID, "reported", "", "must deny")
	require.Error(t, err)
	_, err = f.s.Collaborate(f.ctx, view.Batch.ID, "another-item", "reported", "", "must deny")
	require.Error(t, err)
	rows, err := f.s.Collaboration(f.ctx, view.Batch.ID)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	var current types.FeishuBatchItem
	require.NoError(t, f.db.Where("id = ?", item.ID).First(&current).Error)
	require.Equal(t, "blocked", current.Status)
	require.Zero(t, current.RetryCount)
	var approvals int64
	require.NoError(t, f.db.Model(&types.FeishuSharingApproval{}).Count(&approvals).Error)
	require.Zero(t, approvals)
}

func TestFeishuV2PostgresBatchConcurrentIdempotencyAndAliasRecovery(t *testing.T) {
	f := v2PostgresFixture(t)
	require.NoError(t, f.db.Exec("INSERT INTO feishu_private_kbs(kb_id,owner_id) VALUES('kb','A')").Error)
	_, _, ref, err := f.s.ResolveCredentialReference(f.ctx, types.FeishuUserOAuth, "")
	require.NoError(t, err)
	links := []string{"https://test.feishu.cn/file/real", "https://test.feishu.cn/wiki/node"}
	var ids [2]string
	var errs [2]error
	var wg sync.WaitGroup
	for i := range ids {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			view, e := f.s.CreateBatch(
				f.ctx,
				"kb",
				types.FeishuUserOAuth,
				ref.ConnectionID,
				"same-request",
				links,
			)
			errs[i] = e
			if view != nil {
				ids[i] = view.Batch.ID
			}
		}(i)
	}
	wg.Wait()
	require.NoError(t, errs[0])
	require.NoError(t, errs[1])
	require.Equal(t, ids[0], ids[1])
	history, err := f.s.ListBatches(f.ctx, "kb", 0)
	require.NoError(t, err)
	require.Len(t, history, 1)
	require.Equal(t, ids[0], history[0].ID)
	otherHistory, err := f.s.ListBatches(f.userContext("B"), "kb", 0)
	require.NoError(t, err)
	require.Empty(t, otherHistory, "batch recovery must not reveal another owner's handles")
	_, err = f.s.ListBatches(f.ctx, "kb", -1)
	require.Error(t, err)
	_, err = f.s.CreateBatch(f.ctx, "kb", types.FeishuUserOAuth, ref.ConnectionID, "same-request", links[:1])
	require.Error(t, err)
	// Two workers share the real PostgreSQL claim and canonical import locks.
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if e := f.s.ReconcileBatches(context.Background()); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	view, err := f.s.Batch(f.ctx, ids[0])
	require.NoError(t, err)
	require.Equal(t, 2, view.Counts["dispatched"], "aliases cannot claim completion before indexing")
	var imports int64
	require.NoError(t, f.db.Model(&types.FeishuPersonalImport{}).Count(&imports).Error)
	require.EqualValues(t, 1, imports)
	_, err = f.s.Batch(f.userContext("B"), ids[0])
	require.Error(t, err)
	// Replay after worker/reconciler restart cannot allocate another import.
	require.NoError(t, f.s.ReconcileBatches(context.Background()))
	require.NoError(t, f.db.Model(&types.FeishuPersonalImport{}).Count(&imports).Error)
	require.EqualValues(t, 1, imports)
}

func TestFeishuV2PostgresDirectBatchIsolatesInvalidAndRestoresLease(t *testing.T) {
	f := v2PostgresFixture(t)
	require.NoError(t, f.db.Exec("INSERT INTO feishu_private_kbs(kb_id,owner_id) VALUES('kb','A')").Error)
	_, _, ref, err := f.s.ResolveCredentialReference(f.ctx, types.FeishuUserOAuth, "")
	require.NoError(t, err)
	links := []string{
		"https://test.feishu.cn/file/real?from=share",
		"https://test.feishu.cn/file/real",
		"http://127.0.0.1/file/private",
		"",
	}
	view, err := f.s.CreateBatch(
		f.ctx,
		"kb",
		types.FeishuUserOAuth,
		ref.ConnectionID,
		"direct-no-preview",
		links,
	)
	require.NoError(t, err)
	require.Len(t, view.Items, 2)
	require.Equal(t, "pending", view.Items[0].Status)
	require.Equal(t, "blocked", view.Items[1].Status)
	require.Equal(t, "invalid_feishu_url", view.Items[1].ErrorCategory)
	// A crashed worker's durable claim expires; a replacement worker picks it up.
	require.NoError(t, f.db.Model(&types.FeishuBatchItem{}).Where("id = ?", view.Items[0].ID).
		Updates(map[string]any{
			"status": "processing", "lease_owner": "crashed-worker",
			"lease_until": time.Now().Add(-time.Minute),
		}).
		Error)
	require.NoError(t, f.s.ReconcileBatches(context.Background()))
	view, err = f.s.Batch(f.ctx, view.Batch.ID)
	require.NoError(t, err)
	require.Equal(t, "dispatched", view.Items[0].Status)
	require.NotEmpty(t, view.Items[0].ImportID)
	require.Equal(t, "blocked", view.Items[1].Status)
	repeated, err := f.s.CreateBatch(
		f.ctx,
		"kb",
		types.FeishuUserOAuth,
		ref.ConnectionID,
		"direct-no-preview",
		links,
	)
	require.NoError(t, err)
	require.Equal(t, view.Batch.ID, repeated.Batch.ID)
	require.Equal(t, view.Items[0].ImportID, repeated.Items[0].ImportID)
}

func TestFeishuV2PostgresBatchAutomaticallyRetriesOnlyTemporaryFailures(t *testing.T) {
	for _, code := range []int{http.StatusServiceUnavailable, http.StatusForbidden} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			f := v2PostgresFixture(t)
			require.NoError(
				t,
				f.db.Exec("INSERT INTO feishu_private_kbs(kb_id,owner_id) VALUES('kb','A')").Error,
			)
			server := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(code) }),
			)
			t.Cleanup(server.Close)
			original := f.s.newClient
			f.s.newClient = func(cfg *core.Config) *core.Client { cfg.BaseURL = server.URL; return core.NewClient(cfg) }
			view, err := f.s.CreateBatch(
				f.ctx,
				"kb",
				types.FeishuUserOAuth,
				"",
				"retry-classification",
				[]string{"https://test.feishu.cn/file/real"},
			)
			require.NoError(t, err)
			require.NoError(t, f.s.ReconcileBatches(context.Background()))
			view, err = f.s.Batch(f.ctx, view.Batch.ID)
			require.NoError(t, err)
			item := view.Items[0]
			if code == http.StatusForbidden {
				require.Equal(t, "blocked", item.Status)
				require.Zero(t, item.RetryCount)
				f.s.newClient = original
				require.NoError(t, f.s.ReconcileBatches(context.Background()))
				var current types.FeishuBatchItem
				require.NoError(t, f.db.Where("id = ?", item.ID).First(&current).Error)
				require.Equal(t, "blocked", current.Status)
				return
			}
			require.Equal(t, "pending", item.Status)
			require.Equal(t, 1, item.RetryCount)
			require.True(t, item.UpdatedAt.After(time.Now()))
			// Simulate the durable retry becoming due after a worker restart.
			f.s.newClient = original
			require.NoError(
				t,
				f.db.Model(&types.FeishuBatchItem{}).
					Where("id = ?", item.ID).
					Update("updated_at", time.Now().Add(-time.Second)).
					Error,
			)
			require.NoError(t, f.s.ReconcileBatches(context.Background()))
			view, err = f.s.Batch(f.ctx, view.Batch.ID)
			require.NoError(t, err)
			require.Equal(t, "dispatched", view.Items[0].Status)
			require.Equal(t, 1, view.Items[0].RetryCount)
		})
	}
}

type v2QuickKB struct {
	interfaces.KnowledgeBaseService
	db *gorm.DB
}

func (s *v2QuickKB) GetKnowledgeBaseByID(ctx context.Context, id string) (*types.KnowledgeBase, error) {
	return repository.NewKnowledgeBaseRepository(s.db).GetKnowledgeBaseByID(ctx, id)
}

func (s *v2QuickKB) CreateKnowledgeBase(
	ctx context.Context,
	kb *types.KnowledgeBase,
) (*types.KnowledgeBase, error) {
	kb.ID = uuid.NewString()
	kb.TenantID = types.CallerFromContext(ctx).TenantID
	kb.CreatorID, _ = types.UserIDFromContext(ctx)
	return kb, repository.NewKnowledgeBaseRepository(s.db).CreateKnowledgeBase(ctx, kb)
}

func TestFeishuV2PostgresQuickImportRequiresExplicitTarget(t *testing.T) {
	f := v2PostgresFixture(t)
	require.NoError(t, f.db.AutoMigrate(&types.KnowledgeBase{}))
	f.s.kbs = &v2QuickKB{db: f.db}
	// Leave the ordinary existing KB public. It must never be relabeled or used.
	var results [2]*FeishuBatchView
	var errs [2]error
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = f.s.QuickImport(
				f.ctx,
				"",
				"quick-once",
				[]string{"https://test.feishu.cn/file/real"},
			)
		}(i)
	}
	wg.Wait()
	require.ErrorIs(t, errs[0], ErrFeishuTargetRequired)
	require.ErrorIs(t, errs[1], ErrFeishuTargetRequired)
	require.Nil(t, results[0])
	var count int64
	require.NoError(t, f.db.Table("feishu_private_kbs").Where("owner_id = ?", "A").Count(&count).Error)
	require.Zero(t, count)
	kb, err := f.s.CreatePrivateKnowledgeBase(f.ctx, "explicit-private")
	require.NoError(t, err)
	results[0], err = f.s.QuickImport(
		f.ctx,
		kb.ID,
		"explicit-target",
		[]string{"https://test.feishu.cn/file/real"},
	)
	require.NoError(t, err)
	kbID := results[0].Batch.KBID
	require.Equal(t, kb.ID, kbID)
	owner, err := types.PrivateKBOwner(f.db, kbID)
	require.NoError(t, err)
	require.Equal(t, "A", owner)
	owner, err = types.PrivateKBOwner(f.db, "kb")
	require.NoError(t, err)
	require.Empty(t, owner)
	_, err = f.s.kbs.GetKnowledgeBaseByID(f.userContext("B"), kbID)
	require.Error(t, err)
	require.NoError(t, f.s.ReconcileBatches(context.Background()))
	view, err := f.s.Batch(f.ctx, results[0].Batch.ID)
	require.NoError(t, err)
	require.Equal(t, "dispatched", view.Items[0].Status)
}

func TestFeishuV2PostgresBatchReportsActualFailureWithoutPollingProgress(t *testing.T) {
	f := v2PostgresFixture(t)
	_, _, ref, err := f.s.ResolveCredentialReference(f.ctx, types.FeishuUserOAuth, "")
	require.NoError(t, err)
	link := "https://test.feishu.cn/file/real"
	_, err = f.s.ApproveSharing(f.ctx, "kb", ref.ConnectionID, link, "controlled test", true)
	require.NoError(t, err)
	view, err := f.s.QuickImport(f.ctx, "kb", "actual-progress", []string{link})
	require.NoError(t, err)
	require.Equal(t, "queued", view.Items[0].Stage)
	require.NoError(t, f.s.ReconcileBatches(context.Background()))
	view, err = f.s.Batch(f.ctx, view.Batch.ID)
	require.NoError(t, err)
	lastProgress := *view.Items[0].LastProgressAt
	require.False(t, view.Items[0].IndexVerified)
	require.Equal(t, "dispatched", view.Items[0].Status)
	require.NoError(t, f.db.Model(&types.FeishuBatchItem{}).Where("id = ?", view.Items[0].ID).
		Update("updated_at", time.Now().Add(time.Hour)).Error)
	view, err = f.s.Batch(f.ctx, view.Batch.ID)
	require.NoError(t, err)
	require.True(t, lastProgress.Equal(*view.Items[0].LastProgressAt))
	require.NoError(
		t,
		f.db.Model(&types.FeishuPersonalAttempt{}).Where("import_id = ?", view.Items[0].ImportID).
			Updates(map[string]interface{}{
				"status": "needs_attention", "last_error_category": "storage_permission_denied",
			}).
			Error,
	)
	view, err = f.s.Batch(f.ctx, view.Batch.ID)
	require.NoError(t, err)
	require.Equal(t, "failed", view.Items[0].Status)
	require.Equal(t, "needs_attention", view.Items[0].Stage)
	require.Equal(t, "storage_permission_denied", view.Items[0].ErrorCategory)
}
