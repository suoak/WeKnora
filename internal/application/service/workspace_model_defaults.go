package service

import (
	"context"
	"fmt"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// AttachModelDefaults shares the existing enterprise policy with model-list
// consumers, including background workers that only carry an execution tenant.
func AttachModelDefaults(models interfaces.ModelService, policy interfaces.ModelPolicyService) {
	if s, ok := models.(*modelService); ok {
		s.modelPolicy = policy
	}
}

func (s *modelPolicyService) GetWorkspacePolicy(ctx context.Context) (*types.DefaultSettings, error) {
	id, ok := types.TenantIDFromContext(ctx)
	if !ok || s.tenants == nil {
		return nil, fmt.Errorf("workspace defaults are unavailable")
	}
	tenant, err := s.tenants.GetTenantByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if tenant == nil {
		return nil, fmt.Errorf("workspace not found")
	}
	result := types.DefaultSettings{}
	if tenant.DefaultSettings != nil {
		result = *tenant.DefaultSettings
	}
	return &result, nil
}

func (s *modelPolicyService) UpdateWorkspacePolicy(ctx context.Context, next, previous *types.DefaultSettings) error {
	id, ok := types.TenantIDFromContext(ctx)
	if !ok || s.tenants == nil || next == nil || previous == nil {
		return fmt.Errorf("workspace and previous defaults are required")
	}
	// Route RBAC is supplemented here for non-HTTP callers. Machine principals
	// still require their existing tenant-settings capability at the route.
	if !types.IsSystemAdminFromContext(ctx) && !types.TenantRoleFromContext(ctx).HasPermission(types.TenantRoleAdmin) {
		return fmt.Errorf("workspace administrator access is required")
	}
	candidate := *next
	candidate.Normalize()
	for _, spec := range modelPolicySettings {
		choice := workspaceModelID(&candidate, spec.role)
		// Upstream #4822: let admins repair one stale slot at a time.
		if choice == "" || choice == workspaceModelID(previous, spec.role) {
			continue
		}
		model, err := s.models.GetByID(ctx, id, choice)
		want, _ := requiredTypeForRole(spec.role)
		if err != nil {
			return fmt.Errorf("look up workspace model: %w", err)
		}
		if model == nil || model.DeletedAt.Valid || model.Status != types.ModelStatusActive || !model.SupportsType(want) {
			return fmt.Errorf("model %q is unavailable or does not support %s in this workspace", choice, spec.role)
		}
	}
	return s.tenants.UpdateDefaultSettings(ctx, id, &candidate, previous)
}

func (s *modelService) projectModelDefaults(ctx context.Context, source []*types.Model) ([]*types.Model, error) {
	models := make([]*types.Model, 0, len(source))
	for _, m := range source {
		if m == nil {
			continue
		}
		copy := *m
		copy.DefaultFor = nil
		models = append(models, &copy)
	}
	if s.modelPolicy == nil {
		return models, nil
	}
	enterprise, err := s.modelPolicy.GetPolicy(ctx)
	if err != nil {
		return nil, err
	}
	// Enterprise choices can only reference builtin models. Workspace choices
	// may reference any model the existing repository authorizes for that tenant.
	defaults := &types.DefaultSettings{}
	for _, spec := range modelPolicySettings {
		id := spec.get(enterprise)
		if id != "" && s.modelPolicy.ValidateModelForRole(ctx, spec.role, id) == nil {
			setWorkspaceModelID(defaults, spec.role, id)
		}
	}
	defaults.ApplyModelDefaults(models)
	workspace, err := s.modelPolicy.GetWorkspacePolicy(ctx)
	if err != nil {
		return nil, err
	}
	// Clear purpose projections only for valid workspace overrides, retaining
	// enterprise fallback on deleted, inactive or incompatible historical IDs.
	for _, spec := range modelPolicySettings {
		id := workspaceModelID(workspace, spec.role)
		want, _ := requiredTypeForRole(spec.role)
		for _, m := range models {
			if m.ID == id && m.Status == types.ModelStatusActive && m.SupportsType(want) {
				purpose := string(want)
				if spec.role == types.ModelPolicyRoleSummary {
					purpose = "Utility"
				}
				for _, item := range models {
					filtered := item.DefaultFor[:0]
					for _, p := range item.DefaultFor {
						if p != purpose {
							filtered = append(filtered, p)
						}
					}
					item.DefaultFor = filtered
				}
				break
			}
		}
	}
	workspace.ApplyModelDefaults(models)
	return models, nil
}

func setWorkspaceModelID(p *types.DefaultSettings, role types.ModelPolicyRole, id string) {
	switch role {
	case types.ModelPolicyRoleChat:
		p.ChatModelID = id
	case types.ModelPolicyRoleSummary:
		p.UtilityModelID = id
	case types.ModelPolicyRoleEmbedding:
		p.EmbeddingModelID = id
	case types.ModelPolicyRoleRerank:
		p.RerankModelID = id
	case types.ModelPolicyRoleVLM:
		p.VLMModelID = id
	case types.ModelPolicyRoleASR:
		p.ASRModelID = id
	}
}
