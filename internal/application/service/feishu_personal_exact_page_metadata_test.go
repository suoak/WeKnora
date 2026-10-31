package service

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestPersonalRecoveryExactPageMetadataKeepsAllFilesWithoutContent(t *testing.T) {
	f := newMVPFixture(t)
	t.Setenv("FEISHU_PERSONAL_EXACT_RECOVERY", "1")
	require.NoError(t, f.db.AutoMigrate(&types.KnowledgeBase{}, &types.DataSource{}))
	require.NoError(t, f.db.Exec(`CREATE TABLE feishu_legacy_resources (
id text PRIMARY KEY,tenant_id bigint,knowledge_base_id text,knowledge_id text,state text)`).Error)
	require.NoError(t, f.db.Create(&types.DataSource{
		ID: "restricted", TenantID: 1, KnowledgeBaseID: "kb", Type: "feishu_drive",
		LegacyAccessState: "revoked", Config: types.JSON(`{}`),
	}).Error)
	for i := range 24 {
		id := fmt.Sprintf("doc-%02d", i)
		require.NoError(t, f.db.Create(&types.Knowledge{
			ID: id, TenantID: 1, KnowledgeBaseID: "kb", FileName: id + ".docx",
			FileType: "docx", Type: "file", FolderPath: "visits", ParseStatus: types.ParseStatusPending,
			Title: "private-title-needle", Description: "private-description-marker",
			Source: "https://private.invalid/secret", FilePath: "local://private-path-marker",
			Metadata: types.JSON(`{"private":"private-metadata-marker"}`),
		}).Error)
	}
	for i, op := range []string{"published", "staging"} {
		require.NoError(t, f.db.Create(&types.TaskPendingOp{
			Scope: types.PersonalExactReceiptScope, ScopeID: fmt.Sprintf("doc-%02d", i), TenantID: 1,
			DedupKey: fmt.Sprintf("receipt-%d", i), Op: op, EnqueuedAt: time.Now(),
			Payload: json.RawMessage(`{"knowledge_base_id":"kb"}`),
		}).Error)
	}
	require.NoError(t, f.db.Exec("UPDATE tenant_members SET role='viewer' WHERE user_id='B'").Error)
	types.EnableLegacyReadGuards(f.db)
	ctx := types.WithPrincipal(personalUser("B"), types.Principal{Type: types.PrincipalWebUser, ID: "B"})
	ctx = types.WithCaller(ctx, types.Caller{TenantID: 1, UserID: "B", Role: types.TenantRoleViewer})
	ctx = types.WithExecutionTenant(ctx, 1)
	filter := types.KnowledgeListFilter{FolderPath: "visits", FolderScope: types.FolderScopeExact}
	seen := make(map[string]bool)
	for page := 1; page <= 3; page++ {
		result, err := f.ks.ListPagedKnowledgeByKnowledgeBaseID(ctx, "kb",
			&types.Pagination{Page: page, PageSize: 10}, filter)
		require.NoError(t, err)
		require.EqualValues(t, 24, result.Total)
		for _, row := range result.Data.([]*types.Knowledge) {
			require.False(t, seen[row.ID])
			seen[row.ID] = true
			require.True(t, row.MetadataOnly)
			require.NotEmpty(t, row.FileName)
			data, err := json.Marshal(row)
			require.NoError(t, err)
			for _, marker := range []string{"private-title", "private-description", "private.invalid", "private-path", "private-metadata"} {
				require.NotContains(t, string(data), marker)
			}
		}
	}
	require.Len(t, seen, 24, "a staging receipt must not hide the remaining catalog")
	_, err := f.ks.GetKnowledgeBatch(ctx, 1, []string{"doc-00", "doc-01"})
	require.Error(t, err, "mutation and download callers must retain the strict entity read path")
	catalog := types.WithKnowledgeCatalogRead(ctx)
	for _, batch := range []func() ([]*types.Knowledge, error){
		func() ([]*types.Knowledge, error) {
			return f.ks.GetKnowledgeBatch(catalog, 1, []string{"doc-00", "doc-01"})
		},
		func() ([]*types.Knowledge, error) {
			return f.ks.GetKnowledgeBatchWithSharedAccess(catalog, 1, []string{"doc-00", "doc-01"})
		},
	} {
		rows, err := batch()
		require.NoError(t, err, "status polling must not require a staging-content grant")
		require.Len(t, rows, 2)
		for _, row := range rows {
			require.True(t, row.MetadataOnly)
			require.Empty(t, row.Description)
			require.Empty(t, row.Source)
		}
	}
	filter.Keyword = "needle"
	result, err := f.ks.ListPagedKnowledgeByKnowledgeBaseID(ctx, "kb",
		&types.Pagination{Page: 1, PageSize: 10}, filter)
	require.NoError(t, err)
	require.Zero(t, result.Total, "metadata keyword queries cannot probe hidden titles")
	_, err = f.ks.GetKnowledgeByID(ctx, "doc-01")
	require.Error(t, err, "catalog metadata is not a staging-content grant")
	stranger := types.WithCaller(ctx, types.Caller{TenantID: 1, UserID: "not-a-member", Role: types.TenantRoleAdmin})
	stranger = types.WithPrincipal(stranger, types.Principal{Type: types.PrincipalWebUser, ID: "not-a-member"})
	_, err = f.ks.ListPagedKnowledgeByKnowledgeBaseID(stranger, "kb",
		&types.Pagination{Page: 1, PageSize: 10}, types.KnowledgeListFilter{})
	require.Error(t, err, "a claimed admin role is not live membership")
	_, err = f.p.exactPageMetadata(types.WithExecutionTenant(ctx, 2), 1, "kb", []string{"doc-00"})
	require.Error(t, err, "metadata cannot cross the execution tenant")
	require.NoError(t, f.db.Exec("UPDATE tenant_members SET status='inactive' WHERE user_id='B'").Error)
	_, err = f.ks.ListPagedKnowledgeByKnowledgeBaseID(ctx, "kb",
		&types.Pagination{Page: 1, PageSize: 10}, types.KnowledgeListFilter{})
	require.Error(t, err, "revoked membership closes metadata access too")
	_, err = f.ks.GetKnowledgeBatchWithSharedAccess(catalog, 1, []string{"doc-00", "doc-01"})
	require.Error(t, err, "status polling must honor live membership revocation")
}
