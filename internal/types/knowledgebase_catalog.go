package types

import "context"

type knowledgeBaseCatalogKey struct{}

// WithKnowledgeBaseCatalog exposes only KB metadata in the authenticated
// tenant's catalog. Source restrictions on documents, chunks and Wiki remain.
// This marker must never be constructed from arbitrary client scope headers.
func WithKnowledgeBaseCatalog(ctx context.Context, tenantID uint64) context.Context {
	return context.WithValue(ctx, knowledgeBaseCatalogKey{}, tenantID)
}

func KnowledgeBaseCatalogTenant(ctx context.Context) uint64 {
	tenantID, _ := ctx.Value(knowledgeBaseCatalogKey{}).(uint64)
	return tenantID
}
