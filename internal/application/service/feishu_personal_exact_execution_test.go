package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func exactExecutionFixture(t *testing.T) (*personalRecoveryFixture, *personalExactDocument, context.Context) {
	t.Helper()
	f := newMVPFixture(t)
	m := f.prepare("https://tenant.feishu.cn/file/real")
	require.NoError(t, f.p.Process(f.ctx, f.payload(m.AttemptID)))
	k, _, _ := mvpPublicTask(t, f, m)
	require.NoError(t, f.db.AutoMigrate(&types.StoredResource{}, &types.ResourceBinding{}, &types.DataSource{}))
	require.NoError(t, f.db.Exec(`CREATE TABLE feishu_legacy_resources (
id text PRIMARY KEY, tenant_id bigint,knowledge_base_id text,knowledge_id text,state text,
original_file_path text,external_id text)`).Error)
	require.NoError(t, f.db.Exec(`CREATE TABLE feishu_legacy_attempts (id text,resource_id text,manifest jsonb)`).Error)
	require.NoError(t, f.db.Create(&types.DataSource{
		ID: "held-sibling", TenantID: 1, KnowledgeBaseID: "kb", Type: "feishu_drive",
		Status: "error", LegacyAccessState: "hold", Config: types.JSON(`{}`),
	}).Error)
	a := mustPersonalAttempt(t, f, m.AttemptID)
	manifest, err := f.p.store.read(a)
	require.NoError(t, err)
	resource := types.StoredResource{
		ID: "exact-file", Handle: "abcdefghijklmnopqrstuv", TenantID: 1, Provider: "local",
		PhysicalPath: k.FilePath, LocationHash: "fixture-location", Size: k.FileSize,
		ContentHash: manifest.Hash, State: "active", Lifecycle: "persistent",
	}
	require.NoError(t, f.db.Create(&resource).Error)
	require.NoError(t, f.db.Create(&types.ResourceBinding{
		ID: "exact-binding", ResourceID: resource.ID, TenantID: 1,
		OwnerType: "knowledge", OwnerID: k.ID, Relation: "source_file",
	}).Error)
	require.NoError(t, f.db.Exec("UPDATE knowledges SET file_path=? WHERE id=?",
		types.BuildResourcePath(resource.Handle), k.ID).Error)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer access-A" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		switch r.URL.Path {
		case "/open-apis/drive/v1/metas/batch_query":
			_, _ = fmt.Fprint(
				w,
				`{"code":0,"data":{"metas":[{"doc_type":"file","doc_token":"real","latest_modify_time":"1"}]}}`,
			)
		case "/open-apis/drive/v1/permissions/real/members/auth":
			_, _ = fmt.Fprint(w, `{"code":0,"data":{"auth_result":true}}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	f.s.newClient = func(cfg *core.Config) *core.Client {
		cfg.BaseURL = server.URL
		return core.NewClient(cfg)
	}
	types.EnableLegacyReadGuards(f.db)
	require.NoError(t, f.db.Exec("CREATE TABLE embeddings (knowledge_id text)").Error)
	owner, err := f.p.repo.Claim(context.Background(), a.ID, f.p.lease)
	require.NoError(t, err)
	proof, err := f.p.prepareExactPersonalDocument(context.Background(), owner)
	require.NoError(t, err)
	ctx, err := f.p.exactDocumentExecutionContext(context.Background(), proof)
	require.NoError(t, err)
	return f, proof, ctx
}

func TestPersonalRecoveryExactExecutionWriteFence(t *testing.T) {
	for _, change := range []string{"valid", "generation", "grant", "binding", "expired", "channel"} {
		t.Run(change, func(t *testing.T) {
			f, proof, ctx := exactExecutionFixture(t)
			switch change {
			case "channel":
				require.NoError(t, f.db.Exec("UPDATE knowledges SET channel='web' WHERE id=?", proof.knowledge).Error)
			case "generation":
				require.NoError(t, f.db.Exec("UPDATE feishu_personal_resources SET generation=generation+1").Error)
			case "grant":
				require.NoError(t, f.db.Exec("UPDATE feishu_personal_grant_epochs SET epoch='revoked'").Error)
			case "binding":
				require.NoError(t, f.db.Exec("UPDATE resource_bindings SET owner_id='other'").Error)
			case "expired":
				require.NoError(
					t,
					f.db.Exec("UPDATE feishu_personal_attempts SET lease_until=now()-interval '1 second'").Error,
				)
			}
			err := f.ks.repo.UpdateKnowledgeColumns(ctx, proof.knowledge, map[string]interface{}{
				"error_message": "fenced_write",
			})
			if change == "valid" {
				require.NoError(t, err)
				var row types.Knowledge
				require.NoError(t, f.db.WithContext(ctx).Where("id=?", proof.knowledge).First(&row).Error)
				require.Equal(t, "fenced_write", row.ErrorMessage)
				// A document proof never authorizes its KB profile or another document.
				require.ErrorIs(t, types.CheckLegacySourceRead(f.db.WithContext(ctx), "", proof.kb),
					types.ErrLegacySourceRestricted)
				require.ErrorIs(t, types.CheckLegacySourceRead(f.db.WithContext(ctx), "other", proof.kb),
					types.ErrLegacySourceRestricted)
			} else {
				require.Error(t, err)
				var row struct{ ErrorMessage string }
				require.NoError(t, f.db.Table("knowledges").Select("error_message").
					Where("id=?", proof.knowledge).Take(&row).Error)
				require.NotEqual(t, "fenced_write", row.ErrorMessage)
			}
		})
	}
}

func TestPersonalRecoveryExactExecutionGuardAndDataRollbackTogether(t *testing.T) {
	f, proof, ctx := exactExecutionFixture(t)
	err := types.WithExactDocumentWrite(ctx, f.db, proof.tenant, proof.knowledge, proof.kb, func(tx *gorm.DB) error {
		require.NoError(t, tx.Table("knowledges").Where("id=?", proof.knowledge).
			Update("error_message", "must_rollback").Error)
		return repository.ErrPersonalFence
	})
	require.ErrorIs(t, err, repository.ErrPersonalFence)
	var row struct{ ErrorMessage string }
	require.NoError(t, f.db.Table("knowledges").Select("error_message").
		Where("id=?", proof.knowledge).Take(&row).Error)
	require.NotEqual(t, "must_rollback", row.ErrorMessage)
}

func TestPersonalRecoveryExactReceiptStagesWithoutServingGrant(t *testing.T) {
	f, proof, ctx := exactExecutionFixture(t)
	require.NoError(t, f.p.beginExactPersonalReceipt(ctx, proof))
	var row types.TaskPendingOp
	require.NoError(t, f.db.Where("scope=? AND scope_id=?", types.PersonalExactReceiptScope, proof.knowledge).
		First(&row).Error)
	require.Equal(t, "staging", row.Op)
	imported, _, _, err := f.p.repo.Load(context.Background(), proof.attempt)
	require.NoError(t, err)
	imported.AttemptID = proof.attempt
	statuses := []types.FeishuPersonalImport{*imported}
	require.NoError(t, f.p.firstCreateStatuses(context.Background(), statuses))
	require.Equal(t, "pending", statuses[0].IndexStatus, "staging does not prove execution completed")
	require.NotEqual(t, "synced", statuses[0].Status)
	// A valid owner can inspect its work; ordinary callers cannot receive
	// content just because a receipt exists or a datasource hold is ignored.
	require.NoError(t, types.CheckLegacySourceRead(f.db.WithContext(ctx), proof.knowledge, proof.kb))
	require.ErrorIs(t, types.CheckLegacySourceRead(f.db, proof.knowledge, proof.kb), types.ErrLegacySourceRestricted)
	require.ErrorIs(t, f.p.beginExactPersonalReceipt(ctx, proof), repository.ErrPersonalFence)
	var count int64
	require.NoError(t, f.db.Model(&types.TaskPendingOp{}).Where("scope=?", types.PersonalExactReceiptScope).
		Count(&count).Error)
	require.EqualValues(t, 1, count)
}
