package types

import "context"

type knowledgeProcessingMetadataKey struct{}

// WithKnowledgeProcessingMetadata scopes a status-only route. It is neither a
// document content grant nor a source attestation, and carries no write authority.
func WithKnowledgeProcessingMetadata(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, knowledgeProcessingMetadataKey{}, id)
}

// IsKnowledgeProcessingMetadata excludes all other document IDs and routes.
func IsKnowledgeProcessingMetadata(ctx context.Context, id string) bool {
	target, _ := ctx.Value(knowledgeProcessingMetadataKey{}).(string)
	return id != "" && target == id
}
