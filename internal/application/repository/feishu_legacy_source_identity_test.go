package repository

import (
	"archive/zip"
	"bytes"
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

func identityDOCX(t *testing.T, created, body string) []byte {
	t.Helper()
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for _, part := range [][2]string{
		{"_rels/.rels", `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"/>`},
		{"[Content_Types].xml", `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"/>`},
		{
			"word/document.xml",
			`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` +
				`<w:body>` +
				`<w:p>` +
				`<w:r>` +
				`<w:t>` + body + `</w:t>` +
				`</w:r>` +
				`</w:p>` +
				`</w:body>` +
				`</w:document>`,
		},

		{
			"docProps/core.xml",
			`<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties"` +
				` xmlns:dcterms="http://purl.org/dc/terms/">` +
				`<dcterms:created>` + created + `</dcterms:created>` +
				`</cp:coreProperties>`,
		},
	} {
		w, err := z.Create(part[0])
		require.NoError(t, err)
		_, err = w.Write([]byte(part[1]))
		require.NoError(t, err)
	}
	require.NoError(t, z.Close())
	return b.Bytes()
}

func TestLegacyDOCXSourceIdentityBindsNamesP1(t *testing.T) {
	a, b := identityDOCX(t, "2026-10-24T01:00:00Z", "content"), identityDOCX(t, "2026-10-24T02:00:00Z", "content")
	first, err := LegacyDOCXSourceIdentity(a, "source.docx", "title")
	require.NoError(t, err)
	second, err := LegacyDOCXSourceIdentity(b, "source.docx", "title")
	require.NoError(t, err)
	require.Equal(t, first, second)
	require.NotEqual(t, LegacySourceHash(a, "source.docx", "title"), LegacySourceHash(b, "source.docx", "title"))
	for _, names := range [][2]string{{"rename.docx", "title"}, {"source.docx", "changed title"}} {
		changed, e := LegacyDOCXSourceIdentity(b, names[0], names[1])
		require.NoError(t, e)
		require.NotEqual(t, first, changed)
	}
}

func TestFeishuSourceIdentityPostgresP1(t *testing.T) {
	dsn := feishulegacy.PrivateDSN(t)
	root, err := filepath.Abs("../../..")
	require.NoError(t, err)
	t.Chdir(root)
	require.NoError(t, database.RunMigrations(dsn))
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec("CREATE INDEX source_identity_hnsw3 ON embeddings USING hnsw "+
		"((embedding::halfvec(3)) halfvec_cosine_ops) WHERE dimension=3").Error)
	tenant := &types.Tenant{ID: 809917, Name: "source-identity-synthetic", Status: "active"}
	require.NoError(t, db.Create(tenant).Error)
	model := &types.Model{ID: uuid.NewString(), TenantID: tenant.ID, Name: "synthetic", Type: types.ModelTypeEmbedding}
	require.NoError(t, db.Create(model).Error)
	kb := &types.KnowledgeBase{
		ID:               uuid.NewString(),
		TenantID:         tenant.ID,
		Name:             "identity",
		Type:             "document",
		EmbeddingModelID: model.ID,
	}
	require.NoError(t, db.Create(kb).Error)
	ds := &types.DataSource{
		ID:              uuid.NewString(),
		TenantID:        tenant.ID,
		KnowledgeBaseID: kb.ID,
		Type:            "feishu",
		Status:          "active",
		Config:          types.JSON(`{}`),
	}
	require.NoError(t, db.Create(ds).Error)
	log := &types.SyncLog{
		ID:                  uuid.NewString(),
		TenantID:            tenant.ID,
		DataSourceID:        ds.ID,
		Status:              "running",
		ExecutionGeneration: 1,
		ExecutionClaimed:    true,
	}
	require.NoError(t, db.Create(log).Error)
	ctx := types.WithSyncExecution(context.Background(),
		types.SyncExecution{
			DataSourceID: ds.ID,
			TenantID:     tenant.ID,
			SyncLogID:    log.ID,
			Generation:   1,
		})
	store := NewFeishuLegacyStore(db)
	stamp, err := store.ModelStamp(ctx, model.ID)
	require.NoError(t, err)
	contentA,
		contentB := identityDOCX(t,
		"2026-10-24T01:00:00Z",
		"canary"),
		identityDOCX(t,
			"2026-10-24T02:00:00Z",
			"canary")
	item := &types.FetchedItem{
		ExternalID: "new-export",
		UpdatedAt: time.Date(2026,
			10,
			24,
			1,
			0,
			0,
			0,
			time.UTC),
		FileName: "source.docx",
		Title:    "canary",
		Metadata: map[string]string{"obj_type": "docx"},
	}
	rawA,
		rawB := LegacySourceHash(contentA,
		item.FileName,
		item.Title),
		LegacySourceHash(contentB,
			item.FileName,
			item.Title)
	identity, err := LegacyDOCXSourceIdentity(contentA, item.FileName, item.Title)
	require.NoError(t, err)
	initialResource, first, err := store.BeginWithSourceIdentity(ctx, ds, item, rawA, identity, nil)
	require.NoError(t, err)
	var before types.FeishuLegacyManifest
	require.NoError(t, json.Unmarshal(first.Manifest, &before))
	require.Equal(t, identity, before.SourceIdentity, "persist provenance before parser or Prepare")
	originalManifest := append([]byte(nil), first.Manifest...)
	tampered := before
	tamperedIdentity := *identity
	tamperedIdentity.Digest = LegacySourceHash([]byte("tampered"), "", "")
	tampered.SourceIdentity = &tamperedIdentity
	tamperedJSON, err := json.Marshal(tampered)
	require.NoError(t, err)
	require.NoError(t, db.Model(first).Update("manifest", json.RawMessage(tamperedJSON)).Error)
	_, _, err = store.BeginWithSourceIdentity(ctx, ds, item, rawB, &tamperedIdentity, nil)
	require.ErrorIs(t, err, ErrLegacyVersionFence, "identity equality cannot bypass a damaged manifest SHA")
	var fencedResource types.FeishuLegacyResource
	require.NoError(t, db.First(&fencedResource, "id=?", initialResource.ID).Error)
	require.Equal(t, initialResource.SourceRevision, fencedResource.SourceRevision)
	require.Equal(t, initialResource.Generation, fencedResource.Generation)
	require.NoError(t, db.Model(first).Update("manifest", json.RawMessage(originalManifest)).Error)
	_, _, err = store.BeginWithSourceIdentity(ctx, ds, item, rawB, identity, nil)
	require.ErrorIs(t, err, ErrLegacyVersionBusy, "equivalence cannot bypass live lease")
	require.NoError(t, store.Abandon(ctx, first))
	r, next, err := store.BeginWithSourceIdentity(ctx, ds, item, rawB, identity, nil)
	require.NoError(t, err, "failed-before-manifest retry accepts equivalent export packaging")
	require.Greater(t, next.Generation, first.Generation)
	changed,
		err := LegacyDOCXSourceIdentity(identityDOCX(t,
		"2026-10-24T02:00:00Z",
		"changed body"),
		item.FileName,
		item.Title)
	require.NoError(t, err)
	_, _, err = store.BeginWithSourceIdentity(ctx, ds, item, rawA, changed, nil)
	require.ErrorIs(t, err, ErrLegacyVersionFence, "real same-timestamp content change stays fenced")
	metadata,
		_ := json.Marshal(map[string]string{
		"datasource_id": ds.ID,
		"external_id":   item.ExternalID,
		"obj_type":      "docx",
	})
	manifest := types.FeishuLegacyManifest{
		Knowledge: types.Knowledge{
			ID:              r.KnowledgeID,
			TenantID:        tenant.ID,
			KnowledgeBaseID: kb.ID,
			Title:           item.Title,
			FileName:        item.FileName,
			Type:            "file",
			Channel:         "feishu",
			FileHash:        "raw-file",
			FilePath:        "local://synthetic/archive.docx",
			Metadata:        metadata,
		},

		Chunks: []*types.Chunk{{
			ID:              uuid.NewString(),
			TenantID:        tenant.ID,
			KnowledgeBaseID: kb.ID,
			KnowledgeID:     r.KnowledgeID,
			ChunkType:       "text",
			Content:         "identity canary",
			IsEnabled:       true,
			Metadata:        types.JSON(`{}`),
		}},

		EmbeddingModelID: model.ID, KBConfigSHA256: LegacyKBConfigHash(kb), ModelStamp: stamp, Dimension: 3,
	}
	require.NoError(t, store.Prepare(ctx, ds, next, manifest, [][]float32{{1, 0, 0}}))
	require.NoError(t, db.First(next, "id=?", next.ID).Error)
	var prepared types.FeishuLegacyManifest
	require.NoError(t, json.Unmarshal(next.Manifest, &prepared))
	require.Equal(t, identity, prepared.SourceIdentity)
	_, err = store.Publish(ctx, ds, next)
	require.NoError(t, err)
	_, duplicate, err := store.BeginWithSourceIdentity(ctx, ds, item, rawA, identity, nil)
	require.NoError(t, err)
	require.Equal(t, next.ID, duplicate.ID)
	_, err = store.Receipt(ctx, r, duplicate)
	require.NoError(t, err, "equivalent retry preserves public receipt and graph")

	// A legacy active raw contract can bridge only through its verified original archive.
	item.ExternalID = "legacy-export"
	r, old, err := store.Begin(ctx, ds, item, rawA)
	require.NoError(t, err)
	manifest.Knowledge.ID = r.KnowledgeID
	metadata,
		_ = json.Marshal(map[string]string{
		"datasource_id": ds.ID,
		"external_id":   item.ExternalID,
		"obj_type":      "docx",
	})
	manifest.Knowledge.Metadata = metadata
	manifest.Chunks[0].ID, manifest.Chunks[0].KnowledgeID = uuid.NewString(), r.KnowledgeID
	require.NoError(t, store.Prepare(ctx, ds, old, manifest, [][]float32{{1, 0, 0}}))
	_, err = store.Publish(ctx, ds, old)
	require.NoError(t, err)
	_, _, err = store.BeginWithSourceIdentity(ctx, ds, item, rawB, identity, nil)
	require.ErrorIs(t, err, ErrLegacyVersionFence)
	baseline, err := store.SourceIdentityBaseline(ctx, ds, item.ExternalID)
	require.NoError(t, err)
	_, err = baseline.ProveDOCX(contentB)
	require.ErrorIs(t, err, ErrLegacyVersionFence, "canonical equality alone is no raw archive proof")
	proof, err := baseline.ProveDOCX(contentA)
	require.NoError(t, err)
	_, duplicate, err = store.BeginWithSourceIdentity(ctx, ds, item, rawB, identity, proof)
	require.NoError(t, err)
	require.Equal(t, old.ID, duplicate.ID)
	var preserved types.FeishuLegacyAttempt
	require.NoError(t, db.First(&preserved, "id=?", old.ID).Error)
	require.Equal(t, rawA, preserved.SourceHash)
	require.Equal(t,
		string(baseline.attempt.Manifest),
		string(preserved.Manifest),
		"never rewrite old manifest or receipt")
	_, err = store.Receipt(ctx, r, duplicate)
	require.NoError(t, err)
	require.NoError(t, db.Model(r).Update("generation", r.Generation+1).Error)
	_, _, err = store.BeginWithSourceIdentity(ctx, ds, item, rawB, identity, proof)
	require.ErrorIs(t, err, ErrLegacyVersionFence, "stale generation proof fails closed")

	item.ExternalID = "legacy-failed-unarchived"
	_, failed, err := store.Begin(ctx, ds, item, rawA)
	require.NoError(t, err)
	require.NoError(t, store.Abandon(ctx, failed))
	_, _, err = store.BeginWithSourceIdentity(ctx, ds, item, rawB, identity, nil)
	require.ErrorIs(t, err, ErrLegacyVersionFence)
	_, err = store.SourceIdentityBaseline(ctx, ds, item.ExternalID)
	require.ErrorIs(t, err, ErrLegacyVersionFence, "failed raw observation cannot adopt an unproved canonical digest")
}
