package service

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	pgretrieve "github.com/Tencent/WeKnora/internal/application/repository/retriever/postgres"
	files "github.com/Tencent/WeKnora/internal/application/service/file"
	"github.com/Tencent/WeKnora/internal/database"
	"github.com/Tencent/WeKnora/internal/testutil/feishulegacy"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func legacyIdentityServiceDOCX(t *testing.T, created string) []byte {
	t.Helper()
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for _, p := range [][2]string{
		{"_rels/.rels", `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"/>`},
		{"[Content_Types].xml", `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"/>`},
		{
			"word/document.xml",
			`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` +
				`<w:body>` +
				`<w:p>` +
				`<w:r>` +
				`<w:t>docxidentitycanary</w:t>` +
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
		w, err := z.Create(p[0])
		require.NoError(t, err)
		_, err = w.Write([]byte(p[1]))
		require.NoError(t, err)
	}
	require.NoError(t, z.Close())
	return b.Bytes()
}

func TestFeishuDOCXSourceIdentityServicePostgresP1(t *testing.T) {
	dsn := feishulegacy.PrivateDSN(t)
	root, err := filepath.Abs("../../..")
	require.NoError(t, err)
	t.Chdir(root)
	require.NoError(t, database.RunMigrations(dsn))
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t,
		db.Exec("CREATE INDEX source_identity_hnsw3 ON embeddings USING hnsw "+
			"((embedding::halfvec(3)) halfvec_cosine_ops) WHERE dimension=3").Error)
	tenant := &types.Tenant{ID: 809918, Name: "identity-service-synthetic", Status: "active"}
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
	ctx = context.WithValue(ctx, types.TenantInfoContextKey, tenant)
	ctx = context.WithValue(ctx, types.TenantIDContextKey, tenant.ID)
	localRoot := t.TempDir()
	t.Setenv("LOCAL_STORAGE_BASE_DIR", localRoot)
	ks := &knowledgeService{
		repo:      repository.NewKnowledgeRepository(db),
		kbService: &legacyLiveKB{db: db},
		fileSvc: files.NewLocalFileService(localRoot,
			""),
	}
	store := repository.NewFeishuLegacyStore(db)
	stamp, err := store.ModelStamp(ctx, model.ID)
	require.NoError(t, err)
	oldContent := legacyIdentityServiceDOCX(t, "2026-10-24T01:00:00Z")
	newContent := legacyIdentityServiceDOCX(t, "2026-10-24T02:00:00Z")
	item := &types.FetchedItem{
		ExternalID: "legacy-export",
		UpdatedAt: time.Date(2026,
			10,
			24,
			1,
			0,
			0,
			0,
			time.UTC),
		FileName: "source.docx",
		Title:    "docxidentitycanary",
		Content:  oldContent,
		Metadata: map[string]string{"obj_type": "docx"},
	}
	raw := repository.LegacySourceHash(item.Content, item.FileName, item.Title)
	r, a, err := store.Begin(ctx, ds, item, raw)
	require.NoError(t, err)
	path, err := ks.fileSvc.SaveBytes(ctx, oldContent, tenant.ID, "source.docx", false)
	require.NoError(t, err)
	metadata,
		_ := json.Marshal(map[string]string{
		"datasource_id": ds.ID,
		"external_id":   item.ExternalID,
		"obj_type":      "docx",
	})
	m := types.FeishuLegacyManifest{
		Knowledge: types.Knowledge{
			ID:              r.KnowledgeID,
			TenantID:        tenant.ID,
			KnowledgeBaseID: kb.ID,
			Title:           item.Title,
			FileName:        item.FileName,
			Type:            "file",
			Channel:         "feishu",
			FileHash:        "archive-fingerprint",
			FilePath:        path,
			Metadata:        metadata,
		},

		Chunks: []*types.Chunk{{
			ID:              uuid.NewString(),
			TenantID:        tenant.ID,
			KnowledgeBaseID: kb.ID,
			KnowledgeID:     r.KnowledgeID,
			ChunkType:       "text",
			Content:         "docxidentitycanary",
			IsEnabled:       true,
			Metadata:        types.JSON(`{}`),
		}},

		EmbeddingModelID: model.ID, KBConfigSHA256: repository.LegacyKBConfigHash(kb), ModelStamp: stamp, Dimension: 3,
	}
	require.NoError(t, store.Prepare(ctx, ds, a, m, [][]float32{{1, 0, 0}}))
	_, err = store.Publish(ctx, ds, a)
	require.NoError(t, err)
	item.Content = newContent
	_, duplicate, err := ks.beginLegacySourceIdentity(ctx, kb, store, ds, item)
	require.NoError(t, err, "actual FileService read verifies the archived raw contract")
	require.Equal(t, a.ID, duplicate.ID)
	require.Equal(t, raw, duplicate.SourceHash, "old raw hash is never rewritten")
	_, err = store.Receipt(ctx, r, duplicate)
	require.NoError(t, err)
	for _, meta := range []map[string]string{
		{"obj_type": "file"}, {"obj_type": "doc"}, {"obj_type": "docx", "attachment": "true"},
	} {
		item.Metadata = meta
		_, _, err = ks.beginLegacySourceIdentity(ctx, kb, store, ds, item)
		require.ErrorIs(t, err, repository.ErrLegacyVersionFence, "unconfirmed export paths retain the raw hash contract")
	}
	item.Metadata = map[string]string{"obj_type": "docx"}
	assertRecall := func() {
		t.Helper()
		engine := pgretrieve.NewPostgresRetrieveEngineRepository(types.EnableLegacyReadGuards(db))
		for _, kind := range []types.RetrieverType{types.KeywordsRetrieverType, types.VectorRetrieverType} {
			rows, e := engine.Retrieve(types.WithLegacyReadScope(ctx), types.RetrieveParams{
				Query: "docxidentitycanary", Embedding: []float32{1, 0, 0}, KnowledgeBaseIDs: []string{kb.ID},
				KnowledgeIDs: []string{r.KnowledgeID}, TopK: 5, Threshold: 0, RetrieverType: kind,
			})
			require.NoError(t, e)
			found := 0
			for _, row := range rows {
				require.NoError(t, row.Error)
				found += len(row.Results)
			}
			require.Positive(t, found, "legacy proof preserves actual keyword and vector recall")
		}
	}
	assertRecall()
	require.NoError(t, ks.fileSvc.DeleteFile(ctx, path))
	_, _, err = ks.beginLegacySourceIdentity(ctx, kb, store, ds, item)
	require.Error(t, err, "missing original archive cannot adopt a new digest")
	assertRecall()
	item.ExternalID = "failed-unarchived"
	_, failed, err := store.Begin(ctx, ds, item, raw)
	require.NoError(t, err)
	require.NoError(t, store.Abandon(ctx, failed))
	_, _, err = ks.beginLegacySourceIdentity(ctx, kb, store, ds, item)
	require.ErrorIs(t, err, repository.ErrLegacyVersionFence)
	assertRecall()
	item.ExternalID, item.Content = "uploaded-docx", oldContent
	item.Metadata = map[string]string{"obj_type": "file"}
	_, uploaded, err := ks.beginLegacySourceIdentity(ctx, kb, store, ds, item)
	require.NoError(t, err)
	var rawOnly types.FeishuLegacyManifest
	require.NoError(t, json.Unmarshal(uploaded.Manifest, &rawOnly))
	require.Nil(t, rawOnly.SourceIdentity, "ordinary DOCX uploads never gain export equivalence")
	require.NoError(t, store.Abandon(ctx, uploaded))
	item.Content = newContent
	_, _, err = ks.beginLegacySourceIdentity(ctx, kb, store, ds, item)
	require.ErrorIs(t, err, repository.ErrLegacyVersionFence)
}

func TestLegacyDOCXExportScopeP1(t *testing.T) {
	for _, tc := range []struct {
		name       string
		objectType string
		attachment string
		want       bool
	}{
		{"source.docx", "docx", "", true},
		{"source.docx", "file", "", false},
		{"source.docx", "doc", "", false},
		{"source.docx", "", "", false},
		{"source.docx", "docx", "true", false},
		{"source.md", "docx", "", false},
	} {
		item := &types.FetchedItem{
			FileName: tc.name,
			Metadata: map[string]string{"obj_type": tc.objectType, "attachment": tc.attachment},
		}
		require.Equal(t, tc.want, legacyDOCXExport(item))
	}
}
