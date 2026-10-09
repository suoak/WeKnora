package mcp

import (
	"context"
	"errors"

	"github.com/mark3labs/mcp-go/client/transport"
)

// validateOAuthDiscovery primes and checks the SAME handler's metadataOnce
// cache used by registration, authorization, exchange and refresh. It does not
// issue an independent preflight request, change URLs/clients/redirects, or
// replace the SDK's URL validation and SSRF-safe HTTP client.
// mcp-go v0.52.0 can return (nil, nil) on explicit non-200 discovery, and accepts
// null/empty objects without required endpoints; its consumers dereference nil.
func validateOAuthDiscovery(ctx context.Context, h *transport.OAuthHandler, registration bool) error {
	if err := ctx.Err(); err != nil {
		return safeOAuthError("mcp_oauth_discovery_failed", err)
	}
	metadata, err := h.GetServerMetadata(ctx)
	if err != nil {
		return safeOAuthError("mcp_oauth_discovery_failed", err)
	}
	if metadata == nil || metadata.AuthorizationEndpoint == "" || metadata.TokenEndpoint == "" ||
		(registration && metadata.RegistrationEndpoint == "") {
		return safeOAuthError("mcp_oauth_discovery_failed", errors.New("invalid OAuth metadata"))
	}
	return nil
}
