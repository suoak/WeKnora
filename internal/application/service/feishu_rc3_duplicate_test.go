package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestRC3ApplicationDuplicateNonOwnerContributor(t *testing.T) {
	f, _ := newApplicationGrantFixture(t)
	// Match the already deployed general-availability application path, not
	// the fixture's separate admin-only rollout policy.
	t.Setenv("FEISHU_APPLICATION_GRANT_ENABLED", "1")
	require.NoError(t, f.db.Exec("UPDATE users SET is_system_admin=false WHERE id='B'").Error)
	m := f.prepare("https://test.feishu.cn/docx/real")
	before := f.downloads.Load()
	require.NoError(t, f.db.Exec("UPDATE tenant_members SET role='contributor' WHERE user_id='B'").Error)
	caller := types.Caller{TenantID: 1, UserID: "B", Role: types.TenantRoleContributor}
	ctx := types.WithCaller(personalUser("B"), caller)
	ctx = context.WithValue(ctx, types.SystemAdminContextKey, false)
	grant, err := access.ResolveKB(ctx, access.KBRequest{Caller: caller},
		&types.KnowledgeBase{ID: "kb", TenantID: 1, CreatorID: "A"}, types.OrgRoleEditor, nil, nil)
	require.NoError(t, err)
	ctx = grant.Context(ctx)
	row, err := f.s.Import(ctx, "kb", "https://test.feishu.cn/docx/real")
	require.Nil(t, row, "another importer's private attempt must not be returned")
	require.ErrorIs(t, err, repository.ErrPersonalAlreadyImported)
	require.Equal(t, before, f.downloads.Load(), "duplicate admission must not fetch document content")
	var imports, attempts int64
	require.NoError(t, f.db.Model(&types.FeishuPersonalImport{}).Count(&imports).Error)
	require.NoError(t, f.db.Model(&types.FeishuPersonalAttempt{}).Count(&attempts).Error)
	require.EqualValues(t, 1, imports)
	require.EqualValues(t, 1, attempts)
	_, resource, _, err := f.p.repo.Load(f.ctx, m.AttemptID)
	require.NoError(t, err)
	require.Equal(t, m.LogicalKnowledgeID, resource.LogicalKnowledgeID)
	// A stale caller/editor context cannot override the live membership role.
	require.NoError(t, f.db.Exec("UPDATE tenant_members SET role='viewer' WHERE user_id='B'").Error)
	_, err = f.s.Import(ctx, "kb", "https://test.feishu.cn/docx/real")
	require.ErrorIs(t, err, access.ErrForbidden)
}
