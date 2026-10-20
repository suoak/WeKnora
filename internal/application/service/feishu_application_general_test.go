package service

import (
	"context"
	"encoding/json"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestApplicationGrantGeneralAccessPreservesKBAndAdminBoundaries(t *testing.T) {
	f, _ := newApplicationGrantFixture(t)
	t.Setenv("FEISHU_APPLICATION_GRANT_ENABLED", "1")
	t.Setenv("FEISHU_APPLICATION_GRANT_TEST_KB_IDS", "")
	require.NoError(t, f.db.Exec("UPDATE users SET is_system_admin=false WHERE id='A'").Error)
	ctx := context.WithValue(f.ctx, types.SystemAdminContextKey, false)
	require.Equal(t, true, f.s.Status(ctx)["available"])
	require.Error(t, f.s.SaveApp(ctx, applicationTestConfig(t, "application_grant", "")), "ordinary importers cannot configure the application")
	m, err := f.s.Import(ctx, "kb", "https://test.feishu.cn/docx/real")
	require.NoError(t, err, "a KB writer needs neither SystemAdmin nor a rollout KB allowlist")
	require.NoError(t, f.p.Process(ctx, f.payload(m.AttemptID)), "background authorization must use the same policy")
	k, op, _ := mvpPublicTask(t, f, m)
	var intent types.KnowledgeDispatch
	require.NoError(t, json.Unmarshal(op.Payload, &intent))
	ready, err := f.p.firstCreateDispatchable(ctx, intent)
	require.NoError(t, err)
	require.True(t, ready)
	require.Equal(t, m.LogicalKnowledgeID, k.ID)
	require.NoError(t, f.db.Exec("UPDATE tenant_members SET role='viewer' WHERE user_id='A'").Error)
	_, err = f.p.firstCreateDispatchable(ctx, intent)
	require.Error(t, err, "losing KB write permission must prevent background execution")
}

func TestApplicationGrantFirstCreatePinsVerifiedParserPerDocument(t *testing.T) {
	f, _ := newApplicationGrantFixture(t)
	kb, err := f.ks.kbService.GetKnowledgeBaseByID(f.ctx, "kb")
	require.NoError(t, err)
	kb.ChunkingConfig.ParserEngineRules = []types.ParserEngineRule{{FileTypes: []string{"docx"}, Engine: "builtin"}}
	m := f.prepare("https://test.feishu.cn/docx/real")
	require.NoError(t, f.p.Process(f.ctx, f.payload(m.AttemptID)))
	k, _, _ := mvpPublicTask(t, f, m)
	overrides, err := k.ProcessOverrides()
	require.NoError(t, err)
	require.NotNil(t, overrides, "verified exports cannot fall back to an unrelated KB parser")
	require.Equal(t, "anydoc", ResolveProcessConfig(kb, overrides).ChunkingConfig.ResolveParserEngine("docx"))
	require.ErrorIs(t, verifyApplicationParse(k, &types.ReadResult{MarkdownContent: "different content"}), ErrFeishuIncompleteContent, "parser overrides must not bypass the content receipt")
	require.Equal(t, "builtin", kb.ChunkingConfig.ResolveParserEngine("docx"), "other documents keep the KB parser")
}
