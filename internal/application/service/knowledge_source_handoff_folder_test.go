package service

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Historical handoffs have an obsolete personal attempt and no exact-publication
// receipt. A mixed 8 shared / 16 handed-off folder must remain readable without
// changing channels, receipts, source states or document data.
func TestSourceHandoffChineseFolderCatalogPostgres(t *testing.T) {
	f := newMVPFixture(t)
	require.NoError(t, f.db.AutoMigrate(&types.KnowledgeBase{}, &types.DataSource{}, &types.FeishuLegacyResource{}))
	for i := range 24 {
		id := fmt.Sprintf("visit-%02d", i)
		channel := "feishu"
		metadata := types.JSON(`{}`)
		if i >= 8 {
			channel = "feishu_personal"
			raw, err := json.Marshal(map[string]any{
				"datasource_id":          id,
				"feishu_mvp_attempt":     id,
				"feishu_manifest_sha256": "manifest",
			})
			require.NoError(t, err)
			metadata = raw
			require.NoError(t, f.db.Create(&types.FeishuPersonalImport{
				ID:            id,
				UserID:        "A",
				KBID:          "kb",
				KnowledgeID:   id,
				Status:        "source_managed",
				DocumentToken: id,
				ResourceType:  "docx",
				AppID:         "app",
				OpenID:        "open",
			}).Error)
			require.NoError(t, f.db.Create(&types.DataSource{
				ID:                id,
				TenantID:          1,
				KnowledgeBaseID:   "kb",
				Type:              "feishu",
				Status:            types.DataSourceStatusActive,
				LegacyAccessState: "unmanaged",
				Config:            types.JSON(`{}`),
			}).Error)
			require.NoError(t, f.db.Create(&types.FeishuPersonalResource{
				ImportID:           id,
				LogicalKnowledgeID: id,
				CurrentAttemptID:   id,
				CandidateVersionID: "candidate-" + id,
			}).Error)
			require.NoError(t, f.db.Create(&types.FeishuPersonalAttempt{
				ID:                 id,
				ImportID:           id,
				LogicalKnowledgeID: id,
				CallerTenantID:     1,
				GrantEpoch:         "epoch",
				Status:             "obsolete",
				ManifestHash:       "manifest",
				CandidateVersionID: "candidate-" + id,
			}).Error)
			receipt, _ := json.Marshal(map[string]any{
				"payload_version":         1,
				"knowledge_id":            id,
				"attempt_id":              id,
				"manifest_sha256":         "manifest",
				"file_hash":               "hash",
				"text_chunks":             1,
				"pg_vector_rows_verified": true,
				"bm25_document_readback":  true,
			})
			require.NoError(t, f.db.Create(&types.TaskPendingOp{
				TenantID:   1,
				Scope:      "personal_index_receipt",
				ScopeID:    id,
				DedupKey:   "index-" + id,
				Op:         "verified",
				Payload:    receipt,
				EnqueuedAt: time.Now(),
			}).Error)
		}
		require.NoError(t, f.db.Create(&types.Knowledge{
			ID:              id,
			TenantID:        1,
			KnowledgeBaseID: "kb",
			FileName:        id + ".docx",
			FolderPath:      "拜访纪要",
			Channel:         channel,
			FileHash:        "hash",
			Metadata:        metadata,
			ParseStatus:     types.ParseStatusCompleted,
			EnableStatus:    "enabled",
		}).Error)
	}
	types.EnableLegacyReadGuards(f.db)
	ctx := types.WithExecutionTenant(types.WithPrincipal(personalUser("B"), types.Principal{
		Type: types.PrincipalWebUser,
		ID:   "B",
	}), 1)
	ctx = types.WithCaller(ctx, types.Caller{TenantID: 1, UserID: "B", Role: types.TenantRoleAdmin})

	validHandoff, handoffErr := types.VerifiedSourceHandoff(f.db.WithContext(ctx), "visit-08", "kb")
	require.NoError(t, handoffErr)
	require.True(t, validHandoff)
	require.NoError(t, types.CheckLegacySourceRead(f.db.WithContext(ctx), "visit-08", "kb"))
	var probe types.Knowledge
	require.NoError(t, f.db.WithContext(ctx).Where("id=?", "visit-08").First(&probe).Error)

	filter := types.KnowledgeListFilter{
		FolderPath:  "拜访纪要",
		FolderScope: types.FolderScopeExact,
		SortBy:      types.KnowledgeListSortByFileName,
		SortOrder:   types.KnowledgeListSortAscending,
	}
	check := func(ctx context.Context, path string, scope types.KnowledgeFolderScope, want int64) {
		t.Helper()
		query := filter
		query.FolderPath = path
		query.FolderScope = scope
		seen := map[string]bool{}
		for page := 1; page <= 3; page++ {
			result, err := f.ks.ListPagedKnowledgeByKnowledgeBaseID(ctx, "kb", &types.Pagination{
				Page:     page,
				PageSize: 10,
			}, query)
			require.NoError(t, err)
			require.Equal(t, want, result.Total)
			for _, k := range result.Data.([]*types.Knowledge) {
				require.False(t, seen[k.ID])
				seen[k.ID] = true
			}
		}
		require.Len(t, seen, int(want))
	}
	check(ctx, "拜访纪要", types.FolderScopeExact, 24)
	check(ctx, "", types.FolderScopeExact, 0)
	check(ctx, "", types.FolderScopeSubtree, 24)
	tree, err := f.ks.ListKnowledgeFolderTree(ctx, "kb")
	require.NoError(t, err)
	require.EqualValues(t, 24, tree.TotalDocumentCount)
	// Ordinary viewers get the same local source access as admins.
	require.NoError(t, f.db.Exec("UPDATE tenant_members SET role='viewer' WHERE user_id='B'").Error)
	ctx = types.WithCaller(ctx, types.Caller{TenantID: 1, UserID: "B", Role: types.TenantRoleViewer})
	check(ctx, "拜访纪要", types.FolderScopeExact, 24)
	scoped := types.WithLegacyReadScope(ctx)
	_, err = f.ks.GetKnowledgeByID(scoped, "visit-08")
	require.NoError(t, err)
	require.NoError(t, types.CheckLegacyReadScope(f.db.WithContext(scoped)))
	// A sync hold does not revoke an owning workspace's existing local access.
	require.NoError(t, f.db.Exec("UPDATE data_sources SET legacy_access_state='hold' WHERE id='visit-08'").Error)
	check(ctx, "拜访纪要", types.FolderScopeExact, 24)
	// Revocations during a request are checked again at the output boundary.
	require.NoError(t, f.db.Exec("UPDATE data_sources SET legacy_access_state='revoked' WHERE id='visit-08'").Error)
	require.Error(t, types.CheckLegacyReadScope(f.db.WithContext(scoped)))
	_, err = f.ks.GetKnowledgeByID(ctx, "visit-08")
	require.Error(t, err)
	require.NoError(t, f.db.Exec("UPDATE data_sources SET legacy_access_state='unmanaged' WHERE id='visit-08'").Error)
	// Source handoff is not an API/MCP or cross-workspace content grant.
	for _, other := range []context.Context{
		types.WithExecutionTenant(ctx, 2),
		types.WithCaller(ctx, types.Caller{TenantID: 2, UserID: "B", Role: types.TenantRoleViewer}),
		types.WithPrincipal(ctx, types.Principal{Type: types.PrincipalAPITenant, ID: "key"}),
	} {
		_, err = f.ks.GetKnowledgeByID(other, "visit-08")
		require.Error(t, err)
	}
	// Altered bindings / receipts fail closed, including output after selection.
	for _, mutation := range []string{
		"UPDATE feishu_personal_imports SET status='indexed' WHERE id='visit-08'",
		"UPDATE task_pending_ops SET op='staging' WHERE scope_id='visit-08'",
		"UPDATE knowledges SET file_hash='changed' WHERE id='visit-08'",
		"UPDATE tenant_members SET status='inactive' WHERE user_id='B'",
	} {
		require.Error(t, f.db.Transaction(func(tx *gorm.DB) error {
			require.NoError(t, tx.Exec(mutation).Error)
			valid, e := types.VerifiedSourceHandoff(tx.WithContext(ctx), "visit-08", "kb")
			require.NoError(t, e)
			require.False(t, valid)
			return fmt.Errorf("rollback test mutation")
		}))
	}
	affected, err := f.ks.repo.RenameKnowledgeFolderPath(ctx, 1, "kb", "拜访纪要", "客户/纪要")
	require.NoError(t, err)
	require.EqualValues(t, 24, affected)
	check(ctx, "客户", types.FolderScopeExact, 0)
	check(ctx, "客户", types.FolderScopeSubtree, 24)
	check(ctx, "客户/纪要", types.FolderScopeExact, 24)
	affected, err = f.ks.repo.UpdateKnowledgeFolderPath(ctx, 1, "kb", []string{"visit-08"}, "")
	require.NoError(t, err)
	require.EqualValues(t, 1, affected)
	check(ctx, "", types.FolderScopeExact, 1)
	check(ctx, "客户/纪要", types.FolderScopeExact, 23)
	filtered := filter
	filtered.FolderPath = "客户/纪要"
	filtered.Keyword = "visit-09"
	filtered.ParseStatus = types.ParseStatusCompleted
	result, err := f.ks.ListPagedKnowledgeByKnowledgeBaseID(
		ctx, "kb", &types.Pagination{Page: 1, PageSize: 10}, filtered,
	)
	require.NoError(t, err)
	require.EqualValues(t, 1, result.Total)
	require.NoError(t, f.db.Exec(
		"DELETE FROM task_pending_ops WHERE scope_id='visit-08' AND scope='personal_index_receipt'",
	).Error)
	_, err = f.ks.GetKnowledgeByID(ctx, "visit-08")
	require.Error(t, err, "missing historical proof must stay closed")
}
