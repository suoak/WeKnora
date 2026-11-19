package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type workspacePolicyTenants struct {
	interfaces.TenantRepository
	tenant *types.Tenant
	saved  *types.DefaultSettings
}

func (r *workspacePolicyTenants) GetTenantByID(_ context.Context, id uint64) (*types.Tenant, error) {
	if r.tenant != nil && r.tenant.ID == id {
		return r.tenant, nil
	}
	return nil, ErrInvalidTenantID
}

func (r *workspacePolicyTenants) UpdateDefaultSettings(
	_ context.Context,
	_ uint64,
	next, _ *types.DefaultSettings,
) error {
	copy := *next
	r.saved = &copy
	return nil
}

type workspacePolicyModels struct{ *policyTestModels }

func (r *workspacePolicyModels) GetByID(_ context.Context, tenantID uint64, id string) (*types.Model, error) {
	for _, m := range r.listed {
		if m.ID == id && (m.IsBuiltin || m.TenantID == tenantID) && !m.DeletedAt.Valid {
			return m, nil
		}
	}
	return nil, nil
}

func workspacePolicyContext(id uint64) context.Context {
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, id)
	return context.WithValue(ctx, types.TenantRoleContextKey, types.TenantRoleAdmin)
}

func TestWorkspaceDefaultsIDOnlyWorkerAndEnterpriseInheritance(t *testing.T) {
	system := activeBuiltin("enterprise", types.ModelTypeKnowledgeQA)
	own := &types.Model{ID: "own", TenantID: 7, Type: types.ModelTypeKnowledgeQA, Status: types.ModelStatusActive}
	policy, settings, repo := newPolicyTestService(system, own)
	settings.values[settingDefaultChat] = system.ID
	policy.models = &workspacePolicyModels{repo}
	tenants := &workspacePolicyTenants{tenant: &types.Tenant{ID: 7}}
	policy.tenants = tenants
	ctx := workspacePolicyContext(7)
	assert.Equal(t, system.ID, policy.ResolveChatModelID(ctx), "new workspace inherits dynamically")
	tenants.tenant.DefaultSettings = &types.DefaultSettings{ChatModelID: own.ID}
	assert.Equal(t, own.ID, policy.ResolveChatModelID(ctx), "worker only needs tenant ID")
	own.Status = types.ModelStatusDownloading
	assert.Equal(t, system.ID, policy.ResolveChatModelID(ctx))
	assert.Equal(t, own.ID, tenants.tenant.DefaultSettings.ChatModelID, "stale selection remains visible")
	own.Status = types.ModelStatusActive
	other := context.WithValue(
		ctx,
		types.TenantInfoContextKey,
		&types.Tenant{ID: 999, DefaultSettings: &types.DefaultSettings{ChatModelID: "foreign"}},
	)
	assert.Equal(t, own.ID, policy.ResolveChatModelID(other), "foreign cached tenant cannot override execution tenant")
}

func TestWorkspaceDefaultsProjectionAndNoRepositoryMutation(t *testing.T) {
	system := activeBuiltin("system", types.ModelTypeKnowledgeQA)
	utility := activeBuiltin("utility", types.ModelTypeKnowledgeQA)
	own := &types.Model{ID: "own", TenantID: 7, Type: types.ModelTypeKnowledgeQA, Status: types.ModelStatusActive}
	policy, settings, repo := newPolicyTestService(system, utility, own)
	settings.values[settingDefaultChat] = system.ID
	settings.values[settingDefaultSummary] = utility.ID
	policy.models = &workspacePolicyModels{repo}
	policy.tenants = &workspacePolicyTenants{
		tenant: &types.Tenant{ID: 7, DefaultSettings: &types.DefaultSettings{ChatModelID: own.ID}},
	}
	svc := &modelService{modelPolicy: policy}
	models, err := svc.projectModelDefaults(workspacePolicyContext(7), repo.listed)
	require.NoError(t, err)
	assert.Equal(t, own.ID, types.PickDefaultModel(models, types.ModelTypeKnowledgeQA).ID)
	assert.Equal(t, utility.ID, types.PickUtilityModel(models).ID)
	assert.False(t, own.IsDefault)
	assert.Empty(t, system.DefaultFor)
}

func TestWorkspaceDefaultsAuthorizationCapabilityAndStaleRepair(t *testing.T) {
	own := &types.Model{ID: "own", TenantID: 7, Type: types.ModelTypeKnowledgeQA, Status: types.ModelStatusActive}
	foreign := &types.Model{
		ID:       "foreign",
		TenantID: 9,
		Type:     types.ModelTypeKnowledgeQA,
		Status:   types.ModelStatusActive,
	}
	vision := activeBuiltin("vision", types.ModelTypeKnowledgeQA)
	vision.Parameters.SupportsVision = true
	policy, _, repo := newPolicyTestService(own, foreign, vision)
	policy.models = &workspacePolicyModels{repo}
	tenants := &workspacePolicyTenants{tenant: &types.Tenant{ID: 7}}
	policy.tenants = tenants
	previous := &types.DefaultSettings{ChatModelID: "deleted", UtilityModelID: "deleted"}
	ctx := workspacePolicyContext(7)
	require.NoError(
		t,
		policy.UpdateWorkspacePolicy(
			ctx,
			&types.DefaultSettings{ChatModelID: own.ID, UtilityModelID: "deleted", VLMModelID: vision.ID},
			previous,
		),
	)
	assert.Equal(t, "deleted", previous.ChatModelID)
	assert.Equal(t, own.ID, tenants.saved.ChatModelID)
	for _, next := range []*types.DefaultSettings{{ChatModelID: foreign.ID}, {VLMModelID: own.ID}, {EmbeddingModelID: own.ID}, {UtilityModelID: "missing"}} {
		tenants.saved = nil
		assert.Error(t, policy.UpdateWorkspacePolicy(ctx, next, &types.DefaultSettings{}))
		assert.Nil(t, tenants.saved)
	}
	viewer := context.WithValue(ctx, types.TenantRoleContextKey, types.TenantRoleViewer)
	assert.Error(t, policy.UpdateWorkspacePolicy(viewer, &types.DefaultSettings{}, previous))
	foreignCaller := types.WithCaller(ctx, types.Caller{TenantID: 9, Role: types.TenantRoleAdmin})
	assert.Error(t, policy.UpdateWorkspacePolicy(foreignCaller, &types.DefaultSettings{}, previous))
	readKey := types.WithTenantAPIKeyScope(ctx, types.TenantAPIKeyScope{
		ScopeType: types.APIKeyScopeTenant, Capabilities: types.StringArray{"retrieve"},
	})
	assert.Error(t, policy.UpdateWorkspacePolicy(readKey, &types.DefaultSettings{}, previous))
	manageKey := types.WithTenantAPIKeyScope(ctx, types.TenantAPIKeyScope{
		ScopeType:    types.APIKeyScopeTenant,
		Capabilities: types.StringArray{string(types.APIKeyCapabilityManageTenantSettings)},
	})
	assert.NoError(t, policy.UpdateWorkspacePolicy(manageKey, &types.DefaultSettings{}, previous))
}

func TestEmbeddingIdentityBlocksDimensionAndSameDimensionReplacement(t *testing.T) {
	before := activeBuiltin("embedding", types.ModelTypeEmbedding)
	before.Name = "embed-v1"
	before.Parameters.EmbeddingParameters.Dimension = 1024
	next := *before
	next.Parameters.EmbeddingParameters.Dimension = 2048
	assert.Error(t, validateEmbeddingIdentity(before, &next))
	next = *before
	next.Name = "embed-v2"
	assert.Error(t, validateEmbeddingIdentity(before, &next), "same dimension does not mean compatible vector space")
	next = *before
	next.Parameters.Spec = &types.ModelSpecOverride{API: "different-embedding-protocol"}
	assert.Error(t, validateEmbeddingIdentity(before, &next), "protocol overrides cannot bypass identity protection")
	next = *before
	next.DisplayName = "new label"
	next.Parameters.APIKey = "rotated"
	assert.NoError(t, validateEmbeddingIdentity(before, &next))
}
