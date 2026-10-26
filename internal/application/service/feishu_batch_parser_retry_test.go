package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type parserReadiness struct {
	interfaces.DocumentReader
	connected bool
}

func (r *parserReadiness) IsConnected() bool { return r.connected }

func TestFeishuV2PostgresParserOutageRecoveryPreservesKnowledgeAndFences(t *testing.T) {
	f := v2PostgresFixture(t)
	view, err := f.s.QuickImport(
		f.ctx,
		"kb",
		"parser-outage",
		[]string{"https://test.feishu.cn/file/real"},
		true,
	)
	require.NoError(t, err)
	require.NoError(t, f.s.ReconcileBatches(context.Background()))
	view, err = f.s.Batch(f.ctx, view.Batch.ID)
	require.NoError(t, err)
	var state types.FeishuPersonalResource
	require.NoError(t, f.db.First(&state, "import_id = ?", view.Items[0].ImportID).Error)
	require.NoError(t, f.p.Process(f.ctx, f.payload(state.CurrentAttemptID)))
	m, original, a, err := f.p.repo.Load(f.ctx, state.CurrentAttemptID)
	require.NoError(t, err)
	require.NotEmpty(t, m.KnowledgeID)
	require.NoError(t, f.p.repo.Invalidate(f.ctx, a.ID, "parse_or_index_result_unconfirmed"))
	require.NoError(t, f.db.AutoMigrate(&types.KnowledgeProcessingSpan{}))
	require.NoError(t, f.db.Create(&types.KnowledgeProcessingSpan{
		KnowledgeID: m.KnowledgeID, Attempt: 1, SpanID: "parser-stage", Name: types.StageDocReader,
		Kind: types.SpanKindStage, Status: types.SpanStatusFailed, ErrorCode: "DOCREADER_UNAVAILABLE",
	}).Error)
	require.NoError(t, f.db.Model(&types.FeishuBatchItem{}).Where("id = ?", view.Items[0].ID).
		Updates(map[string]any{"status": "failed", "error_category": "parse_or_index_result_unconfirmed"}).
		Error)
	reader := &parserReadiness{}
	f.ks.documentReader = reader
	require.NoError(t, f.s.requeueUnavailableParserBatches(context.Background()))
	var item types.FeishuBatchItem
	require.NoError(t, f.db.First(&item, "id = ?", view.Items[0].ID).Error)
	require.Equal(t, "failed", item.Status, "an unavailable dependency cannot consume retries")
	reader.connected = true
	require.NoError(t, f.s.ReconcileBatches(context.Background()))
	require.NoError(t, f.db.First(&item, "id = ?", item.ID).Error)
	require.Equal(t, "dispatched", item.Status)
	require.Equal(t, 1, item.RetryCount)
	current, resource, attempt, err := f.p.repo.Load(f.ctx, a.ID)
	require.NoError(t, err)
	require.Equal(t, m.KnowledgeID, current.KnowledgeID)
	require.Equal(t, a.ID, attempt.ID)
	require.Equal(t, "index_pending", attempt.Status)
	require.Greater(
		t,
		resource.Generation,
		original.Generation,
		"recovery increases fencing, never resets it",
	)
	var count int64
	require.NoError(t, f.db.Model(&types.Knowledge{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
}

func TestFeishuV2PostgresParserOutageNeverResumesPartialOrRevoked(t *testing.T) {
	for _, reason := range []string{"partial", "revoked", "parse_error", "retry_exhausted", "source_managed"} {
		t.Run(reason, func(t *testing.T) {
			f := v2PostgresFixture(t)
			view, err := f.s.QuickImport(
				f.ctx,
				"kb",
				"parser-stop",
				[]string{"https://test.feishu.cn/file/real"},
				true,
			)
			require.NoError(t, err)
			require.NoError(t, f.s.ReconcileBatches(context.Background()))
			view, err = f.s.Batch(f.ctx, view.Batch.ID)
			require.NoError(t, err)
			var state types.FeishuPersonalResource
			require.NoError(t, f.db.First(&state, "import_id = ?", view.Items[0].ImportID).Error)
			require.NoError(t, f.p.Process(f.ctx, f.payload(state.CurrentAttemptID)))
			m, _, a, err := f.p.repo.Load(f.ctx, state.CurrentAttemptID)
			require.NoError(t, err)
			require.NoError(t, f.p.repo.Invalidate(f.ctx, a.ID, "parse_or_index_result_unconfirmed"))
			require.NoError(t, f.db.AutoMigrate(&types.KnowledgeProcessingSpan{}))
			code := "DOCREADER_UNAVAILABLE"
			if reason == "parse_error" {
				code = "DOCREADER_PARSE_FAILED"
			}
			require.NoError(t, f.db.Create(&types.KnowledgeProcessingSpan{
				KnowledgeID: m.KnowledgeID, Attempt: 1, SpanID: "parser-stage", Name: types.StageDocReader,
				Kind: types.SpanKindStage, Status: types.SpanStatusFailed, ErrorCode: code,
			}).Error)
			retries := 0
			if reason == "retry_exhausted" {
				retries = 3
			}
			require.NoError(t, f.db.Model(&types.FeishuBatchItem{}).Where("id = ?", view.Items[0].ID).
				Updates(map[string]any{
					"status": "failed", "retry_count": retries,
					"error_category": "parse_or_index_result_unconfirmed",
				}).Error)
			if reason == "partial" {
				require.NoError(t, f.db.Create(&types.Chunk{
					ID: uuid.NewString(), TenantID: 1,
					KnowledgeID: m.KnowledgeID, KnowledgeBaseID: "kb", Content: "synthetic partial output",
				}).Error)
			}
			if reason == "revoked" {
				require.NoError(t, f.s.RevokeSharingApproval(f.ctx, "kb", *view.Items[0].SharingApprovalID))
			}
			if reason == "source_managed" {
				require.NoError(t, f.db.Model(&types.FeishuPersonalImport{}).Where("id = ?", m.ID).
					UpdateColumn("status", "source_managed").Error)
			}
			f.ks.documentReader = &parserReadiness{connected: true}
			require.NoError(t, f.s.requeueUnavailableParserBatches(context.Background()))
			var item types.FeishuBatchItem
			require.NoError(t, f.db.First(&item, "id = ?", view.Items[0].ID).Error)
			require.Equal(t, "failed", item.Status)
			require.Equal(t, retries, item.RetryCount)
		})
	}
}
