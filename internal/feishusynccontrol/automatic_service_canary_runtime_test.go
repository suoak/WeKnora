package feishusynccontrol

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// This fixture uses real SQL claims, staging and atomic publication, and real
// Redis/Asynq health. Its source bytes and vector are explicitly synthetic; it
// does not attest remote Feishu ACL/OAuth or real model quality/performance.
func TestAutomaticServiceNativeSQLCanaryAtomicPublicationRampAndNormal(t *testing.T) {
	f := newAutomaticAuthorityFixture(t)
	health, r, opt := nativeAutomaticService(t)
	startNativeAutomaticConsumer(t, opt)
	c := health.c
	c.Stop = func(ctx context.Context) error {
		_, err := repository.FenceClaimedLegacySyncExecutions(ctx, f.db)
		return err
	}
	m, err := NewAutomaticServiceManager(
		c,
		f.db,
		repository.NewTenantAPIKeyRepository(f.db).(AutomaticSourceKeyScopes),
	)
	require.NoError(t, err)
	priorRecovery, priorGuard := Recovery(), feishusafety.Default()
	InstallRecovery(m)
	feishusafety.InstallDefault(c.Guard)
	t.Cleanup(func() { InstallRecovery(priorRecovery); feishusafety.InstallDefault(priorGuard) })
	s := m.system
	ctx := context.Background()
	step := func() {
		require.Eventually(
			t,
			func() bool { return c.Step(ctx) == nil },
			5*time.Second,
			150*time.Millisecond,
		)
	}

	// Actual publication requires a matching model, config and usable vector
	// index; keep the real project triggers and source/generation fences enabled.
	model := types.Model{
		ID:       uuid.NewString(),
		TenantID: f.tenant.ID,
		Name:     "synthetic-canary-vector",
		Type:     types.ModelTypeEmbedding,
	}
	require.NoError(t, f.db.Create(&model).Error)
	var kb types.KnowledgeBase
	require.NoError(t, f.db.First(&kb, "id=?", f.source.KnowledgeBaseID).Error)
	require.NoError(t, f.db.Model(&kb).Update("embedding_model_id", model.ID).Error)
	require.NoError(t, f.db.First(&kb, "id=?", kb.ID).Error)
	indexName := "automatic_canary_hnsw3_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	require.NoError(t, f.db.Exec("CREATE INDEX "+indexName+" ON embeddings USING hnsw "+
		"((embedding::halfvec(3)) halfvec_cosine_ops) WHERE dimension=3").Error)
	t.Cleanup(func() { require.NoError(t, f.db.Exec("DROP INDEX IF EXISTS "+indexName).Error) })
	nativeAutomaticStable(t, s)
	step()
	initial, _, err := s.current(ctx)
	require.NoError(t, err)
	require.Equal(t, "canary", initial.Phase)
	require.Empty(t, initial.CanarySourceID)

	logs := repository.NewSyncLogRepository(f.db)
	executionRepo := logs.(interfaces.SyncExecutionRepository)
	log := &types.SyncLog{
		ID:           uuid.NewString(),
		DataSourceID: f.source.ID,
		TenantID:     f.tenant.ID,
		Status:       types.SyncLogStatusRunning,
		StartedAt:    time.Now().UTC(),
	}
	created, err := logs.CreateIfNoRunning(ctx, log)
	require.NoError(t, err)
	require.True(t, created)
	work, handled, err := PrepareAutomaticSourceExecution(ctx, f.source.ID, log.ID, f.tenant.ID)
	require.True(t, handled)
	require.NoError(t, err)
	owner, claimed, err := executionRepo.ClaimExecution(work, f.source.ID, log.ID, f.tenant.ID)
	require.NoError(t, err)
	require.True(t, claimed)
	work = types.WithLegacyReceiptSet(types.WithSyncExecution(work, owner))
	work = feishusafety.WithRecoveryExecution(work, f.config, log.ID, owner.Generation)
	work, err = s.bindSource(work, f.source.ID, f.config)
	require.NoError(t, err)
	work = feishusafety.WithScope(work, f.source.ID)
	step()
	require.NoError(t, c.Guard.Check(work))

	body := []byte("Synthetic canary: atomic publication reconciles one chunk and its vector.")
	original := filepath.Join(t.TempDir(), "synthetic-canary.md")
	require.NoError(t, os.WriteFile(original, body, 0o600))
	hash := sha256.Sum256(body)
	sourceHash := hex.EncodeToString(hash[:])
	store := repository.NewFeishuLegacyStore(f.db)
	stamp, err := store.ModelStamp(work, model.ID)
	require.NoError(t, err)
	item := &types.FetchedItem{
		ExternalID: "synthetic-canary-item",
		UpdatedAt:  time.Now().UTC(),
		Content:    body,
		FileName:   "synthetic-canary.md",
	}
	resource, attempt, err := store.Begin(work, &f.source, item, sourceHash)
	require.NoError(t, err)
	metadata, err := json.Marshal(
		map[string]string{
			"datasource_id":      f.source.ID,
			"external_id":        item.ExternalID,
			"source_resource_id": "fixture-root",
		},
	)
	require.NoError(t, err)
	chunk := &types.Chunk{
		ID:                     uuid.NewString(),
		TenantID:               f.tenant.ID,
		KnowledgeBaseID:        kb.ID,
		KnowledgeID:            resource.KnowledgeID,
		Content:                string(body),
		IsEnabled:              true,
		IndexStatus:            "ready",
		ChunkType:              "text",
		RelationChunks:         types.JSON(`[]`),
		IndirectRelationChunks: types.JSON(`[]`),
		Metadata:               types.JSON(`{}`),
	}
	manifest := types.FeishuLegacyManifest{
		Knowledge: types.Knowledge{
			ID:              resource.KnowledgeID,
			TenantID:        f.tenant.ID,
			KnowledgeBaseID: kb.ID,
			Title:           "Synthetic canary",
			Type:            "file",
			Channel:         "feishu",
			FileName:        item.FileName,
			FileType:        "md",
			FilePath:        original,
			FileHash:        sourceHash,
			Metadata:        types.JSON(metadata),
		},
		Chunks: []*types.Chunk{
			chunk,
		},
		EmbeddingModelID: model.ID,
		KBConfigSHA256:   repository.LegacyKBConfigHash(&kb),
		ModelStamp:       stamp,
		Dimension:        3,
	}
	step()
	require.NoError(t, store.Prepare(work, &f.source, attempt, manifest, [][]float32{{1, 0, 0}}))
	step()
	receipt := publishWithCanaryObservation(t, s, f.db, store, work, &f.source, attempt)
	require.EqualValues(t, 1, receipt.Chunks)
	require.EqualValues(t, 1, receipt.Vectors)
	require.Equal(t, attempt.ID, receipt.AttemptID)
	types.AddLegacyReceipt(work, *receipt)
	var published types.Knowledge
	require.NoError(t, f.db.First(&published, "id=?", resource.KnowledgeID).Error)
	require.Contains(t, []string{types.ParseStatusCompleted, types.ParseStatusFinalizing}, published.ParseStatus)
	require.Equal(t, original, published.FilePath)
	var persisted types.FeishuLegacyAttempt
	require.NoError(t, f.db.First(&persisted, "id=?", attempt.ID).Error)
	require.Equal(t, "published", persisted.Status)
	var receiptJSON map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(persisted.Receipt, &receiptJSON))
	require.Contains(t, receiptJSON, "resource_id")
	require.Contains(t, receiptJSON, "knowledge_id")
	require.Contains(t, receiptJSON, "attempt_id")
	require.NotContains(t, receiptJSON, "ResourceID")

	finished := time.Now().UTC()
	log.Status = types.SyncLogStatusSuccess
	log.FinishedAt = &finished
	valid, err := executionRepo.WriteExecution(work, owner, &f.source, log, true)
	require.NoError(t, err)
	require.True(t, valid)
	saved, err := logs.FindByID(ctx, log.ID)
	require.NoError(t, err)
	require.Equal(t, types.SyncLogStatusSuccess, saved.Status)
	require.False(t, saved.ExecutionClaimed)
	state, raw, err := s.current(ctx)
	require.NoError(t, err)
	outcome, err := systemCanaryOutcome(ctx, f.db, state)
	require.NoError(t, err)
	require.Equal(
		t,
		"success",
		outcome,
		"the actual repository Publish receipt must qualify, never a hand-written success receipt",
	)
	// Negative SQL counterexample, not an alternative publication path: remove
	// one required field only inside a rolled-back private test transaction.
	rollback := errors.New("controlled canary receipt counterexample rollback")
	err = f.db.Transaction(func(tx *gorm.DB) error {
		if e := tx.Exec(
			"UPDATE feishu_legacy_attempts SET receipt=receipt-'resource_id' WHERE id=?",
			attempt.ID,
		).Error; e != nil {
			return e
		}
		bad, e := systemCanaryOutcome(ctx, tx, state)
		require.NoError(t, e)
		require.Equal(t, "source_failed", bad)
		return rollback
	})
	require.ErrorIs(t, err, rollback)
	// The production publication fence itself rejects arbitrary deletion of a
	// published vector. Never disable that fence to manufacture a missing row.
	err = f.db.Transaction(func(tx *gorm.DB) error {
		return tx.Exec("DELETE FROM embeddings WHERE knowledge_id=?", resource.KnowledgeID).Error
	})
	require.ErrorContains(t, err, "stale or unmanaged Feishu source write")
	outcome, err = systemCanaryOutcome(ctx, f.db, state)
	require.NoError(t, err)
	require.Equal(
		t,
		"success",
		outcome,
		"both negative transactions must preserve the complete actual publication",
	)
	require.NoError(t, s.advanceCanary(ctx, state, raw))
	ramp, _, err := s.current(ctx)
	require.NoError(t, err)
	require.Equal(t, "ramp", ramp.Phase)
	require.NotEmpty(t, ramp.CanaryProofSHA256)
	witness, err := readCanaryWitness(ctx, f.db, ramp)
	require.NoError(t, err)
	require.Equal(t, resource.DataSourceID, witness.SourceID)
	require.Equal(t, log.ID, witness.RunID)
	require.Equal(t, ramp.Epoch, witness.Epoch)
	require.Greater(t, ramp.RampUntilMS, ramp.NextAdmissionMS)

	// Other lawful sources cannot inherit the successful Canary immediately.
	// Use real persisted intents/private preflight, without a forged run context.
	other := f.source
	other.ID = uuid.NewString()
	other.Name = "synthetic-ramp-source"
	require.NoError(t, f.db.Create(&other).Error)
	otherLog := &types.SyncLog{
		ID:           uuid.NewString(),
		DataSourceID: other.ID,
		TenantID:     other.TenantID,
		Status:       types.SyncLogStatusRunning,
		StartedAt:    time.Now().UTC(),
	}
	created, err = logs.CreateIfNoRunning(ctx, otherLog)
	require.NoError(t, err)
	require.True(t, created)
	_, handled, err = PrepareAutomaticSourceExecution(ctx, other.ID, otherLog.ID, other.TenantID)
	require.True(t, handled)
	require.ErrorIs(t, err, ErrNotReady, "ramp must honor its real five-second admission deadline")
	firstAdmission := time.Now()
	require.Eventually(t, func() bool {
		if c.Step(ctx) != nil {
			return false
		}
		_, _, e := PrepareAutomaticSourceExecution(ctx, other.ID, otherLog.ID, other.TenantID)
		return e == nil
	}, 8*time.Second, 150*time.Millisecond)
	now, err := r.Time(ctx).Result()
	require.NoError(t, err)
	require.GreaterOrEqual(t, now.UnixMilli(), ramp.NextAdmissionMS)
	require.Less(t, time.Since(firstAdmission), 8*time.Second)
	second := f.source
	second.ID = uuid.NewString()
	second.Name = "synthetic-next-ramp-source"
	require.NoError(t, f.db.Create(&second).Error)
	secondLog := &types.SyncLog{
		ID:           uuid.NewString(),
		DataSourceID: second.ID,
		TenantID:     second.TenantID,
		Status:       types.SyncLogStatusRunning,
		StartedAt:    time.Now().UTC(),
	}
	created, err = logs.CreateIfNoRunning(ctx, secondLog)
	require.NoError(t, err)
	require.True(t, created)
	_, _, err = PrepareAutomaticSourceExecution(ctx, second.ID, secondLog.ID, second.TenantID)
	require.ErrorIs(
		t,
		err,
		ErrNotReady,
		"the next distinct source must wait for a new five-second slot",
	)

	// No fast-forward, direct state edit or shortened lease/ramp: the real
	// controller renews health until its sixty-second ramp expires naturally.
	require.Eventually(t, func() bool {
		if c.Step(ctx) != nil {
			return false
		}
		current, _, e := s.current(ctx)
		return e == nil && current.Phase == "normal"
	}, 65*time.Second, 200*time.Millisecond)
	normal, _, err := s.current(ctx)
	require.NoError(t, err)
	now, err = r.Time(ctx).Result()
	require.NoError(t, err)
	require.GreaterOrEqual(t, now.UnixMilli(), ramp.RampUntilMS)
	require.Equal(t, ramp.Epoch, normal.Epoch)
	_, handled, err = PrepareAutomaticSourceExecution(ctx, second.ID, secondLog.ID, second.TenantID)
	require.True(t, handled)
	require.NoError(t, err)
	require.Zero(t, r.Exists(ctx, c.prefix+"trip").Val())
	for _, pending := range []*types.SyncLog{otherLog, secondLog} {
		current, e := logs.FindByID(ctx, pending.ID)
		require.NoError(t, e)
		require.False(t, current.ExecutionClaimed)
	}
	originalAfter, err := os.ReadFile(original)
	require.NoError(t, err)
	require.Equal(t, body, originalAfter)
	var counts struct{ Chunks, Embeddings int64 }
	require.NoError(
		t,
		f.db.Raw("SELECT (SELECT count(*) FROM chunks WHERE knowledge_id=? AND deleted_at IS NULL) AS chunks,"+
			"(SELECT count(*) FROM embeddings WHERE knowledge_id=?) AS embeddings",
			resource.KnowledgeID,
			resource.KnowledgeID).
			Scan(&counts).
			Error,
	)
	require.EqualValues(t, 1, counts.Chunks)
	require.EqualValues(t, 1, counts.Embeddings)
	t.Log(
		"real SQL atomic publication/settlement and Redis/Asynq ramp/normal; synthetic bytes/vector only",
	)
}
