package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/database"
	"github.com/Tencent/WeKnora/internal/testutil/feishulegacy"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestFeishuLegacyEmbeddingCheckpointPostgres(t *testing.T) {
	dsn := feishulegacy.PrivateDSN(t)
	parsedDSN, err := url.Parse(dsn)
	require.NoError(t, err)
	name := strings.TrimPrefix(parsedDSN.Path, "/")
	require.True(t, strings.HasPrefix(name, "feishu_atomic_test_"))
	_, err = uuid.Parse(strings.TrimPrefix(name, "feishu_atomic_test_"))
	require.NoError(t, err)
	t.Cleanup(func() {
		admin, openErr := gorm.Open(postgres.Open(os.Getenv("FEISHU_LEGACY_TEST_DSN")), &gorm.Config{})
		require.NoError(t, openErr)
		conn, poolErr := admin.DB()
		require.NoError(t, poolErr)
		defer func() { require.NoError(t, conn.Close()) }()
		require.NoError(t, admin.Exec("DROP DATABASE "+name+" WITH (FORCE)").Error)
	})
	root, err := filepath.Abs("../../..")
	require.NoError(t, err)
	t.Chdir(root)
	require.NoError(t, database.RunMigrations(dsn))
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec("CREATE INDEX checkpoint_hnsw3 ON embeddings USING hnsw "+
		"((embedding::halfvec(3)) halfvec_cosine_ops) WHERE dimension=3").Error)
	pool, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, pool.Close()) })
	tenant := &types.Tenant{ID: 809936, Name: "checkpoint-private", Status: "active"}
	require.NoError(t, db.Create(tenant).Error)
	model := &types.Model{
		ID:       uuid.NewString(),
		TenantID: tenant.ID,
		Name:     "checkpoint",
		Type:     types.ModelTypeEmbedding,
	}
	require.NoError(t, db.Create(model).Error)
	kb := &types.KnowledgeBase{
		ID: uuid.NewString(), TenantID: tenant.ID, Name: "checkpoint", Type: "document", EmbeddingModelID: model.ID,
	}
	require.NoError(t, db.Create(kb).Error)
	ds := &types.DataSource{
		ID: uuid.NewString(), TenantID: tenant.ID, KnowledgeBaseID: kb.ID,
		Type: "feishu_drive", Status: "active", Config: types.JSON(`{}`),
	}
	require.NoError(t, db.Create(ds).Error)
	log := &types.SyncLog{
		ID: uuid.NewString(), DataSourceID: ds.ID, TenantID: tenant.ID, Status: "running",
		ExecutionGeneration: 1, ExecutionClaimed: true,
	}
	require.NoError(t, db.Create(log).Error)
	ctx := types.WithSyncExecution(context.Background(), types.SyncExecution{
		DataSourceID: ds.ID, SyncLogID: log.ID, TenantID: tenant.ID, Generation: 1,
	})
	store := NewFeishuLegacyStore(db)
	stamp, err := store.ModelStamp(ctx, model.ID)
	require.NoError(t, err)
	item := &types.FetchedItem{
		ExternalID: "same-sheet", FileName: "sheet.md", Title: "sheet",
		Content: []byte("first and second"), UpdatedAt: time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC),
	}
	resource, first, err := store.Begin(
		ctx,
		ds,
		item,
		LegacySourceHash(item.Content, item.FileName, item.Title),
	)
	require.NoError(t, err)
	makeManifest := func(bodies ...string) (
		types.FeishuLegacyManifest, types.FeishuLegacyEmbeddingCheckpoint, []string,
	) {
		metadata, _ := json.Marshal(map[string]string{
			"datasource_id": ds.ID, "external_id": item.ExternalID,
		})
		m := types.FeishuLegacyManifest{Knowledge: types.Knowledge{
			ID: resource.KnowledgeID, TenantID: tenant.ID, KnowledgeBaseID: kb.ID, Title: item.Title,
			Type: "file", Channel: ds.Type, FileHash: LegacySourceHash(item.Content, item.FileName, item.Title),
			Metadata: types.JSON(metadata), EmbeddingModelID: model.ID,
		}, EmbeddingModelID: model.ID, KBConfigSHA256: LegacyKBConfigHash(kb), ModelStamp: stamp, Dimension: 3}
		c := types.FeishuLegacyEmbeddingCheckpoint{
			ModelID: model.ID, ModelStamp: stamp,
			KBConfigSHA256: m.KBConfigSHA256, Dimension: 3,
		}
		var texts []string
		for _, body := range bodies {
			chunk := &types.Chunk{
				ID: uuid.NewString(), TenantID: tenant.ID, KnowledgeBaseID: kb.ID,
				KnowledgeID: resource.KnowledgeID, Content: body, ChunkType: types.ChunkTypeText, IsEnabled: true,
			}
			m.Chunks = append(m.Chunks, chunk)
			input := buildLegacyIndexContent(item.Title, chunk.EmbeddingContent())
			texts = append(texts, input)
			c.Inputs = append(c.Inputs, types.FeishuLegacyEmbeddingInput{
				ChunkID: chunk.ID, SHA256: LegacyEmbeddingInputHash(input),
			})
		}
		return m, c, texts
	}
	_, header, inputs := makeManifest("first", "second")
	require.NoError(t, store.StartEmbeddingCheckpoint(ctx, ds, first, header))
	require.NoError(t, store.SaveEmbeddingCheckpoint(ctx, ds, first, inputs[:1], [][]float32{{1, 0, 0}}))
	count := func(attempt string) int64 {
		var n int64
		require.NoError(
			t,
			db.Table("feishu_legacy_index_candidates").Where("attempt_id=?", attempt).Count(&n).Error,
		)
		return n
	}
	require.EqualValues(t, 1, count(first.ID))
	var public int64
	require.NoError(t, db.Model(&types.Knowledge{}).Where("id=?", resource.KnowledgeID).Count(&public).Error)
	require.Zero(t, public, "partial vectors must not become public knowledge")
	require.NoError(t, store.Abandon(ctx, first))
	_, next, err := store.Begin(ctx, ds, item, LegacySourceHash(item.Content, item.FileName, item.Title))
	require.NoError(t, err)
	require.NotEqual(t, first.ID, next.ID, "attempt audit remains immutable; business identity remains stable")
	m, header, inputs := makeManifest("first", "second")
	require.NoError(t, store.StartEmbeddingCheckpoint(ctx, ds, next, header))
	cache, err := store.ReusableEmbeddingCheckpoint(ctx, ds, next)
	require.NoError(t, err)
	require.Equal(t, []float32{1, 0, 0}, cache[LegacyEmbeddingInputHash(inputs[0])])
	require.NotContains(t, cache, LegacyEmbeddingInputHash(inputs[1]))
	require.ErrorIs(t, store.SaveEmbeddingCheckpoint(ctx, ds, first, inputs[:1], [][]float32{{1, 0, 0}}),
		ErrLegacyVersionFence)
	revoked := types.WithFeishuSourceReadGuard(ctx, func(context.Context) error {
		return types.ErrLegacySourceRestricted
	})
	_, err = store.ReusableEmbeddingCheckpoint(revoked, ds, next)
	require.ErrorIs(t, err, types.ErrLegacySourceRestricted)
	require.ErrorIs(t, store.SaveEmbeddingCheckpoint(revoked, ds, next, inputs[:1], [][]float32{{1, 0, 0}}),
		types.ErrLegacySourceRestricted)
	require.NoError(t, store.SaveEmbeddingCheckpoint(ctx, ds, next, inputs[:1], [][]float32{{1, 0, 0}}))
	require.NoError(t, store.SaveEmbeddingCheckpoint(ctx, ds, next, inputs[:1], [][]float32{{1, 0, 0}}))
	require.EqualValues(t, 1, count(next.ID), "same checkpoint is idempotent")
	require.ErrorIs(t, store.SaveEmbeddingCheckpoint(ctx, ds, next, inputs[1:], [][]float32{{0, 0, 0}}),
		ErrLegacyVersionFence)
	require.EqualValues(t, 1, count(next.ID), "invalid vectors never enter the reusable cache")
	require.ErrorIs(t, store.SaveEmbeddingCheckpoint(ctx, ds, next, inputs[:1], [][]float32{{0, 1, 0}}),
		ErrLegacyVersionFence)
	require.NoError(t, store.SaveEmbeddingCheckpoint(ctx, ds, next, inputs[1:], [][]float32{{0, 1, 0}}))
	require.EqualValues(t, 2, count(next.ID))
	require.Error(t, store.Prepare(ctx, ds, next, m, [][]float32{{1, 0, 0}, {1}}))
	require.EqualValues(
		t,
		2,
		count(next.ID),
		"failed finalization must roll back and keep completed checkpoints",
	)
	require.NoError(t, store.Prepare(ctx, ds, next, m, [][]float32{{1, 0, 0}, {0, 1, 0}}))
	receipt, err := store.Publish(ctx, ds, next)
	require.NoError(t, err)
	require.Equal(t, resource.KnowledgeID, receipt.KnowledgeID)
	require.EqualValues(t, 2, receipt.Vectors)
	// A changed source revision may reuse only exactly unchanged, previously
	// published inputs under the same resource, model and KB configuration.
	item.Content = []byte("first and third")
	item.UpdatedAt = item.UpdatedAt.Add(time.Second)
	_, third, err := store.Begin(ctx, ds, item, LegacySourceHash(item.Content, item.FileName, item.Title))
	require.NoError(t, err)
	_, header, inputs = makeManifest("first", "third")
	require.NoError(t, store.StartEmbeddingCheckpoint(ctx, ds, third, header))
	cache, err = store.ReusableEmbeddingCheckpoint(ctx, ds, third)
	require.NoError(t, err)
	require.Contains(t, cache, LegacyEmbeddingInputHash(inputs[0]))
	require.NotContains(t, cache, LegacyEmbeddingInputHash(inputs[1]))
	// A stale/foreign execution cannot use cache reads as an authorization grant.
	wrong := *ds
	wrong.ID = uuid.NewString()
	_, err = store.ReusableEmbeddingCheckpoint(ctx, &wrong, third)
	require.ErrorIs(t, err, ErrLegacyVersionFence)
	// Even identical content cannot borrow vectors from another source resource.
	other := *item
	other.ExternalID = "another-sheet"
	_, foreign, err := store.Begin(
		ctx,
		ds,
		&other,
		LegacySourceHash(other.Content, other.FileName, other.Title),
	)
	require.NoError(t, err)
	duplicateHeader := header
	duplicateHeader.Inputs = []types.FeishuLegacyEmbeddingInput{
		{ChunkID: uuid.NewString(), SHA256: LegacyEmbeddingInputHash(inputs[0])},
		{ChunkID: uuid.NewString(), SHA256: LegacyEmbeddingInputHash(inputs[0])},
	}
	require.NoError(t, store.StartEmbeddingCheckpoint(ctx, ds, foreign, duplicateHeader))
	cache, err = store.ReusableEmbeddingCheckpoint(ctx, ds, foreign)
	require.NoError(t, err)
	require.Empty(t, cache)
	require.NoError(t, store.SaveEmbeddingCheckpoint(ctx, ds, foreign, inputs[:1], [][]float32{{1, 0, 0}}))
	require.EqualValues(t, 2, count(foreign.ID), "identical inputs keep distinct original chunk identities")
	// An interrupted, unpublished revision is also useful when a live sheet
	// changes between runs. Only the exact current model input may be reused.
	require.NoError(t, store.SaveEmbeddingCheckpoint(ctx, ds, third, inputs[1:], [][]float32{{0, 0, 1}}))
	require.NoError(t, store.Abandon(ctx, third))
	item.Content = []byte("third and fourth")
	item.UpdatedAt = item.UpdatedAt.Add(time.Second)
	_, fourth, err := store.Begin(ctx, ds, item, LegacySourceHash(item.Content, item.FileName, item.Title))
	require.NoError(t, err)
	require.NotEqual(t, third.SourceHash, fourth.SourceHash)
	_, header, inputs = makeManifest("third", "fourth")
	require.NoError(t, store.StartEmbeddingCheckpoint(ctx, ds, fourth, header))
	cache, err = store.ReusableEmbeddingCheckpoint(ctx, ds, fourth)
	require.NoError(t, err)
	require.Equal(t, []float32{0, 0, 1}, cache[LegacyEmbeddingInputHash(inputs[0])])
	require.NotContains(
		t,
		cache,
		LegacyEmbeddingInputHash(inputs[1]),
		"changed inputs still require a model call",
	)
	require.Len(t, cache, 1, "unrequested historical inputs are never returned")
	t.Run("bounded-large-cache-replay-keeps-original-fences", func(t *testing.T) {
		largeItem := *item
		largeItem.ExternalID, largeItem.FileName = "large-replay", "large.xlsx"
		resource, attempt, e := store.Begin(
			ctx,
			ds,
			&largeItem,
			LegacySourceHash(largeItem.Content, largeItem.FileName, largeItem.Title),
		)
		require.NoError(t, e)
		request := types.FeishuDocumentReparse{
			ID: uuid.NewString(), KnowledgeID: resource.KnowledgeID,
			DataSourceID: ds.ID, KnowledgeBaseID: kb.ID, TenantID: tenant.ID,
			RootID: "large-replay", ExternalID: largeItem.ExternalID,
			FileType: "xlsx", ConfigHash: LegacySourceReparseConfigHash(ds),
			ExecutionID: log.ID, Generation: 1, Attempts: 1,
		}
		payload, e := json.Marshal(request)
		require.NoError(t, e)
		operation := types.TaskPendingOp{
			TenantID: tenant.ID, TaskType: sourceDocumentReparseTask,
			Scope: sourceDocumentReparseScope, ScopeID: ds.ID, Op: "processing", DedupKey: resource.KnowledgeID,
			Payload: payload, EnqueuedAt: time.Now(),
		}
		require.NoError(t, db.Create(&operation).Error)
		claimed := context.WithValue(
			ctx,
			sourceDocumentReparseKey{},
			&sourceDocumentReparseClaim{op: operation, request: request},
		)
		large := types.FeishuLegacyEmbeddingCheckpoint{
			Version:        2,
			RequestID:      request.ID,
			SourceConfig:   request.ConfigHash,
			ModelID:        model.ID,
			ModelStamp:     stamp,
			KBConfigSHA256: LegacyKBConfigHash(kb),
			Dimension:      3,
		}
		texts, values := []string{}, [][]float32{}
		for i := 0; i < 10001; i++ {
			text := fmt.Sprintf("bounded-replay-%d", i)
			large.Inputs = append(
				large.Inputs,
				types.FeishuLegacyEmbeddingInput{ChunkID: uuid.NewString(), SHA256: LegacyEmbeddingInputHash(text)},
			)
			if i < 257 {
				texts = append(texts, text)
				values = append(values, []float32{1, 0, 0})
			}
		}
		require.NoError(t, store.StartEmbeddingCheckpoint(claimed, ds, attempt, large))
		require.ErrorIs(
			t,
			store.SaveEmbeddingCheckpoint(claimed, ds, attempt, texts[:21], values[:21]),
			ErrLegacyVersionFence,
		)
		require.ErrorIs(
			t,
			store.SaveReusedEmbeddingCheckpoint(claimed, ds, attempt, texts, values),
			ErrLegacyVersionFence,
		)
		require.NoError(t, store.SaveReusedEmbeddingCheckpoint(claimed, ds, attempt, texts[:256], values[:256]))
		require.EqualValues(t, 256, count(attempt.ID))
		denied := types.WithFeishuSourceReadGuard(
			claimed,
			func(context.Context) error { return types.ErrLegacySourceRestricted },
		)
		require.ErrorIs(
			t,
			store.SaveReusedEmbeddingCheckpoint(denied, ds, attempt, texts[:1], values[:1]),
			types.ErrLegacySourceRestricted,
		)
		request.Generation++
		stale := context.WithValue(
			ctx,
			sourceDocumentReparseKey{},
			&sourceDocumentReparseClaim{op: operation, request: request},
		)
		require.ErrorIs(
			t,
			store.SaveReusedEmbeddingCheckpoint(stale, ds, attempt, texts[:1], values[:1]),
			ErrLegacyVersionFence,
		)
		require.EqualValues(t, 256, count(attempt.ID))
		require.NoError(t, store.Abandon(ctx, attempt))
		request.Generation = 1
		request.ErrorCode = "retry_exhausted"
		failedPayload, e := json.Marshal(request)
		require.NoError(t, e)
		require.NoError(
			t,
			db.Model(&operation).
				Updates(map[string]any{"op": "failed", "payload": json.RawMessage(failedPayload)}).
				Error,
		)
		_, resumed, e := store.Begin(
			ctx,
			ds,
			&largeItem,
			LegacySourceHash(largeItem.Content, largeItem.FileName, largeItem.Title),
		)
		require.NoError(t, e)
		priorID := request.ID
		request.ID, request.ReuseCheckpointID, request.ErrorCode = uuid.NewString(), priorID, ""
		request.Attempts = 0
		newPayload, e := json.Marshal(request)
		require.NoError(t, e)
		newOperation := operation
		newOperation.ID = 0
		newOperation.Op, newOperation.Payload = "processing", newPayload
		require.NoError(t, db.Create(&newOperation).Error)
		resumedContext := context.WithValue(
			ctx,
			sourceDocumentReparseKey{},
			&sourceDocumentReparseClaim{op: newOperation, request: request},
		)
		large.RequestID = request.ID
		for i := range large.Inputs {
			large.Inputs[i].ChunkID = uuid.NewString()
		}
		require.NoError(t, store.StartEmbeddingCheckpoint(resumedContext, ds, resumed, large))
		reused, e := store.ReusableEmbeddingCheckpoint(resumedContext, ds, resumed)
		require.NoError(t, e)
		require.Len(
			t,
			reused,
			256,
			"a newly authorized intent may reuse only its server-linked parent's verified inputs",
		)
		require.Equal(t, []float32{1, 0, 0}, reused[LegacyEmbeddingInputHash(texts[0])])
		changed := request
		changed.ReuseCheckpointID = uuid.NewString()
		forged := context.WithValue(
			ctx,
			sourceDocumentReparseKey{},
			&sourceDocumentReparseClaim{op: newOperation, request: changed},
		)
		_, e = store.ReusableEmbeddingCheckpoint(forged, ds, resumed)
		require.ErrorIs(t, e, ErrLegacyVersionFence, "a caller cannot inject a different checkpoint parent")
		var oldOperation types.TaskPendingOp
		require.NoError(t, db.First(&oldOperation, "id=?", operation.ID).Error)
		require.Equal(t, "failed", oldOperation.Op, "new requests preserve exhausted request history")
	})
}
