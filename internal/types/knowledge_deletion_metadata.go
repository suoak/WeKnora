package types

import "context"

type knowledgeDeletionMetadataKey struct{}

// WithKnowledgeDeletionMetadata identifies the exact DELETE route target. It
// grants no content read or write permission; ownership and KB guards still run.
func WithKnowledgeDeletionMetadata(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, knowledgeDeletionMetadataKey{}, id)
}

// IsKnowledgeDeletionMetadata reports whether id is the DELETE route's target.
func IsKnowledgeDeletionMetadata(ctx context.Context, id string) bool {
	target, _ := ctx.Value(knowledgeDeletionMetadataKey{}).(string)
	return id != "" && target == id
}
