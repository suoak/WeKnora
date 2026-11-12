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

func TestFeishuLatestSnapshotCompleteContent(t *testing.T) {
	item := types.FetchedItem{
		ExternalID: "a",
		FileName:   "a.txt",
		Title:      "a",
		Content:    []byte("current"),
		UpdatedAt:  time.Now().UTC(),
		Metadata:   map[string]string{"obj_token": "token"},
	}
	require.NoError(t, MatchLatestFetchedSnapshot(&item, &item))
	changed := item
	changed.Content = []byte("other")
	require.ErrorIs(t, MatchLatestFetchedSnapshot(&item, &changed), ErrLegacyVersionFence)
	changed = item
	changed.Metadata = map[string]string{"obj_token": "rebound"}
	require.ErrorIs(t, MatchLatestFetchedSnapshot(&item, &changed), ErrLegacyVersionFence)
	changed = item
	changed.UpdatedAt = changed.UpdatedAt.Add(time.Second)
	require.ErrorIs(t, MatchLatestFetchedSnapshot(&item, &changed), ErrLegacyVersionFence)
	changed = item
	changed.SourceResourceID = "other root"
	require.ErrorIs(t, MatchLatestFetchedSnapshot(&item, &changed), ErrLegacyVersionFence)
	// Export formatting may vary; the complete canonical package must agree.
	item.FileName = "a.xlsx"
	item.Metadata = map[string]string{"obj_type": "sheet"}
	item.Content = identityXLSX(t, "10")
	changed = item
	changed.Content = identityXLSX(t, "512.25")
	require.NoError(t, MatchLatestFetchedSnapshot(&item, &changed))
}

func TestFeishuLatestSnapshotPostgres(t *testing.T) {
	dsn := feishulegacy.PrivateDSN(t)
	root, err := filepath.Abs("../../..")
	require.NoError(t, err)
	t.Chdir(root)
	require.NoError(t, database.RunMigrations(dsn))
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	tenant := types.Tenant{ID: 809929, Name: "latest-snapshot-synthetic", Status: "active"}
	require.NoError(t, db.Create(&tenant).Error)
	model := types.Model{
		ID:       uuid.NewString(),
		TenantID: tenant.ID,
		Name:     "synthetic",
		Type:     types.ModelTypeEmbedding,
	}
	require.NoError(t, db.Create(&model).Error)
	kb := types.KnowledgeBase{
		ID:               uuid.NewString(),
		TenantID:         tenant.ID,
		Name:             "latest",
		Type:             "document",
		EmbeddingModelID: model.ID,
	}
	require.NoError(t, db.Create(&kb).Error)
	ds := types.DataSource{
		ID:                uuid.NewString(),
		TenantID:          tenant.ID,
		KnowledgeBaseID:   kb.ID,
		Type:              "feishu",
		Status:            "active",
		Config:            types.JSON(`{}`),
		LegacyAccessState: "active",
	}
	require.NoError(t, db.Create(&ds).Error)
	log := types.SyncLog{
		ID:                  uuid.NewString(),
		TenantID:            tenant.ID,
		DataSourceID:        ds.ID,
		Status:              "running",
		StartedAt:           time.Now().UTC(),
		ExecutionGeneration: 1,
		ExecutionClaimed:    true,
	}
	require.NoError(t, db.Create(&log).Error)
	ctx := types.WithSyncExecution(
		context.Background(),
		types.SyncExecution{DataSourceID: ds.ID, TenantID: tenant.ID, SyncLogID: log.ID, Generation: 1},
	)
	stamp := time.Now().UTC().Truncate(time.Microsecond)
	item := &types.FetchedItem{
		ExternalID: "native-latest",
		Content:    []byte("new complete content"),
		Title:      "latest",
		FileName:   "latest.txt",
		UpdatedAt:  stamp,
	}
	k := types.Knowledge{
		ID:              uuid.NewString(),
		TenantID:        tenant.ID,
		KnowledgeBaseID: kb.ID,
		Title:           item.Title,
		Type:            "file",
		FileName:        item.FileName,
		FilePath:        "local://synthetic/latest.txt",
		FileHash:        "old-public",
		Metadata:        types.JSON(`{}`),
		ParseStatus:     types.ParseStatusCompleted,
	}
	require.NoError(t, db.Create(&k).Error)
	r := types.FeishuLegacyResource{
		ID:                 legacyResourceID(&ds, item.ExternalID),
		TenantID:           tenant.ID,
		KnowledgeBaseID:    kb.ID,
		DataSourceID:       ds.ID,
		ExternalID:         item.ExternalID,
		KnowledgeID:        k.ID,
		State:              "active",
		Generation:         1,
		SourceRevision:     stamp.Format(time.RFC3339Nano),
		QuarantineSnapshot: json.RawMessage(`{}`),
	}
	a := types.FeishuLegacyAttempt{
		ID:         uuid.NewString(),
		ResourceID: r.ID,
		Generation: 1,
		Status:     "published",
		Revision:   r.SourceRevision,
		SourceHash: LegacySourceHash([]byte("old"), item.FileName, item.Title),
		Manifest:   json.RawMessage(`{}`),
		Receipt:    json.RawMessage(`{}`),
	}
	r.ActiveAttempt = a.ID
	r.CurrentAttempt = a.ID
	require.NoError(t, db.Create(&r).Error)
	require.NoError(t, db.Create(&a).Error)
	store := NewFeishuLegacyStore(db)
	raw := LegacySourceHash(item.Content, item.FileName, item.Title)
	_, _, err = store.Begin(ctx, &ds, item, raw)
	require.ErrorIs(t, err, ErrLegacyVersionFence, "strict path still rejects timestamp collisions")
	b, err := store.LatestSnapshotBaseline(ctx, &ds, item.ExternalID)
	require.NoError(t, err)
	proof, err := b.ProveCurrent(item, item)
	require.NoError(t, err)
	require.NoError(t, db.Model(&r).UpdateColumn("generation", 2).Error)
	_, _, err = store.BeginLatestSourceSnapshot(ctx, &ds, item, raw, nil, proof)
	require.ErrorIs(t, err, ErrLegacyVersionFence, "stale baseline cannot replace a newer generation")
	b, err = store.LatestSnapshotBaseline(ctx, &ds, item.ExternalID)
	require.NoError(t, err)
	proof, err = b.ProveCurrent(item, item)
	require.NoError(t, err)
	current, candidate, err := store.BeginLatestSourceSnapshot(ctx, &ds, item, raw, nil, proof)
	require.NoError(t, err)
	require.Equal(t, int64(3), current.Generation)
	require.Equal(
		t,
		a.ID,
		current.ActiveAttempt,
		"old public version remains active until atomic publication",
	)
	var m types.FeishuLegacyManifest
	require.NoError(t, json.Unmarshal(candidate.Manifest, &m))
	require.NotNil(t, m.LatestSnapshot)
	require.Equal(t, a.ID, m.LatestSnapshot.PreviousAttempt)
	require.Equal(t, raw, m.LatestSnapshot.CurrentHash)
	hash, err := LegacyManifestHash(m)
	require.NoError(t, err)
	require.Equal(t, hash, m.SHA256)
	_, _, err = store.BeginLatestSourceSnapshot(ctx, &ds, item, raw, nil, proof)
	require.ErrorIs(t, err, ErrLegacyVersionFence, "proof is single-generation bound")
	var public types.Knowledge
	require.NoError(t, db.First(&public, "id=?", k.ID).Error)
	require.Equal(t, "old-public", public.FileHash)
}
