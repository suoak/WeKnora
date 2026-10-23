package types

import "context"

// WorkspaceWebReadTenant identifies an authenticated workspace browser, never
// an API key, MCP principal, embed visitor or cross-workspace share. A source
// sync's uncertain state does not revoke this member's existing local access.
func WorkspaceWebReadTenant(ctx context.Context) uint64 {
	p, ok := ctx.Value(PrincipalContextKey).(Principal)
	caller := CallerFromContext(ctx)
	if !ok || p.Type != PrincipalWebUser || p.ID == "" || p.ID != caller.UserID ||
		caller.TenantID == 0 || !caller.Role.IsValid() {
		return 0
	}
	if _, keyed := TenantAPIKeyScopeFromContext(ctx); keyed {
		return 0
	}
	return caller.TenantID
}
