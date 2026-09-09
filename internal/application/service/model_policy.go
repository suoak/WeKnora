package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

const (
	settingDefaultChat      = "model.default.chat_id"
	settingDefaultSummary   = "model.default.summary_id"
	settingDefaultEmbedding = "model.default.embedding_id"
	settingDefaultRerank    = "model.default.rerank_id"
	settingDefaultVLM       = "model.default.vlm_id"
	settingDefaultASR       = "model.default.asr_id"
)

var modelPolicySettings = []struct {
	role    types.ModelPolicyRole
	key     string
	envName string
	get     func(*types.DefaultModelPolicy) string
}{
	{types.ModelPolicyRoleChat, settingDefaultChat, "WEKNORA_DEFAULT_CHAT_MODEL_ID", func(p *types.DefaultModelPolicy) string { return p.ChatModelID }},
	{types.ModelPolicyRoleSummary, settingDefaultSummary, "WEKNORA_DEFAULT_SUMMARY_MODEL_ID", func(p *types.DefaultModelPolicy) string { return p.SummaryModelID }},
	{types.ModelPolicyRoleEmbedding, settingDefaultEmbedding, "WEKNORA_DEFAULT_EMBEDDING_MODEL_ID", func(p *types.DefaultModelPolicy) string { return p.EmbeddingModelID }},
	{types.ModelPolicyRoleRerank, settingDefaultRerank, "WEKNORA_DEFAULT_RERANK_MODEL_ID", func(p *types.DefaultModelPolicy) string { return p.RerankModelID }},
	{types.ModelPolicyRoleVLM, settingDefaultVLM, "WEKNORA_DEFAULT_VLM_MODEL_ID", func(p *types.DefaultModelPolicy) string { return p.VLMModelID }},
	{types.ModelPolicyRoleASR, settingDefaultASR, "WEKNORA_DEFAULT_ASR_MODEL_ID", func(p *types.DefaultModelPolicy) string { return p.ASRModelID }},
}

type modelPolicyService struct {
	settings interfaces.SystemSettingService
	models   interfaces.ModelRepository
}

func NewModelPolicyService(settings interfaces.SystemSettingService, models interfaces.ModelRepository) interfaces.ModelPolicyService {
	return &modelPolicyService{settings: settings, models: models}
}

func (s *modelPolicyService) GetPolicy(ctx context.Context) (*types.DefaultModelPolicy, error) {
	return &types.DefaultModelPolicy{
		ChatModelID:      strings.TrimSpace(s.settings.GetString(ctx, settingDefaultChat, "WEKNORA_DEFAULT_CHAT_MODEL_ID", "")),
		SummaryModelID:   strings.TrimSpace(s.settings.GetString(ctx, settingDefaultSummary, "WEKNORA_DEFAULT_SUMMARY_MODEL_ID", "")),
		EmbeddingModelID: strings.TrimSpace(s.settings.GetString(ctx, settingDefaultEmbedding, "WEKNORA_DEFAULT_EMBEDDING_MODEL_ID", "")),
		RerankModelID:    strings.TrimSpace(s.settings.GetString(ctx, settingDefaultRerank, "WEKNORA_DEFAULT_RERANK_MODEL_ID", "")),
		VLMModelID:       strings.TrimSpace(s.settings.GetString(ctx, settingDefaultVLM, "WEKNORA_DEFAULT_VLM_MODEL_ID", "")),
		ASRModelID:       strings.TrimSpace(s.settings.GetString(ctx, settingDefaultASR, "WEKNORA_DEFAULT_ASR_MODEL_ID", "")),
	}, nil
}

func (s *modelPolicyService) UpdatePolicy(ctx context.Context, policy *types.DefaultModelPolicy) (*types.DefaultModelPolicy, error) {
	if policy == nil {
		return nil, fmt.Errorf("model policy is required")
	}
	// Validate the complete document first so ordinary input errors cannot
	// leave a partially written policy.
	for _, spec := range modelPolicySettings {
		if err := s.ValidateModelForRole(ctx, spec.role, spec.get(policy)); err != nil {
			return nil, err
		}
	}
	values := make(map[string]any, len(modelPolicySettings))
	for _, spec := range modelPolicySettings {
		values[spec.key] = strings.TrimSpace(spec.get(policy))
	}
	if _, err := s.settings.UpdateBatch(ctx, values); err != nil {
		return nil, fmt.Errorf("update model policy: %w", err)
	}
	return s.GetPolicy(ctx)
}

func requiredTypeForRole(role types.ModelPolicyRole) (types.ModelType, bool) {
	switch role {
	case types.ModelPolicyRoleChat, types.ModelPolicyRoleSummary:
		return types.ModelTypeKnowledgeQA, true
	case types.ModelPolicyRoleEmbedding:
		return types.ModelTypeEmbedding, true
	case types.ModelPolicyRoleRerank:
		return types.ModelTypeRerank, true
	case types.ModelPolicyRoleVLM:
		return types.ModelTypeVLLM, true
	case types.ModelPolicyRoleASR:
		return types.ModelTypeASR, true
	default:
		return "", false
	}
}

func (s *modelPolicyService) ValidateModelForRole(ctx context.Context, role types.ModelPolicyRole, modelID string) error {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return nil
	}
	want, ok := requiredTypeForRole(role)
	if !ok {
		return fmt.Errorf("unknown model policy role %q", role)
	}
	model, err := s.models.GetBuiltinByID(ctx, modelID)
	if err != nil {
		return fmt.Errorf("look up builtin model %s: %w", modelID, err)
	}
	if model == nil {
		return fmt.Errorf("model %s is not a live builtin model", modelID)
	}
	if model.Status != types.ModelStatusActive {
		return fmt.Errorf("model %s is not active", modelID)
	}
	if model.Type != want {
		return fmt.Errorf("model %s has type %s; %s requires %s", modelID, model.Type, role, want)
	}
	return nil
}

func (s *modelPolicyService) resolve(ctx context.Context, role types.ModelPolicyRole, id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	if err := s.ValidateModelForRole(ctx, role, id); err != nil {
		logger.Warnf(ctx, "Ignoring invalid system default %s model %s: %v", role, id, err)
		return ""
	}
	return id
}

func (s *modelPolicyService) policy(ctx context.Context) *types.DefaultModelPolicy {
	p, _ := s.GetPolicy(ctx)
	return p
}

func (s *modelPolicyService) ResolveChatModelID(ctx context.Context) string {
	p := s.policy(ctx)
	return s.resolve(ctx, types.ModelPolicyRoleChat, p.ChatModelID)
}
func (s *modelPolicyService) ResolveSummaryModelID(ctx context.Context) string {
	p := s.policy(ctx)
	if id := s.resolve(ctx, types.ModelPolicyRoleSummary, p.SummaryModelID); id != "" {
		return id
	}
	return s.resolve(ctx, types.ModelPolicyRoleChat, p.ChatModelID)
}
func (s *modelPolicyService) ResolveEmbeddingModelID(ctx context.Context) string {
	return s.resolve(ctx, types.ModelPolicyRoleEmbedding, s.policy(ctx).EmbeddingModelID)
}
func (s *modelPolicyService) ResolveRerankModelID(ctx context.Context) string {
	return s.resolve(ctx, types.ModelPolicyRoleRerank, s.policy(ctx).RerankModelID)
}
func (s *modelPolicyService) ResolveVLMModelID(ctx context.Context) string {
	return s.resolve(ctx, types.ModelPolicyRoleVLM, s.policy(ctx).VLMModelID)
}
func (s *modelPolicyService) ResolveASRModelID(ctx context.Context) string {
	return s.resolve(ctx, types.ModelPolicyRoleASR, s.policy(ctx).ASRModelID)
}

func firstActiveModel(models []*types.Model, typ types.ModelType) string {
	for _, model := range models {
		if model != nil && model.Type == typ && model.Status == types.ModelStatusActive {
			return model.ID
		}
	}
	return ""
}

func (s *modelPolicyService) ApplyKnowledgeBaseDefaults(ctx context.Context, kb *types.KnowledgeBase) error {
	if kb == nil {
		return fmt.Errorf("knowledge base is required")
	}
	// Explicit > Tenant > System > Legacy fallback. A KB has no separate
	// tenant model override except the IDs already present on the request.
	if strings.TrimSpace(kb.EmbeddingModelID) == "" {
		kb.EmbeddingModelID = s.ResolveEmbeddingModelID(ctx)
	}
	if strings.TrimSpace(kb.SummaryModelID) == "" {
		kb.SummaryModelID = s.ResolveSummaryModelID(ctx)
	}
	if kb.VLMConfig.Enabled && strings.TrimSpace(kb.VLMConfig.ModelID) == "" {
		kb.VLMConfig.ModelID = s.ResolveVLMModelID(ctx)
	}
	if kb.ASRConfig.Enabled && strings.TrimSpace(kb.ASRConfig.ModelID) == "" {
		kb.ASRConfig.ModelID = s.ResolveASRModelID(ctx)
	}

	models, err := s.models.List(ctx, kb.TenantID, "", "")
	if err != nil {
		return fmt.Errorf("list models for knowledge base defaults: %w", err)
	}
	if kb.EmbeddingModelID == "" && kb.NeedsEmbeddingModel() {
		kb.EmbeddingModelID = firstActiveModel(models, types.ModelTypeEmbedding)
	}
	if kb.SummaryModelID == "" {
		kb.SummaryModelID = firstActiveModel(models, types.ModelTypeKnowledgeQA)
	}
	if kb.VLMConfig.Enabled && kb.VLMConfig.ModelID == "" {
		kb.VLMConfig.ModelID = firstActiveModel(models, types.ModelTypeVLLM)
	}
	if kb.ASRConfig.Enabled && kb.ASRConfig.ModelID == "" {
		kb.ASRConfig.ModelID = firstActiveModel(models, types.ModelTypeASR)
	}
	if kb.NeedsEmbeddingModel() && kb.EmbeddingModelID == "" {
		return fmt.Errorf("embedding model is required: configure one explicitly or set a system default")
	}
	return nil
}
