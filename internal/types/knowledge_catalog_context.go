package types

import "context"

type knowledgeCatalogContextKey struct{}

// WithKnowledgeCatalogRead selects a metadata-only response for a catalog GET.
// It grants no authorization; live workspace and document checks still apply.
// Mutation, download and processing callers must retain the strict entity path.
func WithKnowledgeCatalogRead(ctx context.Context) context.Context {
	return context.WithValue(ctx, knowledgeCatalogContextKey{}, true)
}

// IsKnowledgeCatalogRead reports a server-selected metadata-only catalog read.
func IsKnowledgeCatalogRead(ctx context.Context) bool {
	value, _ := ctx.Value(knowledgeCatalogContextKey{}).(bool)
	return value
}
