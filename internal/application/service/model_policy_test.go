package service

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type policyTestSettings struct {
	interfaces.SystemSettingService
	values           map[string]string
	updateBatchCalls int
}

func (s *policyTestSettings) GetString(_ context.Context, key, _, def string) string {
	if value, ok := s.values[key]; ok {
		return value
	}
	return def
}

func (s *policyTestSettings) Update(_ context.Context, key string, raw any) (*types.SystemSetting, error) {
	s.values[key] = raw.(string)
	return &types.SystemSetting{Key: key}, nil
}

func (s *policyTestSettings) UpdateBatch(_ context.Context, values map[string]any) ([]*types.SystemSetting, error) {
	s.updateBatchCalls++
	rows := make([]*types.SystemSetting, 0, len(values))
	for key, raw := range values {
		s.values[key] = raw.(string)
		rows = append(rows, &types.SystemSetting{Key: key})
	}
	return rows, nil
}

type policyTestModels struct {
	interfaces.ModelRepository
	builtin map[string]*types.Model
	listed  []*types.Model
}

func (r *policyTestModels) GetBuiltinByID(_ context.Context, id string) (*types.Model, error) {
	model := r.builtin[id]
	if model == nil || !model.IsBuiltin || model.DeletedAt.Valid {
		return nil, nil
	}
	return model, nil
}

func (r *policyTestModels) List(context.Context, uint64, types.ModelType, types.ModelSource) ([]*types.Model, error) {
	return r.listed, nil
}

func activeBuiltin(id string, typ types.ModelType) *types.Model {
	return &types.Model{ID: id, Type: typ, IsBuiltin: true, Status: types.ModelStatusActive}
}

func newPolicyTestService(models ...*types.Model) (*modelPolicyService, *policyTestSettings, *policyTestModels) {
	settings := &policyTestSettings{values: map[string]string{}}
	repo := &policyTestModels{builtin: map[string]*types.Model{}, listed: models}
	for _, model := range models {
		repo.builtin[model.ID] = model
	}
	return &modelPolicyService{settings: settings, models: repo}, settings, repo
}

func TestModelPolicyDefaultsAreEmpty(t *testing.T) {
	svc, _, _ := newPolicyTestService()
	policy, err := svc.GetPolicy(context.Background())
	require.NoError(t, err)
	assert.Equal(t, &types.DefaultModelPolicy{}, policy)
}

func TestModelPolicyValidation(t *testing.T) {
	chat := activeBuiltin("chat", types.ModelTypeKnowledgeQA)
	embed := activeBuiltin("embed", types.ModelTypeEmbedding)
	private := activeBuiltin("private", types.ModelTypeKnowledgeQA)
	private.IsBuiltin = false
	inactive := activeBuiltin("inactive", types.ModelTypeKnowledgeQA)
	inactive.Status = types.ModelStatusDownloading
	deleted := activeBuiltin("deleted", types.ModelTypeKnowledgeQA)
	deleted.DeletedAt = gorm.DeletedAt{Time: time.Now(), Valid: true}
	svc, _, _ := newPolicyTestService(chat, embed, private, inactive, deleted)
	ctx := context.Background()

	require.NoError(t, svc.ValidateModelForRole(ctx, types.ModelPolicyRoleChat, ""))
	require.NoError(t, svc.ValidateModelForRole(ctx, types.ModelPolicyRoleChat, chat.ID))
	for _, id := range []string{private.ID, inactive.ID, deleted.ID} {
		assert.Error(t, svc.ValidateModelForRole(ctx, types.ModelPolicyRoleChat, id), id)
	}
	assert.Error(t, svc.ValidateModelForRole(ctx, types.ModelPolicyRoleChat, embed.ID))
	assert.Error(t, svc.ValidateModelForRole(ctx, types.ModelPolicyRoleEmbedding, chat.ID))
}

func TestModelPolicyUpdateIsVisibleWithoutRestart(t *testing.T) {
	chat := activeBuiltin("chat", types.ModelTypeKnowledgeQA)
	svc, _, _ := newPolicyTestService(chat)
	updated, err := svc.UpdatePolicy(context.Background(), &types.DefaultModelPolicy{ChatModelID: chat.ID})
	require.NoError(t, err)
	assert.Equal(t, chat.ID, updated.ChatModelID)
	assert.Equal(t, chat.ID, svc.ResolveChatModelID(context.Background()))
}

func TestModelPolicyUpdateValidatesCompleteDocumentBeforeWrite(t *testing.T) {
	chat := activeBuiltin("chat", types.ModelTypeKnowledgeQA)
	embed := activeBuiltin("embed", types.ModelTypeEmbedding)
	invalidVLM := activeBuiltin("not-vlm", types.ModelTypeKnowledgeQA)
	svc, settings, _ := newPolicyTestService(chat, embed, invalidVLM)
	settings.values[settingDefaultChat] = "old-chat"
	settings.values[settingDefaultEmbedding] = "old-embed"

	_, err := svc.UpdatePolicy(context.Background(), &types.DefaultModelPolicy{
		ChatModelID: chat.ID, EmbeddingModelID: embed.ID, VLMModelID: invalidVLM.ID,
	})

	require.Error(t, err)
	assert.Zero(t, settings.updateBatchCalls, "batch persistence must not start before every role validates")
	assert.Equal(t, "old-chat", settings.values[settingDefaultChat])
	assert.Equal(t, "old-embed", settings.values[settingDefaultEmbedding])
}

func TestModelPolicyRuntimeInvalidationDoesNotEraseConfiguredValue(t *testing.T) {
	chat := activeBuiltin("configured-chat", types.ModelTypeKnowledgeQA)
	svc, settings, repo := newPolicyTestService(chat)
	settings.values[settingDefaultChat] = chat.ID
	require.Equal(t, chat.ID, svc.ResolveChatModelID(context.Background()))

	chat.Status = types.ModelStatusDownloading
	assert.Empty(t, svc.ResolveChatModelID(context.Background()), "inactive defaults must be unavailable at runtime")
	assert.Equal(t, chat.ID, settings.values[settingDefaultChat], "runtime validation must preserve the admin-visible setting")

	chat.Status = types.ModelStatusActive
	chat.DeletedAt = gorm.DeletedAt{Time: time.Now(), Valid: true}
	assert.Empty(t, svc.ResolveChatModelID(context.Background()), "soft-deleted defaults must be unavailable at runtime")
	assert.Equal(t, chat.ID, settings.values[settingDefaultChat])

	delete(repo.builtin, chat.ID)
	assert.Empty(t, svc.ResolveChatModelID(context.Background()), "missing defaults must be unavailable at runtime")
}

func TestUnavailableRerankDefaultAllowsLegacyFallback(t *testing.T) {
	configured := activeBuiltin("configured-rerank", types.ModelTypeRerank)
	legacy := activeBuiltin("legacy-rerank", types.ModelTypeRerank)
	policy, settings, _ := newPolicyTestService(configured, legacy)
	settings.values[settingDefaultRerank] = configured.ID
	configured.Status = types.ModelStatusDownloading
	require.Empty(t, policy.ResolveRerankModelID(context.Background()))

	sessions := &sessionService{
		modelPolicy: &stubModelPolicy{rerankID: policy.ResolveRerankModelID(context.Background())},
	}
	assert.Equal(t, legacy.ID, sessions.resolveRerankModelID(
		context.Background(), "", nil, []*types.Model{legacy},
	))
}

func TestInvalidRuntimeDefaultsUseKnowledgeBaseLegacyFallbacks(t *testing.T) {
	configuredEmbed := activeBuiltin("configured-embed", types.ModelTypeEmbedding)
	configuredVLM := activeBuiltin("configured-vlm", types.ModelTypeVLLM)
	configuredASR := activeBuiltin("configured-asr", types.ModelTypeASR)
	legacyEmbed := activeBuiltin("legacy-embed", types.ModelTypeEmbedding)
	legacyVLM := activeBuiltin("legacy-vlm", types.ModelTypeVLLM)
	legacyASR := activeBuiltin("legacy-asr", types.ModelTypeASR)
	svc, settings, repo := newPolicyTestService(
		configuredEmbed, configuredVLM, configuredASR, legacyEmbed, legacyVLM, legacyASR,
	)
	settings.values[settingDefaultEmbedding] = configuredEmbed.ID
	settings.values[settingDefaultVLM] = configuredVLM.ID
	settings.values[settingDefaultASR] = configuredASR.ID
	configuredEmbed.Status = types.ModelStatusDownloading
	configuredVLM.Status = types.ModelStatusDownloading
	configuredASR.Status = types.ModelStatusDownloading
	repo.listed = []*types.Model{legacyEmbed, legacyVLM, legacyASR}

	kb := &types.KnowledgeBase{
		TenantID:  10003,
		VLMConfig: types.VLMConfig{Enabled: true},
		ASRConfig: types.ASRConfig{Enabled: true},
	}
	kb.EnsureDefaults()
	require.NoError(t, svc.ApplyKnowledgeBaseDefaults(context.Background(), kb))
	assert.Equal(t, legacyEmbed.ID, kb.EmbeddingModelID)
	assert.Equal(t, legacyVLM.ID, kb.VLMConfig.ModelID)
	assert.Equal(t, legacyASR.ID, kb.ASRConfig.ModelID)
}

func TestChangingEmbeddingDefaultDoesNotMigrateExistingKnowledgeBase(t *testing.T) {
	embedA := activeBuiltin("embed-a", types.ModelTypeEmbedding)
	embedB := activeBuiltin("embed-b", types.ModelTypeEmbedding)
	svc, settings, _ := newPolicyTestService(embedA, embedB)
	settings.values[settingDefaultEmbedding] = embedA.ID

	kbA := &types.KnowledgeBase{TenantID: 1}
	kbA.EnsureDefaults()
	require.NoError(t, svc.ApplyKnowledgeBaseDefaults(context.Background(), kbA))
	settings.values[settingDefaultEmbedding] = embedB.ID
	kbB := &types.KnowledgeBase{TenantID: 1}
	kbB.EnsureDefaults()
	require.NoError(t, svc.ApplyKnowledgeBaseDefaults(context.Background(), kbB))

	assert.Equal(t, embedA.ID, kbA.EmbeddingModelID)
	assert.Equal(t, embedB.ID, kbB.EmbeddingModelID)
}

func TestApplyKnowledgeBaseDefaultsPreservesExplicitAndFeatureEnablement(t *testing.T) {
	chat := activeBuiltin("system-chat", types.ModelTypeKnowledgeQA)
	embed := activeBuiltin("system-embed", types.ModelTypeEmbedding)
	vlm := activeBuiltin("system-vlm", types.ModelTypeVLLM)
	asr := activeBuiltin("system-asr", types.ModelTypeASR)
	svc, settings, _ := newPolicyTestService(chat, embed, vlm, asr)
	settings.values[settingDefaultSummary] = chat.ID
	settings.values[settingDefaultEmbedding] = embed.ID
	settings.values[settingDefaultVLM] = vlm.ID
	settings.values[settingDefaultASR] = asr.ID

	kb := &types.KnowledgeBase{
		TenantID: 7, EmbeddingModelID: "explicit-embed",
		VLMConfig: types.VLMConfig{Enabled: false}, ASRConfig: types.ASRConfig{Enabled: true},
	}
	kb.EnsureDefaults()
	require.NoError(t, svc.ApplyKnowledgeBaseDefaults(context.Background(), kb))
	assert.Equal(t, "explicit-embed", kb.EmbeddingModelID)
	assert.Equal(t, chat.ID, kb.SummaryModelID)
	assert.Empty(t, kb.VLMConfig.ModelID, "a default must not enable VLM")
	assert.Equal(t, asr.ID, kb.ASRConfig.ModelID)
}

func TestApplyKnowledgeBaseDefaultsUsesLegacyFallbackAndRequiresEmbedding(t *testing.T) {
	legacy := activeBuiltin("legacy-embed", types.ModelTypeEmbedding)
	svc, _, repo := newPolicyTestService(legacy)
	kb := &types.KnowledgeBase{TenantID: 10003}
	kb.EnsureDefaults()
	require.NoError(t, svc.ApplyKnowledgeBaseDefaults(context.Background(), kb))
	assert.Equal(t, legacy.ID, kb.EmbeddingModelID)

	repo.listed = nil
	kb = &types.KnowledgeBase{TenantID: 10000}
	kb.EnsureDefaults()
	assert.ErrorContains(t, svc.ApplyKnowledgeBaseDefaults(context.Background(), kb), "embedding model is required")
}
