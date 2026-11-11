package service

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestWorkspaceSettingsKeepExistingConfigurationDuringImport(t *testing.T) {
	f := newMVPFixture(t)
	t.Setenv("FEISHU_PERSONAL_EXACT_RECOVERY", "1")
	require.NoError(t, f.db.AutoMigrate(&types.KnowledgeBase{}))
	require.NoError(t, f.db.Create(&types.TaskPendingOp{
		Scope: types.PersonalExactReceiptScope, ScopeID: "unfinished", TenantID: 1,
		DedupKey: "settings-receipt", Op: "staging", EnqueuedAt: time.Now(),
		Payload: json.RawMessage(`{"knowledge_base_id":"kb"}`),
	}).Error)
	require.NoError(t, f.db.Exec(`UPDATE knowledge_bases SET description='retained-description',
wiki_config='{"content_instructions":"retained-wiki-instructions"}',
asr_config='{"enabled":true,"model_id":"retained-asr-model"}',
vlm_config='{"enabled":true,"model_id":"retained-vlm-model","api_key":"secret-must-not-leak"}',
generated_profile='{"summary":"unfinished-document-content"}' WHERE id='kb'`).Error)
	ctx := types.WithPrincipal(personalUser("B"), types.Principal{Type: types.PrincipalWebUser, ID: "B"})
	ctx = types.WithCaller(ctx, types.Caller{TenantID: 1, UserID: "B", Role: types.TenantRoleAdmin})
	svc := &knowledgeBaseService{personalRecovery: f.p}
	row, err := svc.GetKnowledgeBaseAccessMetadata(ctx, "kb")
	require.NoError(t, err)
	require.Equal(t, "retained-description", row.Description)
	data, err := json.Marshal(row)
	require.NoError(t, err)
	for _, setting := range []string{"retained-wiki-instructions", "retained-asr-model", "retained-vlm-model"} {
		require.Contains(t, string(data), setting)
	}
	require.NotContains(t, string(data), "secret-must-not-leak")
	require.NotContains(t, string(data), "unfinished-document-content")
}
