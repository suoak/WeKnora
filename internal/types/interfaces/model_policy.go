package interfaces

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

// ModelPolicyService owns deployment-wide model inheritance and validation.
// Business services use this abstraction rather than reading system setting
// keys directly.
type ModelPolicyService interface {
	GetPolicy(ctx context.Context) (*types.DefaultModelPolicy, error)
	UpdatePolicy(ctx context.Context, policy *types.DefaultModelPolicy) (*types.DefaultModelPolicy, error)
	ResolveChatModelID(ctx context.Context) string
	ResolveSummaryModelID(ctx context.Context) string
	ResolveEmbeddingModelID(ctx context.Context) string
	ResolveRerankModelID(ctx context.Context) string
	ResolveVLMModelID(ctx context.Context) string
	ResolveASRModelID(ctx context.Context) string
	ValidateModelForRole(ctx context.Context, role types.ModelPolicyRole, modelID string) error
	ApplyKnowledgeBaseDefaults(ctx context.Context, kb *types.KnowledgeBase) error
}
