package repository

import (
	"context"
	"encoding/json"
	"path/filepath"
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
	model := &types.Model{ID: uuid.NewString(), TenantID: tenant.ID, Name: "checkpoint", Type: types.ModelTypeEmbedding}
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
	resource, first, err := store.Begin(ctx, ds, item, LegacySourceHash(item.Content, item.FileName, item.Title))
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
		require.NoError(t, db.Table("feishu_legacy_index_candidates").Where("attempt_id=?", attempt).Count(&n).Error)
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
	require.EqualValues(t, 2, count(next.ID), "failed finalization must roll back and keep completed checkpoints")
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
	_, foreign, err := store.Begin(ctx, ds, &other, LegacySourceHash(other.Content, other.FileName, other.Title))
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
}
