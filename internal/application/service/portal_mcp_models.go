package service

import (
	"context"
	"math"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/service/retriever"
	"github.com/Tencent/WeKnora/internal/perftrace"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// BindPortalMCPAdapters uses the application's actual model and store registries.
// It does not enable a server or approve any KB. There is no fallback to an
// arbitrary model/store when the target's owner, binding or dimension changes.
func BindPortalMCPAdapters(s *PortalMCPKnowledge, kbs interfaces.KnowledgeBaseRepository, tenants interfaces.TenantRepository, models interfaces.ModelService, registry interfaces.RetrieveEngineRegistry, ownership retriever.TenantStoreOwnership) {
	load := func(ctx context.Context, d access.PortalKBDescriptor) (*types.KnowledgeBase, error) {
		if kbs == nil {
			return nil, access.ErrForbidden
		}
		kb, err := kbs.GetKnowledgeBaseByID(ctx, d.ID)
		if err != nil || kb == nil || kb.TenantID != d.TenantID || kb.EmbeddingModelID != d.EmbeddingModelID || kb.DeletedAt.Valid || kb.IsTemporary {
			return nil, access.ErrForbidden
		}
		return kb, nil
	}
	s.Engine = func(ctx context.Context, d access.PortalKBDescriptor) (access.DocumentRetriever, error) {
		kb, err := load(ctx, d)
		if err != nil || registry == nil || tenants == nil {
			return nil, access.ErrForbidden
		}
		var engine interfaces.RetrieveEngineService
		if kb.VectorStoreID != nil && *kb.VectorStoreID != "" {
			if ownership == nil {
				return nil, access.ErrForbidden
			}
			ok, e := ownership.StoreOwnedBy(ctx, *kb.VectorStoreID, d.TenantID)
			if e != nil || !ok {
				return nil, access.ErrForbidden
			}
			engine, err = registry.GetOrLoadByStoreID(ctx, d.TenantID, *kb.VectorStoreID)
		} else {
			tenant, e := tenants.GetTenantByID(ctx, d.TenantID)
			if e != nil || tenant == nil || tenant.Status != "active" || tenant.DeletedAt.Valid {
				return nil, access.ErrForbidden
			}
			engines := tenant.GetEffectiveEngines()
			if len(engines) == 0 {
				return nil, access.ErrForbidden
			}
			for _, e := range engines {
				if e.RetrieverEngineType != types.PostgresRetrieverEngineType {
					return nil, access.ErrForbidden
				}
			}
			engine, err = registry.GetRetrieveEngineService(types.PostgresRetrieverEngineType)
		}
		if err != nil || engine == nil || engine.EngineType() != types.PostgresRetrieverEngineType {
			return nil, access.ErrForbidden
		}
		return engine, nil
	}
	s.Embed = func(ctx context.Context, d access.PortalKBDescriptor, query string) ([]float32, error) {
		if _, err := load(ctx, d); err != nil || models == nil {
			return nil, access.ErrForbidden
		}
		owner := types.WithExecutionTenant(ctx, d.TenantID)
		model, err := models.GetModelByID(owner, d.EmbeddingModelID)
		if err != nil || model == nil || model.TenantID != d.TenantID || model.Type != types.ModelTypeEmbedding || model.Status != types.ModelStatusActive {
			return nil, access.ErrForbidden
		}
		dimension := model.Parameters.EmbeddingParameters.Dimension
		if dimension <= 0 || dimension > 4096 {
			return nil, access.ErrForbidden
		}
		embedder, err := models.GetEmbeddingModelForTenant(owner, d.EmbeddingModelID, d.TenantID)
		if err != nil {
			return nil, err
		}
		done := perftrace.Measure(ctx, perftrace.Embedding)
		vector, err := embedder.Embed(types.WithEmbedQuery(owner), query)
		done()
		if err != nil {
			return nil, err
		}
		if len(vector) != dimension {
			return nil, access.ErrForbidden
		}
		for _, x := range vector {
			if math.IsNaN(float64(x)) || math.IsInf(float64(x), 0) {
				return nil, access.ErrForbidden
			}
		}
		return vector, nil
	}
}
