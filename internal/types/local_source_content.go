package types

import "context"

type localSourceContentReadKey struct{}

// WithLocalSourceContentRead selects local serving policy for browser/API/MCP
// requests. This is not an authorization grant: tenant, KB, key scope, private
// owner checks still apply. Connector work retains its
// source validation and publication fences.
func WithLocalSourceContentRead(ctx context.Context) context.Context {
	return context.WithValue(ctx, localSourceContentReadKey{}, true)
}

func LocalSourceContentRead(ctx context.Context) bool {
	local, _ := ctx.Value(localSourceContentReadKey{}).(bool)
	return local
}
