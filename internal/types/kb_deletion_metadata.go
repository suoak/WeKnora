package types

import "context"

type kbDeletionMetadataKey struct{}

// WithKBDeletionMetadata is set only by the DELETE route. It authorizes no
// content: repositories must return only deletion/ownership metadata, and the
// usual tenant, private-owner and canonical write guards still run.
func WithKBDeletionMetadata(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, kbDeletionMetadataKey{}, id)
}

func IsKBDeletionMetadata(ctx context.Context, id string) bool {
	v, _ := ctx.Value(kbDeletionMetadataKey{}).(string)
	return id != "" && v == id
}
