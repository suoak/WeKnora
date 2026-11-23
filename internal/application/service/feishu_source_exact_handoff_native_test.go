//go:build anydoc && personal_exact_live

package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	storagefiles "github.com/Tencent/WeKnora/internal/application/service/file"
	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/infrastructure/docparser"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type sourceHandoffNativeReader struct{ *docparser.AnydocReader }

func (sourceHandoffNativeReader) Reconnect(string) error { return nil }
func (sourceHandoffNativeReader) IsConnected() bool      { return true }
func (sourceHandoffNativeReader) ListEngines(context.Context, map[string]string) ([]types.ParserEngineInfo, error) {
	return nil, nil
}

// Synthetic remote APIs and models; real parser, exact publication manifests,
// source fences, PostgreSQL indexes and the final publication transaction.
func TestPersonalExactNativeSourceHandoffPreservesOldPublicationOnRollback(t *testing.T) {
	t.Setenv("FEISHU_ACCESS_V2_ENABLED", "1")
	t.Setenv("FEISHU_SAFE_INCREMENTAL_ENABLED", "true")
	t.Setenv("FEISHU_SYNC_SAFETY_ENABLED", "false") // No deployment controller in this private fixture.
	t.Setenv("RETRIEVE_DRIVER", "postgres")
	exerciseNativeResumeWithFormat(t, false, false, resumeNativeDOCX(t), nativeResumeFormatOptions{
		Format: "docx", ReturnAfterPublication: true, UserOAuthImport: true,
		ConfigureFixture: func(f *personalRecoveryFixture) {
			cfg, err := f.s.app(f.ctx)
			require.NoError(t, err)
			cfg.Capabilities = &types.FeishuAccessCapabilities{PersonalEnabled: true}
			admin := context.WithValue(f.ctx, types.SystemAdminContextKey, true)
			require.NoError(t, f.s.SaveApp(admin, cfg))
		},
		AssertPublished: func(f *personalRecoveryFixture, _ *personalExactDocument, old types.PersonalExactReceipt) {
			exerciseExactSourceHandoff(t, f, old)
		},
	})
}

func exerciseExactSourceHandoff(t *testing.T, f *personalRecoveryFixture, old types.PersonalExactReceipt) {
	t.Helper()
	// The first-create fixture installed migration 3036 before its embedding
	// table existed. Apply its unchanged candidate/index block to this private
	// schema now, matching the normal full-schema production migration order.
	ddl, err := os.ReadFile("../../../migrations/versioned/003036_feishu_legacy_atomic.up.sql")
	require.NoError(t, err)
	start := strings.Index(string(ddl), "DO $$ BEGIN")
	require.GreaterOrEqual(t, start, 0)
	end := strings.Index(string(ddl)[start:], "END $$;")
	require.GreaterOrEqual(t, end, 0)
	candidateDDL := string(ddl)[start : start+end+len("END $$;")]
	candidateDDL = strings.ReplaceAll(candidateDDL, "public.embeddings", "embeddings")
	require.NoError(t, f.db.Exec(candidateDDL).Error)
	f.s.dataSources = &v2HandoffSources{db: f.db}
	ds, err := f.s.EnableImportSource(f.ctx, old.KnowledgeBaseID, old.ImportID, "")
	require.NoError(t, err)
	require.Equal(t, types.DataSourceStatusActive, ds.Status)
	require.NoError(t, f.p.verifyExactPublishedDatabase(f.ctx, old), "enrollment retains the old serving receipt")
	_, err = f.s.EnableImportSource(f.userContext("B"), old.KnowledgeBaseID, old.ImportID, "")
	require.Error(t, err, "another KB administrator cannot adopt this import's credential identity")
	for _, field := range []string{"owner", "account", "configuration"} {
		t.Run("reject_changed_"+field, func(t *testing.T) {
			changed := *ds
			cfg, e := ds.ParseConfig()
			require.NoError(t, e)
			ref, e := types.ParseFeishuSourceReference(cfg)
			require.NoError(t, e)
			switch field {
			case "owner":
				ref.OwnerUserID = "B"
			case "account":
				ref.ConnectionID = "unrelated-account"
			case "configuration":
				ref.IdentityVersion = "unrelated-configuration"
			}
			cfg.Settings["credential_reference"] = ref
			changed.Config, e = cfg.ToJSON()
			require.NoError(t, e)
			_, e = repository.FindPersonalExactSourceHandoff(f.db.WithContext(f.ctx), &changed,
				"file:real", false)
			require.Error(t, e)
			require.NoError(t, f.p.verifyExactPublishedDatabase(f.ctx, old))
		})
	}
	var imported types.FeishuPersonalImport
	require.NoError(t, f.db.Where("id=?", old.ImportID).Take(&imported).Error)
	require.Equal(t, "indexed", imported.Status)
	// A later administrator pause is never undone by retrying enrollment.
	require.NoError(t, f.db.Model(ds).Update("status", types.DataSourceStatusPaused).Error)
	paused, err := f.s.EnableImportSource(f.ctx, old.KnowledgeBaseID, old.ImportID, "")
	require.NoError(t, err)
	require.Equal(t, types.DataSourceStatusPaused, paused.Status)
	// The fixture's explicit owner resume is separate from enrollment retry.
	require.NoError(t, f.s.dataSources.ResumeDataSource(f.ctx, ds.ID))
	ds, err = f.s.dataSources.GetDataSource(f.ctx, ds.ID)
	require.NoError(t, err)
	require.NoError(t, f.db.AutoMigrate(&types.SyncLog{}))
	log := &types.SyncLog{
		ID: uuid.NewString(), DataSourceID: ds.ID, TenantID: ds.TenantID,
		Status: types.SyncLogStatusRunning, ExecutionGeneration: 1, ExecutionClaimed: true,
	}
	require.NoError(t, f.db.Create(log).Error)
	f.ks.kbService = &v2PipelineKB{legacyLiveKB{db: f.db}}
	f.ks.tenantService = &legacyNoCloud{}
	f.ks.documentReader = sourceHandoffNativeReader{docparser.NewAnydocReader(nil, nil)}
	f.ks.resourceCatalog = NewResourceCatalog(repository.NewResourceRepository(f.db))
	f.ks.fileSvc = storagefiles.NewResourceCatalogFileService(f.ks.fileSvc, f.ks.resourceCatalog)
	ctx := types.WithSyncExecution(f.ctx, types.SyncExecution{
		DataSourceID: ds.ID,
		SyncLogID:    log.ID, TenantID: ds.TenantID, Generation: 1,
	})
	ctx, err = restoreSummaryRefreshTenantInfo(ctx, f.ks.tenantRepo, ds.TenantID)
	require.NoError(t, err)
	ingest := &DataSourceService{knowledgeService: f.ks, feishuIdentity: f.s}
	f.revision.Add(60)
	bound, err := ingest.bindFeishuSource(types.WithLegacyReceiptSet(ctx), ds)
	require.NoError(t, err)
	cfg, err := f.s.app(f.ctx)
	require.NoError(t, err)
	client := f.s.client(&core.Config{AppID: cfg.AppID, AppSecret: cfg.Secret})
	resource, err := core.ParsePersonalResourceURL(imported.URL)
	require.NoError(t, err)
	resource, err = client.ResolveBoundResource(bound, resource)
	require.NoError(t, err)
	items, err := (core.ReferenceLinkOps{}).Fetch(bound, client, resource, "links", false)
	require.NoError(t, err)
	require.Len(t, items, 1)
	regressed := *items[0]
	regressed.UpdatedAt = time.Unix(0, 0)
	hash := sha256.Sum256(regressed.Content)
	_, _, err = repository.NewFeishuLegacyStore(f.db).Begin(bound, ds, &regressed, hex.EncodeToString(hash[:]))
	require.ErrorContains(t, err, "source_revision_regressed")
	require.NoError(t, f.p.verifyExactPublishedDatabase(f.ctx, old))
	stale := types.WithSyncExecution(bound, types.SyncExecution{
		DataSourceID: ds.ID,
		SyncLogID:    log.ID, TenantID: ds.TenantID, Generation: 2,
	})
	_, err = ingest.ingestItem(stale, ds, items[0], nil)
	require.Error(t, err, "a stale execution cannot replace or retire the exact publication")
	require.NoError(t, f.p.verifyExactPublishedDatabase(f.ctx, old))
	// Fail after the new indexes have been written, while moving the old proof
	// to audit history. PostgreSQL must roll back every public write together.
	require.NoError(t, f.db.Exec(`CREATE FUNCTION reject_test_source_handoff() RETURNS trigger
LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'controlled handoff publication failure'; END $$;
CREATE TRIGGER reject_test_source_handoff BEFORE UPDATE OF scope ON task_pending_ops
FOR EACH ROW WHEN (OLD.scope='personal_exact_receipt_v1')
EXECUTE FUNCTION reject_test_source_handoff()`).Error)
	_, err = ingest.ingestItem(bound, ds, items[0], nil)
	require.ErrorContains(t, err, "controlled handoff publication failure")
	require.NoError(t, f.p.verifyExactPublishedDatabase(f.ctx, old),
		"failed publication retains all old output manifests")
	var receipt types.TaskPendingOp
	require.NoError(t, f.db.Where("scope=? AND scope_id=?", types.PersonalExactReceiptScope,
		old.KnowledgeID).Take(&receipt).Error)
	require.Equal(t, "published", receipt.Op)
	require.NoError(t, f.db.Exec("DROP TRIGGER reject_test_source_handoff ON task_pending_ops; "+
		"DROP FUNCTION reject_test_source_handoff()").Error)
	updated, err := ingest.ingestItem(bound, ds, items[0], nil)
	require.NoError(t, err)
	require.True(t, updated)
	receipts := types.LegacyReceipts(bound)
	require.Len(t, receipts, 1)
	require.Equal(t, old.KnowledgeID, receipts[0].KnowledgeID)
	require.Positive(t, receipts[0].Vectors)
	require.NoError(t, f.db.Where("scope=? AND scope_id=?", types.PersonalExactSourceHistoryScope,
		old.KnowledgeID).Take(&receipt).Error)
	var retained types.PersonalExactReceipt
	require.NoError(t, json.Unmarshal(receipt.Payload, &retained))
	require.Equal(t, old, retained, "the complete old receipt survives as audit history")
	require.NoError(t, f.db.Where("id=?", old.ImportID).Take(&imported).Error)
	require.Equal(t, "source_managed", imported.Status)
	var count int64
	require.NoError(t, f.db.Model(&types.Knowledge{}).Where("id=?", old.KnowledgeID).Count(&count).Error)
	require.EqualValues(t, 1, count, "one stable Knowledge ID survives the handoff")
}
