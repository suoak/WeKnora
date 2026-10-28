package repository

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

func identityXLSX(t *testing.T, width string) []byte {
	t.Helper()
	var out bytes.Buffer
	z := zip.NewWriter(&out)
	for _, p := range [][2]string{
		{"[Content_Types].xml", `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"/>`},
		{"_rels/.rels", `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"/>`},
		{"xl/workbook.xml", `<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"/>`},
		{"xl/worksheets/sheet1.xml", `<worksheet ` +
			`xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><cols>` +
			`<col min="1" max="1" width="` + width + `" customWidth="1"/></cols>` +
			`<sheetData><row r="1"><c r="A1"><v>42</v></c></row></sheetData></worksheet>`},
	} {
		w, err := z.Create(p[0])
		require.NoError(t, err)
		_, err = w.Write([]byte(p[1]))
		require.NoError(t, err)
	}
	require.NoError(t, z.Close())
	return out.Bytes()
}

func TestFeishuXLSXSourceIdentityPostgres(t *testing.T) {
	dsn := feishulegacy.PrivateDSN(t)
	root, err := filepath.Abs("../../..")
	require.NoError(t, err)
	t.Chdir(root)
	require.NoError(t, database.RunMigrations(dsn))
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	tenant := types.Tenant{ID: 809919, Name: "xlsx-identity-synthetic", Status: "active"}
	require.NoError(t, db.Create(&tenant).Error)
	model := types.Model{ID: uuid.NewString(), TenantID: tenant.ID, Name: "synthetic", Type: types.ModelTypeEmbedding}
	require.NoError(t, db.Create(&model).Error)
	kb := types.KnowledgeBase{
		ID: uuid.NewString(), TenantID: tenant.ID, Name: "xlsx", Type: "document",
		EmbeddingModelID: model.ID,
	}
	require.NoError(t, db.Create(&kb).Error)
	ds := types.DataSource{
		ID: uuid.NewString(), TenantID: tenant.ID, KnowledgeBaseID: kb.ID,
		Type: "feishu", Status: "active", Config: types.JSON(`{}`), LegacyAccessState: "active",
	}
	require.NoError(t, db.Create(&ds).Error)
	log := types.SyncLog{
		ID: uuid.NewString(), TenantID: tenant.ID, DataSourceID: ds.ID,
		Status: "running", StartedAt: time.Now().UTC(), ExecutionGeneration: 1, ExecutionClaimed: true,
	}
	require.NoError(t, db.Create(&log).Error)
	ctx := types.WithSyncExecution(context.Background(), types.SyncExecution{
		DataSourceID: ds.ID, TenantID: tenant.ID, SyncLogID: log.ID, Generation: 1,
	})
	old, current := identityXLSX(t, "10"), identityXLSX(t, "11.5")
	metadata, _ := json.Marshal(map[string]string{
		"obj_type": "sheet", "datasource_id": ds.ID,
		"external_id": "sheet:synthetic",
	})
	k := types.Knowledge{
		ID: uuid.NewString(), TenantID: tenant.ID, KnowledgeBaseID: kb.ID,
		Type: "file", FileName: "source.xlsx", Title: "source", FilePath: "local://synthetic/source.xlsx",
		FileHash: "fixture-file", Metadata: metadata, ParseStatus: types.ParseStatusCompleted,
	}
	require.NoError(t, db.Create(&k).Error)
	manifest := types.FeishuLegacyManifest{Knowledge: k}
	manifest.SHA256, err = LegacyManifestHash(manifest)
	require.NoError(t, err)
	rawManifest, err := json.Marshal(manifest)
	require.NoError(t, err)
	a := types.FeishuLegacyAttempt{
		ID: uuid.NewString(), Generation: 1, Status: "published",
		SourceHash: LegacySourceHash(old, k.FileName, k.Title), Manifest: rawManifest,
	}
	r := types.FeishuLegacyResource{
		ID: legacyResourceID(&ds, "sheet:synthetic"), TenantID: tenant.ID,
		KnowledgeBaseID: kb.ID, DataSourceID: ds.ID, ExternalID: "sheet:synthetic", KnowledgeID: k.ID,
		State: "active", Generation: 1, ActiveAttempt: a.ID, CurrentAttempt: a.ID,
		SourceRevision: time.Now().UTC().Format(time.RFC3339Nano),
	}
	a.ResourceID = r.ID
	a.Revision = r.SourceRevision
	require.NoError(t, db.Create(&r).Error)
	require.NoError(t, db.Create(&a).Error)
	store := NewFeishuLegacyStore(db)
	baseline, err := store.SourceIdentityBaseline(ctx, &ds, r.ExternalID)
	require.NoError(t, err)
	proof, err := baseline.ProveXLSX(old)
	require.NoError(t, err)
	_, err = baseline.ProveXLSX(append(append([]byte(nil), old...), 0))
	require.ErrorIs(t, err, ErrLegacyVersionFence, "archive raw hash must match before canonicalization")
	identity, err := LegacyXLSXSourceIdentity(current, k.FileName, k.Title)
	require.NoError(t, err)
	revision, err := time.Parse(time.RFC3339Nano, r.SourceRevision)
	require.NoError(t, err)
	item := &types.FetchedItem{
		ExternalID: r.ExternalID, UpdatedAt: revision,
		FileName: k.FileName, Title: k.Title, Metadata: map[string]string{"obj_type": "sheet"},
	}
	_, reused, err := store.BeginWithSourceIdentity(ctx, &ds, item,
		LegacySourceHash(current, k.FileName, k.Title), identity, proof)
	require.NoError(t, err)
	require.Equal(t, a.ID, reused.ID, "unchanged active export does not allocate another attempt or Knowledge")
	for _, kind := range []string{"docx", "file"} {
		invalid := *item
		invalid.Metadata = map[string]string{"obj_type": kind}
		_, _, e := store.BeginWithSourceIdentity(ctx, &ds, &invalid,
			LegacySourceHash(current, k.FileName, k.Title), identity, proof)
		require.ErrorIs(t, e, ErrLegacyVersionFence, "table identity cannot authorize another resource type")
	}
	for _, names := range [][2]string{{"renamed.xlsx", k.Title}, {k.FileName, "new title"}} {
		changed, e := LegacyXLSXSourceIdentity(current, names[0], names[1])
		require.NoError(t, e)
		require.NotEqual(t, identity, changed)
	}
	r.State, ds.LegacyAccessState = "hold", "hold"
	require.NoError(t, db.Model(&r).UpdateColumn("state", "hold").Error)
	require.NoError(t, db.Model(&ds).UpdateColumn("legacy_access_state", "hold").Error)
	_, err = store.SourceIdentityBaseline(ctx, &ds, r.ExternalID)
	require.Error(t, err, "ordinary execution cannot read held baseline")
	recovering := types.WithLegacyFullReconciliation(ctx)
	_, err = store.SourceIdentityBaseline(recovering, &ds, r.ExternalID)
	require.Error(t, err, "a caller flag cannot replace the validated publication guard")
	denied := errors.New("synthetic revoked recovery authority")
	recovering = types.WithFeishuSourceCommitGuard(recovering, func(*gorm.DB) error { return denied })
	_, err = store.SourceIdentityBaseline(recovering, &ds, r.ExternalID)
	require.ErrorIs(t, err, denied)
	recovering = types.WithFeishuSourceCommitGuard(types.WithLegacyFullReconciliation(ctx),
		func(*gorm.DB) error { return nil })
	baseline, err = store.SourceIdentityBaseline(recovering, &ds, r.ExternalID)
	require.NoError(t, err)
	proof, err = baseline.ProveXLSX(old)
	require.NoError(t, err)
	require.NoError(t, db.WithContext(recovering).Transaction(func(tx *gorm.DB) error {
		match, e := legacyAttemptSourceMatches(tx, &r, &a,
			LegacySourceHash(current, k.FileName, k.Title), identity, proof)
		require.NoError(t, e)
		require.True(t, match)
		return nil
	}))
	var after types.DataSource
	require.NoError(t, db.First(&after, "id=?", ds.ID).Error)
	require.Equal(t, "hold", after.LegacyAccessState, "equivalence proof never releases a hold")
}
