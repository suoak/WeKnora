package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func failedAcquisitionFixture(t *testing.T, claimed ...bool) (*personalRecoveryFixture, *FeishuBatchView,
	*types.FeishuPersonalAttempt,
) {
	t.Helper()
	f := v2PostgresFixture(t)
	require.NoError(t, f.db.AutoMigrate(&types.TaskDeadLetter{}, &types.StoredResource{}, &types.ResourceBinding{}))
	view, err := f.s.QuickImport(f.ctx, "kb", "failed-acquisition", []string{"https://test.feishu.cn/file/real"})
	require.NoError(t, err)
	require.NoError(t, f.s.ReconcileBatches(context.Background()))
	var item types.FeishuBatchItem
	require.NoError(t, f.db.Where("id=?", view.Items[0].ID).Take(&item).Error)
	require.NotEmpty(t, item.ImportID)
	var resource types.FeishuPersonalResource
	require.NoError(t, f.db.Where("import_id=?", item.ImportID).Take(&resource).Error)
	require.NoError(t, f.db.Model(&types.FeishuPersonalAttempt{}).Where("id=?", resource.CurrentAttemptID).
		Update("retry_count", 2).Error)
	if len(claimed) > 0 && claimed[0] {
		require.NoError(t, f.db.Model(&types.FeishuPersonalAttempt{}).Where("id=?", resource.CurrentAttemptID).
			Update("retry_count", 3).Error)
		normal := f.s.newClient
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/open-apis/drive/v1/metas/batch_query" {
				_, _ = fmt.Fprint(w, `{"code":0,"data":{"metas":[{"doc_type":"file",`+
					`"doc_token":"real","title":"test.pdf","latest_modify_time":"1"}]}}`)
				return
			}
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		t.Cleanup(server.Close)
		f.s.newClient = func(cfg *core.Config) *core.Client {
			cfg.BaseURL = server.URL
			return core.NewClient(cfg)
		}
		require.Error(t, f.p.Process(f.ctx, f.payload(resource.CurrentAttemptID)))
		f.s.newClient = normal
	} else {
		require.NoError(t, f.p.repo.Invalidate(f.ctx, resource.CurrentAttemptID, "retry_exhausted"))
	}
	attempt := mustPersonalAttempt(t, f, resource.CurrentAttemptID)
	require.Equal(t, "needs_attention", attempt.Status)
	return f, view, attempt
}

func TestFeishuV2PostgresPersonalAcquisitionReadmissionAfterExhaustedClaim(t *testing.T) {
	f, original, before := failedAcquisitionFixture(t, true)
	var resource types.FeishuPersonalResource
	require.NoError(t, f.db.Where("import_id=?", before.ImportID).Take(&resource).Error)
	require.Equal(t, before.Fence, resource.Generation, "normal terminal source failure keeps its retired generation")
	require.Empty(t, before.OwnerNonce)
	require.Nil(t, before.LeaseUntil)
	require.NoError(t, f.s.RetryBatch(f.ctx, original.Batch.ID))
	require.NoError(t, f.s.ReconcileBatches(context.Background()))
	next := mustPersonalAttempt(t, f, before.ID)
	require.Equal(t, before.RetryCount, next.RetryCount)
	require.NoError(t, f.p.Process(f.ctx, f.payload(next.ID)))
	require.Equal(t, "index_pending", mustPersonalAttempt(t, f, before.ID).Status)
}

func TestFeishuV2PostgresPersonalAcquisitionReadmission(t *testing.T) {
	for _, renewed := range []bool{false, true} {
		t.Run(fmt.Sprint("renewed=", renewed), func(t *testing.T) {
			f, original, before := failedAcquisitionFixture(t)
			if renewed {
				require.NoError(t, f.db.Exec(
					"UPDATE feishu_personal_grant_epochs SET epoch='renewed-acquisition' WHERE user_id='A'",
				).Error)
			}
			view, err := f.s.Batch(f.ctx, original.Batch.ID)
			require.NoError(t, err)
			require.True(t, view.Items[0].CanRetry)
			require.NoError(t, f.s.RetryBatch(f.ctx, original.Batch.ID))
			require.Equal(t, before.RetryCount, mustPersonalAttempt(t, f, before.ID).RetryCount)
			view, err = f.s.Batch(f.ctx, original.Batch.ID)
			require.NoError(t, err)
			require.Equal(t, "pending", view.Items[0].Status)
			require.False(t, view.Items[0].CanRetry, "queued admission must not look like an unhandled failure")
			require.Equal(t, 1, view.Items[0].RetryCount)
			require.NoError(t, f.s.ReconcileBatches(context.Background()))
			var resource types.FeishuPersonalResource
			require.NoError(t, f.db.Where("import_id=?", before.ImportID).Take(&resource).Error)
			next := mustPersonalAttempt(t, f, resource.CurrentAttemptID)
			require.Equal(t, before.LogicalKnowledgeID, next.LogicalKnowledgeID)
			require.Empty(t, next.OwnerNonce, "admission is not a source execution Claim")
			if !renewed {
				require.Equal(t, before.ID, next.ID)
				require.Equal(t, before.RetryCount, next.RetryCount)
			} else {
				require.Equal(t, "renewed-acquisition", next.GrantEpoch)
				require.Equal(t, "obsolete", mustPersonalAttempt(t, f, before.ID).Status)
			}
			require.NoError(t, f.p.Process(f.ctx, f.payload(next.ID)))
			current := mustPersonalAttempt(t, f, next.ID)
			require.Equal(t, "index_pending", current.Status)
			require.NotEmpty(t, current.ManifestHash)
			var count int64
			require.NoError(
				t,
				f.db.Table("chunks").Where("knowledge_id=?", next.LogicalKnowledgeID).Count(&count).Error,
			)
			require.Zero(t, count, "fetch retry must not impersonate the subsequent parser/index consumer")
		})
	}
}

func TestFeishuV2PostgresPersonalAcquisitionReadmissionRejectsUnsafeOutput(t *testing.T) {
	for _, change := range []string{
		"unsupported", "budget", "knowledge", "historical_output", "receipt", "source", "dispatch",
	} {
		t.Run(change, func(t *testing.T) {
			f, original, attempt := failedAcquisitionFixture(t)
			switch change {
			case "unsupported":
				require.NoError(t, f.db.Model(&types.FeishuPersonalAttempt{}).Where("id=?", attempt.ID).
					Update("last_error_category", "candidate_media_not_supported").Error)
			case "budget":
				require.NoError(t, f.db.Model(&types.FeishuBatchItem{}).Where("id=?", original.Items[0].ID).
					Update("retry_count", 3).Error)
			case "knowledge":
				require.NoError(t, f.db.Create(&types.Knowledge{
					ID: attempt.LogicalKnowledgeID, TenantID: 1, KnowledgeBaseID: "kb", Title: "unproved output",
					Type: "file", ParseStatus: "failed", EnableStatus: "disabled",
				}).Error)
			case "receipt":
				require.NoError(t, f.db.Create(&types.TaskPendingOp{
					TenantID: 1, Scope: types.PersonalExactReceiptScope, ScopeID: attempt.LogicalKnowledgeID,
					TaskType: "knowledge:exact_receipt", Op: "staging", DedupKey: "unproved-receipt",
					Payload: []byte(`{}`),
				}).Error)
			case "historical_output":
				prior := *attempt
				prior.ID = "historical-attempt"
				prior.Revision = "historical-revision"
				prior.Status = "obsolete"
				prior.LogicalKnowledgeID = "historical-output"
				prior.CandidateVersionID = "historical-candidate"
				require.NoError(t, f.db.Create(&prior).Error)
				require.NoError(t, f.db.Create(&types.Knowledge{
					ID: prior.LogicalKnowledgeID, TenantID: 1, KnowledgeBaseID: "kb", Title: "historical output",
					Type: "file", ParseStatus: "failed", EnableStatus: "disabled",
				}).Error)
			case "source":
				require.NoError(t, f.db.Create(&types.DataSource{
					ID: attempt.ImportID, TenantID: 1, KnowledgeBaseID: "kb", Type: "feishu_drive",
					Status: "error", LegacyAccessState: "hold", Config: types.JSON(`{}`),
				}).Error)
			case "dispatch":
				require.NoError(t, f.db.Model(&types.TaskPendingOp{}).Where("scope=? AND scope_id=?",
					types.KnowledgeDispatchScope, attempt.CandidateVersionID).Update("payload", []byte(`{}`)).Error)
			}
			before := personalRetryState(t, f, attempt.ID)
			require.Error(t, f.s.RetryBatch(f.ctx, original.Batch.ID))
			require.JSONEq(t, before, personalRetryState(t, f, attempt.ID))
			var item types.FeishuBatchItem
			require.NoError(t, f.db.Where("id=?", original.Items[0].ID).Take(&item).Error)
			require.NotEqual(t, "pending", item.Status)
		})
	}
}
