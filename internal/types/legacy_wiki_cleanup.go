package types

import "context"

type (
	legacyWikiCleanupKey struct{}
	legacyWikiCleanup    struct {
		tenant uint64
		kb     string
	}
)

// WithLegacyWikiCleanup is for an already authorized document-deletion worker.
// It scopes derived-page cleanup to one persisted workspace/KB; it is never
// constructed from request headers and grants no source-document body access.
func WithLegacyWikiCleanup(ctx context.Context, tenant uint64, kb string) context.Context {
	return context.WithValue(ctx, legacyWikiCleanupKey{}, legacyWikiCleanup{tenant: tenant, kb: kb})
}

// LegacyWikiCleanupKB returns the knowledge base scoped to legacy Wiki cleanup.
func LegacyWikiCleanupKB(ctx context.Context) string {
	scope, ok := ctx.Value(legacyWikiCleanupKey{}).(legacyWikiCleanup)
	tenant, _ := TenantIDFromContext(ctx)
	if !ok || scope.tenant == 0 || scope.tenant != tenant {
		return ""
	}
	return scope.kb
}
