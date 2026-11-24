package mcpserver

import (
	"context"

	"github.com/Tencent/WeKnora/internal/application/access"
)

type (
	personalCatalogAdmissionKey struct{}
	personalCatalogResultKey    struct{}
)

// Reuse only this HTTP request's catalog membership, never an authorization
// decision. Revalidate checks the live identity, Key, source and permissions;
// changed facts fall back to the normal resolver. The output fence still runs.
func revalidatedCatalogTargets(ctx context.Context) ([]*access.PortalKBTarget, bool) {
	targets, _ := ctx.Value(personalCatalogAdmissionKey{}).([]*access.PortalKBTarget)
	if len(targets) == 0 {
		return nil, false
	}
	for _, target := range targets {
		if err := target.Revalidate(ctx); err != nil {
			return nil, false
		}
	}
	return targets, true
}

// These metadata projections use the exact scope admitted for this HTTP request.
func personalProjectsTargets(name string) bool {
	switch name {
	case "list_knowledge_bases", "get_knowledge_base", "list_documents":
		return true
	}
	return false
}
