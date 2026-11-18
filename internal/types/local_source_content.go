package types

import "context"

const localSourceContentReadContextKey ContextKey = "local_source_content_read"

// WithLocalSourceContentRead selects local serving policy for browser/API/MCP
// requests. This is not an authorization grant: tenant, KB, key scope, private
// owner checks still apply. Connector work retains its
// source validation and publication fences.
func WithLocalSourceContentRead(ctx context.Context) context.Context {
	return context.WithValue(ctx, localSourceContentReadContextKey, true)
}

// LocalSourceContentRead reports whether this request serves platform content.
func LocalSourceContentRead(ctx context.Context) bool {
	local, _ := ctx.Value(localSourceContentReadContextKey).(bool)
	return local
}
